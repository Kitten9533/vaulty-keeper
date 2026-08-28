package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAESConfigListCRUD(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AI_TOOLS_AES_CONFIG", filepath.Join(dir, "aes.json"))

	entries, err := AESConfigList()
	if err != nil || len(entries) != 0 {
		t.Fatalf("load missing = %#v, %v", entries, err)
	}
	if err := AESConfigAdd("oss", "k1", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := AESConfigAdd("default", "k2", "v2"); err != nil {
		t.Fatal(err)
	}
	if err := AESConfigAdd("oss", "k3", "v3"); err == nil {
		t.Fatal("duplicate name should fail")
	}
	entries, err = AESConfigList()
	if err != nil || len(entries) != 2 {
		t.Fatalf("after add = %#v, %v", entries, err)
	}
	if err := AESConfigRemove("oss"); err != nil {
		t.Fatal(err)
	}
	entries, err = AESConfigList()
	if err != nil || len(entries) != 1 || entries[0].Name != "default" {
		t.Fatalf("after remove = %#v, %v", entries, err)
	}
	e, err := AESConfigGet("default")
	if err != nil || e == nil || e.SecretKey != "k2" || e.IV != "v2" {
		t.Fatalf("get = %#v, %v", e, err)
	}
	if _, err := AESConfigGet("missing"); err != nil {
		t.Fatal(err)
	}
}

func TestAESConfigMigratesLegacyObject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aes.json")
	t.Setenv("AI_TOOLS_AES_CONFIG", path)
	os.WriteFile(path, []byte(`{"key":"oldk","iv":"oldv"}`), 0o600)

	entries, err := AESConfigList()
	if err != nil || len(entries) != 1 || entries[0].Name != "default" || entries[0].SecretKey != "oldk" || entries[0].IV != "oldv" {
		t.Fatalf("migrated = %#v, %v", entries, err)
	}

	// saving writes the array form
	if err := AESConfigAdd("extra", "x", "y"); err != nil {
		t.Fatal(err)
	}
	entries, err = AESConfigList()
	if err != nil || len(entries) != 2 {
		t.Fatalf("after add = %#v, %v", entries, err)
	}
}

func TestGenKey(t *testing.T) {
	key, iv, err := GenKey(16, 12)
	if err != nil || len(key) != 16 || len(iv) != 12 {
		t.Fatalf("GenKey = %q %q %v", key, iv, err)
	}
	if _, _, err := GenKey(15, 12); err == nil {
		t.Fatal("GenKey(15) succeeded")
	}
	if _, _, err := GenKey(16, 11); err == nil {
		t.Fatal("GenKey(iv 11) succeeded")
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key, iv, _ := GenKey(16, 16)
	ct, err := Encrypt(key, iv, "hello 世界")
	if err != nil {
		t.Fatal(err)
	}
	pt, err := Decrypt(key, iv, ct)
	if err != nil || pt != "hello 世界" {
		t.Fatalf("round trip = %q, %v", pt, err)
	}
}
