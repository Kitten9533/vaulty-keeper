package app

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"ai-tools/internal/aesx"
	"ai-tools/internal/apollo"
)

// Import parses Apollo key/value text and writes a new encrypted snapshot.
// It does not check for an existing snapshot; callers decide overwrite policy.
// Returns the number of entries written.
func Import(dir, name, appID, text string, snapKey []byte) (int, error) {
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
		if err := s.Set(snapKey, kv.Key, kv.Value, nil); err != nil {
			return 0, err
		}
	}
	if err := s.Save(apollo.SnapPath(dir, name, appID)); err != nil {
		return 0, err
	}
	return len(kvs), nil
}

// GetValue returns a decrypted value and whether it exists.
func GetValue(dir, name, appID string, snapKey []byte, key string) (string, bool, error) {
	if err := apollo.ValidateKey(key); err != nil {
		return "", false, err
	}
	s, err := load(dir, name, appID)
	if err != nil {
		return "", false, err
	}
	return s.Get(snapKey, key)
}

// SetValue upserts an item and saves. Returns the safe view of the item.
func SetValue(dir, name, appID, key, value string, secret *bool, snapKey []byte) (*apollo.VisibleItem, error) {
	if err := apollo.ValidateKey(key); err != nil {
		return nil, err
	}
	s, err := load(dir, name, appID)
	if err != nil {
		return nil, err
	}
	if err := s.Set(snapKey, key, value, secret); err != nil {
		return nil, err
	}
	if err := s.Save(apollo.SnapPath(dir, name, appID)); err != nil {
		return nil, err
	}
	visible, err := s.VisibleItems(snapKey)
	if err != nil {
		return nil, err
	}
	it, ok := visible[key]
	if !ok {
		return nil, fmt.Errorf("item %q not found after save", key)
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
func Compare(dir, from, fromAppID, to, toAppID string, snapKey []byte) ([]apollo.Change, error) {
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
	return a.Diff(b, snapKey)
}

// Export returns the full plaintext "KEY = value" listing, keys sorted.
func Export(dir, name, appID string, snapKey []byte) (string, error) {
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
		v, err := s.Items[k].DecryptValue(snapKey)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&buf, "%s = %s\n", k, v)
	}
	return buf.String(), nil
}

// EditLoad returns the snapshot as plaintext "KEY = value" lines, keys sorted
// — the same projection as Export, consumed by the bulk-edit flow.
func EditLoad(dir, name, appID string, snapKey []byte) (string, error) {
	return Export(dir, name, appID, snapKey)
}

// EditApply parses plaintext and re-encrypts the whole snapshot. Returns the
// new entry count. Fails without saving if nothing parses.
func EditApply(dir, name, appID string, snapKey []byte, text string) (int, error) {
	s, err := load(dir, name, appID)
	if err != nil {
		return 0, err
	}
	kvs, _ := apollo.ParseKV(text)
	if len(kvs) == 0 {
		return 0, errors.New("no key/value entries after edit; snapshot not changed")
	}
	ns := apollo.NewSnapshot(s.Meta.Name, s.Meta.AppID)
	for _, kv := range kvs {
		if err := ns.Set(snapKey, kv.Key, kv.Value, nil); err != nil {
			return 0, err
		}
	}
	if err := ns.Save(apollo.SnapPath(dir, name, appID)); err != nil {
		return 0, err
	}
	return len(kvs), nil
}

// Reveal decrypts AES-protected targets using the snapshot's AES config
// (imile.fs.aes.secret-key / imile.fs.aes.iv). Overrides take precedence over
// env vars, which take precedence over the snapshot. Fails without returning
// any plaintext if any target fails.
func Reveal(dir, name, appID string, snapKey []byte, targets []string, aesKeyOverride, aesIVOverride string) (map[string]string, error) {
	s, err := load(dir, name, appID)
	if err != nil {
		return nil, err
	}
	aesKey := aesKeyOverride
	if aesKey == "" {
		aesKey = os.Getenv("AI_TOOLS_AES_KEY")
	}
	if aesKey == "" {
		if v, ok, err := s.Get(snapKey, "imile.fs.aes.secret-key"); err != nil {
			return nil, err
		} else if ok {
			aesKey = v
		}
	}
	aesIV := aesIVOverride
	if aesIV == "" {
		aesIV = os.Getenv("AI_TOOLS_AES_IV")
	}
	if aesIV == "" {
		if v, ok, err := s.Get(snapKey, "imile.fs.aes.iv"); err != nil {
			return nil, err
		} else if ok {
			aesIV = v
		}
	}
	if aesKey == "" || aesIV == "" {
		return nil, errors.New("AES key/iv not found (pass --key/--iv, set AI_TOOLS_AES_KEY/IV, or keep imile.fs.aes.secret-key / imile.fs.aes.iv in the snapshot)")
	}
	plain := map[string]string{}
	for _, k := range targets {
		if err := apollo.ValidateKey(k); err != nil {
			return nil, err
		}
		cipher, ok, err := s.Get(snapKey, k)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("key %q not found in snapshot %q", k, name)
		}
		p, err := aesx.Decrypt(aesKey, aesIV, cipher)
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
	return apollo.Load(apollo.SnapPath(dir, name, appID))
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
