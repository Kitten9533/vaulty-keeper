package apollo

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

var testKey = make([]byte, 32)

func TestStoreRoundTrip(t *testing.T) {
	s := NewSnapshot("prod", "imile-fs")
	for i := 0; i < len(testKey); i++ {
		testKey[i] = byte(i + 1)
	}
	mustSet(t, s, "APP_NAME", "merdi", nil)
	mustSet(t, s, "PASSWORD_SALT", "10", nil)
	mustSet(t, s, "imile.fs.oss.secret-key", "ciphertext-x", nil)

	if !s.Items["PASSWORD_SALT"].Secret {
		t.Error("PASSWORD_SALT should be auto-marked secret")
	}
	if !s.Items["imile.fs.oss.secret-key"].Secret {
		t.Error("secret-key should be auto-marked secret")
	}
	if s.Items["APP_NAME"].Secret {
		t.Error("APP_NAME should not be secret")
	}

	v, ok, err := s.Get(testKey, "APP_NAME")
	if err != nil || !ok || v != "merdi" {
		t.Fatalf("get APP_NAME: %q %v %v", v, ok, err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "prod.json")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("snapshot file mode = %o, want 600", fi.Mode().Perm())
	}

	raw, _ := os.ReadFile(path)
	if contains(raw, "merdi") || contains(raw, "ciphertext-x") {
		t.Error("snapshot file contains plaintext values")
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := loaded.Get(testKey, "APP_NAME")
	if err != nil || got != "merdi" {
		t.Fatalf("loaded get APP_NAME: %q %v", got, err)
	}
}

func TestStoreDiff(t *testing.T) {
	a := NewSnapshot("prod", "")
	b := NewSnapshot("test", "")
	mustSet(t, a, "K1", "same", nil)
	mustSet(t, a, "K2", "old", nil)
	mustSet(t, a, "K3", "gone", nil)
	mustSet(t, b, "K1", "same", nil)
	mustSet(t, b, "K2", "new", nil)
	mustSet(t, b, "K4", "fresh", nil)

	changes, err := a.Diff(b, testKey)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Change{}
	for _, c := range changes {
		got[c.Key] = c
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 changes, got %v", changes)
	}
	if got["K2"].Kind != "changed" || got["K2"].Old != "old" || got["K2"].New != "new" {
		t.Errorf("K2: %+v", got["K2"])
	}
	if got["K3"].Kind != "removed" {
		t.Errorf("K3: %+v", got["K3"])
	}
	if got["K4"].Kind != "added" || got["K4"].New != "fresh" {
		t.Errorf("K4: %+v", got["K4"])
	}
	if _, ok := got["K1"]; ok {
		t.Error("K1 unchanged, should not appear")
	}
}

func TestStoreDiffFailsOnDecryptError(t *testing.T) {
	a := NewSnapshot("prod", "")
	b := NewSnapshot("test", "")
	mustSet(t, a, "K1", "same", nil)
	mustSet(t, b, "K1", "same", nil)
	// corrupt b's ciphertext so it cannot be decrypted
	bad := b.Items["K1"]
	bad.Enc = "AAAA"
	b.Items["K1"] = bad

	if _, err := a.Diff(b, testKey); err == nil {
		t.Fatal("Diff should fail when a value cannot be decrypted")
	}
}

func TestStoreDelete(t *testing.T) {
	s := NewSnapshot("x", "")
	mustSet(t, s, "A", "1", nil)
	if !s.Delete("A") {
		t.Fatal("delete should succeed")
	}
	if s.Delete("A") {
		t.Error("second delete should fail")
	}
}

func TestValidateSnapshotName(t *testing.T) {
	for _, name := range []string{"prod", "prod-us", "prod.v2", "a_1"} {
		if err := ValidateSnapshotName(name); err != nil {
			t.Errorf("ValidateSnapshotName(%q): %v", name, err)
		}
	}
	for _, name := range []string{"", ".", "../prod", "prod/name", "/tmp/prod", "prod space"} {
		if err := ValidateSnapshotName(name); err == nil {
			t.Errorf("ValidateSnapshotName(%q) succeeded, want error", name)
		}
	}
}

func TestSnapshotVisibleItemsMaskSensitiveValues(t *testing.T) {
	s := NewSnapshot("prod", "")
	mustSet(t, s, "APP_NAME", "merdi", nil)
	mustSet(t, s, "SECRET_TOKEN", "do-not-expose", nil)

	items, err := s.VisibleItems(testKey)
	if err != nil {
		t.Fatal(err)
	}
	if items["APP_NAME"].Value == nil || *items["APP_NAME"].Value != "merdi" {
		t.Fatalf("normal item = %#v", items["APP_NAME"])
	}
	secret := items["SECRET_TOKEN"]
	if !secret.Sensitive || secret.Value != nil || secret.Length != len("do-not-expose") {
		t.Fatalf("sensitive item = %#v", secret)
	}
}

func TestListSnapshots(t *testing.T) {
	dir := t.TempDir()
	if refs, err := ListSnapshots(dir); err != nil || len(refs) != 0 {
		t.Fatalf("empty dir: %v %v", refs, err)
	}
	mustSet(t, NewSnapshot("prod", ""), "A", "1", nil).Save(filepath.Join(dir, "prod.json"))
	mustSet(t, NewSnapshot("prod", "app-x"), "A", "1", nil).Save(filepath.Join(dir, "prod__app-x.json"))
	mustSet(t, NewSnapshot("test", ""), "A", "1", nil).Save(filepath.Join(dir, "test.json"))
	os.WriteFile(filepath.Join(dir, "notjson.txt"), []byte("x"), 0o600)
	refs, err := ListSnapshots(dir)
	if err != nil || len(refs) != 3 {
		t.Fatalf("got %v %v", refs, err)
	}
	// The meta Name is the env; AppID is carried alongside.
	var prodRefs []SnapshotRef
	for _, r := range refs {
		if r.Name == "prod" {
			prodRefs = append(prodRefs, r)
		}
	}
	if len(prodRefs) != 2 {
		t.Fatalf("prod refs = %#v", prodRefs)
	}
	seen := map[string]bool{}
	for _, r := range prodRefs {
		seen[r.AppID] = true
	}
	if !seen[""] || !seen["app-x"] {
		t.Fatalf("prod refs = %#v", prodRefs)
	}
}

func TestValidateAppID(t *testing.T) {
	for _, id := range []string{"merdi-portal", "app_1", "A.B2"} {
		if err := ValidateAppID(id); err != nil {
			t.Errorf("ValidateAppID(%q): %v", id, err)
		}
	}
	for _, id := range []string{"", ".", "../x", "a/b", "a b", "-lead"} {
		if err := ValidateAppID(id); err == nil {
			t.Errorf("ValidateAppID(%q) succeeded, want error", id)
		}
	}
}

func TestFileName(t *testing.T) {
	if got := FileName("prod", "app-x"); got != "prod__app-x.json" {
		t.Fatalf("FileName = %q", got)
	}
	if got := FileName("prod", ""); got != "prod.json" {
		t.Fatalf("FileName(empty) = %q", got)
	}
	if got := SnapPath("/d", "prod", "app-x"); got != filepath.Join("/d", "prod__app-x.json") {
		t.Fatalf("SnapPath = %q", got)
	}
}

func contains(b []byte, sub string) bool {
	return bytes.Contains(b, []byte(sub))
}

func mustSet(t *testing.T, s *Snapshot, k, v string, secret *bool) *Snapshot {
	t.Helper()
	if err := s.Set(testKey, k, v, secret); err != nil {
		t.Fatalf("set %s: %v", k, err)
	}
	return s
}
