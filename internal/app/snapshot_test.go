package app

import (
	"os"
	"testing"

	"ai-tools/internal/aesx"
	"ai-tools/internal/apollo"
)

func testSnapshot(t *testing.T, dir, name, appID string, entries map[string]string) []byte {
	t.Helper()
	key := make([]byte, 32)
	s := apollo.NewSnapshot(name, appID)
	for k, v := range entries {
		if err := s.Set(key, k, v, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Save(apollo.SnapPath(dir, name, appID)); err != nil {
		t.Fatal(err)
	}
	return key
}

func TestImportGetSetDelete(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, 32)
	n, err := Import(dir, "prod", "app-x", "A = 1\nSECRET_TOKEN = x\n", key)
	if err != nil || n != 2 {
		t.Fatalf("Import = %d, %v", n, err)
	}
	if v, ok, err := GetValue(dir, "prod", "app-x", key, "A"); err != nil || !ok || v != "1" {
		t.Fatalf("GetValue = %q %v %v", v, ok, err)
	}
	it, err := SetValue(dir, "prod", "app-x", "A", "2", nil, key)
	if err != nil || it.Value == nil || *it.Value != "2" {
		t.Fatalf("SetValue = %#v, %v", it, err)
	}
	ok, err := DeleteValue(dir, "prod", "app-x", "A", key)
	if err != nil || !ok {
		t.Fatalf("DeleteValue = %v, %v", ok, err)
	}
	if _, ok, _ := GetValue(dir, "prod", "app-x", key, "A"); ok {
		t.Fatal("A still exists after delete")
	}
}

func TestCompare(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, 32)
	testSnapshot(t, dir, "prod", "", map[string]string{"A": "1", "B": "same", "SECRET_TOKEN": "x"})
	testSnapshot(t, dir, "test", "", map[string]string{"B": "same", "C": "new", "SECRET_TOKEN": "y"})
	changes, err := Compare(dir, "prod", "", "test", "", key)
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
	key := testSnapshot(t, dir, "prod", "app-x", map[string]string{"APP_NAME": "merdi", "SECRET_TOKEN": "s3cr3t"})
	text, err := Export(dir, "prod", "app-x", key)
	if err != nil {
		t.Fatal(err)
	}
	want := "APP_NAME = merdi\nSECRET_TOKEN = s3cr3t\n"
	if text != want {
		t.Fatalf("Export = %q, want %q", text, want)
	}
	loadText, err := EditLoad(dir, "prod", "app-x", key)
	if err != nil || loadText != want {
		t.Fatalf("EditLoad = %q, %v", loadText, err)
	}
	n, err := EditApply(dir, "prod", "app-x", key, "APP_NAME = merdi\nSECRET_TOKEN = s3cr3t\nNEW = 1\n")
	if err != nil || n != 3 {
		t.Fatalf("EditApply = %d, %v", n, err)
	}
	s, err := apollo.Load(apollo.SnapPath(dir, "prod", "app-x"))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"APP_NAME", "SECRET_TOKEN", "NEW"} {
		if v, ok, err := s.Get(key, k); err != nil || !ok || v == "" {
			t.Fatalf("after apply %s: %q %v %v", k, v, ok, err)
		}
	}
}

func TestReveal(t *testing.T) {
	t.Setenv("AI_TOOLS_AES_KEY", "")
	t.Setenv("AI_TOOLS_AES_IV", "")
	dir := t.TempDir()
	key := make([]byte, 32)
	aesKey, aesIV := "0123456789abcdef", "abcdefghijklmnop"
	enc, err := aesx.Encrypt(aesKey, aesIV, "REAL-SECRET")
	if err != nil {
		t.Fatal(err)
	}
	s := apollo.NewSnapshot("prod", "")
	for _, kv := range []struct{ k, v string }{
		{"imile.fs.aes.secret-key", aesKey},
		{"imile.fs.aes.iv", aesIV},
		{"imile.fs.oss.secret-key", enc},
	} {
		if err := s.Set(key, kv.k, kv.v, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Save(apollo.SnapPath(dir, "prod", "")); err != nil {
		t.Fatal(err)
	}
	plain, err := Reveal(dir, "prod", "", key, []string{"imile.fs.oss.secret-key"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if plain["imile.fs.oss.secret-key"] != "REAL-SECRET" {
		t.Fatalf("reveal = %#v", plain)
	}
	enc2, _ := aesx.Encrypt("fedcba9876543210", "ponmlkjihgfedcba", "OTHER")
	if err := s.Set(key, "imile.fs.oss.secret-key", enc2, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(apollo.SnapPath(dir, "prod", "")); err != nil {
		t.Fatal(err)
	}
	plain2, err := Reveal(dir, "prod", "", key, []string{"imile.fs.oss.secret-key"}, "fedcba9876543210", "ponmlkjihgfedcba")
	if err != nil || plain2["imile.fs.oss.secret-key"] != "OTHER" {
		t.Fatalf("override reveal = %#v, %v", plain2, err)
	}
}

func TestImportRejectsInvalidName(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, 32)
	if _, err := Import(dir, "../escape", "app-x", "A = 1\n", key); err == nil {
		t.Fatal("Import with traversal name succeeded")
	}
}

func TestRemove(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, 32)
	if _, err := Import(dir, "prod", "app-x", "A = 1\n", key); err != nil {
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
