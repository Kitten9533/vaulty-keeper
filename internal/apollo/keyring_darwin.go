//go:build darwin

package apollo

import (
	"fmt"
	"os/exec"
	"strings"
)

// StoreName returns the human-readable name of the platform secret store.
func StoreName() string { return "macOS Keychain" }

// keyStoreGet reads a secret from the macOS Keychain. Items were created with
// an empty trust list (-T ""), so access normally prompts the user; a
// non-interactive process therefore cannot silently read the keys.
func keyStoreGetImpl(account string) (string, error) {
	out, err := exec.Command("security", "find-generic-password",
		"-a", account, "-s", KeychainService, "-w").Output()
	if err != nil {
		return "", fmt.Errorf("keychain lookup failed: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func keyStoreSetImpl(account, pass string) error {
	return exec.Command("security", "add-generic-password",
		"-U", "-a", account, "-s", KeychainService, "-w", pass,
		"-T", "").Run()
}
