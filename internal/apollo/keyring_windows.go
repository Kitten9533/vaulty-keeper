//go:build windows

package apollo

import (
	"fmt"

	"github.com/danieljoos/wincred"
)

// StoreName returns the human-readable name of the platform secret store.
func StoreName() string { return "Windows Credential Manager" }

// targetName maps an vaulty-keeper account to a Windows Credential Manager target.
func targetName(account string) string { return KeychainService + ":" + account }

// keyStoreGet reads a secret from Windows Credential Manager.
func keyStoreGetImpl(account string) (string, error) {
	cred, err := wincred.GetGenericCredential(targetName(account))
	if err != nil {
		return "", fmt.Errorf("credential lookup failed: %w", err)
	}
	return string(cred.CredentialBlob), nil
}

func keyStoreSetImpl(account, pass string) error {
	cred := wincred.NewGenericCredential(targetName(account))
	cred.UserName = "vaulty-keeper"
	cred.CredentialBlob = []byte(pass)
	if err := cred.Write(); err != nil {
		return fmt.Errorf("credential write failed: %w", err)
	}
	return nil
}
