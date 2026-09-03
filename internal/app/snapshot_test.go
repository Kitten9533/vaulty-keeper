package app

import (
	"os"
	"strings"
	"testing"

	"vaulty-keeper/internal/aesx"
	"vaulty-keeper/internal/apollo"
)

func testSnapshot(t *testing.T, dir, name, appID string, entries map[string]string) ([]byte, []byte) {
	t.Helper()
	key := make([]byte, 32)
	sensitiveKey := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	for i := range sensitiveKey {
		sensitiveKey[i] = byte(200 - i)
	}
	s := apollo.NewSnapshot(name, appID)
	for k, v := range entries {
		if err := s.Set(key, sensitiveKey, k, v, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Save(apollo.SnapPath(dir, name, appID)); err != nil {
		t.Fatal(err)
	}
	return key, sensitiveKey
}

func TestImportGetSetDelete(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, 32)
	sensitiveKey := make([]byte, 32)
	n, err := Import(dir, "prod", "app-x", "A = 1\nSECRET_TOKEN = x\n", key, sensitiveKey)
	if err != nil || n != 2 {
		t.Fatalf("Import = %d, %v", n, err)
	}
	if v, ok, err := GetValue(dir, "prod", "app-x", key, sensitiveKey, "A"); err != nil || !ok || v != "1" {
		t.Fatalf("GetValue = %q %v %v", v, ok, err)
	}
	if v, ok, err := GetValue(dir, "prod", "app-x", key, sensitiveKey, "SECRET_TOKEN"); err != nil || !ok || v != "x" {
		t.Fatalf("GetValue sensitive = %q %v %v", v, ok, err)
	}
	plain := false
	it, err := SetValue(dir, "prod", "app-x", "A", "2", &plain, key, sensitiveKey)
	if err != nil || it.Value == nil || *it.Value != "2" {
		t.Fatalf("SetValue = %#v, %v", it, err)
	}
	ok, err := DeleteValue(dir, "prod", "app-x", "A", key)
	if err != nil || !ok {
		t.Fatalf("DeleteValue = %v, %v", ok, err)
	}
	if _, ok, _ := GetValue(dir, "prod", "app-x", key, sensitiveKey, "A"); ok {
		t.Fatal("A still exists after delete")
	}
}

func TestCompare(t *testing.T) {
	dir := t.TempDir()
	key, sensitiveKey := testSnapshot(t, dir, "prod", "", map[string]string{"A": "1", "B": "same", "SECRET_TOKEN": "x"})
	testSnapshot(t, dir, "test", "", map[string]string{"B": "same", "C": "new", "SECRET_TOKEN": "y"})
	changes, err := Compare(dir, "prod", "", "test", "", key, sensitiveKey)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range changes {
		got[c.Key] = c.Kind
	}
	if got["A"] != "removed" || got["C"] != "added" || got["SECRET_TOKEN"] != "changed" {
		t.Fatalf("compare = %#v", got)
	}
}

func TestExportAndEditRoundTrip(t *testing.T) {
	dir := t.TempDir()
	key, sensitiveKey := testSnapshot(t, dir, "prod", "app-x", map[string]string{"APP_NAME": "merdi", "SECRET_TOKEN": "s3cr3t"})
	text, err := Export(dir, "prod", "app-x", key, sensitiveKey)
	if err != nil {
		t.Fatal(err)
	}
	want := "APP_NAME = merdi\nSECRET_TOKEN = s3cr3t\n"
	if text != want {
		t.Fatalf("Export = %q, want %q", text, want)
	}
	loadText, err := EditLoad(dir, "prod", "app-x", key, sensitiveKey)
	if err != nil || loadText != want {
		t.Fatalf("EditLoad = %q, %v", loadText, err)
	}
	n, err := EditApply(dir, "prod", "app-x", key, sensitiveKey, "APP_NAME = merdi\nSECRET_TOKEN = s3cr3t\nNEW = 1\n")
	if err != nil || n != 3 {
		t.Fatalf("EditApply = %d, %v", n, err)
	}
	s, err := apollo.Load(apollo.SnapPath(dir, "prod", "app-x"))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"APP_NAME", "SECRET_TOKEN", "NEW"} {
		if v, ok, err := s.Get(key, sensitiveKey, k); err != nil || !ok || v == "" {
			t.Fatalf("after apply %s: %q %v %v", k, v, ok, err)
		}
	}
}

func TestRevealShowsSensitiveValue(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, 32)
	sensitiveKey := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	for i := range sensitiveKey {
		sensitiveKey[i] = byte(200 - i)
	}
	s := apollo.NewSnapshot("prod", "")
	if err := s.Set(key, sensitiveKey, "SECRET_TOKEN", "plain-secret", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(key, sensitiveKey, "APP_NAME", "merdi", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(apollo.SnapPath(dir, "prod", "")); err != nil {
		t.Fatal(err)
	}

	plain, err := Reveal(dir, "prod", "", key, sensitiveKey, []string{"SECRET_TOKEN", "APP_NAME"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if plain["SECRET_TOKEN"] != "plain-secret" || plain["APP_NAME"] != "merdi" {
		t.Fatalf("reveal = %#v", plain)
	}
}

func TestRevealOverrideDecryptsExternalCiphertext(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, 32)
	sensitiveKey := make([]byte, 32)
	aesKey, aesIV := "0123456789abcdef", "abcdefghijklmnop"
	enc, err := aesx.Encrypt(aesKey, aesIV, "REAL-SECRET")
	if err != nil {
		t.Fatal(err)
	}
	s := apollo.NewSnapshot("prod", "")
	// stored value is an external CryptoUtil ciphertext, encrypted at rest with
	// the sensitive key
	if err := s.Set(key, sensitiveKey, "imile.fs.oss.secret-key", enc, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(apollo.SnapPath(dir, "prod", "")); err != nil {
		t.Fatal(err)
	}

	// without overrides the stored ciphertext (snapshot layer) is returned
	plain, err := Reveal(dir, "prod", "", key, sensitiveKey, []string{"imile.fs.oss.secret-key"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if plain["imile.fs.oss.secret-key"] != enc {
		t.Fatalf("reveal without override = %#v", plain)
	}

	// with overrides the external AES layer is decrypted
	plain2, err := Reveal(dir, "prod", "", key, sensitiveKey, []string{"imile.fs.oss.secret-key"}, aesKey, aesIV)
	if err != nil || plain2["imile.fs.oss.secret-key"] != "REAL-SECRET" {
		t.Fatalf("override reveal = %#v, %v", plain2, err)
	}
}

func TestRevealOldFormatFallsBackToSnapshotKey(t *testing.T) {
	// old-format snapshots encrypted sensitive values with the snapshot key;
	// reveal falls back to it so they stay readable
	dir := t.TempDir()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	otherKey := make([]byte, 32)
	for i := range otherKey {
		otherKey[i] = byte(100 - i)
	}
	s := apollo.NewSnapshot("prod", "")
	if err := s.Set(key, key, "SECRET_TOKEN", "old-format", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(apollo.SnapPath(dir, "prod", "")); err != nil {
		t.Fatal(err)
	}
	plain, err := Reveal(dir, "prod", "", key, otherKey, []string{"SECRET_TOKEN"}, "", "")
	if err != nil || plain["SECRET_TOKEN"] != "old-format" {
		t.Fatalf("old-format reveal = %#v, %v", plain, err)
	}
}

func TestImportRejectsInvalidName(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, 32)
	if _, err := Import(dir, "../escape", "app-x", "A = 1\n", key, key); err == nil {
		t.Fatal("Import with traversal name succeeded")
	}
}

func TestRemove(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, 32)
	if _, err := Import(dir, "prod", "app-x", "A = 1\n", key, key); err != nil {
		t.Fatal(err)
	}
	ok, err := Remove(dir, "prod", "app-x")
	if err != nil || !ok {
		t.Fatalf("Remove = %v, %v", ok, err)
	}
	if _, err := os.Stat(apollo.SnapPath(dir, "prod", "app-x")); !os.IsNotExist(err) {
		t.Fatal("file still exists after Remove")
	}
	ok, err = Remove(dir, "prod", "app-x")
	if err != nil || ok {
		t.Fatalf("second Remove = %v, %v", ok, err)
	}
}

func TestLoadErrorMentionsAppIDAndHints(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, 32)
	for _, appID := range []string{"a", "merdi2"} {
		if _, err := Import(dir, "test", appID, "A = 1\n", key, key); err != nil {
			t.Fatal(err)
		}
	}
	_, _, _, err := GetValueSafe(dir, "test", "merdi", key, key, "A")
	if err == nil {
		t.Fatal("expected an error for a missing snapshot")
	}
	msg := err.Error()
	for _, want := range []string{`"test"（appid merdi）`, "appid a", "appid merdi2"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q:\n%s", want, msg)
		}
	}
}
