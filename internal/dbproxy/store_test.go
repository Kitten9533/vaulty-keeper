package dbproxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return key
}

func TestConnTypeFromURL(t *testing.T) {
	cases := []struct {
		raw  string
		typ  string
		wantErr bool
	}{
		{"postgres://u:p@h:5432/db", "postgres", false},
		{"postgresql://u:p@h/db", "postgres", false},
		{"mysql://u:p@h:3306/db", "mysql", false},
		{"redis://h:6379/0", "redis", false},
		{"rediss://h:6380", "redis", false},
		{"mongodb://u:p@h/db", "", true},
		{"http://h", "", true},
		{":not a url", "", true},
	}
	for _, c := range cases {
		typ, err := ConnTypeFromURL(c.raw)
		if c.wantErr {
			if err == nil {
				t.Errorf("ConnTypeFromURL(%q): want error, got %q", c.raw, typ)
			}
			continue
		}
		if err != nil {
			t.Errorf("ConnTypeFromURL(%q): %v", c.raw, err)
			continue
		}
		if typ != c.typ {
			t.Errorf("ConnTypeFromURL(%q) = %q, want %q", c.raw, typ, c.typ)
		}
	}
}

func TestAddListResolveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	key := testKey(t)

	if err := Add(path, key, "cache", "redis://redis.example.com:6379/0", 0); err != nil {
		t.Fatal(err)
	}
	if err := Add(path, key, "prod", "postgres://u:secret@db.example.com:5432/mydb", 0); err != nil {
		t.Fatal(err)
	}

	conns, err := List(path, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(conns) != 2 {
		t.Fatalf("List() = %d conns, want 2", len(conns))
	}
	// sorted by name: cache, prod
	if conns[0].Name != "cache" || conns[0].Type != "redis" {
		t.Errorf("conns[0] = %+v", conns[0])
	}
	if conns[1].Name != "prod" || conns[1].Type != "postgres" {
		t.Errorf("conns[1] = %+v", conns[1])
	}
	// ports auto-allocated: cache 15432, prod 15433
	if conns[0].Port != DefaultPortBase || conns[1].Port != DefaultPortBase+1 {
		t.Errorf("ports = %d, %d; want %d, %d", conns[0].Port, conns[1].Port, DefaultPortBase, DefaultPortBase+1)
	}
	// List must never expose the URL
	for _, c := range conns {
		if c.URL != "" {
			t.Errorf("List() leaked URL for %s", c.Name)
		}
	}

	c, err := Resolve(path, key, "prod")
	if err != nil {
		t.Fatal(err)
	}
	if c.URL != "postgres://u:secret@db.example.com:5432/mydb" {
		t.Errorf("Resolve().URL = %q", c.URL)
	}
	if c.Port != DefaultPortBase+1 {
		t.Errorf("Resolve().Port = %d", c.Port)
	}
}

func TestAddRejectsUnknownSchemeAndBadName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	key := testKey(t)
	if err := Add(path, key, "x", "mongodb://u:p@h/db", 0); err == nil {
		t.Fatal("Add() accepted mongodb scheme")
	}
	if err := Add(path, key, "-bad", "redis://h", 0); err == nil {
		t.Fatal("Add() accepted invalid name")
	}
}

func TestRemove(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	key := testKey(t)
	if err := Add(path, key, "a", "redis://h", 0); err != nil {
		t.Fatal(err)
	}
	if err := Remove(path, key, "missing"); err == nil {
		t.Fatal("Remove() accepted missing name")
	}
	if err := Remove(path, key, "a"); err != nil {
		t.Fatal(err)
	}
	conns, err := List(path, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(conns) != 0 {
		t.Fatalf("conns after remove = %d", len(conns))
	}
}

func TestFileContainsNoPlaintextURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	key := testKey(t)
	secret := "postgres://root:S3cr3t!@db.internal:5432/prod"
	if err := Add(path, key, "prod", secret, 0); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "S3cr3t!") || strings.Contains(string(b), "db.internal") {
		t.Fatal("db.json contains plaintext connection URL")
	}
}

func TestAllocPortConflicts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	key := testKey(t)
	if err := Add(path, key, "a", "redis://h", DefaultPortBase); err != nil {
		t.Fatal(err)
	}
	// explicit collision
	if err := Add(path, key, "b", "redis://h2", DefaultPortBase); err == nil {
		t.Fatal("Add() allowed explicit port collision")
	}
	// auto-alloc must skip the taken port
	if err := Add(path, key, "b", "redis://h2", 0); err != nil {
		t.Fatal(err)
	}
	conns, err := List(path, key)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range conns {
		if c.Port == DefaultPortBase && c.Name != "a" {
			t.Errorf("port %d reused by %s", DefaultPortBase, c.Name)
		}
	}
}

func TestResolveMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	key := testKey(t)
	if _, err := Resolve(path, key, "nope"); err == nil {
		t.Fatal("Resolve() accepted missing connection")
	}
}

func TestAddOverwriteKeepsPort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	key := testKey(t)
	if err := Add(path, key, "m", "redis://h1", 0); err != nil {
		t.Fatal(err)
	}
	conns, _ := List(path, key)
	orig := conns[0].Port

	// re-add same name without --port -> port unchanged
	if err := Add(path, key, "m", "redis://h2", 0); err != nil {
		t.Fatal(err)
	}
	conns, _ = List(path, key)
	if conns[0].Port != orig {
		t.Fatalf("overwrite changed port: %d -> %d", orig, conns[0].Port)
	}
	if c, _ := Resolve(path, key, "m"); c.URL != "redis://h2" {
		t.Fatalf("URL not updated: %q", c.URL)
	}

	// explicit --port does change it
	if err := Add(path, key, "m", "redis://h3", 15499); err != nil {
		t.Fatal(err)
	}
	if c, _ := Resolve(path, key, "m"); c.Port != 15499 {
		t.Fatalf("explicit port ignored: %d", c.Port)
	}
}

func TestKeyMismatchDiagnosedPrecisely(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	key1 := testKey(t)
	if err := Add(path, key1, "m", "redis://h", 0); err != nil {
		t.Fatal(err)
	}
	// a different key: List reports the entry as Broken (not fatal)...
	key2 := make([]byte, 32)
	for i := range key2 {
		key2[i] = byte(i + 7)
	}
	conns, err := List(path, key2)
	if err != nil {
		t.Fatalf("List with wrong key should not fail: %v", err)
	}
	if len(conns) != 1 || !conns[0].Broken {
		t.Fatalf("expected one broken entry, got %+v", conns)
	}
	if conns[0].Type != "redis" {
		t.Fatalf("broken entry should keep stored type, got %q", conns[0].Type)
	}
	// ...while Resolve gives a precise key_id mismatch error
	_, err = Resolve(path, key2, "m")
	if err == nil {
		t.Fatal("Resolve with wrong key succeeded")
	}
	if !strings.Contains(err.Error(), "密钥不匹配") ||
		!strings.Contains(err.Error(), keyID(key1)) ||
		!strings.Contains(err.Error(), keyID(key2)) {
		t.Fatalf("mismatch error not precise: %v", err)
	}
	if !strings.Contains(err.Error(), "db add") {
		t.Fatalf("mismatch error should suggest re-register: %v", err)
	}
}
