package app

import (
	"encoding/base64"
	"testing"

	"vaulty-keeper/internal/apollo"
)

func TestKeyAvailableWithEnvKey(t *testing.T) {
	valid := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv(apollo.EnvKey, valid)
	if !KeyAvailable() {
		t.Fatal("KeyAvailable() = false with valid env key")
	}
	t.Setenv(apollo.EnvKey, "not-base64!")
	if KeyAvailable() {
		t.Fatal("KeyAvailable() = true with invalid env key")
	}
}

func TestSnapshotKeyFromEnv(t *testing.T) {
	key := make([]byte, 32)
	key[0] = 7
	enc := base64.StdEncoding.EncodeToString(key)
	t.Setenv(apollo.EnvKey, enc)
	got, err := SnapshotKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 32 || got[0] != 7 {
		t.Fatalf("SnapshotKey() = %x", got)
	}
}

func TestDBKeyFromEnv(t *testing.T) {
	key := make([]byte, 32)
	key[0] = 9
	enc := base64.StdEncoding.EncodeToString(key)
	t.Setenv(apollo.EnvDBKey, enc)
	got, err := apollo.DBKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 32 || got[0] != 9 {
		t.Fatalf("DBKey() = %x", got)
	}
	t.Setenv(apollo.EnvDBKey, "not-base64!")
	if _, err := apollo.DBKey(); err == nil {
		t.Fatal("DBKey() accepted invalid env key")
	}
}

func TestGenerateAndStoreDBKey(t *testing.T) {
	// This test must NOT touch the real Keychain: it would silently rotate the
	// DB key and break previously registered connections. The actual DB-key
	// logic is exercised in internal/apollo/keyring_test.go with an in-memory
	// store; here we only verify the env path (which needs no keyring).
	key := make([]byte, 32)
	key[0] = 9
	enc := base64.StdEncoding.EncodeToString(key)
	t.Setenv(apollo.EnvDBKey, enc)
	got, err := apollo.DBKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 32 || got[0] != 9 {
		t.Fatalf("DBKey() = %x", got)
	}
}
