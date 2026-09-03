//go:build linux

package apollo

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// StoreName returns the human-readable name of the platform secret store.
func StoreName() string { return "Secret Service（gnome-keyring / kwallet）" }

// Linux backend: Secret Service over D-Bus (pure Go, no cgo). Desktop
// sessions with gnome-keyring/kwallet work out of the box; headless machines
// without a secret service must fall back to the environment variable
// overrides (VAULTY_KEEPER_APOLLO_KEY / VAULTY_KEEPER_SENSITIVE_KEY /
// VAULTY_KEEPER_DB_KEY) — callers already check env first.
func keyStoreGetImpl(account string) (string, error) {
	v, err := keyring.Get(KeychainService, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", fmt.Errorf("密钥 %s 不存在于 Secret Service（先运行对应 init 命令，或改用环境变量 %s / %s / %s）", account, EnvKey, EnvSensitiveKey, EnvDBKey)
	}
	if err != nil {
		return "", fmt.Errorf("Secret Service 不可用（需要 gnome-keyring/kwallet 与桌面会话；无头服务器请改用环境变量 %s / %s / %s）：%w", EnvKey, EnvSensitiveKey, EnvDBKey, err)
	}
	return v, nil
}

func keyStoreSetImpl(account, pass string) error {
	if err := keyring.Set(KeychainService, account, pass); err != nil {
		return fmt.Errorf("Secret Service 不可用（需要 gnome-keyring/kwallet 与桌面会话；无头服务器请改用环境变量 %s / %s / %s）：%w", EnvKey, EnvSensitiveKey, EnvDBKey, err)
	}
	return nil
}