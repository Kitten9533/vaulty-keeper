package apollo

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Key management: a platform secret store (macOS Keychain, Windows
// Credential Manager) with env fallback. Keys never live in a plaintext file.
// Two independent keys are used:
//   - the snapshot key (KeychainAccount) encrypts every non-sensitive value;
//   - the sensitive key (SensitiveKeychainAccount) encrypts sensitive values,
//     so revealing them additionally requires the sensitive key.
//
// Env overrides exist so headless machines without a keyring can still work,
// but they are considered red-line secrets: never export them into an agent
// session or pass them on the command line.
//
// The platform store is provided by keyStoreGet/keyStoreSet (see
// keyring_darwin.go, keyring_windows.go, keyring_linux.go).

const (
	KeychainService = "vaulty-keeper"
	KeychainAccount = "apollo-snapshot-key"
	EnvKey          = "VAULTY_KEEPER_APOLLO_KEY"

	SensitiveKeychainAccount = "sensitive-key"
	EnvSensitiveKey          = "VAULTY_KEEPER_SENSITIVE_KEY"

	// DBKeychainAccount is the key encrypting database connection URLs in
	// ~/.vaulty/db.json (internal/dbproxy). It is independent from the
	// snapshot and sensitive keys so a compromised snapshot key cannot decrypt
	// connection strings.
	DBKeychainAccount = "db-key"
	EnvDBKey          = "VAULTY_KEEPER_DB_KEY"
)

// keyStoreGet/keyStoreSet are overridable so tests never touch the real
// platform secret store; production uses the platform implementations
// (keyStoreGetImpl/keyStoreSetImpl in the build-tagged keyring_*.go files).
var (
	keyStoreGet = keyStoreGetImpl
	keyStoreSet = keyStoreSetImpl
)

// SnapshotKey resolves the snapshot encryption key: env override first, then
// the platform secret store.
func SnapshotKey() ([]byte, error) {
	if v := os.Getenv(EnvKey); v != "" {
		return decodeKey(v)
	}
	v, err := keyStoreGet(KeychainAccount)
	if err != nil {
		return nil, fmt.Errorf("未找到快照密钥（运行 'vaulty-keeper apollo init' 或设置 %s）：%w", EnvKey, err)
	}
	return decodeKey(v)
}

// SensitiveKey resolves the sensitive-value encryption key: env override
// first, then the platform secret store. Used to encrypt/decrypt sensitive
// values.
func SensitiveKey() ([]byte, error) {
	if v := os.Getenv(EnvSensitiveKey); v != "" {
		return decodeKey(v)
	}
	v, err := keyStoreGet(SensitiveKeychainAccount)
	if err != nil {
		return nil, fmt.Errorf("未找到敏感值密钥（运行 'vaulty-keeper sensitive init' 或设置 %s）：%w", EnvSensitiveKey, err)
	}
	return decodeKey(v)
}

// GenerateAndStoreKey creates a fresh 32-byte snapshot key and stores it in
// the platform secret store. Errors if a key already exists.
func GenerateAndStoreKey(force bool) error {
	if !force {
		if _, err := keyStoreGet(KeychainAccount); err == nil {
			return errors.New("系统密钥库（" + StoreName() + "）中已存在快照密钥，用 --force 重新生成")
		}
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	return keyStoreSet(KeychainAccount, base64.StdEncoding.EncodeToString(key))
}

// GenerateAndStoreSensitiveKey creates a fresh 32-byte sensitive key and
// stores it in the platform secret store. Errors if a key already exists.
func GenerateAndStoreSensitiveKey(force bool) error {
	if !force {
		if _, err := keyStoreGet(SensitiveKeychainAccount); err == nil {
			return errors.New("系统密钥库（" + StoreName() + "）中已存在敏感值密钥，用 --force 重新生成")
		}
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	return keyStoreSet(SensitiveKeychainAccount, base64.StdEncoding.EncodeToString(key))
}

// DBKey resolves the database-connection encryption key: env override first,
// then the platform secret store.
func DBKey() ([]byte, error) {
	if v := os.Getenv(EnvDBKey); v != "" {
		return decodeKey(v)
	}
	v, err := keyStoreGet(DBKeychainAccount)
	if err != nil {
		return nil, fmt.Errorf("未找到数据库密钥（运行 'vaulty-keeper db init' 或设置 %s）：%w", EnvDBKey, err)
	}
	return decodeKey(v)
}

// GenerateAndStoreDBKey creates a fresh 32-byte database key and stores it in
// the platform secret store. Errors if a key already exists.
func GenerateAndStoreDBKey(force bool) error {
	if !force {
		if _, err := keyStoreGet(DBKeychainAccount); err == nil {
			return errors.New("系统密钥库（" + StoreName() + "）中已存在数据库密钥，用 --force 重新生成")
		}
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	return keyStoreSet(DBKeychainAccount, base64.StdEncoding.EncodeToString(key))
}

func decodeKey(s string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("密钥必须是 base64 编码：%w", err)
	}
	if len(b) != 32 {
		return nil, fmt.Errorf("密钥必须解码为 32 字节，实际为 %d 字节", len(b))
	}
	return b, nil
}
