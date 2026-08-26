package app

import (
	"encoding/base64"
	"testing"

	"ai-tools/internal/apollo"
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
