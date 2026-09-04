//go:build !darwin && !windows && !linux

package apollo

import "errors"

// StoreName returns the human-readable name of the platform secret store.
func StoreName() string { return "system keyring" }

// This platform has no keyring backend; the environment variable overrides
// (VAULTY_KEEPER_APOLLO_KEY / VAULTY_KEEPER_SENSITIVE_KEY) are the only option.
func keyStoreGetImpl(account string) (string, error) {
	return "", errors.New("no keychain backend on this platform; use the environment variables " + EnvKey + " / " + EnvSensitiveKey + " instead")
}

func keyStoreSetImpl(account, pass string) error {
	return errors.New("no keychain backend on this platform; use the environment variables " + EnvKey + " / " + EnvSensitiveKey + " instead")
}
