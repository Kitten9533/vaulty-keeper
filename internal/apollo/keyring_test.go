package apollo

import (
	"errors"
	"testing"
)

// TestGenerateAndStoreDBKeyInMemory exercises key generation against an
// in-memory store. It deliberately does NOT touch the real Keychain: a real
// write here would silently rotate the DB key and break every previously
// registered database connection (this exact bug happened via a test once).
func TestGenerateAndStoreDBKeyInMemory(t *testing.T) {
	store := map[string]string{}
	oldGet, oldSet := keyStoreGet, keyStoreSet
	keyStoreGet = func(acct string) (string, error) {
		if v, ok := store[acct]; ok {
			return v, nil
		}
		return "", errors.New("not found")
	}
	keyStoreSet = func(acct, val string) error {
		store[acct] = val
		return nil
	}
	t.Cleanup(func() {
		keyStoreGet, keyStoreSet = oldGet, oldSet
	})

	if err := GenerateAndStoreDBKey(true); err != nil {
		t.Fatal(err)
	}
	if _, ok := store[DBKeychainAccount]; !ok {
		t.Fatal("key not stored")
	}
	// forced regeneration overwrites
	if err := GenerateAndStoreDBKey(true); err != nil {
		t.Fatal(err)
	}
	// non-forced refuses when a key exists
	if err := GenerateAndStoreDBKey(false); err == nil {
		t.Fatal("GenerateAndStoreDBKey(false) with existing key should fail")
	}

	// stored key round-trips through DBKey (no env set)
	t.Setenv(EnvDBKey, "")
	if _, err := DBKey(); err != nil {
		t.Fatalf("DBKey() from in-memory store: %v", err)
	}
}
