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

// Snapshot key management: macOS Keychain with env fallback. The key never
// lives in a plaintext file.

const (
	KeychainService = "ai-tools"
	KeychainAccount = "apollo-snapshot-key"
	EnvKey          = "AI_TOOLS_APOLLO_KEY"
)

// SnapshotKey resolves the snapshot encryption key: env override first, then
// the macOS Keychain.
func SnapshotKey() ([]byte, error) {
	if v := os.Getenv(EnvKey); v != "" {
		return decodeKey(v)
	}
	v, err := keychainGet()
	if err != nil {
		return nil, fmt.Errorf("no snapshot key found (run 'ai-tools apollo init' or set %s): %w", EnvKey, err)
	}
	return decodeKey(v)
}

// GenerateAndStoreKey creates a fresh 32-byte key and stores it in the
// Keychain. Errors if a key already exists.
func GenerateAndStoreKey(force bool) error {
	if !force {
		if _, err := keychainGet(); err == nil {
			return errors.New("a snapshot key already exists in the Keychain (use --force to regenerate)")
		}
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	return keychainSet(base64.StdEncoding.EncodeToString(key))
}

func decodeKey(s string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("snapshot key must be base64: %w", err)
	}
	if len(b) != 32 {
		return nil, fmt.Errorf("snapshot key must decode to 32 bytes, got %d", len(b))
	}
	return b, nil
}

func keychainSet(pass string) error {
	return exec.Command("security", "add-generic-password",
		"-U", "-a", KeychainAccount, "-s", KeychainService, "-w", pass,
		"-T", "").Run()
}

func keychainGet() (string, error) {
	out, err := exec.Command("security", "find-generic-password",
		"-a", KeychainAccount, "-s", KeychainService, "-w").Output()
	if err != nil {
		return "", fmt.Errorf("keychain lookup failed: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
