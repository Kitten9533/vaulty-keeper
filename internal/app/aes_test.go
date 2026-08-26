package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAESConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AI_TOOLS_AES_CONFIG", filepath.Join(dir, "aes.json"))

	c, err := AESConfigLoad()
	if err != nil || c.Key != "" || c.IV != "" {
		t.Fatalf("load missing = %#v, %v", c, err)
	}
	if err := AESConfigSave("k", "v"); err != nil {
		t.Fatal(err)
	}
	c, err = AESConfigLoad()
	if err != nil || c.Key != "k" || c.IV != "v" {
		t.Fatalf("load after save = %#v, %v", c, err)
	}
	if err := AESConfigClear(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "aes.json")); !os.IsNotExist(err) {
		t.Fatal("file still exists after clear")
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
