package apollo

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Key management: macOS Keychain with env fallback. Keys never live in a
// plaintext file. Two independent keys are used:
//   - the snapshot key (KeychainAccount) encrypts every non-sensitive value;
//   - the sensitive key (SensitiveKeychainAccount) encrypts sensitive values,
//     so revealing them additionally requires the sensitive key.
//
// Env overrides exist so headless machines without a Keychain can still work,
// but they are considered red-line secrets: never export them into an agent
// session or pass them on the command line.

const (
	KeychainService = "ai-tools"
	KeychainAccount = "apollo-snapshot-key"
	EnvKey          = "AI_TOOLS_APOLLO_KEY"

	SensitiveKeychainAccount = "sensitive-key"
	EnvSensitiveKey          = "AI_TOOLS_SENSITIVE_KEY"
)

// SnapshotKey resolves the snapshot encryption key: env override first, then
// the macOS Keychain.
func SnapshotKey() ([]byte, error) {
	if v := os.Getenv(EnvKey); v != "" {
		return decodeKey(v)
	}
	v, err := keychainGet(KeychainAccount)
	if err != nil {
		return nil, fmt.Errorf("no snapshot key found (run 'ai-tools apollo init' or set %s): %w", EnvKey, err)
	}
	return decodeKey(v)
}

// SensitiveKey resolves the sensitive-value encryption key: env override
// first, then the macOS Keychain. Used to encrypt/decrypt sensitive values.
func SensitiveKey() ([]byte, error) {
	if v := os.Getenv(EnvSensitiveKey); v != "" {
		return decodeKey(v)
	}
	v, err := keychainGet(SensitiveKeychainAccount)
	if err != nil {
		return nil, fmt.Errorf("no sensitive key found (run 'ai-tools sensitive init' or set %s): %w", EnvSensitiveKey, err)
	}
	return decodeKey(v)
}

// GenerateAndStoreKey creates a fresh 32-byte snapshot key and stores it in
// the Keychain. Errors if a key already exists.
func GenerateAndStoreKey(force bool) error {
	if !force {
		if _, err := keychainGet(KeychainAccount); err == nil {
			return errors.New("a snapshot key already exists in the Keychain (use --force to regenerate)")
		}
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	return keychainSet(KeychainAccount, base64.StdEncoding.EncodeToString(key))
}

// GenerateAndStoreSensitiveKey creates a fresh 32-byte sensitive key and
// stores it in the Keychain. Errors if a key already exists.
func GenerateAndStoreSensitiveKey(force bool) error {
	if !force {
		if _, err := keychainGet(SensitiveKeychainAccount); err == nil {
			return errors.New("a sensitive key already exists in the Keychain (use --force to regenerate)")
		}
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	return keychainSet(SensitiveKeychainAccount, base64.StdEncoding.EncodeToString(key))
}

func decodeKey(s string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("key must be base64: %w", err)
	}
	if len(b) != 32 {
		return nil, fmt.Errorf("key must decode to 32 bytes, got %d", len(b))
	}
	return b, nil
}

func keychainSet(account, pass string) error {
	return exec.Command("security", "add-generic-password",
		"-U", "-a", account, "-s", KeychainService, "-w", pass,
		"-T", "").Run()
}

func keychainGet(account string) (string, error) {
	out, err := exec.Command("security", "find-generic-password",
		"-a", account, "-s", KeychainService, "-w").Output()
	if err != nil {
		return "", fmt.Errorf("keychain lookup failed: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
