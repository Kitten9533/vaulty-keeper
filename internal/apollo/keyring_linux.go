//go:build linux

package apollo

import "errors"

// StoreName returns the human-readable name of the platform secret store.
func StoreName() string { return "Linux keyring" }

// Linux has no single default keyring wired up; headless users are expected
// to use the environment variable overrides (VAULTY_KEEPER_APOLLO_KEY /
// VAULTY_KEEPER_SENSITIVE_KEY). A Secret Service backend can be added later.
func keyStoreGetImpl(account string) (string, error) {
	return "", errors.New("linux 上无密钥库后端；请改用环境变量 " + EnvKey + " / " + EnvSensitiveKey)
}

func keyStoreSetImpl(account, pass string) error {
	return errors.New("linux 上无密钥库后端；请改用环境变量 " + EnvKey + " / " + EnvSensitiveKey)
}
