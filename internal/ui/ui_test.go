package ui

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"vaulty-keeper/internal/aesx"
	"vaulty-keeper/internal/apollo"
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
	// reverse default: even the non-sensitive APP_NAME is masked in GET
	// unless explicitly marked safe; the key name is still visible
	if strings.Contains(view.Body.String(), "hide") || strings.Contains(view.Body.String(), "merdi") || !strings.Contains(view.Body.String(), "APP_NAME") {
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
	return NewHandler(Config{Dir: t.TempDir(), AllowPlaintext: true, SnapshotKey: func() ([]byte, error) { return fixedKey(), nil }, SensitiveKey: func() ([]byte, error) { return fixedSensitiveKey(), nil }})
}

func newTestHandler(t *testing.T, kvText string) http.Handler {
	t.Helper()
	key := fixedKey()
	sensitiveKey := fixedSensitiveKey()
	kvs, _ := apollo.ParseKV(kvText)
	s := apollo.NewSnapshot("prod", "")
	for _, kv := range kvs {
		if err := s.Set(key, sensitiveKey, kv.Key, kv.Value, nil); err != nil {
			t.Fatalf("set %s: %v", kv.Key, err)
		}
	}
	dir := t.TempDir()
	if err := s.Save(filepath.Join(dir, "prod.json")); err != nil {
		t.Fatal(err)
	}
	return NewHandler(Config{Dir: dir, AllowPlaintext: true, SnapshotKey: func() ([]byte, error) { return key, nil }, SensitiveKey: func() ([]byte, error) { return sensitiveKey, nil }})
}

func newCompareHandler(t *testing.T) http.Handler {
	t.Helper()
	key := fixedKey()
	sensitiveKey := fixedSensitiveKey()
	dir := t.TempDir()
	for name, secret := range map[string]string{"prod": "prod-secret", "test": "test-secret"} {
		s := apollo.NewSnapshot(name, "")
		if err := s.Set(key, sensitiveKey, "SECRET_TOKEN", secret, nil); err != nil {
			t.Fatalf("set %s: %v", name, err)
		}
		if err := s.Save(filepath.Join(dir, name+".json")); err != nil {
			t.Fatal(err)
		}
	}
	return NewHandler(Config{Dir: dir, AllowPlaintext: true, SnapshotKey: func() ([]byte, error) { return key, nil }, SensitiveKey: func() ([]byte, error) { return sensitiveKey, nil }})
}

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
	r = httptest.NewRequest(http.MethodDelete, "/api/aes/config?name=x&t=s3cr3t-token", nil)
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
		errCh <- Start(ctx, Config{Dir: t.TempDir(), AllowPlaintext: true, SnapshotKey: func() ([]byte, error) { return make([]byte, 32), nil }}, port, false, &buf)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if strings.Contains(buf.String(), "vaulty-keeper UI available at") {
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
	tok := strings.TrimSpace(strings.SplitN(strings.SplitN(url, "?t=", 2)[1], "\n", 2)[0])
	addr := strings.TrimPrefix(strings.SplitN(url, "://", 2)[1], "vaulty-keeper UI available at ")
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

func TestRevealWithExplicitKeyIV(t *testing.T) {
	dir := t.TempDir()
	key := fixedKey()
	sensitiveKey := fixedSensitiveKey()
	aesKey, aesIV := "0123456789abcdef", "abcdefghijklmnop"
	enc, err := aesx.Encrypt(aesKey, aesIV, "REAL-SECRET")
	if err != nil {
		t.Fatal(err)
	}
	s := apollo.NewSnapshot("prod", "")
	if err := s.Set(key, sensitiveKey, "imile.fs.oss.secret-key", enc, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(filepath.Join(dir, "prod.json")); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(Config{Dir: dir, AllowPlaintext: true, SnapshotKey: func() ([]byte, error) { return key, nil }, SensitiveKey: func() ([]byte, error) { return sensitiveKey, nil }})

	// the stored value is an external CryptoUtil ciphertext; without overrides
	// reveal shows the stored ciphertext (decrypted from the snapshot layer),
	// not the underlying secret
	r := httptest.NewRequest(http.MethodPost, "/api/snapshots/prod/reveal",
		strings.NewReader(`{"targets":["imile.fs.oss.secret-key"],"confirm":true}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status without key/iv = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "REAL-SECRET") {
		t.Fatalf("reveal leaked external plaintext without key/iv: %s", w.Body.String())
	}

	// explicit key/iv from the dialog decrypts the value
	body := fmt.Sprintf(`{"targets":["imile.fs.oss.secret-key"],"confirm":true,"key":%q,"iv":%q}`, aesKey, aesIV)
	r = httptest.NewRequest(http.MethodPost, "/api/snapshots/prod/reveal", strings.NewReader(body))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status with key/iv = %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "REAL-SECRET") {
		t.Fatalf("reveal body missing plaintext: %s", w.Body.String())
	}
}

func TestRevealShowsSensitiveValue(t *testing.T) {
	dir := t.TempDir()
	key := fixedKey()
	sensitiveKey := fixedSensitiveKey()
	s := apollo.NewSnapshot("prod", "")
	if err := s.Set(key, sensitiveKey, "SECRET_TOKEN", "plain-secret", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(filepath.Join(dir, "prod.json")); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(Config{Dir: dir, AllowPlaintext: true, SnapshotKey: func() ([]byte, error) { return key, nil }, SensitiveKey: func() ([]byte, error) { return sensitiveKey, nil }})

	// masked in the list view
	r := httptest.NewRequest(http.MethodGet, "/api/snapshots/prod", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if strings.Contains(w.Body.String(), "plain-secret") {
		t.Fatalf("list view leaked plaintext: %s", w.Body.String())
	}

	// reveal shows the plaintext via the sensitive key
	body := `{"targets":["SECRET_TOKEN"],"confirm":true}`
	r = httptest.NewRequest(http.MethodPost, "/api/snapshots/prod/reveal", strings.NewReader(body))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "plain-secret") {
		t.Fatalf("reveal body missing plaintext: %s", w.Body.String())
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
	t.Setenv("VAULTY_KEEPER_AES_CONFIG", filepath.Join(dir, "aes.json"))
	h := NewHandler(Config{Dir: dir, AllowPlaintext: true, SnapshotKey: func() ([]byte, error) { return make([]byte, 32), nil }})

	get := httptest.NewRecorder()
	h.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/aes/config", nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"entries"`) {
		t.Fatalf("get empty config = %d %s", get.Code, get.Body.String())
	}

	// add an entry (replaces the old PUT single-object flow)
	post := httptest.NewRecorder()
	h.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/api/aes/config",
		strings.NewReader(`{"name":"oss","secret-key":"kk","iv":"ivv"}`)))
	if post.Code != http.StatusCreated {
		t.Fatalf("post config = %d %s", post.Code, post.Body.String())
	}

	get2 := httptest.NewRecorder()
	h.ServeHTTP(get2, httptest.NewRequest(http.MethodGet, "/api/aes/config", nil))
	if get2.Code != http.StatusOK || !strings.Contains(get2.Body.String(), `"name":"oss"`) {
		t.Fatalf("get after post = %d %s", get2.Code, get2.Body.String())
	}

	// duplicate name is rejected
	post2 := httptest.NewRecorder()
	h.ServeHTTP(post2, httptest.NewRequest(http.MethodPost, "/api/aes/config",
		strings.NewReader(`{"name":"oss","secret-key":"k2","iv":"iv2"}`)))
	if post2.Code != http.StatusBadRequest {
		t.Fatalf("duplicate post = %d %s", post2.Code, post2.Body.String())
	}

	del := httptest.NewRecorder()
	h.ServeHTTP(del, httptest.NewRequest(http.MethodDelete, "/api/aes/config?name=oss", nil))
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete config = %d", del.Code)
	}

	get3 := httptest.NewRecorder()
	h.ServeHTTP(get3, httptest.NewRequest(http.MethodGet, "/api/aes/config", nil))
	if get3.Code != http.StatusOK || strings.Contains(get3.Body.String(), `"name":"oss"`) {
		t.Fatalf("get after delete = %d %s", get3.Code, get3.Body.String())
	}
}

func TestSensitiveKeyAPI(t *testing.T) {
	h := NewHandler(Config{Dir: t.TempDir(), SnapshotKey: func() ([]byte, error) { return make([]byte, 32), nil }, SensitiveKey: func() ([]byte, error) { return make([]byte, 32), nil }})
	r := httptest.NewRequest(http.MethodGet, "/api/sensitive/key", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"available":true`) {
		t.Fatalf("sensitive key status = %d %s", w.Code, w.Body.String())
	}

	// init when a key is already available is rejected
	r = httptest.NewRequest(http.MethodPost, "/api/sensitive/init", strings.NewReader(`{"force":false}`))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("init when available = %d, want %d", w.Code, http.StatusConflict)
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
		if strings.Contains(buf.String(), "vaulty-keeper UI available at") {
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
	// reverse default: mask everything except explicitly-safe keys
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
	for _, sv := range out.Rows[0].Values {
		if sv.Value != nil || !sv.Sensitive {
			t.Fatalf("DB_HOST value should be masked by default: %#v", sv)
		}
	}

	// mark DB_HOST safe in both snapshots; it then shows plaintext
	for _, ref := range []struct{ name, appID, text string }{
		{"prod", "app-x", "db.prod"},
		{"test", "app-y", "db.test"},
	} {
		body := strings.NewReader(`{"value":"` + ref.text + `","secret":false}`)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/snapshots/"+ref.name+"/items/DB_HOST?appid="+ref.appID, body))
		if w.Code != http.StatusOK {
			t.Fatalf("mark safe %s = %d: %s", ref.name, w.Code, w.Body.String())
		}
	}
	r = httptest.NewRequest(http.MethodPost, "/api/compare/multi",
		strings.NewReader(`{"refs":[{"name":"prod","app_id":"app-x"},{"name":"test","app_id":"app-y"}]}`))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Rows[0].Values[0].Value == nil || *out.Rows[0].Values[0].Value != "db.prod" {
		t.Fatalf("prod value after marking safe = %#v", out.Rows[0].Values[0])
	}
	if out.Rows[0].Values[1].Value == nil || *out.Rows[0].Values[1].Value != "db.test" {
		t.Fatalf("test value after marking safe = %#v", out.Rows[0].Values[1])
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

func TestFingerprintIsHMACKeyed(t *testing.T) {
	k1 := fixedKey()
	k2 := fixedSensitiveKey()
	v := "common-weak-password"

	f1 := safeValue(true, v, true, k1)
	f2 := safeValue(true, v, true, k2)
	if f1.Fingerprint == "" || f2.Fingerprint == "" {
		t.Fatal("missing fingerprint")
	}
	// different snapshot keys must yield different fingerprints, so an
	// attacker cannot offline-enumerate weak values against the token-free
	// GET compare endpoints
	if f1.Fingerprint == f2.Fingerprint {
		t.Fatal("fingerprint must differ across keys (offline dictionary attack vector)")
	}
	// same key + same value → same fingerprint (cross-env equality still works)
	f1b := safeValue(true, v, true, k1)
	if f1.Fingerprint != f1b.Fingerprint {
		t.Fatal("same key/value must share a fingerprint")
	}
	// quoted normalization preserved
	fq := safeValue(true, `"`+v+`"`, true, k1)
	if fq.Fingerprint != f1.Fingerprint {
		t.Fatal("quoted value must share fingerprint")
	}
	// non-sensitive values expose plaintext, no fingerprint
	fs := safeValue(true, v, false, k1)
	if fs.Fingerprint != "" || fs.Value == nil {
		t.Fatalf("plain value = %#v", fs)
	}
}

func TestTokenFailuresAreThrottled(t *testing.T) {
	h := NewHandler(Config{Dir: t.TempDir(), Token: "s3cr3t-token", SnapshotKey: func() ([]byte, error) { return fixedKey(), nil }, SensitiveKey: func() ([]byte, error) { return fixedSensitiveKey(), nil }})
	// consecutive bad tokens are all rejected (throttled with backoff)
	for i := 0; i < 3; i++ {
		r := httptest.NewRequest(http.MethodPost, "/api/sensitive/init", strings.NewReader(`{"force":false}`))
		r.Header.Set("X-Auth-Token", "wrong")
		w := httptest.NewRecorder()
		start := time.Now()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("bad token attempt %d = %d, want 401", i, w.Code)
		}
		if i > 0 && time.Since(start) < 40*time.Millisecond {
			t.Fatalf("attempt %d was not throttled", i)
		}
	}
	// a valid token still works and resets the counter
	r := httptest.NewRequest(http.MethodPost, "/api/sensitive/init", strings.NewReader(`{"force":false}`))
	r.Header.Set("X-Auth-Token", "s3cr3t-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("valid token after failures = %d, want 409 (key exists)", w.Code)
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

// TestPlaintextDisabledByDefault verifies that plaintext-emitting endpoints
// (reveal/export/edit/AES decrypt) are refused by default and only enabled
// with Config.AllowPlaintext, so a leaked token cannot dump plaintext unless
// the operator explicitly opted in.
func TestPlaintextDisabledByDefault(t *testing.T) {
	dir := t.TempDir()
	key := fixedKey()
	sensitiveKey := fixedSensitiveKey()
	s := apollo.NewSnapshot("prod", "")
	if err := s.Set(key, sensitiveKey, "SECRET_TOKEN", "secret", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(filepath.Join(dir, "prod.json")); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(Config{
		Dir:          dir,
		Token:        "t",
		SnapshotKey:  func() ([]byte, error) { return key, nil },
		SensitiveKey: func() ([]byte, error) { return sensitiveKey, nil },
	})

	post := func(path, body string) int {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.Header.Set("X-Auth-Token", "t")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if code := post("/api/snapshots/prod/reveal", `{"targets":["SECRET_TOKEN"],"confirm":true}`); code != http.StatusForbidden {
		t.Fatalf("reveal = %d, want 403", code)
	}
	if code := post("/api/snapshots/prod/export", `{"confirm":true}`); code != http.StatusForbidden {
		t.Fatalf("export = %d, want 403", code)
	}
	if code := post("/api/snapshots/prod/edit", `{"confirm":true}`); code != http.StatusForbidden {
		t.Fatalf("edit load = %d, want 403", code)
	}
	if code := post("/api/aes/transform", `{"op":"decrypt","key":"k","iv":"i","text":"x"}`); code != http.StatusForbidden {
		t.Fatalf("aes decrypt = %d, want 403", code)
	}

	// /api/config reports the state
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"allow_plaintext":false`) {
		t.Fatalf("config = %d %s", w.Code, w.Body.String())
	}

	// with AllowPlaintext the same reveal succeeds
	h2 := NewHandler(Config{
		Dir:            dir,
		Token:          "t",
		AllowPlaintext: true,
		SnapshotKey:    func() ([]byte, error) { return key, nil },
		SensitiveKey:   func() ([]byte, error) { return sensitiveKey, nil },
	})
	if code := postWithToken(h2, "/api/snapshots/prod/reveal", `{"targets":["SECRET_TOKEN"],"confirm":true}`); code != http.StatusOK {
		t.Fatalf("reveal with allow-plaintext = %d, want 200", code)
	}
}

func postWithToken(h http.Handler, path, body string) int {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("X-Auth-Token", "t")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}

// ---- database tunnel endpoints ----

func newDBHandler(t *testing.T, allowPlain bool) http.Handler {
	t.Helper()
	key := fixedKey()
	store := filepath.Join(t.TempDir(), "db.json")
	t.Setenv("VAULTY_KEEPER_BRIDGE_TOKEN", "bridge-tok")
	return NewHandler(Config{
		Dir:            t.TempDir(),
		SnapshotKey:    func() ([]byte, error) { return key, nil },
		SensitiveKey:   func() ([]byte, error) { return fixedSensitiveKey(), nil },
		AllowPlaintext: allowPlain,
		Token:          "ui-tok",
		DBStore:        store,
		DBKey:          func() ([]byte, error) { return key, nil },
	})
}

func TestDBListAddConnectRemove(t *testing.T) {
	h := newDBHandler(t, true)

	// key status
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/db/key", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"available":true`) {
		t.Fatalf("db key status = %d %s", w.Code, w.Body.String())
	}

	// add requires token
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/db/connections",
		bytes.NewBufferString(`{"name":"pg","url":"postgres://app:realpass@db.internal:5432/appdb"}`)))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("add without token = %d, want 401", w.Code)
	}

	// add with token
	w = httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/db/connections",
		bytes.NewBufferString(`{"name":"pg","url":"postgres://app:realpass@db.internal:5432/appdb"}`))
	r.Header.Set("X-Auth-Token", "ui-tok")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("add = %d: %s", w.Code, w.Body.String())
	}

	// list is open GET and never leaks the URL/password
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/db/list", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list = %d: %s", w.Code, w.Body.String())
	}
	for _, secret := range []string{"realpass", "db.internal"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("db list leaked %q: %s", secret, w.Body.String())
		}
	}
	if !strings.Contains(w.Body.String(), `"name":"pg"`) || !strings.Contains(w.Body.String(), `"type":"postgres"`) {
		t.Fatalf("db list missing connection: %s", w.Body.String())
	}

	// connect info: per-connection tunnel token, no real password
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/db/connect?name=pg", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"token":"`) {
		t.Fatalf("connect = %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "bridge-tok") {
		t.Fatalf("connect should use a per-connection token, not the global bridge token: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "realpass") || strings.Contains(w.Body.String(), "db.internal") {
		t.Fatalf("connect leaked real credentials: %s", w.Body.String())
	}

	// show requires POST + token + plaintext
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/api/db/show", bytes.NewBufferString(`{"name":"pg"}`))
	r.Header.Set("X-Auth-Token", "ui-tok")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "realpass") {
		t.Fatalf("show = %d %s", w.Code, w.Body.String())
	}

	// remove requires token
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/db/connections/pg", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("rm without token = %d", w.Code)
	}
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodDelete, "/api/db/connections/pg", nil)
	r.Header.Set("X-Auth-Token", "ui-tok")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("rm = %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/db/list", nil))
	if strings.Contains(w.Body.String(), `"name":"pg"`) {
		t.Fatalf("connection not removed: %s", w.Body.String())
	}
}

func TestDBShowGatedByPlaintext(t *testing.T) {
	h := newDBHandler(t, false) // plaintext disabled
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/db/show", bytes.NewBufferString(`{"name":"x"}`))
	r.Header.Set("X-Auth-Token", "ui-tok")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("db show without --allow-plaintext = %d, want 403", w.Code)
	}
}

// encryptForDBURL simulates the browser: derive an AES-256-GCM key from the
// UI's published ECDH public key and seal a database URL with it.
func encryptForDBURL(t *testing.T, pub *ecdh.PublicKey, dsn string) (ephRaw, ivRaw, ctRaw []byte) {
	t.Helper()
	eph, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := eph.ECDH(pub)
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(secret)
	if err != nil {
		t.Fatal(err)
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	iv := make([]byte, g.NonceSize())
	if _, err := rand.Read(iv); err != nil {
		t.Fatal(err)
	}
	return eph.PublicKey().Bytes(), iv, g.Seal(nil, iv, []byte(dsn), nil)
}

func TestDBPubKeyAndEncryptedAdd(t *testing.T) {
	h := newDBHandler(t, true)

	// pubkey is an open GET
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/db/pubkey", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("pubkey = %d %s", w.Code, w.Body.String())
	}
	var pk struct {
		Alg string `json:"alg"`
		Pub string `json:"pub"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &pk); err != nil {
		t.Fatalf("pubkey json: %v", err)
	}
	if pk.Alg != "ECDH-P256" || pk.Pub == "" {
		t.Fatalf("unexpected pubkey: %#v", pk)
	}
	pubRaw, err := base64.StdEncoding.DecodeString(pk.Pub)
	if err != nil {
		t.Fatalf("pub b64: %v", err)
	}
	serverPub, err := ecdh.P256().NewPublicKey(pubRaw)
	if err != nil {
		t.Fatalf("server pub: %v", err)
	}

	const dsn = "mysql://app:secrectpw@db.remote:3306/orders"
	eph, iv, ct := encryptForDBURL(t, serverPub, dsn)
	body, err := json.Marshal(map[string]any{
		"name": "enc-pg",
		"url_enc": map[string]string{
			"eph": base64.StdEncoding.EncodeToString(eph),
			"iv":  base64.StdEncoding.EncodeToString(iv),
			"ct":  base64.StdEncoding.EncodeToString(ct),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// encrypted add requires the token
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/db/connections", bytes.NewReader(body)))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("encrypted add without token = %d, want 401", w.Code)
	}

	w = httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/db/connections", bytes.NewReader(body))
	r.Header.Set("X-Auth-Token", "ui-tok")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("encrypted add = %d %s", w.Code, w.Body.String())
	}

	// show proves the decrypted URL round-tripped exactly (mysql type too)
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/api/db/show", bytes.NewBufferString(`{"name":"enc-pg"}`))
	r.Header.Set("X-Auth-Token", "ui-tok")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), dsn) {
		t.Fatalf("show encrypted-added = %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"mysql"`) {
		t.Fatalf("show type = %s", w.Body.String())
	}
}

func TestDBTestURLGatedByToken(t *testing.T) {
	h := newDBHandler(t, true)
	bad := bytes.NewBufferString(`{"url_enc":{"eph":"x","iv":"y","ct":"z"}}`)

	// no token -> 401
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/db/test-url", bad))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("test-url without token = %d, want 401", w.Code)
	}

	// token + garbage ciphertext -> 400 (no panic), not a crash
	w = httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/db/test-url",
		bytes.NewBufferString(`{"url_enc":{"eph":"x","iv":"y","ct":"z"}}`))
	r.Header.Set("X-Auth-Token", "ui-tok")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("test-url bad payload = %d, want 400", w.Code)
	}
}

// addConn adds a connection via the plaintext legacy field.
func addConn(t *testing.T, h http.Handler, name, dsn string) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/db/connections",
		bytes.NewBufferString(fmt.Sprintf(`{"name":%q,"url":%q}`, name, dsn)))
	r.Header.Set("X-Auth-Token", "ui-tok")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("add %s = %d: %s", name, w.Code, w.Body.String())
	}
}

func connectToken(t *testing.T, h http.Handler, name string) string {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/db/connect?name="+name, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("connect %s = %d", name, w.Code)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("connect %s json: %v", name, err)
	}
	if out.Token == "" {
		t.Fatalf("connect %s: empty token", name)
	}
	return out.Token
}

func TestDBRegenToken(t *testing.T) {
	h := newDBHandler(t, true)
	addConn(t, h, "pg", "postgres://app:realpass@db.internal:5432/appdb")
	addConn(t, h, "rd", "redis://:rpw@db.internal:6379/0")

	// regen requires the token
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/db/regen",
		bytes.NewBufferString(`{"name":"pg"}`)))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("regen without token = %d, want 401", w.Code)
	}

	old := connectToken(t, h, "pg")
	oldRD := connectToken(t, h, "rd")

	// single regen: new token differs, and connect now returns it
	w = httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/db/regen", bytes.NewBufferString(`{"name":"pg"}`))
	r.Header.Set("X-Auth-Token", "ui-tok")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("regen pg = %d: %s", w.Code, w.Body.String())
	}
	newTok := connectToken(t, h, "pg")
	if newTok == old {
		t.Fatalf("regen did not rotate the token: %q", newTok)
	}
	if connectToken(t, h, "rd") != oldRD {
		t.Fatalf("regen of one connection must not affect another")
	}
	if strings.Contains(w.Body.String(), "realpass") || strings.Contains(w.Body.String(), "db.internal") {
		t.Fatalf("regen response leaked real credentials: %s", w.Body.String())
	}

	// regen all: both connections rotate
	rdAfterSingle := connectToken(t, h, "rd")
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/api/db/regen", bytes.NewBufferString(`{"all":true}`))
	r.Header.Set("X-Auth-Token", "ui-tok")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("regen all = %d: %s", w.Code, w.Body.String())
	}
	if connectToken(t, h, "pg") == newTok {
		t.Fatalf("regen all did not rotate pg token")
	}
	if connectToken(t, h, "rd") == rdAfterSingle {
		t.Fatalf("regen all did not rotate rd token")
	}

	// unknown name -> 4xx
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/api/db/regen", bytes.NewBufferString(`{"name":"nope"}`))
	r.Header.Set("X-Auth-Token", "ui-tok")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound {
		t.Fatalf("regen unknown = %d, want 4xx", w.Code)
	}
}
