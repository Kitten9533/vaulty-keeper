package app

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"vaulty-keeper/internal/aesx"
	"vaulty-keeper/internal/apollo"
)

// Import parses Apollo key/value text and writes a new encrypted snapshot.
// Sensitive values are encrypted with sensitiveKey, others with snapKey.
// It does not check for an existing snapshot; callers decide overwrite policy.
// Returns the number of entries written.
func Import(dir, name, appID, text string, snapKey, sensitiveKey []byte) (int, error) {
	if err := apollo.ValidateSnapshotName(name); err != nil {
		return 0, err
	}
	if err := apollo.ValidateAppID(appID); err != nil {
		return 0, err
	}
	kvs, _ := apollo.ParseKV(text)
	if len(kvs) == 0 {
		return 0, errors.New("no key/value entries parsed")
	}
	s := apollo.NewSnapshot(name, appID)
	for _, kv := range kvs {
		if err := s.Set(snapKey, sensitiveKey, kv.Key, kv.Value, nil); err != nil {
			return 0, err
		}
	}
	if err := s.Save(apollo.SnapPath(dir, name, appID)); err != nil {
		return 0, err
	}
	return len(kvs), nil
}

// GetValue returns a decrypted value and whether it exists.
func GetValue(dir, name, appID string, snapKey, sensitiveKey []byte, key string) (string, bool, error) {
	if err := apollo.ValidateKey(key); err != nil {
		return "", false, err
	}
	s, err := load(dir, name, appID)
	if err != nil {
		return "", false, err
	}
	return s.Get(snapKey, sensitiveKey, key)
}

// GetValueSafe returns a decrypted value, whether the item is explicitly
// marked safe for scripts/AI (set --plain), and whether it exists.
func GetValueSafe(dir, name, appID string, snapKey, sensitiveKey []byte, key string) (string, bool, bool, error) {
	if err := apollo.ValidateKey(key); err != nil {
		return "", false, false, err
	}
	s, err := load(dir, name, appID)
	if err != nil {
		return "", false, false, err
	}
	it, ok := s.Items[key]
	if !ok {
		return "", false, false, nil
	}
	v, err := s.DecryptItem(it, snapKey, sensitiveKey)
	if err != nil {
		return "", false, true, err
	}
	return v, it.Safe, true, nil
}

// MarkValue flips the explicit safe/secret flag on an existing item without
// changing its value, re-encrypting with the appropriate key. Returns whether
// the key exists.
func MarkValue(dir, name, appID, key string, safe bool, snapKey, sensitiveKey []byte) (bool, error) {
	if err := apollo.ValidateKey(key); err != nil {
		return false, err
	}
	s, err := load(dir, name, appID)
	if err != nil {
		return false, err
	}
	it, ok := s.Items[key]
	if !ok {
		return false, nil
	}
	v, err := s.DecryptItem(it, snapKey, sensitiveKey)
	if err != nil {
		return false, err
	}
	secret := !safe
	if err := s.Set(snapKey, sensitiveKey, key, v, &secret); err != nil {
		return false, err
	}
	if err := s.Save(apollo.SnapPath(dir, name, appID)); err != nil {
		return false, err
	}
	return true, nil
}

// SetValue upserts an item and saves. Returns the safe view of the item.
func SetValue(dir, name, appID, key, value string, secret *bool, snapKey, sensitiveKey []byte) (*apollo.VisibleItem, error) {
	if err := apollo.ValidateKey(key); err != nil {
		return nil, err
	}
	s, err := load(dir, name, appID)
	if err != nil {
		return nil, err
	}
	if err := s.Set(snapKey, sensitiveKey, key, value, secret); err != nil {
		return nil, err
	}
	if err := s.Save(apollo.SnapPath(dir, name, appID)); err != nil {
		return nil, err
	}
	visible, err := s.VisibleItems(snapKey, sensitiveKey)
	if err != nil {
		return nil, err
	}
	it, ok := visible[key]
	if !ok {
		return nil, fmt.Errorf("item %q not found after saving", key)
	}
	return &it, nil
}

// DeleteValue removes an item. Returns whether it existed.
func DeleteValue(dir, name, appID, key string, snapKey []byte) (bool, error) {
	if err := apollo.ValidateKey(key); err != nil {
		return false, err
	}
	s, err := load(dir, name, appID)
	if err != nil {
		return false, err
	}
	ok := s.Delete(key)
	if !ok {
		return false, nil
	}
	if err := s.Save(apollo.SnapPath(dir, name, appID)); err != nil {
		return false, err
	}
	return true, nil
}

// Compare returns the added/removed/changed diff between two snapshots.
func Compare(dir, from, fromAppID, to, toAppID string, snapKey, sensitiveKey []byte) ([]apollo.Change, error) {
	if err := apollo.ValidateSnapshotName(from); err != nil {
		return nil, err
	}
	if err := apollo.ValidateSnapshotName(to); err != nil {
		return nil, err
	}
	a, err := load(dir, from, fromAppID)
	if err != nil {
		return nil, err
	}
	b, err := load(dir, to, toAppID)
	if err != nil {
		return nil, err
	}
	return a.Diff(b, snapKey, sensitiveKey)
}

// Export returns the full plaintext "KEY = value" listing, keys sorted.
func Export(dir, name, appID string, snapKey, sensitiveKey []byte) (string, error) {
	s, err := load(dir, name, appID)
	if err != nil {
		return "", err
	}
	keys := make([]string, 0, len(s.Items))
	for k := range s.Items {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf strings.Builder
	for _, k := range keys {
		v, err := s.DecryptItem(s.Items[k], snapKey, sensitiveKey)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&buf, "%s = %s\n", k, v)
	}
	return buf.String(), nil
}

// EditLoad returns the snapshot as plaintext "KEY = value" lines, keys sorted
// — the same projection as Export, consumed by the bulk-edit flow.
func EditLoad(dir, name, appID string, snapKey, sensitiveKey []byte) (string, error) {
	return Export(dir, name, appID, snapKey, sensitiveKey)
}

// EditApply parses plaintext and re-encrypts the whole snapshot. Returns the
// new entry count. Fails without saving if nothing parses.
func EditApply(dir, name, appID string, snapKey, sensitiveKey []byte, text string) (int, error) {
	s, err := load(dir, name, appID)
	if err != nil {
		return 0, err
	}
	kvs, _ := apollo.ParseKV(text)
	if len(kvs) == 0 {
		return 0, errors.New("no key/value entries parsed after editing; snapshot unchanged")
	}
	ns := apollo.NewSnapshot(s.Meta.Name, s.Meta.AppID)
	for _, kv := range kvs {
		if err := ns.Set(snapKey, sensitiveKey, kv.Key, kv.Value, nil); err != nil {
			return 0, err
		}
	}
	if err := ns.Save(apollo.SnapPath(dir, name, appID)); err != nil {
		return 0, err
	}
	return len(kvs), nil
}

// Reveal returns plaintext for the requested keys. Without key/iv overrides it
// decrypts each target with its own key (the sensitive key for sensitive
// values, the snapshot key otherwise) — the "show" path for masked values.
// With explicit key/iv overrides it decrypts the raw stored value as a Java
// CryptoUtil AES ciphertext (e.g. OSS AK/SK pulled from Apollo as ciphertext).
// Fails without returning any plaintext if any target fails.
func Reveal(dir, name, appID string, snapKey, sensitiveKey []byte, targets []string, aesKeyOverride, aesIVOverride string) (map[string]string, error) {
	s, err := load(dir, name, appID)
	if err != nil {
		return nil, err
	}
	plain := map[string]string{}
	for _, k := range targets {
		if err := apollo.ValidateKey(k); err != nil {
			return nil, err
		}
		it, ok := s.Items[k]
		if !ok {
			return nil, fmt.Errorf("key %q not found in snapshot %q", k, name)
		}
		if aesKeyOverride != "" && aesIVOverride != "" {
			readKey := snapKey
			if it.Secret {
				readKey = sensitiveKey
			}
			cipher, err := it.DecryptValue(readKey)
			if err != nil {
				return nil, fmt.Errorf("reading %q: %w", k, err)
			}
			p, err := aesx.Decrypt(aesKeyOverride, aesIVOverride, cipher)
			if err != nil {
				return nil, fmt.Errorf("decrypt %q: %w", k, err)
			}
			plain[k] = p
			continue
		}
		p, err := s.DecryptItem(it, snapKey, sensitiveKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt %q: %w", k, err)
		}
		plain[k] = p
	}
	return plain, nil
}

func load(dir, name, appID string) (*apollo.Snapshot, error) {
	if err := apollo.ValidateSnapshotName(name); err != nil {
		return nil, err
	}
	path := apollo.SnapPath(dir, name, appID)
	s, err := apollo.Load(path)
	if err != nil {
		if os.IsNotExist(err) {
			msg := fmt.Sprintf("snapshot %q not found", name)
			if appID != "" {
				msg = fmt.Sprintf("snapshot %q (appid %s) not found", name, appID)
			}
			if hints := snapshotHints(dir, name, appID); len(hints) > 0 {
				msg += "; similar snapshots: " + strings.Join(hints, ", ")
			}
			return nil, errors.New(msg)
		}
		return nil, err
	}
	return s, nil
}

// snapshotHints lists other snapshots sharing the same environment name, so a
// typo'd or missing app id is surfaced instead of a bare "not found" error.
func snapshotHints(dir, name, appID string) []string {
	refs, err := apollo.ListSnapshots(dir)
	if err != nil {
		return nil
	}
	var hints []string
	for _, r := range refs {
		if r.Name != name || r.AppID == appID {
			continue
		}
		if r.AppID != "" {
			hints = append(hints, fmt.Sprintf("%s (appid %s)", r.Name, r.AppID))
		} else {
			hints = append(hints, r.Name)
		}
	}
	return hints
}

// Remove deletes a snapshot file. Returns whether it existed.
func Remove(dir, name, appID string) (bool, error) {
	if err := apollo.ValidateSnapshotName(name); err != nil {
		return false, err
	}
	path := apollo.SnapPath(dir, name, appID)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if err := os.Remove(path); err != nil {
		return false, err
	}
	return true, nil
}
