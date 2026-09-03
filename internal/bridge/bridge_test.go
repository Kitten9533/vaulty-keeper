package bridge

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-tools/internal/apollo"
	"ai-tools/internal/dbproxy"
)

func fixedKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return key
}

func fixedSensitiveKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(200 - i)
	}
	return key
}

// testHandler builds a bridge handler with one snapshot ("prod") containing:
//   - APP_NAME    (plain value, not marked safe)
//   - PUBLIC      (plain value, marked safe with set --plain)
//   - SECRET_TOKEN (sensitive)
//
// The bridge must mask ALL of them, including PUBLIC.
func testHandler(t *testing.T) http.Handler {
	t.Helper()
	key := fixedKey()
	sensitiveKey := fixedSensitiveKey()
	s := apollo.NewSnapshot("prod", "app-x")
	for _, kv := range []struct {
		k, v string
		sec  bool
	}{{"APP_NAME", "merdi", false}, {"PUBLIC", "hello", false}, {"SECRET_TOKEN", "do-not-expose", true}} {
		if err := s.Set(key, sensitiveKey, kv.k, kv.v, &kv.sec); err != nil {
			t.Fatalf("set %s: %v", kv.k, err)
		}
	}
	dir := t.TempDir()
	if err := s.Save(filepath.Join(dir, "prod__app-x.json")); err != nil {
		t.Fatal(err)
	}
	return NewHandler(Config{
		Dir:          dir,
		SnapshotKey:  func() ([]byte, error) { return key, nil },
		SensitiveKey: func() ([]byte, error) { return sensitiveKey, nil },
		Token:        "test-token",
	})
}

func TestHealthOpenWithoutToken(t *testing.T) {
	h := testHandler(t)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("health = %d: %s", w.Code, w.Body.String())
	}
}

func TestAPINeedsToken(t *testing.T) {
	h := testHandler(t)
	for _, tc := range []struct {
		name  string
		path  string
		token string
	}{
		{"no token", "/api/snapshots", ""},
		{"wrong token", "/api/snapshots", "wrong"},
		{"no token snapshot", "/api/snapshot?name=prod&appid=app-x", ""},
		{"no token get", "/api/get?name=prod&appid=app-x&key=APP_NAME", ""},
		{"no token compare", "/api/compare?from=prod&to=prod", ""},
	} {
		r := httptest.NewRequest(http.MethodGet, tc.path, nil)
		if tc.token != "" {
			r.Header.Set("X-Auth-Token", tc.token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d, want 401: %s", tc.name, w.Code, w.Body.String())
		}
	}
}

func TestBadTokensThrottled(t *testing.T) {
	h := testHandler(t)
	for i := 0; i < 3; i++ {
		r := httptest.NewRequest(http.MethodGet, "/api/snapshots", nil)
		r.Header.Set("X-Auth-Token", "wrong")
		w := httptest.NewRecorder()
		start := time.Now()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401", i, w.Code)
		}
		if i > 0 && time.Since(start) < 40*time.Millisecond {
			t.Fatalf("attempt %d was not throttled", i)
		}
	}
}

func TestSnapshotViewMasksEverything(t *testing.T) {
	h := testHandler(t)
	r := httptest.NewRequest(http.MethodGet, "/api/snapshot?name=prod&appid=app-x", nil)
	r.Header.Set("X-Auth-Token", "test-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, plain := range []string{"merdi", "hello", "do-not-expose"} {
		if strings.Contains(body, plain) {
			t.Fatalf("response leaks plaintext %q: %s", plain, body)
		}
	}
	var res struct {
		Items []struct {
			Key    string `json:"key"`
			Masked Masked `json:"value"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 3 {
		t.Fatalf("items = %d, want 3", len(res.Items))
	}
	byKey := map[string]Masked{}
	for _, it := range res.Items {
		byKey[it.Key] = it.Masked
	}
	for _, k := range []string{"APP_NAME", "PUBLIC", "SECRET_TOKEN"} {
		m, ok := byKey[k]
		if !ok {
			t.Fatalf("missing key %q in response", k)
		}
		if !strings.HasPrefix(m.Value, "*** (") {
			t.Fatalf("key %q value not masked: %q", k, m.Value)
		}
		if m.Fingerprint == "" {
			t.Fatalf("key %q missing fingerprint", k)
		}
	}
	// lengths are real, even for the safe-marked PUBLIC item
	if byKey["PUBLIC"].Length != len("hello") {
		t.Fatalf("PUBLIC length = %d, want %d", byKey["PUBLIC"].Length, len("hello"))
	}
	if byKey["SECRET_TOKEN"].Length != len("do-not-expose") {
		t.Fatalf("SECRET_TOKEN length = %d", byKey["SECRET_TOKEN"].Length)
	}
}

func TestGetMasksSafeItem(t *testing.T) {
	h := testHandler(t)
	r := httptest.NewRequest(http.MethodGet, "/api/get?name=prod&appid=app-x&key=PUBLIC", nil)
	r.Header.Set("X-Auth-Token", "test-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "hello") {
		t.Fatalf("get leaked plaintext for safe-marked key: %s", w.Body.String())
	}
	var res struct {
		Value Masked `json:"value"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Value.Value != "*** (5 chars)" {
		t.Fatalf("value = %q", res.Value.Value)
	}
	if res.Value.Fingerprint == "" {
		t.Fatal("missing fingerprint")
	}
}

func TestCompareMasksEverything(t *testing.T) {
	key := fixedKey()
	sensitiveKey := fixedSensitiveKey()
	dir := t.TempDir()
	write := func(name, appID, secretVal string) {
		s := apollo.NewSnapshot(name, appID)
		if err := s.Set(key, sensitiveKey, "APP_NAME", name, nil); err != nil {
			t.Fatal(err)
		}
		if err := s.Set(key, sensitiveKey, "SECRET_TOKEN", secretVal, nil); err != nil {
			t.Fatal(err)
		}
		if err := s.Save(filepath.Join(dir, name+"__"+appID+".json")); err != nil {
			t.Fatal(err)
		}
	}
	write("prod", "app-x", "secret-one")
	write("test", "app-x", "secret-two")

	h := NewHandler(Config{
		Dir:          dir,
		SnapshotKey:  func() ([]byte, error) { return key, nil },
		SensitiveKey: func() ([]byte, error) { return sensitiveKey, nil },
		Token:        "test-token",
	})
	r := httptest.NewRequest(http.MethodGet, "/api/compare?from=prod&from_appid=app-x&to=test&to_appid=app-x", nil)
	r.Header.Set("X-Auth-Token", "test-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, plain := range []string{"secret-one", "secret-two"} {
		if strings.Contains(body, plain) {
			t.Fatalf("compare leaks plaintext %q: %s", plain, body)
		}
	}
	var res struct {
		Changes []struct {
			Key  string `json:"key"`
			Kind string `json:"kind"`
			Old  Masked `json:"old"`
			New  Masked `json:"new"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	var changed *struct {
		Key  string `json:"key"`
		Kind string `json:"kind"`
		Old  Masked `json:"old"`
		New  Masked `json:"new"`
	}
	for i := range res.Changes {
		if res.Changes[i].Key == "SECRET_TOKEN" {
			changed = &res.Changes[i]
		}
	}
	if changed == nil {
		t.Fatalf("SECRET_TOKEN change not found: %s", body)
	}
	if changed.Kind != "changed" {
		t.Fatalf("kind = %q", changed.Kind)
	}
	if changed.Old.Fingerprint == changed.New.Fingerprint {
		t.Fatalf("different secrets produced identical fingerprints: %s", body)
	}
}

func TestIdenticalValuesShareFingerprint(t *testing.T) {
	key := fixedKey()
	m1 := mask(true, "same-value", true, key)
	m2 := mask(true, "same-value", true, key)
	m3 := mask(true, "different", true, key)
	if m1.Fingerprint != m2.Fingerprint {
		t.Fatalf("same value, different fingerprints: %q vs %q", m1.Fingerprint, m2.Fingerprint)
	}
	if m1.Fingerprint == m3.Fingerprint {
		t.Fatalf("different values share fingerprint %q", m1.Fingerprint)
	}
	if m1.Value != "*** (10 chars)" {
		t.Fatalf("mask = %q", m1.Value)
	}
}

func TestMaskWithoutKeyHasNoFingerprint(t *testing.T) {
	m := mask(true, "value", true, nil)
	if m.Fingerprint != "" {
		t.Fatalf("fingerprint without key = %q", m.Fingerprint)
	}
}

func TestDBList(t *testing.T) {
	// inject a DB key + a small db store
	key := fixedKey()
	enc := base64.StdEncoding.EncodeToString(key)
	t.Setenv(apollo.EnvDBKey, enc)
	dir := t.TempDir()
	store := filepath.Join(dir, dbproxy.FileName)
	if err := dbproxy.Add(store, key, "prod", "postgres://app:secret@db.example.com:5432/appdb", 0); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(Config{
		Dir:          dir,
		SnapshotKey:  func() ([]byte, error) { return key, nil },
		SensitiveKey: func() ([]byte, error) { return fixedSensitiveKey(), nil },
		Token:        "test-token",
		DBStore:      store,
	})

	// without token -> 401
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/db/list", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", w.Code)
	}

	// with token -> names/types/ports, never URLs
	w = httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/db/list", nil)
	r.Header.Set("X-Auth-Token", "test-token")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("dblist = %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Connections []struct {
			Name string `json:"name"`
			Type string `json:"type"`
			Port int    `json:"port"`
		} `json:"connections"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Connections) != 1 || res.Connections[0].Name != "prod" || res.Connections[0].Type != "postgres" {
		t.Fatalf("connections = %+v", res.Connections)
	}
	if strings.Contains(w.Body.String(), "db.example.com") || strings.Contains(w.Body.String(), "secret") {
		t.Fatal("dblist leaked the database URL")
	}
}
