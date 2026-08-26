// Package app is the single domain-logic layer shared by the CLI and the Web
// UI. It never formats output; callers own presentation.
package app

import "ai-tools/internal/apollo"

// InitKey generates and stores a fresh snapshot key. force=true regenerates
// even when a key already exists.
func InitKey(force bool) error {
	return apollo.GenerateAndStoreKey(force)
}

// KeyAvailable reports whether a snapshot key can be resolved right now.
func KeyAvailable() bool {
	_, err := apollo.SnapshotKey()
	return err == nil
}

// SnapshotKey resolves the snapshot encryption key (env or Keychain).
func SnapshotKey() ([]byte, error) {
	return apollo.SnapshotKey()
}
