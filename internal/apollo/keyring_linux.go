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
		return "", fmt.Errorf("key %s not found in Secret Service (run the matching init command first, or use the environment variables %s / %s / %s)", account, EnvKey, EnvSensitiveKey, EnvDBKey)
	}
	if err != nil {
		return "", fmt.Errorf("Secret Service unavailable (needs gnome-keyring/kwallet with a desktop session; on headless servers use the environment variables %s / %s / %s): %w", EnvKey, EnvSensitiveKey, EnvDBKey, err)
	}
	return v, nil
}

func keyStoreSetImpl(account, pass string) error {
	if err := keyring.Set(KeychainService, account, pass); err != nil {
		return fmt.Errorf("Secret Service unavailable (needs gnome-keyring/kwallet with a desktop session; on headless servers use the environment variables %s / %s / %s): %w", EnvKey, EnvSensitiveKey, EnvDBKey, err)
	}
	return nil
}
