package app

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"

	"ai-tools/internal/aesx"
)

const keyCharset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// AESEntry is one named AES key/iv pair in the config list.
type AESEntry struct {
	Name      string `json:"name"`
	SecretKey string `json:"secret-key"`
	IV        string `json:"iv"`
}

// AESConfig is the persisted list of named AES key/iv pairs
// (~/.ai-tools/aes.json), kept as a JSON array of AESEntry.
type AESConfig struct {
	Entries []AESEntry `json:"entries"`
	Path    string     `json:"path"`
}

// AESConfigPath returns the file holding the AES key/iv list.
func AESConfigPath() string {
	if p := os.Getenv("AI_TOOLS_AES_CONFIG"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".ai-tools", "aes.json")
	}
	return filepath.Join(home, ".ai-tools", "aes.json")
}

// AESConfigList reads the named key/iv list. A missing file is not an error;
// it returns an empty list. The legacy single-object format
// {"key":..., "iv":...} is transparently migrated to one "default" entry.
func AESConfigList() ([]AESEntry, error) {
	b, err := os.ReadFile(AESConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	trimmed := trimSpace(b)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var old struct {
			Key string `json:"key"`
			IV  string `json:"iv"`
		}
		if err := json.Unmarshal(b, &old); err != nil {
			return nil, err
		}
		if old.Key == "" && old.IV == "" {
			return nil, nil
		}
		return []AESEntry{{Name: "default", SecretKey: old.Key, IV: old.IV}}, nil
	}
	var entries []AESEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// AESConfigSave writes the whole key/iv list with 0600 permissions.
func AESConfigSave(entries []AESEntry) error {
	if entries == nil {
		entries = []AESEntry{}
	}
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	p := AESConfigPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, append(b, '\n'), 0o600)
}

// AESConfigAdd appends a named entry. The name must be unique.
func AESConfigAdd(name, key, iv string) error {
	if name == "" {
		return errors.New("name is required")
	}
	if key == "" || iv == "" {
		return errors.New("key and iv are required")
	}
	entries, err := AESConfigList()
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name == name {
			return fmt.Errorf("entry %q already exists", name)
		}
	}
	entries = append(entries, AESEntry{Name: name, SecretKey: key, IV: iv})
	return AESConfigSave(entries)
}

// AESConfigRemove deletes the named entry. A missing name is not an error.
func AESConfigRemove(name string) error {
	entries, err := AESConfigList()
	if err != nil {
		return err
	}
	kept := entries[:0]
	for _, e := range entries {
		if e.Name != name {
			kept = append(kept, e)
		}
	}
	return AESConfigSave(kept)
}

// AESConfigGet returns the named entry, or nil.
func AESConfigGet(name string) (*AESEntry, error) {
	entries, err := AESConfigList()
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if entries[i].Name == name {
			return &entries[i], nil
		}
	}
	return nil, nil
}

func trimSpace(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && (b[start] == ' ' || b[start] == '\t' || b[start] == '\n' || b[start] == '\r') {
		start++
	}
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\n' || b[end-1] == '\r') {
		end--
	}
	return b[start:end]
}

// GenKey returns a fresh printable AES key/iv pair.
func GenKey(keyBytes, ivBytes int) (key, iv string, err error) {
	if keyBytes != 16 && keyBytes != 24 && keyBytes != 32 {
		return "", "", errors.New("key must be 16/24/32 bytes")
	}
	if ivBytes != 12 && ivBytes != 16 {
		return "", "", errors.New("iv must be 12 or 16 bytes")
	}
	key, err = randString(keyBytes)
	if err != nil {
		return "", "", err
	}
	iv, err = randString(ivBytes)
	if err != nil {
		return "", "", err
	}
	return key, iv, nil
}

func randString(n int) (string, error) {
	b := make([]byte, n)
	for i := range b {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(keyCharset))))
		if err != nil {
			return "", err
		}
		b[i] = keyCharset[idx.Int64()]
	}
	return string(b), nil
}

// Encrypt wraps aesx.Encrypt (Java CryptoUtil compatible).
func Encrypt(key, iv, plaintext string) (string, error) {
	return aesx.Encrypt(key, iv, plaintext)
}

// Decrypt wraps aesx.Decrypt (Java CryptoUtil compatible).
func Decrypt(key, iv, ciphertext string) (string, error) {
	return aesx.Decrypt(key, iv, ciphertext)
}
