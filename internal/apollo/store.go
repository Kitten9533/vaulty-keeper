package apollo

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// An encrypted snapshot: values are stored as AES-256-GCM ciphertext with a
// random per-value nonce, keyed by the snapshot key (Keychain/env). The file
// never contains plaintext values.

var snapshotNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

type Meta struct {
	Name       string `json:"name"`
	AppID      string `json:"app_id,omitempty"`
	CapturedAt string `json:"captured_at"`
}

type Item struct {
	Enc    string `json:"enc"`
	Nonce  string `json:"nonce"`
	Secret bool   `json:"secret"`
}

type Snapshot struct {
	Meta  Meta            `json:"meta"`
	Items map[string]Item `json:"items"`
}

func NewSnapshot(name, appID string) *Snapshot {
	return &Snapshot{
		Meta:  Meta{Name: name, AppID: appID, CapturedAt: time.Now().UTC().Format(time.RFC3339)},
		Items: map[string]Item{},
	}
}

func ValidateSnapshotName(name string) error {
	if !snapshotNameRe.MatchString(name) {
		return errors.New("snapshot name must start with a letter or number and contain only letters, numbers, dot, dash, or underscore")
	}
	return nil
}

// appIDRe allows the same character set as snapshot names (empty is allowed
// for reading legacy {env}.json snapshots; writing requires a non-empty ID).
var appIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// ValidateAppID validates an App ID used to locate a snapshot file. An empty
// ID is allowed only for reading legacy snapshots; callers that create
// snapshots must reject it themselves.
func ValidateAppID(appID string) error {
	if appID == "" {
		return errors.New("app id must not be empty")
	}
	if !appIDRe.MatchString(appID) {
		return errors.New("app id must start with a letter or number and contain only letters, numbers, dot, dash, or underscore")
	}
	return nil
}

// SnapshotRef identifies one snapshot by its environment name and App ID.
type SnapshotRef struct {
	Name  string `json:"name"`
	AppID string `json:"app_id"`
}

// FileName returns the on-disk file name for a snapshot. Snapshots created
// with an App ID are stored as {env}__{appid}.json; legacy snapshots without
// one use {env}.json.
func FileName(name, appID string) string {
	if appID == "" {
		return name + ".json"
	}
	return name + "__" + appID + ".json"
}

// SnapPath joins a directory with FileName.
func SnapPath(dir, name, appID string) string {
	return filepath.Join(dir, FileName(name, appID))
}

type VisibleItem struct {
	Key       string  `json:"key"`
	Sensitive bool    `json:"sensitive"`
	Value     *string `json:"value"`
	Length    int     `json:"length,omitempty"`
}

// VisibleItems decrypts every value (for length) but only exposes plaintext
// for non-sensitive items; sensitive values are masked to their length.
func (s *Snapshot) VisibleItems(key []byte) (map[string]VisibleItem, error) {
	items := make(map[string]VisibleItem, len(s.Items))
	for name, item := range s.Items {
		value, err := item.DecryptValue(key)
		if err != nil {
			return nil, err
		}
		view := VisibleItem{Key: name, Sensitive: item.Secret}
		if item.Secret {
			view.Length = len(value)
		} else {
			view.Value = &value
		}
		items[name] = view
	}
	return items, nil
}

func (s *Snapshot) Set(key []byte, k, v string, secret *bool) error {
	secretVal := IsSensitive(k)
	if secret != nil {
		secretVal = *secret
	}
	it, err := encryptValue(key, v)
	if err != nil {
		return err
	}
	it.Secret = secretVal
	s.Items[k] = it
	return nil
}

func (s *Snapshot) Delete(k string) bool {
	_, ok := s.Items[k]
	delete(s.Items, k)
	return ok
}

func (s *Snapshot) Get(key []byte, k string) (string, bool, error) {
	it, ok := s.Items[k]
	if !ok {
		return "", false, nil
	}
	v, err := it.DecryptValue(key)
	if err != nil {
		return "", true, err
	}
	return v, true, nil
}

type Change struct {
	Key    string
	Kind   string // added | removed | changed
	Old    string
	New    string
	Secret bool
}

// Diff compares two snapshots (both encrypted with key). Missing values are
// empty strings. It returns an error if any value fails to decrypt, so a
// compare never silently glosses over corrupted data.
func (a *Snapshot) Diff(b *Snapshot, key []byte) ([]Change, error) {
	keys := map[string]bool{}
	for k := range a.Items {
		keys[k] = true
	}
	for k := range b.Items {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	plain := func(it Item) (string, error) {
		v, err := it.DecryptValue(key)
		if err != nil {
			return "", err
		}
		return v, nil
	}

	var out []Change
	for _, k := range sorted {
		ai, aok := a.Items[k]
		bi, bok := b.Items[k]
		switch {
		case !aok && bok:
			v, err := plain(bi)
			if err != nil {
				return nil, fmt.Errorf("decrypt %q: %w", k, err)
			}
			out = append(out, Change{Key: k, Kind: "added", New: v, Secret: bi.Secret})
		case aok && !bok:
			v, err := plain(ai)
			if err != nil {
				return nil, fmt.Errorf("decrypt %q: %w", k, err)
			}
			out = append(out, Change{Key: k, Kind: "removed", Old: v, Secret: ai.Secret})
		default:
			av, err := plain(ai)
			if err != nil {
				return nil, fmt.Errorf("decrypt %q: %w", k, err)
			}
			bv, err := plain(bi)
			if err != nil {
				return nil, fmt.Errorf("decrypt %q: %w", k, err)
			}
			if NormValue(av) != NormValue(bv) {
				out = append(out, Change{Key: k, Kind: "changed", Old: av, New: bv, Secret: ai.Secret || bi.Secret})
			}
		}
	}
	return out, nil
}

func (it Item) DecryptValue(key []byte) (string, error) {
	if key == nil {
		return "", errors.New("snapshot key not available")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce, err := base64.StdEncoding.DecodeString(it.Nonce)
	if err != nil {
		return "", err
	}
	ct, err := base64.StdEncoding.DecodeString(it.Enc)
	if err != nil {
		return "", err
	}
	pt, err := g.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt %q failed: %w", "value", err)
	}
	return string(pt), nil
}

func encryptValue(key []byte, value string) (Item, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return Item{}, err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return Item{}, err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return Item{}, err
	}
	ct := g.Seal(nil, nonce, []byte(value), nil)
	return Item{Enc: base64.StdEncoding.EncodeToString(ct), Nonce: base64.StdEncoding.EncodeToString(nonce)}, nil
}

func Load(path string) (*Snapshot, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	if s.Items == nil {
		s.Items = map[string]Item{}
	}
	return &s, nil
}

func (s *Snapshot) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

// ListSnapshots returns snapshot references in a directory, each carrying its
// environment name and App ID (read from each snapshot's meta).
func ListSnapshots(dir string) ([]SnapshotRef, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var refs []SnapshotRef
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		s, err := Load(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		refs = append(refs, SnapshotRef{Name: s.Meta.Name, AppID: s.Meta.AppID})
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Name != refs[j].Name {
			return refs[i].Name < refs[j].Name
		}
		return refs[i].AppID < refs[j].AppID
	})
	return refs, nil
}
