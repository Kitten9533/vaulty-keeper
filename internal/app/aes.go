package app

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"

	"ai-tools/internal/aesx"
)

const keyCharset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// AESConfig is the persisted custom AES key/iv pair (~/.ai-tools/aes.json).
type AESConfig struct {
	Key string `json:"key"`
	IV  string `json:"iv"`
}

// AESConfigPath returns the file holding the custom AES key/iv.
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

// AESConfigLoad reads the custom key/iv file. A missing file is not an error;
// it returns an empty config.
func AESConfigLoad() (AESConfig, error) {
	b, err := os.ReadFile(AESConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return AESConfig{}, nil
		}
		return AESConfig{}, err
	}
	var c AESConfig
	if err := json.Unmarshal(b, &c); err != nil {
		return AESConfig{}, err
	}
	return c, nil
}

// AESConfigSave writes the custom key/iv file with 0600 permissions.
func AESConfigSave(key, iv string) error {
	b, err := json.MarshalIndent(AESConfig{Key: key, IV: iv}, "", "  ")
	if err != nil {
		return err
	}
	p := AESConfigPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, append(b, '\n'), 0o600)
}

// AESConfigClear removes the custom key/iv file. A missing file is not an
// error.
func AESConfigClear() error {
	if err := os.Remove(AESConfigPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
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
