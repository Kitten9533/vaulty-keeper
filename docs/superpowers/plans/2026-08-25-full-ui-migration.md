# Full UI Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate all vaulty-keeper functionality (apollo + aes + key management) into the loopback-only Web UI, with a shared `internal/app` domain layer that both the UI and the preserved CLI call, keeping every CLI command, flag, and output format byte-identical.

**Architecture:** Create `internal/app` as the single domain-logic layer (snapshot ops, aes ops, aes.json config, key init). Refactor `internal/cli` into a thin adapter that keeps its exact output contract, and extend `internal/ui`'s handler + embedded frontend with new APIs (init, reveal, edit, aes) and two new rail sections (AES tools, settings). Plaintext exits (reveal, edit-load, export) require explicit `confirm: true` and are never persisted by the browser.

**Tech Stack:** Go standard library (`net/http`, `embed`, `httptest`), existing `internal/apollo` + `internal/aesx`, vanilla HTML/CSS/JS.

**Spec:** `docs/superpowers/specs/2026-08-25-full-ui-migration-design.md`

---

## File Structure

- Create: `internal/app/key.go` — snapshot key init/availability wrappers.
- Create: `internal/app/key_test.go`
- Create: `internal/app/aes.go` — aes encrypt/decrypt, gen-key, `aes.json` config.
- Create: `internal/app/aes_test.go`
- Create: `internal/app/snapshot.go` — import/get/set/delete/compare/export/reveal/edit ops.
- Create: `internal/app/snapshot_test.go`
- Modify: `internal/cli/cli.go` — aes + apollo commands call `internal/app`; output unchanged.
- Modify: `internal/cli/interactive.go` — aes.json logic moved to `internal/app`.
- Modify: `internal/cli/cli_test.go` — only if a test needs to align with the refactor (should be none).
- Modify: `internal/ui/ui.go` — new routes: init, reveal, edit, aes.
- Modify: `internal/ui/ui_test.go` — new handler tests.
- Modify: `internal/ui/static/index.html` — rail nav sections, AES tools view, settings view, reveal/edit dialogs.
- Modify: `internal/ui/static/app.css` — styles for new sections/views.
- Modify: `internal/ui/static/app.js` — view switching, AES tools, settings, reveal, bulk edit.
- Modify: `README.md` — document full coverage.

## Completion Notes

- Do not create commits: the repository has no initial commit and the user did not request one.
- `internal/apollo` and `internal/aesx` are NOT modified by this plan (they already hold the encryption primitives).
- Keychain-backed `InitKey` is not unit-tested (writing to the macOS Keychain cannot be isolated in a test); it is covered by the manual verification checklist and the UI handler's non-keychain paths.

---

### Task 1: Create the `internal/app` Package Skeleton and Key Operations

**Files:**
- Create: `internal/app/key.go`
- Create: `internal/app/key_test.go`

- [ ] **Step 1: Write the failing key tests**

Create `internal/app/key_test.go`:

```go
package app

import (
	"encoding/base64"
	"testing"

	"vaulty-keeper/internal/apollo"
)

func TestKeyAvailableWithEnvKey(t *testing.T) {
	valid := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv(apollo.EnvKey, valid)
	if !KeyAvailable() {
		t.Fatal("KeyAvailable() = false with valid env key")
	}
	t.Setenv(apollo.EnvKey, "not-base64!")
	if KeyAvailable() {
		t.Fatal("KeyAvailable() = true with invalid env key")
	}
}

func TestSnapshotKeyFromEnv(t *testing.T) {
	key := make([]byte, 32)
	key[0] = 7
	enc := base64.StdEncoding.EncodeToString(key)
	t.Setenv(apollo.EnvKey, enc)
	got, err := SnapshotKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 32 || got[0] != 7 {
		t.Fatalf("SnapshotKey() = %x", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/app -run 'TestKeyAvailableWithEnvKey|TestSnapshotKeyFromEnv' -v`
Expected: build failure — package `vaulty-keeper/internal/app` does not exist.

- [ ] **Step 3: Implement the key operations**

Create `internal/app/key.go`:

```go
// Package app is the single domain-logic layer shared by the CLI and the Web
// UI. It never formats output; callers own presentation.
package app

import "vaulty-keeper/internal/apollo"

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
```

- [ ] **Step 4: Run the key tests to verify they pass**

Run: `go test ./internal/app -run 'TestKeyAvailableWithEnvKey|TestSnapshotKeyFromEnv' -v`
Expected: PASS.

- [ ] **Step 5: Run the full package test for the new package**

Run: `go test ./internal/app`
Expected: PASS (no other tests yet).

---

### Task 2: Create the `internal/app` AES Operations

**Files:**
- Create: `internal/app/aes.go`
- Create: `internal/app/aes_test.go`

- [ ] **Step 1: Write the failing AES tests**

Create `internal/app/aes_test.go`:

```go
package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAESConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VAULTY_KEEPER_AES_CONFIG", filepath.Join(dir, "aes.json"))

	c, err := AESConfigLoad()
	if err != nil || c.Key != "" || c.IV != "" {
		t.Fatalf("load missing = %#v, %v", c, err)
	}
	if err := AESConfigSave("k", "v"); err != nil {
		t.Fatal(err)
	}
	c, err = AESConfigLoad()
	if err != nil || c.Key != "k" || c.IV != "v" {
		t.Fatalf("load after save = %#v, %v", c, err)
	}
	if err := AESConfigClear(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "aes.json")); !os.IsNotExist(err) {
		t.Fatal("file still exists after clear")
	}
}

func TestGenKey(t *testing.T) {
	key, iv, err := GenKey(16, 12)
	if err != nil || len(key) != 16 || len(iv) != 12 {
		t.Fatalf("GenKey = %q %q %v", key, iv, err)
	}
	if _, _, err := GenKey(15, 12); err == nil {
		t.Fatal("GenKey(15) succeeded")
	}
	if _, _, err := GenKey(16, 11); err == nil {
		t.Fatal("GenKey(iv 11) succeeded")
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key, iv, _ := GenKey(16, 16)
	ct, err := Encrypt(key, iv, "hello 世界")
	if err != nil {
		t.Fatal(err)
	}
	pt, err := Decrypt(key, iv, ct)
	if err != nil || pt != "hello 世界" {
		t.Fatalf("round trip = %q, %v", pt, err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/app -run 'TestAESConfigRoundTrip|TestGenKey|TestEncryptDecryptRoundTrip' -v`
Expected: build failure — `AESConfigLoad`, `GenKey`, `Encrypt`, `Decrypt` undefined.

- [ ] **Step 3: Implement the AES operations**

Create `internal/app/aes.go`:

```go
package app

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"

	"vaulty-keeper/internal/aesx"
)

const keyCharset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// AESConfig is the persisted custom AES key/iv pair (~/.vaulty/aes.json).
type AESConfig struct {
	Key string `json:"key"`
	IV  string `json:"iv"`
}

// AESConfigPath returns the file holding the custom AES key/iv.
func AESConfigPath() string {
	if p := os.Getenv("VAULTY_KEEPER_AES_CONFIG"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".vaulty", "aes.json")
	}
	return filepath.Join(home, ".vaulty", "aes.json")
}

// AESConfigLoad reads the custom key/iv file. A missing file is not an error;
// it returns an empty config.
func AESConfigLoad() (AESConfig, error) {
	b, err := os.ReadFile(AESConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return AESConfig{}, nil
		}
		return AESConfig{}, err
	}
	var c AESConfig
	if err := json.Unmarshal(b, &c); err != nil {
		return AESConfig{}, err
	}
	return c, nil
}

// AESConfigSave writes the custom key/iv file with 0600 permissions.
func AESConfigSave(key, iv string) error {
	b, err := json.MarshalIndent(AESConfig{Key: key, IV: iv}, "", "  ")
	if err != nil {
		return err
	}
	p := AESConfigPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, append(b, '\n'), 0o600)
}

// AESConfigClear removes the custom key/iv file. A missing file is not an
// error.
func AESConfigClear() error {
	if err := os.Remove(AESConfigPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// GenKey returns a fresh printable AES key/iv pair.
func GenKey(keyBytes, ivBytes int) (key, iv string, err error) {
	if keyBytes != 16 && keyBytes != 24 && keyBytes != 32 {
		return "", "", errors.New("key must be 16/24/32 bytes")
	}
	if ivBytes != 12 && ivBytes != 16 {
		return "", "", errors.New("iv must be 12 or 16 bytes")
	}
	key, err = randString(keyBytes)
	if err != nil {
		return "", "", err
	}
	iv, err = randString(ivBytes)
	if err != nil {
		return "", "", err
	}
	return key, iv, nil
}

func randString(n int) (string, error) {
	b := make([]byte, n)
	for i := range b {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(keyCharset))))
		if err != nil {
			return "", err
		}
		b[i] = keyCharset[idx.Int64()]
	}
	return string(b), nil
}

// Encrypt wraps aesx.Encrypt (Java CryptoUtil compatible).
func Encrypt(key, iv, plaintext string) (string, error) {
	return aesx.Encrypt(key, iv, plaintext)
}

// Decrypt wraps aesx.Decrypt (Java CryptoUtil compatible).
func Decrypt(key, iv, ciphertext string) (string, error) {
	return aesx.Decrypt(key, iv, ciphertext)
}
```

- [ ] **Step 4: Run the AES tests to verify they pass**

Run: `go test ./internal/app -run 'TestAESConfigRoundTrip|TestGenKey|TestEncryptDecryptRoundTrip' -v`
Expected: PASS.

---

### Task 3: Create the `internal/app` Snapshot Operations

**Files:**
- Create: `internal/app/snapshot.go`
- Create: `internal/app/snapshot_test.go`

- [ ] **Step 1: Write the failing snapshot tests**

Create `internal/app/snapshot_test.go`:

```go
package app

import (
	"path/filepath"
	"testing"

	"vaulty-keeper/internal/aesx"
	"vaulty-keeper/internal/apollo"
)

func testSnapshot(t *testing.T, dir, name string, entries map[string]string) []byte {
	t.Helper()
	key := make([]byte, 32)
	s := apollo.NewSnapshot(name, "")
	for k, v := range entries {
		if err := s.Set(key, k, v, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Save(filepath.Join(dir, name+".json")); err != nil {
		t.Fatal(err)
	}
	return key
}

func TestImportGetSetDelete(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, 32)
	n, err := Import(dir, "prod", "app-1", "A = 1\nSECRET_TOKEN = x\n", key)
	if err != nil || n != 2 {
		t.Fatalf("Import = %d, %v", n, err)
	}
	if v, ok, err := GetValue(dir, "prod", key, "A"); err != nil || !ok || v != "1" {
		t.Fatalf("GetValue = %q %v %v", v, ok, err)
	}
	it, err := SetValue(dir, "prod", "A", "2", nil, key)
	if err != nil || it.Value == nil || *it.Value != "2" {
		t.Fatalf("SetValue = %#v, %v", it, err)
	}
	ok, err := DeleteValue(dir, "prod", "A", key)
	if err != nil || !ok {
		t.Fatalf("DeleteValue = %v, %v", ok, err)
	}
	if _, ok, _ := GetValue(dir, "prod", "A", key); ok {
		t.Fatal("A still exists after delete")
	}
}

func TestCompare(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, 32)
	testSnapshot(t, dir, "prod", map[string]string{"A": "1", "B": "same", "SECRET_TOKEN": "x"})
	testSnapshot(t, dir, "test", map[string]string{"B": "same", "C": "new", "SECRET_TOKEN": "y"})
	changes, err := Compare(dir, "prod", "test", key)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range changes {
		got[c.Key] = c.Kind
	}
	if got["A"] != "removed" || got["C"] != "added" || got["SECRET_TOKEN"] != "changed" {
		t.Fatalf("compare = %#v", got)
	}
}

func TestExportAndEditRoundTrip(t *testing.T) {
	dir := t.TempDir()
	key := testSnapshot(t, dir, "prod", map[string]string{"APP_NAME": "merdi", "SECRET_TOKEN": "s3cr3t"})
	text, err := Export(dir, "prod", key)
	if err != nil {
		t.Fatal(err)
	}
	want := "APP_NAME = merdi\nSECRET_TOKEN = s3cr3t\n"
	if text != want {
		t.Fatalf("Export = %q, want %q", text, want)
	}
	loadText, err := EditLoad(dir, "prod", key)
	if err != nil || loadText != want {
		t.Fatalf("EditLoad = %q, %v", loadText, err)
	}
	n, err := EditApply(dir, "prod", key, "APP_NAME = merdi\nSECRET_TOKEN = s3cr3t\nNEW = 1\n")
	if err != nil || n != 3 {
		t.Fatalf("EditApply = %d, %v", n, err)
	}
	s, err := apollo.Load(filepath.Join(dir, "prod.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"APP_NAME", "SECRET_TOKEN", "NEW"} {
		if v, ok, err := s.Get(key, k); err != nil || !ok || v == "" {
			t.Fatalf("after apply %s: %q %v %v", k, v, ok, err)
		}
	}
}

func TestReveal(t *testing.T) {
	t.Setenv("VAULTY_KEEPER_AES_KEY", "")
	t.Setenv("VAULTY_KEEPER_AES_IV", "")
	dir := t.TempDir()
	key := make([]byte, 32)
	aesKey, aesIV := "0123456789abcdef", "abcdefghijklmnop"
	enc, err := aesx.Encrypt(aesKey, aesIV, "REAL-SECRET")
	if err != nil {
		t.Fatal(err)
	}
	s := apollo.NewSnapshot("prod", "")
	for _, kv := range []struct{ k, v string }{
		{"imile.fs.aes.secret-key", aesKey},
		{"imile.fs.aes.iv", aesIV},
		{"imile.fs.oss.secret-key", enc},
	} {
		if err := s.Set(key, kv.k, kv.v, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Save(filepath.Join(dir, "prod.json")); err != nil {
		t.Fatal(err)
	}
	plain, err := Reveal(dir, "prod", key, []string{"imile.fs.oss.secret-key"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if plain["imile.fs.oss.secret-key"] != "REAL-SECRET" {
		t.Fatalf("reveal = %#v", plain)
	}
	enc2, _ := aesx.Encrypt("fedcba9876543210", "ponmlkjihgfedcba", "OTHER")
	if err := s.Set(key, "imile.fs.oss.secret-key", enc2, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(filepath.Join(dir, "prod.json")); err != nil {
		t.Fatal(err)
	}
	plain2, err := Reveal(dir, "prod", key, []string{"imile.fs.oss.secret-key"}, "fedcba9876543210", "ponmlkjihgfedcba")
	if err != nil || plain2["imile.fs.oss.secret-key"] != "OTHER" {
		t.Fatalf("override reveal = %#v, %v", plain2, err)
	}
}

func TestImportRejectsInvalidName(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, 32)
	if _, err := Import(dir, "../escape", "", "A = 1\n", key); err == nil {
		t.Fatal("Import with traversal name succeeded")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/app -run 'TestImportGetSetDelete|TestCompare|TestExportAndEditRoundTrip|TestReveal|TestImportRejectsInvalidName' -v`
Expected: build failure — functions undefined.

- [ ] **Step 3: Implement the snapshot operations**

Create `internal/app/snapshot.go`:

```go
package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"vaulty-keeper/internal/aesx"
	"vaulty-keeper/internal/apollo"
)

// Import parses Apollo key/value text and writes a new encrypted snapshot.
// It does not check for an existing snapshot; callers decide overwrite policy.
// Returns the number of entries written.
func Import(dir, name, appID, text string, snapKey []byte) (int, error) {
	if err := apollo.ValidateSnapshotName(name); err != nil {
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
	if err := s.Save(filepath.Join(dir, name+".json")); err != nil {
		return 0, err
	}
	return len(kvs), nil
}

// GetValue returns a decrypted value and whether it exists.
func GetValue(dir, name string, snapKey []byte, key string) (string, bool, error) {
	if err := apollo.ValidateKey(key); err != nil {
		return "", false, err
	}
	s, err := load(dir, name)
	if err != nil {
		return "", false, err
	}
	return s.Get(snapKey, key)
}

// SetValue upserts an item and saves. Returns the safe view of the item.
func SetValue(dir, name, key, value string, secret *bool, snapKey []byte) (*apollo.VisibleItem, error) {
	if err := apollo.ValidateKey(key); err != nil {
		return nil, err
	}
	s, err := load(dir, name)
	if err != nil {
		return nil, err
	}
	if err := s.Set(snapKey, key, value, secret); err != nil {
		return nil, err
	}
	if err := s.Save(filepath.Join(dir, name+".json")); err != nil {
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
func DeleteValue(dir, name, key string, snapKey []byte) (bool, error) {
	if err := apollo.ValidateKey(key); err != nil {
		return false, err
	}
	s, err := load(dir, name)
	if err != nil {
		return false, err
	}
	ok := s.Delete(key)
	if !ok {
		return false, nil
	}
	if err := s.Save(filepath.Join(dir, name+".json")); err != nil {
		return false, err
	}
	return true, nil
}

// Compare returns the added/removed/changed diff between two snapshots.
func Compare(dir, from, to string, snapKey []byte) ([]apollo.Change, error) {
	if err := apollo.ValidateSnapshotName(from); err != nil {
		return nil, err
	}
	if err := apollo.ValidateSnapshotName(to); err != nil {
		return nil, err
	}
	a, err := load(dir, from)
	if err != nil {
		return nil, err
	}
	b, err := load(dir, to)
	if err != nil {
		return nil, err
	}
	return a.Diff(b, snapKey), nil
}

// Export returns the full plaintext "KEY = value" listing, keys sorted.
func Export(dir, name string, snapKey []byte) (string, error) {
	s, err := load(dir, name)
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
func EditLoad(dir, name string, snapKey []byte) (string, error) {
	return Export(dir, name, snapKey)
}

// EditApply parses plaintext and re-encrypts the whole snapshot. Returns the
// new entry count. Fails without saving if nothing parses.
func EditApply(dir, name string, snapKey []byte, text string) (int, error) {
	s, err := load(dir, name)
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
	if err := ns.Save(filepath.Join(dir, name+".json")); err != nil {
		return 0, err
	}
	return len(kvs), nil
}

// Reveal decrypts AES-protected targets using the snapshot's AES config
// (imile.fs.aes.secret-key / imile.fs.aes.iv). Overrides take precedence over
// env vars, which take precedence over the snapshot. Fails without returning
// any plaintext if any target fails.
func Reveal(dir, name string, snapKey []byte, targets []string, aesKeyOverride, aesIVOverride string) (map[string]string, error) {
	s, err := load(dir, name)
	if err != nil {
		return nil, err
	}
	aesKey := aesKeyOverride
	if aesKey == "" {
		aesKey = os.Getenv("VAULTY_KEEPER_AES_KEY")
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
		aesIV = os.Getenv("VAULTY_KEEPER_AES_IV")
	}
	if aesIV == "" {
		if v, ok, err := s.Get(snapKey, "imile.fs.aes.iv"); err != nil {
			return nil, err
		} else if ok {
			aesIV = v
		}
	}
	if aesKey == "" || aesIV == "" {
		return nil, errors.New("AES key/iv not found (pass --key/--iv, set VAULTY_KEEPER_AES_KEY/IV, or keep imile.fs.aes.secret-key / imile.fs.aes.iv in the snapshot)")
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

func load(dir, name string) (*apollo.Snapshot, error) {
	if err := apollo.ValidateSnapshotName(name); err != nil {
		return nil, err
	}
	return apollo.Load(filepath.Join(dir, name+".json"))
}
```

- [ ] **Step 4: Run the snapshot tests to verify they pass**

Run: `go test ./internal/app -v`
Expected: PASS.

- [ ] **Step 5: Run gofmt and vet on the new package**

Run: `gofmt -l internal/app && go vet ./internal/app`
Expected: no output from gofmt, vet exits 0.

---

### Task 4: Refactor the CLI AES Commands and Interactive aes.json to Use `internal/app`

**Files:**
- Modify: `internal/cli/cli.go` — `aesOp`, `aesGenKey`
- Modify: `internal/cli/interactive.go` — aes.json load/save/clear via `internal/app`
- Test: `internal/cli/cli_test.go` (existing tests must stay green)

- [ ] **Step 1: Add the `app` import to `internal/cli/cli.go`**

In the import block of `internal/cli/cli.go`, add `"vaulty-keeper/internal/app"` (next to the existing `"vaulty-keeper/internal/aesx"` import).

- [ ] **Step 2: Refactor `aesGenKey` to use `app.GenKey`**

Replace the body of `aesGenKey` (from `key, err := randString(*keyBytes)` through the end) with:

```go
	key, iv, err := app.GenKey(*keyBytes, *ivBytes)
	if err != nil {
		return fail("aes gen-key: %v", err)
	}
	fmt.Printf("SECRET_KEY: %s\n", key)
	fmt.Printf("IV: %s\n", iv)
	return 0
```

Keep the flag definitions, the `*keyBytes`/`*ivBytes` range checks, and the exact output format. After this change the local helpers `keyCharset` and `randString` become unused — remove them from `internal/cli/cli.go`, and remove the now-unused `"crypto/rand"` and `"math/big"` imports.

- [ ] **Step 3: Refactor `aesOp` to use `app.Encrypt` / `app.Decrypt`**

In `aesOp`, keep all input handling (flags, env fallback, --file/args/stdin) exactly as-is. Replace only the operation call:

```go
	var (
		out string
		err error
	)
	if encrypt {
		out, err = app.Encrypt(k, i, input)
	} else {
		out, err = app.Decrypt(k, i, input)
	}
	if err != nil {
		return fail("aes: %v", err)
	}
	fmt.Println(out)
	return 0
```

Keep the `"vaulty-keeper/internal/aesx"` import for now — `apolloReveal` still uses it until Task 6 removes it (see Task 6 Step 3).

- [ ] **Step 4: Refactor `interactive.go` to use `app.AESConfig`**

In `internal/cli/interactive.go`:

1. Add `"vaulty-keeper/internal/app"` to the imports.
2. Delete the local `aesConfigPath`, `loadAesConfig`, and `saveAesConfig` functions (and the `interactive` fields they read/write if they become unused — see Step 5).
3. Replace `it.loadAesConfig()` in `runInteractive` with:

```go
	if c, err := app.AESConfigLoad(); err == nil {
		it.aesKey, it.aesIV = c.Key, c.IV
	}
```

4. Find the `cmdCustomKeyIv` handler (around line 995). Replace its save call and the printed path with:

```go
	if err := app.AESConfigSave(it.aesKey, it.aesIV); err != nil {
		fmt.Fprintf(os.Stderr, "warning: 保存自定义 key/iv 失败: %v\n", err)
		return
	}
	fmt.Println(green(fmt.Sprintf("已设置自定义 key/iv（%s）", app.AESConfigPath())))
```

- [ ] **Step 5: Run gofmt and the existing CLI tests**

Run: `gofmt -l internal/cli && go test ./internal/cli -v`
Expected: gofmt clean; all existing CLI tests PASS. Fix any unused-variable/import compile errors by removing the now-dead interactive helpers only if truly unreferenced.

---

### Task 5: Refactor the CLI Apollo Commands (init/import/get/set/unset/compare/export) to Use `internal/app`

**Files:**
- Modify: `internal/cli/cli.go`
- Test: `internal/cli/cli_test.go` (existing tests must stay green)

- [ ] **Step 1: Refactor `apolloInit`**

Replace the `apollo.GenerateAndStoreKey` call with:

```go
	if err := app.InitKey(*force); err != nil {
		return fail("apollo init: %v", err)
	}
```

Output line stays `snapshot key created and stored in macOS Keychain`.

- [ ] **Step 2: Refactor `apolloImport`**

Keep the flag parsing, input reading, warning printing, and the `len(kvs) == 0` check exactly as-is. Replace the block from `key, err := apollo.SnapshotKey()` through the `s.Save(path)` call with:

```go
	key, err := apollo.SnapshotKey()
	if err != nil {
		return fail("apollo import: %v", err)
	}
	dirPath, err := snapDir(*dir)
	if err != nil {
		return fail("apollo import: %v", err)
	}
	n, err := app.Import(dirPath, *name, *appID, text, key)
	if err != nil {
		return fail("apollo import: %v", err)
	}
	fmt.Println(green(fmt.Sprintf("imported %d entries into snapshot %q (%s)", n, *name, filepath.Join(dirPath, *name+".json"))))
```

Note: CLI import keeps its existing overwrite behavior; `app.Import` does not check existence, so behavior is identical.

- [ ] **Step 3: Refactor `apolloGet`**

Keep flag parsing and argument checks. Replace the body from `path, err := snapPath(dirPath, name)` through the end with:

```go
	v, ok, err := app.GetValue(dirPath, name, key, k)
	if err != nil {
		return fail("apollo get: %v", err)
	}
	if !ok {
		return fail("apollo get: key %q not found in snapshot %q", k, name)
	}
	fmt.Println(v)
	return 0
```

- [ ] **Step 4: Refactor `apolloSet`**

Keep flag parsing, argument checks, and the `secret` computation. Replace the block from `path, err := snapPath(dirPath, name)` through `s.Save(path)` with:

```go
	if _, err := app.SetValue(dirPath, name, k, v, secret, key); err != nil {
		return fail("apollo set: %v", err)
	}
	fmt.Println(green(fmt.Sprintf("set %s.%s", name, k)))
	return 0
```

- [ ] **Step 5: Refactor `apolloUnset`**

Replace the block from `path, err := snapPath(dirPath, name)` through `s.Save(path)` with:

```go
	ok, err := app.DeleteValue(dirPath, name, k, key)
	if err != nil {
		return fail("apollo unset: %v", err)
	}
	if !ok {
		return fail("apollo unset: key %q not found in snapshot %q", k, name)
	}
	fmt.Println(green(fmt.Sprintf("unset %s.%s", name, k)))
	return 0
```

- [ ] **Step 6: Refactor `apolloCompare`**

Keep flag parsing, argument checks, `dirPath`/`key` resolution, and all output formatting (the `val` helper, JSON branch, colored rows) exactly as-is. Replace the snapshot loading block:

```go
	changes, err := app.Compare(dirPath, nameA, nameB, key)
	if err != nil {
		return fail("apollo compare: %v", err)
	}
```

The rest (`if len(changes) == 0 ...`, `val`, JSON, colored output) is unchanged.

- [ ] **Step 7: Refactor `apolloExport`**

Keep flag parsing, argument checks, and the `--copy` pbcopy block exactly as-is. Replace the block from `path, err := snapPath(dirPath, name)` through the `buf` construction with:

```go
	text, err := app.Export(dirPath, name, key)
	if err != nil {
		return fail("apollo export: %v", err)
	}
	var out strings.Builder
	out.WriteString(text)
```

- [ ] **Step 8: Run gofmt and the existing CLI tests**

Run: `gofmt -l internal/cli && go test ./internal/cli -v`
Expected: gofmt clean; all existing CLI tests PASS. If a test asserted an error message string that changed wording, adjust the test to the app-layer wording (the refactor must not change success output).

---

### Task 6: Refactor the CLI Reveal and Edit Commands to Use `internal/app`

**Files:**
- Modify: `internal/cli/cli.go`
- Test: `internal/cli/cli_test.go` (existing tests must stay green)

- [ ] **Step 1: Refactor `apolloReveal`**

Keep flag parsing, argument checks, `dirPath`/`snapKey` resolution. Replace everything from `aesKey := *keyFlag` through the end with:

```go
	plain, err := app.Reveal(dirPath, name, snapKey, targets, *keyFlag, *ivFlag)
	if err != nil {
		return fail("apollo reveal: %v", err)
	}

	if *jsonOut {
		b, err := json.MarshalIndent(plain, "", "  ")
		if err != nil {
			return fail("apollo reveal: %v", err)
		}
		fmt.Println(string(b))
		return 0
	}
	if len(targets) == 1 {
		fmt.Println(cyan(plain[targets[0]]))
		return 0
	}
	for _, k := range targets {
		fmt.Printf("%s = %s\n", k, plain[k])
	}
	return 0
```

Note: `app.Reveal` resolves override → env → snapshot, matching the previous CLI order. `snapKey` here is the variable already resolved from `apollo.SnapshotKey()`; pass the existing `name`, `targets`, `*keyFlag`, `*ivFlag`.

- [ ] **Step 2: Refactor `apolloEdit`**

Keep flag parsing, argument checks, `dirPath`/`snapKey` resolution. Replace the block from `keys := make(...)` through `ns.Save(path)` with:

```go
	text, err := app.EditLoad(dirPath, name, snapKey)
	if err != nil {
		return fail("apollo edit: %v", err)
	}

	tmp, err := os.CreateTemp("", "vaulty-keeper-edit-*.txt")
	if err != nil {
		return fail("apollo edit: %v", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(text); err != nil {
		tmp.Close()
		return fail("apollo edit: %v", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fail("apollo edit: %v", err)
	}
	tmp.Close()

	ed := *editor
	if ed == "" {
		ed = os.Getenv("EDITOR")
	}
	if ed == "" {
		ed = "vi"
	}
	cmd := exec.Command("sh", "-c", ed+" "+strconv.Quote(tmpPath))
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fail("apollo edit: editor failed: %v", err)
	}

	content, err := os.ReadFile(tmpPath)
	if err != nil {
		return fail("apollo edit: %v", err)
	}
	kvs, warnings := apollo.ParseKV(string(content))
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	n, err := app.EditApply(dirPath, name, snapKey, string(content))
	if err != nil {
		return fail("apollo edit: %v", err)
	}
	fmt.Println(green(fmt.Sprintf("updated snapshot %q: %d entries", name, n)))
	return 0
```

- [ ] **Step 3: Run gofmt and the existing CLI tests**

Run: `gofmt -l internal/cli && go test ./internal/cli -v`
Expected: gofmt clean; all existing CLI tests PASS. Verify the reveal/edit behavior tests (fake-editor script, multi-key reveal JSON) still pass. After this task `apolloReveal` no longer calls `aesx` directly — if `"vaulty-keeper/internal/aesx"` is now unreferenced in `internal/cli/cli.go`, remove that import.

- [ ] **Step 4: Run the whole test suite**

Run: `go test ./...`
Expected: all packages PASS.

---

### Task 7: Add the New UI Handler Endpoints

**Files:**
- Modify: `internal/ui/ui.go`
- Modify: `internal/ui/ui_test.go`

- [ ] **Step 1: Write the failing handler tests**

Append to `internal/ui/ui_test.go`:

```go
func TestRevealRequiresConfirmation(t *testing.T) {
	h := newTestHandler(t, "SECRET_TOKEN = x\n")
	body := strings.NewReader(`{"targets":["SECRET_TOKEN"],"confirm":false}`)
	r := httptest.NewRequest(http.MethodPost, "/api/snapshots/prod/reveal", body)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestEditLoadRequiresConfirmation(t *testing.T) {
	h := newTestHandler(t, "APP_NAME = merdi\n")
	r := httptest.NewRequest(http.MethodPost, "/api/snapshots/prod/edit", strings.NewReader(`{"confirm":false}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestEditApplyReencrypts(t *testing.T) {
	h := newTestHandler(t, "APP_NAME = merdi\n")
	body := strings.NewReader(`{"text":"APP_NAME = merdi\nNEW = 1\n"}`)
	r := httptest.NewRequest(http.MethodPut, "/api/snapshots/prod/edit", body)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "merdi") {
		t.Fatalf("edit apply response contains plaintext: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"total":2`) {
		t.Fatalf("edit apply total missing: %s", w.Body.String())
	}
}

func TestAESGenKey(t *testing.T) {
	h := newTestHandler(t, "")
	r := httptest.NewRequest(http.MethodPost, "/api/aes/gen-key", strings.NewReader(`{"bytes":16,"iv_bytes":12}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"key"`) || !strings.Contains(w.Body.String(), `"iv"`) {
		t.Fatalf("gen-key body: %s", w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
}

func TestAESGenKeyRejectsBadSizes(t *testing.T) {
	h := newTestHandler(t, "")
	r := httptest.NewRequest(http.MethodPost, "/api/aes/gen-key", strings.NewReader(`{"bytes":15,"iv_bytes":12}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAESTransformRoundTrip(t *testing.T) {
	h := newTestHandler(t, "")
	enc := httptest.NewRecorder()
	h.ServeHTTP(enc, httptest.NewRequest(http.MethodPost, "/api/aes/transform",
		strings.NewReader(`{"op":"encrypt","key":"0123456789abcdef","iv":"abcdefghijklmnop","text":"hello"}`)))
	if enc.Code != http.StatusOK {
		t.Fatalf("encrypt status = %d: %s", enc.Code, enc.Body.String())
	}
	var er struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(enc.Body.Bytes(), &er); err != nil || er.Result == "" {
		t.Fatalf("encrypt body = %s (%v)", enc.Body.String(), err)
	}
	dec := httptest.NewRecorder()
	h.ServeHTTP(dec, httptest.NewRequest(http.MethodPost, "/api/aes/transform",
		strings.NewReader(`{"op":"decrypt","key":"0123456789abcdef","iv":"abcdefghijklmnop","text":"`+er.Result+`"}`)))
	if dec.Code != http.StatusOK || !strings.Contains(dec.Body.String(), `"hello"`) {
		t.Fatalf("decrypt = %d %s", dec.Code, dec.Body.String())
	}
}

func TestAESConfigAPI(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VAULTY_KEEPER_AES_CONFIG", filepath.Join(dir, "aes.json"))
	h := NewHandler(Config{Dir: dir, SnapshotKey: func() ([]byte, error) { return make([]byte, 32), nil }})

	get := httptest.NewRecorder()
	h.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/aes/config", nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"key":""`) {
		t.Fatalf("get empty config = %d %s", get.Code, get.Body.String())
	}

	put := httptest.NewRecorder()
	h.ServeHTTP(put, httptest.NewRequest(http.MethodPut, "/api/aes/config",
		strings.NewReader(`{"key":"kk","iv":"ivv"}`)))
	if put.Code != http.StatusOK {
		t.Fatalf("put config = %d %s", put.Code, put.Body.String())
	}

	get2 := httptest.NewRecorder()
	h.ServeHTTP(get2, httptest.NewRequest(http.MethodGet, "/api/aes/config", nil))
	if get2.Code != http.StatusOK || !strings.Contains(get2.Body.String(), `"key":"kk"`) {
		t.Fatalf("get after put = %d %s", get2.Code, get2.Body.String())
	}

	del := httptest.NewRecorder()
	h.ServeHTTP(del, httptest.NewRequest(http.MethodDelete, "/api/aes/config", nil))
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete config = %d", del.Code)
	}
}

func TestInitRejectsWhenKeyAvailable(t *testing.T) {
	h := NewHandler(Config{Dir: t.TempDir(), SnapshotKey: func() ([]byte, error) { return make([]byte, 32), nil }})
	r := httptest.NewRequest(http.MethodPost, "/api/init", strings.NewReader(`{"force":false}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestInitRejectsWrongMethod(t *testing.T) {
	h := NewHandler(Config{Dir: t.TempDir(), SnapshotKey: func() ([]byte, error) { return make([]byte, 32), nil }})
	r := httptest.NewRequest(http.MethodGet, "/api/init", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}
```

Check the existing `ui_test.go` imports: add `"encoding/json"` and `"path/filepath"` if not already present.

- [ ] **Step 2: Run the handler tests to verify they fail**

Run: `go test ./internal/ui -run 'TestRevealRequiresConfirmation|TestEditLoadRequiresConfirmation|TestEditApplyReencrypts|TestAESGenKey|TestAESGenKeyRejectsBadSizes|TestAESTransformRoundTrip|TestAESConfigAPI|TestInitRejectsWhenKeyAvailable|TestInitRejectsWrongMethod' -v`
Expected: FAIL — routes not registered (404).

- [ ] **Step 3: Add the `app` import and new routes to `ui.go`**

In `internal/ui/ui.go`:
1. Add `"vaulty-keeper/internal/app"` to imports.
2. In `NewHandler`, register:

```go
	mux.HandleFunc("/api/init", h.initKey)
	mux.HandleFunc("/api/aes/gen-key", h.aesGenKey)
	mux.HandleFunc("/api/aes/transform", h.aesTransform)
	mux.HandleFunc("/api/aes/config", h.aesConfig)
```

- [ ] **Step 4: Extend `snapshotView` routing**

In `snapshotView`, add two cases before the `default` branch:

```go
	case len(parts) == 2 && parts[1] == "reveal":
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		h.reveal(w, r, parts[0])
	case len(parts) == 2 && parts[1] == "edit":
		switch r.Method {
		case http.MethodPost:
			h.editLoad(w, r, parts[0])
		case http.MethodPut:
			h.editApply(w, r, parts[0])
		default:
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
```

- [ ] **Step 5: Implement the init, reveal, and edit handlers**

Append to `internal/ui/ui.go`:

```go
type initRequest struct {
	Force bool `json:"force"`
}

func (h *handler) initKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var req initRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}
	if !req.Force {
		if _, err := h.cfg.SnapshotKey(); err == nil {
			writeAPIError(w, http.StatusConflict, "key_exists", "a snapshot key already exists")
			return
		}
	}
	if err := app.InitKey(req.Force); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "key_init_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

type revealRequest struct {
	Targets []string `json:"targets"`
	Confirm bool     `json:"confirm"`
}

func (h *handler) reveal(w http.ResponseWriter, r *http.Request, name string) {
	var req revealRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}
	if !req.Confirm {
		writeAPIError(w, http.StatusBadRequest, "confirm_required", "confirmation required for plaintext reveal")
		return
	}
	if len(req.Targets) == 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_key", "no targets provided")
		return
	}
	if err := apollo.ValidateSnapshotName(name); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_snapshot_name", err.Error())
		return
	}
	snapKey, err := h.cfg.SnapshotKey()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "snapshot_key_unavailable", fmt.Sprintf("snapshot key unavailable (%s); run 'vaulty-keeper apollo init' or set %s", err, apollo.EnvKey))
		return
	}
	plain, err := app.Reveal(h.cfg.Dir, name, snapKey, req.Targets, "", "")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "decrypt_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"values": plain})
}

type editLoadRequest struct {
	Confirm bool `json:"confirm"`
}

func (h *handler) editLoad(w http.ResponseWriter, r *http.Request, name string) {
	var req editLoadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}
	if !req.Confirm {
		writeAPIError(w, http.StatusBadRequest, "confirm_required", "confirmation required for plaintext edit")
		return
	}
	snapKey, err := h.cfg.SnapshotKey()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "snapshot_key_unavailable", fmt.Sprintf("snapshot key unavailable (%s); run 'vaulty-keeper apollo init' or set %s", err, apollo.EnvKey))
		return
	}
	text, err := app.EditLoad(h.cfg.Dir, name, snapKey)
	if err != nil {
		h.loadError(w, name, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"text": text})
}

type editApplyRequest struct {
	Text string `json:"text"`
}

func (h *handler) editApply(w http.ResponseWriter, r *http.Request, name string) {
	var req editApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}
	if req.Text == "" {
		writeAPIError(w, http.StatusBadRequest, "empty_import", "no config items found")
		return
	}
	snapKey, err := h.cfg.SnapshotKey()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "snapshot_key_unavailable", fmt.Sprintf("snapshot key unavailable (%s); run 'vaulty-keeper apollo init' or set %s", err, apollo.EnvKey))
		return
	}
	n, err := app.EditApply(h.cfg.Dir, name, snapKey, req.Text)
	if err != nil {
		if strings.Contains(err.Error(), "no key/value entries") {
			writeAPIError(w, http.StatusBadRequest, "empty_import", err.Error())
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "snapshot_edit_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "total": n})
}
```

- [ ] **Step 6: Implement the AES handlers**

Append to `internal/ui/ui.go`:

```go
type aesGenKeyRequest struct {
	Bytes   int `json:"bytes"`
	IVBytes int `json:"iv_bytes"`
}

func (h *handler) aesGenKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var req aesGenKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}
	key, iv, err := app.GenKey(req.Bytes, req.IVBytes)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_aes_params", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": key, "iv": iv})
}

type aesTransformRequest struct {
	Op   string `json:"op"`
	Key  string `json:"key"`
	IV   string `json:"iv"`
	Text string `json:"text"`
}

func (h *handler) aesTransform(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var req aesTransformRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}
	if req.Key == "" || req.IV == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_aes_params", "key and iv are required")
		return
	}
	var (
		out string
		err error
	)
	switch req.Op {
	case "encrypt":
		out, err = app.Encrypt(req.Key, req.IV, req.Text)
	case "decrypt":
		out, err = app.Decrypt(req.Key, req.IV, req.Text)
	default:
		writeAPIError(w, http.StatusBadRequest, "invalid_aes_params", "op must be encrypt or decrypt")
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "aes_op_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": out})
}

type aesConfigRequest struct {
	Key string `json:"key"`
	IV  string `json:"iv"`
}

func (h *handler) aesConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		c, err := app.AESConfigLoad()
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "aes_config_io", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"key": c.Key, "iv": c.IV, "path": app.AESConfigPath()})
	case http.MethodPut:
		var req aesConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
			return
		}
		if req.Key == "" || req.IV == "" {
			writeAPIError(w, http.StatusBadRequest, "invalid_aes_params", "key and iv are required")
			return
		}
		if err := app.AESConfigSave(req.Key, req.IV); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "aes_config_io", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case http.MethodDelete:
		if err := app.AESConfigClear(); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "aes_config_io", err.Error())
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	default:
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}
```

- [ ] **Step 7: Run the handler tests to verify they pass**

Run: `go test ./internal/ui -v`
Expected: PASS (new + existing).

- [ ] **Step 8: Run gofmt and vet**

Run: `gofmt -l internal/ui && go vet ./internal/ui`
Expected: clean.

---

### Task 8: Extend the Frontend — Rail Sections, AES Tools, Settings, Reveal, Bulk Edit

**Files:**
- Modify: `internal/ui/static/index.html`
- Modify: `internal/ui/static/app.css`
- Modify: `internal/ui/static/app.js`

- [ ] **Step 1: Add a failing static shell test for the new rail sections**

Append to `internal/ui/ui_test.go`:

```go
func TestRootServesFullWorkspace(t *testing.T) {
	h := NewHandler(Config{Dir: t.TempDir(), SnapshotKey: func() ([]byte, error) { return make([]byte, 32), nil }})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	for _, id := range []string{`id="view-aes"`, `id="view-settings"`, `id="reveal-dialog"`, `id="edit-dialog"`} {
		if !strings.Contains(w.Body.String(), id) {
			t.Fatalf("workspace shell missing %s", id)
		}
	}
}
```

Run: `go test ./internal/ui -run TestRootServesFullWorkspace -v`
Expected: FAIL — the new elements do not exist yet.

- [ ] **Step 2: Rewrite `internal/ui/static/index.html`**

Replace the whole file with:

```html
<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1.0">
<title>vaulty-keeper · 配置工作台</title>
<link rel="icon" href="data:,">
<link rel="stylesheet" href="/app.css">
</head>
<body>
<div class="app">
  <aside>
    <div class="brand"><span class="mark">✦</span> vaulty-keeper</div>
    <button class="new" type="button" data-action="import"><b>+</b> 导入配置快照</button>
    <div class="group">最近工作</div>
    <div id="recent-work"></div>
    <div class="group gap-top">快照</div>
    <div id="snapshot-list"></div>
    <div class="group gap-top">工具</div>
    <div class="nav-item" id="nav-aes">AES 加解密</div>
    <div class="nav-item" id="nav-settings">设置</div>
    <div class="spacer"></div>
    <div class="foot"><div class="avatar">K</div><div><b>本地工作区</b><span>仅此设备可访问</span></div></div>
  </aside>
  <main id="app">
    <header class="topbar">
      <div id="breadcrumb" class="crumb"></div>
      <button class="more" type="button" aria-label="更多操作">•••</button>
    </header>

    <div class="view" id="view-snapshots">
      <div class="canvas">
        <div id="hero" class="hero">
          <div>
            <h1 id="hero-title">检查配置状态。</h1>
            <p class="sub">快照在本地加密，敏感值默认遮罩。</p>
          </div>
          <button class="cta" type="button" data-action="import">导入快照</button>
        </div>
        <div id="error-region" class="error-region" hidden></div>
        <div class="layout">
          <section>
            <div id="snapshot-context" class="context">
              <div class="env">·</div>
              <div><strong id="context-name">—</strong><p id="context-meta">加载中…</p></div>
              <div class="secure"><i></i>已加密</div>
            </div>
            <div class="sec-head"><h2>配置</h2><span id="config-count" class="count"></span></div>
            <div class="search" id="config-search">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><circle cx="11" cy="11" r="7"/><line x1="16.5" y1="16.5" x2="21" y2="21"/></svg>
              <input id="search-input" type="search" placeholder="搜索 key 或可见值…" aria-label="搜索配置" autocomplete="off" spellcheck="false">
            </div>
            <table id="config-table">
              <tbody id="config-body"></tbody>
            </table>
            <div id="compare-panel" hidden>
              <div class="sec-head"><h2 id="compare-heading">配置对比</h2><button class="link" type="button" data-action="compare">完整对比 →</button></div>
              <div id="compare-body" class="diff"></div>
            </div>
          </section>
          <aside class="assist" id="next-steps">
            <div class="card">
              <div class="card-title">下一步</div>
              <div class="task"><button class="task-btn" type="button" data-action="compare"><strong>比较环境</strong></button><span>检查新增、删除与变更。</span></div>
              <div class="task"><button class="task-btn" type="button" data-action="focus-search"><strong>查看或修改单项</strong></button><span>按 key 搜索；敏感内容默认隐藏。</span></div>
              <div class="task"><button class="task-btn" type="button" data-action="bulk-edit"><strong>明文编辑全部</strong></button><span>确认后以明文编辑并重新加密。</span></div>
              <div class="task"><button class="task-btn" type="button" data-action="export"><strong>导出配置</strong></button><span>生成明文前需要再次确认。</span></div>
              <div class="task"><button class="task-btn" type="button" data-action="aes-tools"><strong>AES 加解密</strong></button><span>手动 key/iv 加解密与生成。</span></div>
            </div>
            <div class="note">🔒 <b>本地优先。</b> 明文只在确认后短暂显示。</div>
          </aside>
        </div>
      </div>
    </div>

    <div class="view" id="view-aes" hidden>
      <div class="canvas">
        <div class="hero">
          <div>
            <h1>AES 加解密工具。</h1>
            <p class="sub">Java CryptoUtil 兼容（AES/GCM/NoPadding）。key/iv 手动输入，或从本机 aes.json 预填。</p>
          </div>
        </div>
        <div id="aes-error" class="error-region" hidden></div>
        <div class="panel">
          <div class="fields">
            <label class="field" for="aes-key">AES key（16/24/32 字节 UTF-8）
              <input id="aes-key" type="text" autocomplete="off" spellcheck="false">
            </label>
            <label class="field" for="aes-iv">IV（UTF-8 字节）
              <input id="aes-iv" type="text" autocomplete="off" spellcheck="false">
            </label>
          </div>
          <div class="row-actions">
            <button class="ghost" type="button" id="aes-load-config">从 aes.json 预填</button>
            <button class="ghost" type="button" id="aes-gen-key">生成 key/iv</button>
            <button class="ghost" type="button" id="aes-save-config">保存为默认</button>
          </div>
          <label class="field" for="aes-input">输入
            <textarea id="aes-input" rows="6" placeholder="明文或 base64 密文…" spellcheck="false"></textarea>
          </label>
          <div class="row-actions">
            <button class="primary" type="button" id="aes-encrypt-btn">加密</button>
            <button class="primary" type="button" id="aes-decrypt-btn">解密</button>
          </div>
          <label class="field" for="aes-output">结果
            <textarea id="aes-output" rows="6" readonly spellcheck="false"></textarea>
          </label>
          <div class="row-actions">
            <button class="ghost" type="button" id="aes-copy">复制结果</button>
          </div>
        </div>
      </div>
    </div>

    <div class="view" id="view-settings" hidden>
      <div class="canvas">
        <div class="hero">
          <div>
            <h1>设置。</h1>
            <p class="sub">本地密钥与 AES 默认配置管理。</p>
          </div>
        </div>
        <div id="settings-error" class="error-region" hidden></div>
        <div class="panel">
          <div class="sec-head"><h2>快照密钥</h2></div>
          <div id="key-status" class="status-line">检测中…</div>
          <div class="row-actions">
            <button class="primary" type="button" id="init-key-btn" hidden>生成快照密钥</button>
          </div>
          <div class="sec-head"><h2>自定义 AES key/iv（aes.json）</h2></div>
          <div id="aes-config-status" class="status-line">检测中…</div>
          <div class="row-actions">
            <button class="ghost" type="button" id="clear-aes-config-btn" hidden>清除已保存的 key/iv</button>
          </div>
        </div>
      </div>
    </div>
  </main>
</div>

<dialog id="import-dialog" aria-labelledby="import-title">
  <form id="import-form" class="dialog">
    <h3 id="import-title">导入配置快照</h3>
    <label class="field" for="import-name">快照名称
      <input id="import-name" type="text" placeholder="例如 prod" autocomplete="off" spellcheck="false">
    </label>
    <label class="field" for="import-appid">应用 ID（可选）
      <input id="import-appid" type="text" placeholder="例如 merdi-portal" autocomplete="off" spellcheck="false">
    </label>
    <label class="field" for="import-text">粘贴 key=value 配置
      <textarea id="import-text" rows="8" placeholder="APP_NAME = merdi&#10;SECRET_TOKEN = ..." spellcheck="false"></textarea>
    </label>
    <div id="import-preview" class="preview" hidden></div>
    <div id="import-error" class="error" hidden></div>
    <div class="actions">
      <button type="button" class="ghost" data-close>取消</button>
      <button type="button" class="ghost" id="import-preview-btn">预览</button>
      <button type="submit" class="primary" id="import-confirm-btn" hidden>确认导入</button>
    </div>
  </form>
</dialog>

<dialog id="entry-dialog" aria-labelledby="entry-title">
  <form id="entry-form" class="dialog">
    <h3 id="entry-title">编辑配置项</h3>
    <div class="entry-head">
      <span class="env" id="entry-env">k</span>
      <code id="entry-key" class="entry-key"></code>
    </div>
    <p id="entry-warning" class="warning" hidden>当前为敏感值，无法显示。输入新值将替换它；留空并保存不修改。</p>
    <label class="field" for="entry-value">值
      <input id="entry-value" type="text" autocomplete="off" spellcheck="false">
    </label>
    <div id="entry-error" class="error" hidden></div>
    <div class="actions">
      <button type="button" class="ghost" data-close>取消</button>
      <button type="button" class="danger" id="entry-delete-btn">删除</button>
      <button type="submit" class="primary" id="entry-save-btn">保存</button>
    </div>
  </form>
</dialog>

<dialog id="delete-dialog" aria-labelledby="delete-title">
  <form method="dialog" class="dialog">
    <h3 id="delete-title">删除配置项</h3>
    <p>确定要删除 <code id="delete-key"></code> 吗？此操作不可撤销。</p>
    <div id="delete-error" class="error" hidden></div>
    <div class="actions">
      <button type="button" class="ghost" data-close>取消</button>
      <button type="button" class="danger" id="delete-confirm-btn">删除</button>
    </div>
  </form>
</dialog>

<dialog id="compare-dialog" aria-labelledby="compare-title">
  <form method="dialog" class="dialog">
    <h3 id="compare-title">比较环境</h3>
    <p class="muted">将 <code id="compare-from"></code> 与以下环境对比：</p>
    <label class="field" for="compare-target">目标环境
      <select id="compare-target"></select>
    </label>
    <div id="compare-error" class="error" hidden></div>
    <div class="actions">
      <button type="button" class="ghost" data-close>取消</button>
      <button type="button" class="primary" id="compare-confirm-btn">开始对比</button>
    </div>
  </form>
</dialog>

<dialog id="export-dialog" aria-labelledby="export-title">
  <form method="dialog" class="dialog">
    <h3 id="export-title">导出配置</h3>
    <p>将生成 <code id="export-name"></code> 的明文 key=value 文件，并在本机浏览器中下载。</p>
    <p class="warning">⚠ 明文包含敏感值，请仅在本机查看，不要转发。</p>
    <div id="export-error" class="error" hidden></div>
    <div class="actions">
      <button type="button" class="ghost" data-close>取消</button>
      <button type="button" class="danger" id="export-copy-btn">复制到剪贴板</button>
      <button type="button" class="danger" id="export-confirm-btn">确认导出</button>
    </div>
  </form>
</dialog>

<dialog id="reveal-dialog" aria-labelledby="reveal-title">
  <form method="dialog" class="dialog">
    <h3 id="reveal-title">解密显示</h3>
    <p class="warning">⚠ 将显示 <code id="reveal-key"></code> 的明文值，请仅在本机查看。</p>
    <div id="reveal-value" class="reveal-box"></div>
    <div id="reveal-error" class="error" hidden></div>
    <div class="actions">
      <button type="button" class="ghost" data-close>关闭</button>
      <button type="button" class="danger" id="reveal-confirm-btn">解密并显示</button>
    </div>
  </form>
</dialog>

<dialog id="edit-dialog" aria-labelledby="edit-title">
  <form id="edit-form" class="dialog">
    <h3 id="edit-title">明文编辑</h3>
    <p class="warning">⚠ 将以明文编辑 <code id="edit-name"></code> 的全部条目，保存后重新加密。</p>
    <label class="field" for="edit-text">配置内容
      <textarea id="edit-text" rows="14" spellcheck="false"></textarea>
    </label>
    <div id="edit-error" class="error" hidden></div>
    <div class="actions">
      <button type="button" class="ghost" data-close>取消</button>
      <button type="button" class="ghost" id="edit-load-btn" hidden>已加载</button>
      <button type="submit" class="primary" id="edit-save-btn" hidden>保存并重新加密</button>
    </div>
  </form>
</dialog>

<dialog id="summary-dialog" aria-labelledby="summary-title">
  <form method="dialog" class="dialog">
    <h3 id="summary-title">安全 AI 摘要</h3>
    <p>安全摘要仅基于可见值与本机元数据生成，不包含原始密钥。此能力在当前版本为占位，将在后续版本提供。</p>
    <div class="actions">
      <button type="button" class="ghost" data-close>关闭</button>
    </div>
  </form>
</dialog>

<script src="/app.js" defer></script>
</body>
</html>
```

- [ ] **Step 3: Extend `internal/ui/static/app.css`**

Append to `app.css`:

```css
/* ---- rail nav ---- */
.nav-item{padding:7px 11px;border-radius:8px;margin:1px 2px;color:#57534e;cursor:pointer}
.nav-item:hover{background:#f6f4ef}
.nav-item.active{background:var(--accent-soft);color:var(--accent-ink);font-weight:600}

/* ---- views ---- */
.view{display:block}
.view[hidden]{display:none}

/* ---- tool panels ---- */
.panel{background:var(--panel);border:1px solid var(--line);border-radius:var(--r-card);padding:22px;max-width:640px}
.panel .fields{display:grid;grid-template-columns:1fr 1fr;gap:14px}
.panel .row-actions{display:flex;gap:9px;margin:2px 0 16px;flex-wrap:wrap}
.panel .row-actions button{font:inherit;font-size:13px;border-radius:var(--r-ctl);padding:8px 13px;cursor:pointer;border:1px solid var(--line);background:#fff;color:#4a4641}
.panel .row-actions button:hover{background:#f7f5f0}
.panel .row-actions .primary{background:var(--ink);border-color:var(--ink);color:#fff}
.panel .row-actions .primary:hover{background:#3a3834}
.panel .field textarea{font:12.5px/1.6 ui-monospace,SFMono-Regular,Menlo,monospace;resize:vertical}
.panel textarea[readonly]{background:#faf8f4;color:#5b554d}
.status-line{font-size:13px;color:var(--mute);padding:9px 13px;background:var(--code);border-radius:10px;margin-bottom:12px}
.status-line.ok{color:var(--green);background:var(--green-soft)}
.status-line.warn{color:#984d36;background:var(--accent-soft)}

/* ---- reveal box ---- */
.reveal-box{margin:0 0 14px;padding:12px 14px;border:1px solid var(--line);border-radius:10px;background:#fdfbf8;
  font:12.5px/1.6 ui-monospace,SFMono-Regular,Menlo,monospace;color:#4c4842;overflow-wrap:anywhere;white-space:pre-wrap;max-height:200px;overflow:auto}
</style>
```

(Note: the trailing `</style>` line in the snippet above is a mistake — omit it. The CSS is appended to the existing file, not wrapped in a `<style>` tag.)

- [ ] **Step 4: Extend `internal/ui/static/app.js`**

The existing file keeps all current behavior. Apply these changes:

1. Extend the state object:

```js
const state = { snapshots: [], active: null, snapshot: null, compare: null, recentWork: [], view: 'snapshots' };
```

2. No new API helper is needed: the reveal/edit endpoints return JSON, which the existing `api()` parses; the export-to-clipboard path reads the plaintext body from the returned `Response` via `res.text()`.

3. Add view switching after `renderRecent`:

```js
function switchView(name) {
  state.view = name;
  for (const v of ['snapshots', 'aes', 'settings']) {
    const el = $(`view-${v}`);
    if (el) el.hidden = v !== name;
    const nav = $(`nav-${v}`);
    if (nav) nav.classList.toggle('active', v === name);
  }
  $('breadcrumb').innerHTML = name === 'snapshots'
    ? '<b>配置工作台</b>'
    : `<b>${name === 'aes' ? 'AES 加解密' : '设置'}</b>`;
  if (name === 'aes') loadAESConfigIntoForm();
  if (name === 'settings') loadSettings();
}
```

4. Add the AES tools section (replace the placeholder wiring — keep function bodies concise):

```js
let aesConfigCache = null;

async function loadAESConfigIntoForm() {
  if (!aesConfigCache) {
    try { aesConfigCache = await api('/api/aes/config'); } catch (_) { aesConfigCache = { key: '', iv: '' }; }
  }
  if (aesConfigCache.key) { $('aes-key').value = aesConfigCache.key; $('aes-iv').value = aesConfigCache.iv; }
}

async function runAES(op) {
  const key = $('aes-key').value.trim();
  const iv = $('aes-iv').value.trim();
  const text = $('aes-input').value;
  if (!key || !iv) { showAESError('请填写 key 和 iv。'); return; }
  if (!text) { showAESError('请填写输入内容。'); return; }
  try {
    const data = await api('/api/aes/transform', jsonOptions('POST', { op, key, iv, text }));
    $('aes-output').value = data.result;
    showAESError('');
  } catch (err) { showAESError(err.message); }
}

async function genAESKey() {
  try {
    const data = await api('/api/aes/gen-key', jsonOptions('POST', { bytes: 16, iv_bytes: 12 }));
    $('aes-key').value = data.key;
    $('aes-iv').value = data.iv;
    showAESError('已生成 key/iv。');
  } catch (err) { showAESError(err.message); }
}

async function saveAESConfig() {
  const key = $('aes-key').value.trim();
  const iv = $('aes-iv').value.trim();
  if (!key || !iv) { showAESError('请填写 key 和 iv。'); return; }
  try {
    await api('/api/aes/config', jsonOptions('PUT', { key, iv }));
    aesConfigCache = { key, iv };
    showAESError('已保存到 aes.json。');
  } catch (err) { showAESError(err.message); }
}

function showAESError(msg) {
  $('aes-error').textContent = msg;
  $('aes-error').hidden = !msg;
}

function copyAESOutput() {
  const text = $('aes-output').value;
  if (!text) return;
  navigator.clipboard.writeText(text).then(() => showAESError('已复制到剪贴板。')).catch(() => showAESError('复制失败。'));
}
```

5. Add settings:

```js
async function loadSettings() {
  const status = $('key-status');
  const initBtn = $('init-key-btn');
  try {
    await api('/api/snapshots');
    status.textContent = '快照密钥可用（Keychain 或环境变量）。';
    status.className = 'status-line ok';
    initBtn.hidden = true;
  } catch (_) {
    status.textContent = '快照密钥不可用。点击下方按钮生成本机密钥。';
    status.className = 'status-line warn';
    initBtn.hidden = false;
  }
  try {
    const c = await api('/api/aes/config');
    const el = $('aes-config-status');
    if (c.key) {
      el.textContent = `已保存（${c.path}）。`;
      el.className = 'status-line ok';
      $('clear-aes-config-btn').hidden = false;
    } else {
      el.textContent = `未保存自定义 key/iv（${c.path}）。`;
      el.className = 'status-line';
      $('clear-aes-config-btn').hidden = true;
    }
  } catch (err) { showSettingsError(err.message); }
}

async function initKey() {
  try {
    await api('/api/init', jsonOptions('POST', { force: false }));
    $('init-key-btn').hidden = true;
    $('key-status').textContent = '快照密钥已生成。';
    $('key-status').className = 'status-line ok';
  } catch (err) { showSettingsError(err.message); }
}

async function clearAESConfig() {
  try {
    await api('/api/aes/config', { method: 'DELETE' });
    aesConfigCache = null;
    loadSettings();
  } catch (err) { showSettingsError(err.message); }
}

function showSettingsError(msg) {
  $('settings-error').textContent = msg;
  $('settings-error').hidden = !msg;
}
```

6. Add reveal handling:

```js
let revealItem = null;

function openReveal(item) {
  revealItem = item;
  $('reveal-key').textContent = item.key;
  $('reveal-value').textContent = '';
  $('reveal-value').hidden = true;
  $('reveal-confirm-btn').hidden = false;
  openDialog('reveal-dialog');
}

async function confirmReveal() {
  if (!revealItem) return;
  const key = revealItem.key;
  try {
    const data = await api(`/api/snapshots/${encodeURIComponent(state.active)}/reveal`,
      jsonOptions('POST', { targets: [key], confirm: true }));
    $('reveal-value').textContent = data.values[key] != null ? data.values[key] : '(空)';
    $('reveal-value').hidden = false;
    $('reveal-confirm-btn').hidden = true;
  } catch (err) {
    dialogError('reveal-dialog', err.message);
  }
}
```

7. Add export-to-clipboard (next to the existing download `confirmExport`):

```js
async function copyExport() {
  const name = state.active;
  if (!name) { dialogError('export-dialog', '当前没有已选快照。'); return; }
  try {
    const res = await api(`/api/snapshots/${encodeURIComponent(name)}/export`,
      jsonOptions('POST', { confirm: true }));
    const text = await res.text();
    await navigator.clipboard.writeText(text);
    closeDialog('export-dialog');
    addRecent(`复制 ${name}`);
  } catch (err) {
    dialogError('export-dialog', err.message);
  }
}
```

8. Add bulk edit:

```js
let editLoaded = false;

function openBulkEdit() {
  if (!state.active) { showError('当前没有已选快照。'); return; }
  $('edit-name').textContent = state.active;
  $('edit-text').value = '';
  $('edit-load-btn').hidden = true;
  $('edit-save-btn').hidden = true;
  editLoaded = false;
  openDialog('edit-dialog');
  $('edit-load-btn').textContent = '确认加载明文';
  $('edit-load-btn').hidden = false;
}

async function loadBulkEdit() {
  if (!state.active) return;
  try {
    const data = await api(`/api/snapshots/${encodeURIComponent(state.active)}/edit`,
      jsonOptions('POST', { confirm: true }));
    $('edit-text').value = data.text;
    $('edit-load-btn').hidden = true;
    $('edit-save-btn').hidden = false;
    editLoaded = true;
  } catch (err) {
    dialogError('edit-dialog', err.message);
  }
}

async function saveBulkEdit() {
  if (!state.active || !editLoaded) return;
  const text = $('edit-text').value;
  try {
    await api(`/api/snapshots/${encodeURIComponent(state.active)}/edit`,
      jsonOptions('PUT', { text }));
    closeDialog('edit-dialog');
    addRecent(`明文编辑 ${state.active}`);
    await refreshSnapshots(state.active);
  } catch (err) {
    dialogError('edit-dialog', err.message);
  }
}
```

9. In `renderTable`, add a reveal button to sensitive rows. Replace the sensitive branch of the row rendering:

```js
    if (it.sensitive) {
      tdVal.className = 'masked';
      tdVal.textContent = MASK;
      const i = document.createElement('i');
      i.textContent = `${it.length} 字符`;
      tdVal.appendChild(i);
      const btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'reveal-btn';
      btn.textContent = '解密显示';
      tdVal.appendChild(btn);
      btn.addEventListener('click', (e) => {
        e.stopPropagation();
        openReveal(it);
      });
    } else {
      tdVal.textContent = it.value || '';
    }
```

10. In `wire()`:
   - Add data-action cases `bulk-edit` → `openBulkEdit()` and `aes-tools` → `switchView('aes')`.
   - Add nav click handlers:

```js
  $('nav-aes').addEventListener('click', () => switchView('aes'));
  $('nav-settings').addEventListener('click', () => switchView('settings'));
```

   - Add AES tool wiring:

```js
  $('aes-encrypt-btn').addEventListener('click', () => runAES('encrypt'));
  $('aes-decrypt-btn').addEventListener('click', () => runAES('decrypt'));
  $('aes-gen-key').addEventListener('click', genAESKey);
  $('aes-load-config').addEventListener('click', loadAESConfigIntoForm);
  $('aes-save-config').addEventListener('click', saveAESConfig);
  $('aes-copy').addEventListener('click', copyAESOutput);
```

   - Add settings wiring:

```js
  $('init-key-btn').addEventListener('click', initKey);
  $('clear-aes-config-btn').addEventListener('click', clearAESConfig);
```

   - Add reveal/edit wiring:

```js
  $('reveal-confirm-btn').addEventListener('click', confirmReveal);
  $('edit-form').addEventListener('submit', (e) => { e.preventDefault(); saveBulkEdit(); });
  $('edit-load-btn').addEventListener('click', loadBulkEdit);
```

   - Add export-to-clipboard wiring:

```js
  $('export-copy-btn').addEventListener('click', copyExport);
```

11. In `init()`, after `wire()`, keep the snapshot default view: `switchView('snapshots')`.

12. Add a `.reveal-btn` style to `app.css` (append):

```css
.reveal-btn{margin-left:8px;border:1px solid var(--line);background:#fff;color:#984d36;font:11.5px inherit;border-radius:6px;padding:2px 8px;cursor:pointer}
.reveal-btn:hover{background:var(--accent-soft)}
```

- [ ] **Step 5: Run the static shell test and all UI tests**

Run: `go test ./internal/ui -run 'TestRootServesFullWorkspace|TestRootServesEmbeddedWorkspace' -v && go test ./internal/ui -v`
Expected: PASS.

- [ ] **Step 6: Smoke-test the server and API endpoints**

Run:

```sh
go build -o /tmp/vaulty-keeper-ui .
D=$(mktemp -d)
/tmp/vaulty-keeper-ui ui --dir "$D" --no-open --port 0 > /tmp/ui.log 2>&1 &
sleep 1
URL=$(grep -o 'http://127.0.0.1:[0-9]*' /tmp/ui.log | head -1)
curl -s -o /dev/null -w "GET / -> %{http_code}\n" "$URL/"
curl -s -X POST "$URL/api/aes/gen-key" -d '{"bytes":16,"iv_bytes":12}'
curl -s -X POST "$URL/api/init" -d '{"force":false}'
kill %1
rm -f /tmp/vaulty-keeper-ui
```

Expected: `GET / -> 200`; gen-key returns `{"key":"...","iv":"..."}`; init either succeeds (201, first run) or returns `409 key_exists` (if a Keychain key already exists — acceptable on a dev machine).

---

### Task 9: Update README and Run Full Verification

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update the README Web UI section**

Replace the `## 本地 Web UI` section with:

```md
## 本地 Web UI

```sh
vaulty-keeper ui
vaulty-keeper ui --dir /path/to/snapshots --port 8080
vaulty-keeper ui --no-open
```

- 仅监听 `127.0.0.1`，不会暴露到局域网。
- 覆盖全部功能：快照浏览/搜索/增删改、导入、环境对比、明文编辑、导出、AES 加解密、密钥初始化。
- 明文出口（reveal / 明文编辑 / 导出 / AES 解密）均需二次确认后才显示，响应 `Cache-Control: no-store`，浏览器不持久化明文。
- 浏览器端不持久化快照内容。
```

Also update the opening paragraph (README line 3): change `vaulty-keeper ui` 提供本地 Web UI 浏览快照（仅本机监听）` to state that the UI covers all snapshot and AES tools (still loopback-only). The `## 其他` command list entry for `vaulty-keeper ui` stays as-is.

- [ ] **Step 2: Run gofmt on all Go files**

Run: `gofmt -l internal/app internal/cli internal/ui`
Expected: no output.

- [ ] **Step 3: Run the full test suite**

Run: `go test ./...`
Expected: all packages PASS.

- [ ] **Step 4: Run go vet**

Run: `go vet ./...`
Expected: exits 0.

- [ ] **Step 5: Perform the focused manual safety check**

Run `vaulty-keeper ui` against temporary snapshots whose sensitive values are distinctive strings, then verify in the browser (or via curl):

1. `GET /api/snapshots/prod` contains neither sensitive plaintext nor ciphertext.
2. `GET /api/compare?from=prod&to=test` contains neither old nor new sensitive plaintext.
3. Reveal shows plaintext only after the confirmation dialog is accepted; the dialog input is never prefilled.
4. Bulk edit loads plaintext only after confirmation and re-encrypts on save (reload shows the new values; sensitive values still masked).
5. AES tools: encrypt → decrypt round-trips; gen-key fills the form; save-to-aes.json persists across reload.
6. Settings: with no Keychain key, "生成快照密钥" appears and works; with a key, it is hidden.
7. Every plaintext response carries `Cache-Control: no-store`; the server URL begins with `http://127.0.0.1:`.
