package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"ai-tools/internal/apollo"
)

func TestSnapshotViewMasksSensitiveValue(t *testing.T) {
	h := newTestHandler(t, "APP_NAME = merdi\nSECRET_TOKEN = do-not-expose\n")
	r := httptest.NewRequest(http.MethodGet, "/api/snapshots/prod", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if strings.Contains(w.Body.String(), "do-not-expose") {
		t.Fatalf("safe response contains plaintext: %s", w.Body.String())
	}
}

func TestCompareMasksSensitiveValues(t *testing.T) {
	h := newCompareHandler(t)
	r := httptest.NewRequest(http.MethodGet, "/api/compare?from=prod&to=test", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "prod-secret") || strings.Contains(w.Body.String(), "test-secret") {
		t.Fatalf("compare leaked secret: %s", w.Body.String())
	}
}

func TestHandlerRejectsUnsafeSnapshotName(t *testing.T) {
	h := newTestHandler(t, "APP_NAME = merdi\n")
	r := httptest.NewRequest(http.MethodGet, "/api/snapshots/bad%20name", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestImportPreviewReturnsWarningsWithoutWriting(t *testing.T) {
	h := newEmptyHandler(t)
	body := strings.NewReader(`{"text":"A = 1B = 2\n"}`)
	r := httptest.NewRequest(http.MethodPost, "/api/import/preview", body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "warnings") {
		t.Fatalf("preview = %d %s", w.Code, w.Body.String())
	}
}

func TestCreateSnapshotRejectsDuplicateName(t *testing.T) {
	h := newTestHandler(t, "APP_NAME = merdi\n")
	body := strings.NewReader(`{"name":"prod","app_id":"app-x","text":"APP_NAME = other"}`)
	r := httptest.NewRequest(http.MethodPost, "/api/snapshots", body)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("first create = %d: %s", w.Code, w.Body.String())
	}
	body = strings.NewReader(`{"name":"prod","app_id":"app-x","text":"APP_NAME = other"}`)
	r = httptest.NewRequest(http.MethodPost, "/api/snapshots", body)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate create = %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateSnapshotRequiresAppID(t *testing.T) {
	h := newEmptyHandler(t)
	r := httptest.NewRequest(http.MethodPost, "/api/snapshots", strings.NewReader(`{"name":"prod","text":"APP_NAME = other"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestDeleteSnapshot(t *testing.T) {
	h := newTestHandler(t, "APP_NAME = merdi\n")
	// create a prod/app-x snapshot then delete it
	create := httptest.NewRecorder()
	h.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/api/snapshots",
		strings.NewReader(`{"name":"prod","app_id":"app-x","text":"APP_NAME = other"}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", create.Code, create.Body.String())
	}
	del := httptest.NewRecorder()
	h.ServeHTTP(del, httptest.NewRequest(http.MethodDelete, "/api/snapshots/prod?appid=app-x", nil))
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete = %d: %s", del.Code, del.Body.String())
	}
	del2 := httptest.NewRecorder()
	h.ServeHTTP(del2, httptest.NewRequest(http.MethodDelete, "/api/snapshots/prod?appid=app-x", nil))
	if del2.Code != http.StatusNotFound {
		t.Fatalf("second delete = %d, want %d", del2.Code, http.StatusNotFound)
	}
	view := httptest.NewRecorder()
	h.ServeHTTP(view, httptest.NewRequest(http.MethodGet, "/api/snapshots/prod?appid=app-x", nil))
	if view.Code != http.StatusNotFound {
		t.Fatalf("view after delete = %d, want %d", view.Code, http.StatusNotFound)
	}
}

func TestSnapshotViewWithAppID(t *testing.T) {
	h := newEmptyHandler(t)
	create := httptest.NewRecorder()
	h.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/api/snapshots",
		strings.NewReader(`{"name":"prod","app_id":"app-x","text":"APP_NAME = merdi\nSECRET_TOKEN = hide\n"}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", create.Code, create.Body.String())
	}
	view := httptest.NewRecorder()
	h.ServeHTTP(view, httptest.NewRequest(http.MethodGet, "/api/snapshots/prod?appid=app-x", nil))
	if view.Code != http.StatusOK {
		t.Fatalf("view = %d: %s", view.Code, view.Body.String())
	}
	if strings.Contains(view.Body.String(), "hide") || !strings.Contains(view.Body.String(), "merdi") {
		t.Fatalf("view body: %s", view.Body.String())
	}
}

func TestUpdateSensitiveItemDoesNotReturnPlaintext(t *testing.T) {
	h := newTestHandler(t, "SECRET_TOKEN = old-secret\n")
	body := strings.NewReader(`{"value":"new-secret","secret":true}`)
	r := httptest.NewRequest(http.MethodPut, "/api/snapshots/prod/items/SECRET_TOKEN", body)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "new-secret") {
		t.Fatalf("update = %d %s", w.Code, w.Body.String())
	}
}

func TestExportRequiresConfirmation(t *testing.T) {
	h := newTestHandler(t, "APP_NAME = merdi\n")
	r := httptest.NewRequest(http.MethodPost, "/api/snapshots/prod/export", strings.NewReader(`{"confirm":false}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestRootServesEmbeddedWorkspace(t *testing.T) {
	h := NewHandler(Config{Dir: t.TempDir(), SnapshotKey: func() ([]byte, error) { return make([]byte, 32), nil }})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `id="app"`) {
		t.Fatalf("workspace shell missing: %s", w.Body.String())
	}
}

func TestRootServesFullWorkspace(t *testing.T) {
	h := NewHandler(Config{Dir: t.TempDir(), SnapshotKey: func() ([]byte, error) { return make([]byte, 32), nil }})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	for _, id := range []string{`id="view-aes"`, `id="view-settings"`, `id="reveal-dialog"`, `id="edit-dialog"`} {
		if !strings.Contains(w.Body.String(), id) {
			t.Fatalf("workspace shell missing %s", id)
		}
	}
}

func TestDeleteItem(t *testing.T) {
	h := newTestHandler(t, "APP_NAME = merdi\n")
	r := httptest.NewRequest(http.MethodDelete, "/api/snapshots/prod/items/APP_NAME", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d: %s", w.Code, w.Body.String())
	}
	r = httptest.NewRequest(http.MethodDelete, "/api/snapshots/prod/items/APP_NAME", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("second delete status = %d", w.Code)
	}
}

func newEmptyHandler(t *testing.T) http.Handler {
	t.Helper()
	return NewHandler(Config{Dir: t.TempDir(), SnapshotKey: func() ([]byte, error) { return fixedKey(), nil }})
}

func newTestHandler(t *testing.T, kvText string) http.Handler {
	t.Helper()
	key := fixedKey()
	kvs, _ := apollo.ParseKV(kvText)
	s := apollo.NewSnapshot("prod", "")
	for _, kv := range kvs {
		if err := s.Set(key, kv.Key, kv.Value, nil); err != nil {
			t.Fatalf("set %s: %v", kv.Key, err)
		}
	}
	dir := t.TempDir()
	if err := s.Save(filepath.Join(dir, "prod.json")); err != nil {
		t.Fatal(err)
	}
	return NewHandler(Config{Dir: dir, SnapshotKey: func() ([]byte, error) { return key, nil }})
}

func newCompareHandler(t *testing.T) http.Handler {
	t.Helper()
	key := fixedKey()
	dir := t.TempDir()
	for name, secret := range map[string]string{"prod": "prod-secret", "test": "test-secret"} {
		s := apollo.NewSnapshot(name, "")
		if err := s.Set(key, "SECRET_TOKEN", secret, nil); err != nil {
			t.Fatalf("set %s: %v", name, err)
		}
		if err := s.Save(filepath.Join(dir, name+".json")); err != nil {
			t.Fatal(err)
		}
	}
	return NewHandler(Config{Dir: dir, SnapshotKey: func() ([]byte, error) { return key, nil }})
}

func fixedKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return key
}

func TestRejectsCrossOriginRequest(t *testing.T) {
	h := newTestHandler(t, "SECRET_TOKEN = x\n")

	// mismatched Origin is rejected before reaching the handler
	r := httptest.NewRequest(http.MethodPost, "/api/snapshots/prod/reveal",
		strings.NewReader(`{"targets":["SECRET_TOKEN"],"confirm":true}`))
	r.Header.Set("Origin", "http://evil.example.com")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d, want %d", w.Code, http.StatusForbidden)
	}

	// a null Origin (sandboxed iframe / file://) is also rejected
	r = httptest.NewRequest(http.MethodGet, "/api/snapshots/prod", nil)
	r.Header.Set("Origin", "null")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("null origin status = %d, want %d", w.Code, http.StatusForbidden)
	}

	// a matching Origin passes the middleware (the handler then runs)
	r = httptest.NewRequest(http.MethodGet, "/api/snapshots/prod", nil)
	r.Header.Set("Origin", "http://"+r.Host)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("same-origin status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestTokenGatesStateChangingRequests(t *testing.T) {
	dir := t.TempDir()
	h := NewHandler(Config{Dir: dir, Token: "s3cr3t-token", SnapshotKey: func() ([]byte, error) { return fixedKey(), nil }})

	// state-changing POST without token is rejected
	r := httptest.NewRequest(http.MethodPost, "/api/aes/transform", strings.NewReader(`{"op":"encrypt","key":"0123456789abcdef","iv":"abcdefghijklmnop","text":"hello"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("POST without token = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	// wrong token is rejected
	r = httptest.NewRequest(http.MethodPost, "/api/aes/transform", strings.NewReader(`{"op":"encrypt","key":"0123456789abcdef","iv":"abcdefghijklmnop","text":"hello"}`))
	r.Header.Set("X-Auth-Token", "wrong")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("POST with wrong token = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	// correct token in header works
	r = httptest.NewRequest(http.MethodPost, "/api/aes/transform", strings.NewReader(`{"op":"encrypt","key":"0123456789abcdef","iv":"abcdefghijklmnop","text":"hello"}`))
	r.Header.Set("X-Auth-Token", "s3cr3t-token")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("POST with token = %d: %s", w.Code, w.Body.String())
	}

	// correct token in query param works
	r = httptest.NewRequest(http.MethodDelete, "/api/aes/config?t=s3cr3t-token", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE with query token = %d", w.Code)
	}

	// read-only GET stays open without a token
	r = httptest.NewRequest(http.MethodGet, "/api/key", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GET without token = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestStartPrintsTokenizedURL(t *testing.T) {
	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	port := blocker.Addr().(*net.TCPAddr).Port

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var buf syncBuf
	errCh := make(chan error, 1)
	go func() {
		errCh <- Start(ctx, Config{Dir: t.TempDir(), SnapshotKey: func() ([]byte, error) { return make([]byte, 32), nil }}, port, false, &buf)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if strings.Contains(buf.String(), "ai-tools UI available at") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server did not start: " + buf.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	url := buf.String()
	if !strings.Contains(url, "?t=") {
		t.Fatalf("started URL missing token: %s", url)
	}
	// the token from the printed URL actually gates state-changing requests
	tok := strings.TrimSuffix(strings.SplitN(url, "?t=", 2)[1], "\n")
	addr := strings.TrimPrefix(strings.SplitN(url, "://", 2)[1], "ai-tools UI available at ")
	base := "http://" + strings.TrimSpace(strings.Split(addr, "?t=")[0])

	// POST without the token is rejected
	req, _ := http.NewRequest(http.MethodPost, base+"/api/aes/transform", strings.NewReader(`{"op":"encrypt","key":"0123456789abcdef","iv":"abcdefghijklmnop","text":"hello"}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST without token = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	// POST with the token from the URL passes
	req, _ = http.NewRequest(http.MethodPost, base+"/api/aes/transform", strings.NewReader(`{"op":"encrypt","key":"0123456789abcdef","iv":"abcdefghijklmnop","text":"hello"}`))
	req.Header.Set("X-Auth-Token", tok)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST with token = %d", resp.StatusCode)
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown timed out")
	}
}

func TestRevealRequiresConfirmation(t *testing.T) {
	h := newTestHandler(t, "SECRET_TOKEN = x\n")
	body := strings.NewReader(`{"targets":["SECRET_TOKEN"],"confirm":false}`)
	r := httptest.NewRequest(http.MethodPost, "/api/snapshots/prod/reveal", body)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestEditLoadRequiresConfirmation(t *testing.T) {
	h := newTestHandler(t, "APP_NAME = merdi\n")
	r := httptest.NewRequest(http.MethodPost, "/api/snapshots/prod/edit", strings.NewReader(`{"confirm":false}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestEditApplyReencrypts(t *testing.T) {
	h := newTestHandler(t, "APP_NAME = merdi\n")
	body := strings.NewReader(`{"text":"APP_NAME = merdi\nNEW = 1\n"}`)
	r := httptest.NewRequest(http.MethodPut, "/api/snapshots/prod/edit", body)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "merdi") {
		t.Fatalf("edit apply response contains plaintext: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"total":2`) {
		t.Fatalf("edit apply total missing: %s", w.Body.String())
	}
}

func TestAESGenKey(t *testing.T) {
	h := newTestHandler(t, "")
	r := httptest.NewRequest(http.MethodPost, "/api/aes/gen-key", strings.NewReader(`{"bytes":16,"iv_bytes":12}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"key"`) || !strings.Contains(w.Body.String(), `"iv"`) {
		t.Fatalf("gen-key body: %s", w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
}

func TestAESGenKeyRejectsBadSizes(t *testing.T) {
	h := newTestHandler(t, "")
	r := httptest.NewRequest(http.MethodPost, "/api/aes/gen-key", strings.NewReader(`{"bytes":15,"iv_bytes":12}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAESTransformRoundTrip(t *testing.T) {
	h := newTestHandler(t, "")
	enc := httptest.NewRecorder()
	h.ServeHTTP(enc, httptest.NewRequest(http.MethodPost, "/api/aes/transform",
		strings.NewReader(`{"op":"encrypt","key":"0123456789abcdef","iv":"abcdefghijklmnop","text":"hello"}`)))
	if enc.Code != http.StatusOK {
		t.Fatalf("encrypt status = %d: %s", enc.Code, enc.Body.String())
	}
	var er struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(enc.Body.Bytes(), &er); err != nil || er.Result == "" {
		t.Fatalf("encrypt body = %s (%v)", enc.Body.String(), err)
	}
	dec := httptest.NewRecorder()
	h.ServeHTTP(dec, httptest.NewRequest(http.MethodPost, "/api/aes/transform",
		strings.NewReader(`{"op":"decrypt","key":"0123456789abcdef","iv":"abcdefghijklmnop","text":"`+er.Result+`"}`)))
	if dec.Code != http.StatusOK || !strings.Contains(dec.Body.String(), `"hello"`) {
		t.Fatalf("decrypt = %d %s", dec.Code, dec.Body.String())
	}
}

func TestAESConfigAPI(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AI_TOOLS_AES_CONFIG", filepath.Join(dir, "aes.json"))
	h := NewHandler(Config{Dir: dir, SnapshotKey: func() ([]byte, error) { return make([]byte, 32), nil }})

	get := httptest.NewRecorder()
	h.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/aes/config", nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"key":""`) {
		t.Fatalf("get empty config = %d %s", get.Code, get.Body.String())
	}

	put := httptest.NewRecorder()
	h.ServeHTTP(put, httptest.NewRequest(http.MethodPut, "/api/aes/config",
		strings.NewReader(`{"key":"kk","iv":"ivv"}`)))
	if put.Code != http.StatusOK {
		t.Fatalf("put config = %d %s", put.Code, put.Body.String())
	}

	get2 := httptest.NewRecorder()
	h.ServeHTTP(get2, httptest.NewRequest(http.MethodGet, "/api/aes/config", nil))
	if get2.Code != http.StatusOK || !strings.Contains(get2.Body.String(), `"key":"kk"`) {
		t.Fatalf("get after put = %d %s", get2.Code, get2.Body.String())
	}

	del := httptest.NewRecorder()
	h.ServeHTTP(del, httptest.NewRequest(http.MethodDelete, "/api/aes/config", nil))
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete config = %d", del.Code)
	}
}

func TestInitRejectsWhenKeyAvailable(t *testing.T) {
	h := NewHandler(Config{Dir: t.TempDir(), SnapshotKey: func() ([]byte, error) { return make([]byte, 32), nil }})
	r := httptest.NewRequest(http.MethodPost, "/api/init", strings.NewReader(`{"force":false}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestInitRejectsWrongMethod(t *testing.T) {
	h := NewHandler(Config{Dir: t.TempDir(), SnapshotKey: func() ([]byte, error) { return make([]byte, 32), nil }})
	r := httptest.NewRequest(http.MethodGet, "/api/init", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestKeyStatus(t *testing.T) {
	h := NewHandler(Config{Dir: t.TempDir(), SnapshotKey: func() ([]byte, error) { return make([]byte, 32), nil }})
	r := httptest.NewRequest(http.MethodGet, "/api/key", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"available":true`) {
		t.Fatalf("key status with key = %d %s", w.Code, w.Body.String())
	}
	h2 := NewHandler(Config{Dir: t.TempDir(), SnapshotKey: func() ([]byte, error) { return nil, errors.New("no key") }})
	w2 := httptest.NewRecorder()
	h2.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/api/key", nil))
	if w2.Code != http.StatusOK || !strings.Contains(w2.Body.String(), `"available":false`) {
		t.Fatalf("key status without key = %d %s", w2.Code, w2.Body.String())
	}
}

func TestExportFilenameIncludesAppID(t *testing.T) {
	h := newEmptyHandler(t)
	create := httptest.NewRecorder()
	h.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/api/snapshots",
		strings.NewReader(`{"name":"prod","app_id":"app-x","text":"A = 1"}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", create.Code, create.Body.String())
	}
	r := httptest.NewRequest(http.MethodPost, "/api/snapshots/prod/export?appid=app-x",
		strings.NewReader(`{"confirm":true}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("export = %d: %s", w.Code, w.Body.String())
	}
	cd := w.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "prod__app-x.json") {
		t.Fatalf("Content-Disposition = %q", cd)
	}
}

func TestStartRollsForwardOnBusyPort(t *testing.T) {
	// Occupy a free port as the busy blocker, then start on it and expect the
	// server to roll forward to some other port.
	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	busyPort := blocker.Addr().(*net.TCPAddr).Port

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var buf syncBuf
	errCh := make(chan error, 1)
	go func() {
		errCh <- Start(ctx, Config{Dir: t.TempDir(), SnapshotKey: func() ([]byte, error) { return make([]byte, 32), nil }}, busyPort, false, &buf)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if strings.Contains(buf.String(), "ai-tools UI available at") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server did not start: " + buf.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	url := buf.String()
	if strings.Contains(url, strconv.Itoa(busyPort)) {
		t.Fatalf("expected roll-forward off busy port %d, got: %s", busyPort, url)
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown timed out")
	}
}

// syncBuf is a mutex-protected writer so Start's goroutine and the test can
// share it without a data race.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestCompareMulti(t *testing.T) {
	h := newEmptyHandler(t)
	for _, ref := range []struct{ name, appID, text string }{
		{"prod", "app-x", "DB_HOST = db.prod\nSHARED = yes\n"},
		{"test", "app-y", "DB_HOST = db.test\nSHARED = yes\nEXTRA = 1\n"},
	} {
		body := strings.NewReader(`{"name":"` + ref.name + `","app_id":"` + ref.appID + `","text":"` + strings.ReplaceAll(ref.text, "\n", "\\n") + `"}`)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/snapshots", body))
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s = %d: %s", ref.name, w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/api/compare/multi",
		strings.NewReader(`{"refs":[{"name":"prod","app_id":"app-x"},{"name":"test","app_id":"app-y"}]}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("multi = %d: %s", w.Code, w.Body.String())
	}
	var out struct {
		Refs []struct {
			Name string `json:"name"`
		} `json:"refs"`
		Rows []struct {
			Key    string      `json:"key"`
			Values []SafeValue `json:"values"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Refs) != 2 || len(out.Rows) != 3 {
		t.Fatalf("refs=%d rows=%d: %s", len(out.Refs), len(out.Rows), w.Body.String())
	}
	if out.Rows[0].Key != "DB_HOST" || len(out.Rows[0].Values) != 2 {
		t.Fatalf("first row = %#v", out.Rows[0])
	}
	if out.Rows[0].Values[0].Value == nil || *out.Rows[0].Values[0].Value != "db.prod" {
		t.Fatalf("prod value = %#v", out.Rows[0].Values[0])
	}
	if out.Rows[0].Values[1].Value == nil || *out.Rows[0].Values[1].Value != "db.test" {
		t.Fatalf("test value = %#v", out.Rows[0].Values[1])
	}
}

func TestCompareMultiRejectsSingleRef(t *testing.T) {
	h := newEmptyHandler(t)
	r := httptest.NewRequest(http.MethodPost, "/api/compare/multi",
		strings.NewReader(`{"refs":[{"name":"prod","app_id":"app-x"}]}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestCompareKey(t *testing.T) {
	h := newEmptyHandler(t)
	for _, ref := range []struct{ name, appID, text string }{
		{"prod", "app-x", "DB_HOST = db.prod\nSECRET_TOKEN = s1\n"},
		{"test", "app-y", "DB_HOST = db.test\nSECRET_TOKEN = s2\n"},
	} {
		body := strings.NewReader(`{"name":"` + ref.name + `","app_id":"` + ref.appID + `","text":"` + strings.ReplaceAll(ref.text, "\n", "\\n") + `"}`)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/snapshots", body))
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s = %d: %s", ref.name, w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/api/compare/key?key=DB_HOST", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("key compare = %d: %s", w.Code, w.Body.String())
	}
	var out struct {
		Key  string `json:"key"`
		Rows []struct {
			Name  string    `json:"name"`
			Value SafeValue `json:"value"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Key != "DB_HOST" || len(out.Rows) != 2 {
		t.Fatalf("out = %#v", out)
	}
	// sensitive key must not leak plaintext
	r2 := httptest.NewRequest(http.MethodGet, "/api/compare/key?key=SECRET_TOKEN", nil)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if strings.Contains(w2.Body.String(), "s1") || strings.Contains(w2.Body.String(), "s2") {
		t.Fatalf("leaked plaintext: %s", w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), "fingerprint") {
		t.Fatalf("missing fingerprint: %s", w2.Body.String())
	}
}

func TestCompareKeyFiltersByAppID(t *testing.T) {
	h := newEmptyHandler(t)
	for _, ref := range []struct{ name, appID, text string }{
		{"prod", "app-x", "DB_HOST = db.prod\n"},
		{"test", "app-x", "DB_HOST = db.test\n"},
		{"other", "app-y", "DB_HOST = db.other\n"},
	} {
		body := strings.NewReader(`{"name":"` + ref.name + `","app_id":"` + ref.appID + `","text":"` + strings.ReplaceAll(ref.text, "\n", "\\n") + `"}`)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/snapshots", body))
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s = %d: %s", ref.name, w.Code, w.Body.String())
		}
	}
	// filter to app-x: only prod and test
	r := httptest.NewRequest(http.MethodGet, "/api/compare/key?key=DB_HOST&appid=app-x", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var out struct {
		Rows []struct {
			Name string `json:"name"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Rows) != 2 {
		t.Fatalf("filtered rows = %d: %s", len(out.Rows), w.Body.String())
	}
	for _, row := range out.Rows {
		if row.Name == "other" {
			t.Fatalf("app-y snapshot leaked into app-x filter: %#v", out.Rows)
		}
	}
	// filter to app-y: only other
	r2 := httptest.NewRequest(http.MethodGet, "/api/compare/key?key=DB_HOST&appid=app-y", nil)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if err := json.Unmarshal(w2.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Rows) != 1 || out.Rows[0].Name != "other" {
		t.Fatalf("app-y rows = %#v", out.Rows)
	}
	// no filter: all three
	r3 := httptest.NewRequest(http.MethodGet, "/api/compare/key?key=DB_HOST", nil)
	w3 := httptest.NewRecorder()
	h.ServeHTTP(w3, r3)
	if err := json.Unmarshal(w3.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Rows) != 3 {
		t.Fatalf("unfiltered rows = %d", len(out.Rows))
	}
}
