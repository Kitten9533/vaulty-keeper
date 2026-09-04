package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"vaulty-keeper/internal/aesx"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	w.Close()
	b, _ := io.ReadAll(r)
	return string(b)
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()
	fn()
	w.Close()
	b, _ := io.ReadAll(r)
	return string(b)
}

// asTTY makes commands run as if stdin were a terminal, bypassing the
// non-TTY plaintext gates, so tests focus on the behavior under test.
func asTTY(t *testing.T) {
	t.Helper()
	old := isTerminalFunc
	isTerminalFunc = func() bool { return true }
	t.Cleanup(func() { isTerminalFunc = old })
}

func TestNormalizeArgs(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	_ = fs.String("name", "", "")
	_ = fs.Bool("reveal", false, "")
	_ = fs.String("dir", "", "")

	cases := [][]string{
		{"--name", "prod", "file.txt"},
		{"file.txt", "--name", "prod", "--dir", "/tmp/x"},
		{"a", "--reveal", "b", "--name=prod"},
		// values that start with "-" stay positional
		{"set", "prod", "LIMIT", "-1"},
		{"a", "--reveal", "-b", "c"},
		// a registered flag's value may itself look like a flag
		{"file.txt", "--name", "-prod", "--dir", "/tmp/x"},
	}
	want := [][]string{
		{"--name", "prod", "file.txt"},
		{"--name", "prod", "--dir", "/tmp/x", "file.txt"},
		{"--reveal", "--name=prod", "a", "b"},
		{"set", "prod", "LIMIT", "-1"},
		{"--reveal", "a", "-b", "c"},
		{"--name", "-prod", "--dir", "/tmp/x", "file.txt"},
	}
	for i, c := range cases {
		got := normalizeArgs(fs, c)
		if !reflect.DeepEqual(got, want[i]) {
			t.Errorf("case %d: got %v want %v", i, got, want[i])
		}
	}
}

func TestIsBoolFlag(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.Bool("reveal", false, "")
	fs.String("name", "", "")
	if !isBoolFlag(fs, "--reveal") {
		t.Error("--reveal should be bool")
	}
	if isBoolFlag(fs, "--name") {
		t.Error("--name should not be bool")
	}
	if isBoolFlag(fs, "--nope") {
		t.Error("unknown flag should not be bool")
	}
}

// Runs the full import/list/set/compare pipeline with an env-injected key
// (no Keychain dependency) and a temp snapshot dir.
func TestApolloPipeline(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("VAULTY_KEEPER_APOLLO_KEY", key)
	t.Setenv("VAULTY_KEEPER_SENSITIVE_KEY", key)
	dir := t.TempDir()
	snap := filepath.Join(dir, "snap")

	in := filepath.Join(dir, "paste.txt")
	os.WriteFile(in, []byte("# comment\nFOO = 1\nBAR = 2\nSECRET_TOKEN = abc\n"), 0o600)

	if code := Run([]string{"apollo", "import", in, "--name", "prod", "--dir", snap, "--app-id", "app-x"}); code != 0 {
		t.Fatalf("import failed with code %d", code)
	}
	if code := Run([]string{"apollo", "import", in, "--name", "test", "--dir", snap, "--app-id", "app-x"}); code != 0 {
		t.Fatalf("import test failed with code %d", code)
	}
	if code := Run([]string{"apollo", "set", "test", "FOO", "2", "--dir", snap, "--appid", "app-x"}); code != 0 {
		t.Fatalf("set failed with code %d", code)
	}

	// get should print plaintext
	if code := Run([]string{"apollo", "get", "prod", "FOO", "--dir", snap, "--appid", "app-x"}); code != 0 {
		t.Fatalf("get failed with code %d", code)
	}
	// compare prod vs test: FOO changed 1->2 (masked output is fine)
	if code := Run([]string{"apollo", "compare", "prod", "test", "--dir", snap, "--appid", "app-x", "--appid-to", "app-x"}); code != 0 {
		t.Fatalf("compare failed with code %d", code)
	}

	// snapshot file must not contain plaintext values
	raw, err := os.ReadFile(filepath.Join(snap, "prod__app-x.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("abc")) {
		t.Error("snapshot file contains plaintext secret")
	}
}

// TestPlainMarkGuardRefusesSensitiveKey verifies that marking a
// sensitive-looking key as safe (--plain) is refused in non-TTY contexts,
// so an AI cannot self-authorize plaintext reads.
func TestPlainMarkGuardRefusesSensitiveKey(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("VAULTY_KEEPER_APOLLO_KEY", key)
	t.Setenv("VAULTY_KEEPER_SENSITIVE_KEY", key)
	dir := t.TempDir()
	snap := filepath.Join(dir, "snap")
	in := filepath.Join(dir, "paste.txt")
	os.WriteFile(in, []byte("APP_NAME = merdi\nSECRET_TOKEN = abc\n"), 0o600)
	if code := Run([]string{"apollo", "import", in, "--name", "prod", "--dir", snap, "--app-id", "app-x"}); code != 0 {
		t.Fatalf("import failed with code %d", code)
	}

	old := isTerminalFunc
	isTerminalFunc = func() bool { return false }
	defer func() { isTerminalFunc = old }()

	if code := Run([]string{"apollo", "set", "prod", "SECRET_TOKEN", "xyz", "--plain", "--dir", snap, "--appid", "app-x"}); code == 0 {
		t.Fatal("set --plain on sensitive key succeeded in non-TTY, want refusal")
	}
	if code := Run([]string{"apollo", "mark", "prod", "SECRET_TOKEN", "--plain", "--dir", snap, "--appid", "app-x"}); code == 0 {
		t.Fatal("mark --plain on sensitive key succeeded in non-TTY, want refusal")
	}
	// a genuinely safe key is still allowed
	if code := Run([]string{"apollo", "set", "prod", "APP_NAME", "other", "--plain", "--dir", snap, "--appid", "app-x"}); code != 0 {
		t.Fatalf("set --plain on safe key failed with code %d", code)
	}
	if code := Run([]string{"apollo", "mark", "prod", "APP_NAME", "--plain", "--dir", snap, "--appid", "app-x"}); code != 0 {
		t.Fatalf("mark --plain on safe key failed with code %d", code)
	}
}

func TestImportAutoName(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("VAULTY_KEEPER_APOLLO_KEY", key)
	t.Setenv("VAULTY_KEEPER_SENSITIVE_KEY", key)
	dir := t.TempDir()
	snap := filepath.Join(dir, "snap")
	in := filepath.Join(dir, "merdi_portal.txt")
	os.WriteFile(in, []byte("FOO = 1\n"), 0o600)

	if code := Run([]string{"apollo", "import", in, "--dir", snap, "--app-id", "app-x"}); code != 0 {
		t.Fatalf("import failed with code %d", code)
	}
	if _, err := os.Stat(filepath.Join(snap, "merdi_portal__app-x.json")); err != nil {
		t.Fatalf("snapshot not auto-named from filename: %v", err)
	}
}

func TestImportRefusesOverwriteWithoutForce(t *testing.T) {
	old := isTerminalFunc
	isTerminalFunc = func() bool { return false }
	defer func() { isTerminalFunc = old }()

	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("VAULTY_KEEPER_APOLLO_KEY", key)
	t.Setenv("VAULTY_KEEPER_SENSITIVE_KEY", key)
	dir := t.TempDir()
	snap := filepath.Join(dir, "snap")
	in := filepath.Join(dir, "paste.txt")
	os.WriteFile(in, []byte("FOO = 1\n"), 0o600)

	if code := Run([]string{"apollo", "import", in, "--name", "prod", "--dir", snap, "--app-id", "app-x"}); code != 0 {
		t.Fatalf("first import failed with code %d", code)
	}
	// overwrite without --force on non-TTY is refused
	os.WriteFile(in, []byte("FOO = 2\n"), 0o600)
	err := captureStderr(t, func() {
		if code := Run([]string{"apollo", "import", in, "--name", "prod", "--dir", snap, "--app-id", "app-x"}); code != 1 {
			t.Fatalf("second import returned %d, want 1", code)
		}
	})
	if !strings.Contains(err, "--force") {
		t.Fatalf("stderr %q missing --force hint", err)
	}
	// value unchanged (marked plain so it stays readable when piped)
	if code := Run([]string{"apollo", "set", "prod", "FOO", "1", "--plain", "--dir", snap, "--appid", "app-x"}); code != 0 {
		t.Fatalf("set FOO --plain failed with code %d", code)
	}
	out := captureStdout(t, func() {
		if code := Run([]string{"apollo", "get", "prod", "FOO", "--dir", snap, "--appid", "app-x"}); code != 0 {
			t.Fatalf("get failed with code %d", code)
		}
	})
	if strings.TrimSpace(out) != "1" {
		t.Fatalf("FOO = %q, want 1 (unchanged)", out)
	}
	// --force overwrites
	if code := Run([]string{"apollo", "import", in, "--name", "prod", "--dir", snap, "--app-id", "app-x", "--force"}); code != 0 {
		t.Fatalf("import --force failed with code %d", code)
	}
	if code := Run([]string{"apollo", "set", "prod", "FOO", "2", "--plain", "--dir", snap, "--appid", "app-x"}); code != 0 {
		t.Fatalf("set FOO --plain failed with code %d", code)
	}
	out = captureStdout(t, func() {
		if code := Run([]string{"apollo", "get", "prod", "FOO", "--dir", snap, "--appid", "app-x"}); code != 0 {
			t.Fatalf("get failed with code %d", code)
		}
	})
	if strings.TrimSpace(out) != "2" {
		t.Fatalf("FOO = %q, want 2 after --force", out)
	}
}

func TestReveal(t *testing.T) {
	asTTY(t)
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("VAULTY_KEEPER_APOLLO_KEY", key)
	t.Setenv("VAULTY_KEEPER_SENSITIVE_KEY", key)
	dir := t.TempDir()
	snap := filepath.Join(dir, "snap")

	aesKey := "MyAesSecretKey12" // 16 bytes
	aesIV := "0123456789abcdef"  // 16 bytes
	ct, err := aesx.Encrypt(aesKey, aesIV, "LTAI5t-secret-SK")
	if err != nil {
		t.Fatal(err)
	}
	in := filepath.Join(dir, "paste.txt")
	os.WriteFile(in, []byte("imile.fs.oss.secret-key = "+ct+"\nSECRET_TOKEN = plain-secret\n"), 0o600)
	if code := Run([]string{"apollo", "import", in, "--name", "prod", "--dir", snap, "--app-id", "app-x"}); code != 0 {
		t.Fatalf("import failed with code %d", code)
	}

	// reveal of a sensitive value shows its plaintext (decrypted with the
	// sensitive key); an external ciphertext value is returned as stored
	out := captureStdout(t, func() {
		if code := Run([]string{"apollo", "reveal", "prod", "SECRET_TOKEN", "--dir", snap, "--appid", "app-x"}); code != 0 {
			t.Fatalf("reveal failed with code %d", code)
		}
	})
	if strings.TrimSpace(out) != "plain-secret" {
		t.Errorf("reveal sensitive got %q", out)
	}
	out = captureStdout(t, func() {
		if code := Run([]string{"apollo", "reveal", "prod", "imile.fs.oss.secret-key", "--dir", snap, "--appid", "app-x"}); code != 0 {
			t.Fatalf("reveal ciphertext failed with code %d", code)
		}
	})
	if strings.TrimSpace(out) != ct {
		t.Errorf("reveal without override got %q, want stored ciphertext", out)
	}

	// reveal with explicit --key/--iv override decrypts the external ciphertext
	out = captureStdout(t, func() {
		if code := Run([]string{"apollo", "reveal", "prod", "imile.fs.oss.secret-key", "--key", aesKey, "--iv", aesIV, "--dir", snap, "--appid", "app-x"}); code != 0 {
			t.Fatalf("reveal --key failed with code %d", code)
		}
	})
	if strings.TrimSpace(out) != "LTAI5t-secret-SK" {
		t.Errorf("reveal --key got %q", out)
	}

	// multi-key json
	ctAK, err := aesx.Encrypt(aesKey, aesIV, "LTAI5t-secret-AK")
	if err != nil {
		t.Fatal(err)
	}
	if code := Run([]string{"apollo", "set", "prod", "imile.fs.oss.access-key-id", ctAK, "--dir", snap, "--appid", "app-x"}); code != 0 {
		t.Fatalf("set failed with code %d", code)
	}
	out = captureStdout(t, func() {
		if code := Run([]string{"apollo", "reveal", "prod", "imile.fs.oss.secret-key", "imile.fs.oss.access-key-id", "--key", aesKey, "--iv", aesIV, "--json", "--dir", snap, "--appid", "app-x"}); code != 0 {
			t.Fatalf("reveal --json failed with code %d", code)
		}
	})
	var m map[string]string
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("reveal --json not valid json: %v\n%s", err, out)
	}
	if m["imile.fs.oss.secret-key"] != "LTAI5t-secret-SK" || m["imile.fs.oss.access-key-id"] != "LTAI5t-secret-AK" {
		t.Errorf("reveal --json got %v", m)
	}
}

func TestRevealPlainValue(t *testing.T) {
	asTTY(t)
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("VAULTY_KEEPER_APOLLO_KEY", key)
	t.Setenv("VAULTY_KEEPER_SENSITIVE_KEY", key)
	dir := t.TempDir()
	snap := filepath.Join(dir, "snap")
	in := filepath.Join(dir, "paste.txt")
	os.WriteFile(in, []byte("FOO = bar\n"), 0o600)
	if code := Run([]string{"apollo", "import", in, "--name", "prod", "--dir", snap, "--app-id", "app-x"}); code != 0 {
		t.Fatalf("import failed with code %d", code)
	}
	// reveal of a plain non-sensitive value decrypts with the snapshot key
	out := captureStdout(t, func() {
		if code := Run([]string{"apollo", "reveal", "prod", "FOO", "--dir", snap, "--appid", "app-x"}); code != 0 {
			t.Fatalf("reveal FOO failed with code %d", code)
		}
	})
	if strings.TrimSpace(out) != "bar" {
		t.Errorf("reveal FOO got %q", out)
	}
}

func TestEdit(t *testing.T) {
	asTTY(t)
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("VAULTY_KEEPER_APOLLO_KEY", key)
	t.Setenv("VAULTY_KEEPER_SENSITIVE_KEY", key)
	dir := t.TempDir()
	snap := filepath.Join(dir, "snap")
	in := filepath.Join(dir, "paste.txt")
	os.WriteFile(in, []byte("FOO = 1\nBAR = 2\n"), 0o600)
	if code := Run([]string{"apollo", "import", in, "--name", "prod", "--dir", snap, "--app-id", "app-x"}); code != 0 {
		t.Fatalf("import failed with code %d", code)
	}

	// fake editor: appends a new key and changes FOO
	editor := filepath.Join(dir, "editor.sh")
	os.WriteFile(editor, []byte("#!/bin/sh\nsed 's/FOO = 1/FOO = 9/' \"$1\" > \"$1.tmp\" && mv \"$1.tmp\" \"$1\"\nprintf 'NEW_KEY = 3\\n' >> \"$1\"\n"), 0o700)

	if code := Run([]string{"apollo", "edit", "prod", "--editor", editor, "--dir", snap, "--appid", "app-x"}); code != 0 {
		t.Fatalf("edit failed with code %d", code)
	}

	out := captureStdout(t, func() {
		if code := Run([]string{"apollo", "get", "prod", "FOO", "--dir", snap, "--appid", "app-x"}); code != 0 {
			t.Fatalf("get failed with code %d", code)
		}
	})
	if strings.TrimSpace(out) != "9" {
		t.Errorf("FOO after edit = %q, want 9", out)
	}
	out = captureStdout(t, func() {
		if code := Run([]string{"apollo", "get", "prod", "NEW_KEY", "--dir", snap, "--appid", "app-x"}); code != 0 {
			t.Fatalf("get NEW_KEY failed with code %d", code)
		}
	})
	if strings.TrimSpace(out) != "3" {
		t.Errorf("NEW_KEY after edit = %q, want 3", out)
	}
}

func TestListAndCompareJSON(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("VAULTY_KEEPER_APOLLO_KEY", key)
	t.Setenv("VAULTY_KEEPER_SENSITIVE_KEY", key)
	dir := t.TempDir()
	snap := filepath.Join(dir, "snap")
	in := filepath.Join(dir, "paste.txt")
	os.WriteFile(in, []byte("FOO = 1\nSECRET_TOKEN = abc\n"), 0o600)
	if code := Run([]string{"apollo", "import", in, "--name", "prod", "--dir", snap, "--app-id", "app-x"}); code != 0 {
		t.Fatalf("import failed with code %d", code)
	}
	if code := Run([]string{"apollo", "import", in, "--name", "test", "--dir", snap, "--app-id", "app-x"}); code != 0 {
		t.Fatalf("import test failed with code %d", code)
	}
	if code := Run([]string{"apollo", "set", "test", "FOO", "2", "--dir", snap, "--appid", "app-x"}); code != 0 {
		t.Fatalf("set failed with code %d", code)
	}
	// mark FOO safe so non-TTY list can reveal it; SECRET_TOKEN stays masked
	if code := Run([]string{"apollo", "mark", "prod", "FOO", "--plain", "--dir", snap, "--appid", "app-x"}); code != 0 {
		t.Fatalf("mark FOO --plain failed with code %d", code)
	}
	if code := Run([]string{"apollo", "mark", "test", "FOO", "--plain", "--dir", snap, "--appid", "app-x"}); code != 0 {
		t.Fatalf("mark test FOO --plain failed with code %d", code)
	}

	// list --json: secret masked
	out := captureStdout(t, func() {
		if code := Run([]string{"apollo", "list", "prod", "--json", "--dir", snap, "--appid", "app-x"}); code != 0 {
			t.Fatalf("list --json failed with code %d", code)
		}
	})
	var lm struct {
		Name  string            `json:"name"`
		Items map[string]string `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &lm); err != nil {
		t.Fatalf("list --json not valid json: %v\n%s", err, out)
	}
	if lm.Items["FOO"] != "1" {
		t.Errorf("list json FOO = %q", lm.Items["FOO"])
	}
	if !strings.HasPrefix(lm.Items["SECRET_TOKEN"], "***") {
		t.Errorf("list json SECRET_TOKEN should be masked, got %q", lm.Items["SECRET_TOKEN"])
	}

	// compare --json
	out = captureStdout(t, func() {
		if code := Run([]string{"apollo", "compare", "prod", "test", "--json", "--dir", snap, "--appid", "app-x", "--appid-to", "app-x"}); code != 0 {
			t.Fatalf("compare --json failed with code %d", code)
		}
	})
	var cm struct {
		Changed map[string]any `json:"changed"`
	}
	if err := json.Unmarshal([]byte(out), &cm); err != nil {
		t.Fatalf("compare --json not valid json: %v\n%s", err, out)
	}
	if _, ok := cm.Changed["FOO"]; !ok {
		t.Errorf("compare --json missing FOO: %s", out)
	}
}

func TestApolloRejectsUnsafeSnapshotName(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("VAULTY_KEEPER_APOLLO_KEY", key)
	t.Setenv("VAULTY_KEEPER_SENSITIVE_KEY", key)
	err := captureStderr(t, func() {
		if code := Run([]string{"apollo", "list", "../outside", "--dir", t.TempDir(), "--appid", "app-x"}); code != 1 {
			t.Fatalf("list with unsafe name returned %d, want 1", code)
		}
	})
	if !strings.Contains(err, "快照名必须以字母或数字开头") {
		t.Fatalf("stderr %q missing name validation message", err)
	}
}

func TestGenKey(t *testing.T) {
	out := captureStdout(t, func() {
		if code := Run([]string{"aes", "gen-key"}); code != 0 {
			t.Fatalf("gen-key failed with code %d", code)
		}
	})
	var key, iv string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.HasPrefix(line, "SECRET_KEY: ") {
			key = strings.TrimPrefix(line, "SECRET_KEY: ")
		}
		if strings.HasPrefix(line, "IV: ") {
			iv = strings.TrimPrefix(line, "IV: ")
		}
	}
	if len(key) != 16 || len(iv) != 16 {
		t.Fatalf("gen-key got key=%q(%d) iv=%q(%d)", key, len(key), iv, len(iv))
	}
	// generated key must actually work
	ct, err := aesx.Encrypt(key, iv, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if pt, err := aesx.Decrypt(key, iv, ct); err != nil || pt != "hello" {
		t.Fatalf("gen-key key/iv unusable: %v %q", err, pt)
	}
}

func TestUIOptions(t *testing.T) {
	port, err := parseUIPort("0")
	if err != nil || port != 0 {
		t.Fatalf("parseUIPort(0) = %d, %v", port, err)
	}
	if _, err := parseUIPort("70000"); err == nil {
		t.Fatal("parseUIPort(70000) succeeded")
	}
}

// TestEnsureKeys exercises checkKey's three branches: key present (silent),
// missing on non-TTY (hint only), missing on TTY with confirm (init runs).
func TestEnsureKeys(t *testing.T) {
	// key available → silent, init never called
	called := false
	out := captureStdout(t, func() {
		checkKey("快照", "vaulty-keeper apollo init", func() bool { return true },
			func() error { called = true; return nil })
	})
	if called || out != "" {
		t.Fatalf("available key should be silent (called=%v out=%q)", called, out)
	}

	// missing + non-TTY → hint on stderr, init never called
	called = false
	errOut := captureStderr(t, func() {
		checkKey("快照", "vaulty-keeper apollo init", func() bool { return false },
			func() error { called = true; return nil })
	})
	if called {
		t.Error("init must not run on non-TTY")
	}
	if !strings.Contains(errOut, "vaulty-keeper apollo init") {
		t.Errorf("missing init hint, got %q", errOut)
	}

	// missing + TTY + confirm → init runs and reports created
	asTTY(t)
	oldConfirm := confirmKeyInit
	confirmKeyInit = func(string) bool { return true }
	t.Cleanup(func() { confirmKeyInit = oldConfirm })
	called = false
	out = captureStdout(t, func() {
		checkKey("快照", "vaulty-keeper apollo init", func() bool { return false },
			func() error { called = true; return nil })
	})
	if !called {
		t.Error("init should run after TTY confirm")
	}
	if !strings.Contains(out, "已创建") {
		t.Errorf("expected created message, got %q", out)
	}

	// missing + TTY but declined → init not called
	confirmKeyInit = func(string) bool { return false }
	called = false
	captureStdout(t, func() {
		checkKey("快照", "vaulty-keeper apollo init", func() bool { return false },
			func() error { called = true; return nil })
	})
	if called {
		t.Error("init must not run when the user declines")
	}
}

// TestEnsureDirsAndAESConfig verifies a fresh setup creates ~/.vaulty/apollo
// and seeds aes.json with a "default" entry on the first bare run.
func TestEnsureDirsAndAESConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ensureDirs()
	apolloDir := filepath.Join(home, ".vaulty", "apollo")
	if fi, err := os.Stat(apolloDir); err != nil || !fi.IsDir() {
		t.Fatalf("~/.vaulty/apollo not created: %v", err)
	}

	out := captureStdout(t, func() { ensureAESConfig() })
	p := filepath.Join(home, ".vaulty", "aes.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("aes.json not created: %v", err)
	}
	if !strings.Contains(string(b), "default") || strings.Contains(string(b), `[]`) {
		t.Errorf("expected a seeded default entry, got %s", b)
	}
	if !strings.Contains(out, "已初始化") {
		t.Errorf("expected init message, got %q", out)
	}

	// existing entries → silent, no rewrite
	out = captureStdout(t, func() { ensureAESConfig() })
	if out != "" {
		t.Errorf("existing config should be silent, got %q", out)
	}
}

func TestCompletion(t *testing.T) {
	for _, shell := range []string{"zsh", "bash", "fish"} {
		out := captureStdout(t, func() {
			if code := Run([]string{"completion", shell}); code != 0 {
				t.Fatalf("completion %s failed with code %d", shell, code)
			}
		})
		if !strings.Contains(out, "apollo") {
			t.Errorf("completion %s missing apollo", shell)
		}
	}
}

// TestHelpDoesNotRunCommand guards against -h printing help and then
// continuing to run the command (the old parseFlags behavior let
// `aes gen-key -h` generate a key and `apollo list -h` list snapshots).
func TestHelpDoesNotRunCommand(t *testing.T) {
	// apollo list -h must print the syntax line, not list real snapshots.
	out := captureStdout(t, func() {
		if code := Run([]string{"apollo", "list", "-h"}); code != 0 {
			t.Fatalf("apollo list -h returned code %d", code)
		}
	})
	if !strings.Contains(out, "vaulty-keeper apollo list") {
		t.Errorf("-h output missing syntax line:\n%s", out)
	}
	if strings.Contains(out, "test (a)") {
		t.Errorf("apollo list -h ran the command and listed snapshots:\n%s", out)
	}

	// aes gen-key -h must not actually generate and print a key.
	out = captureStdout(t, func() {
		if code := Run([]string{"aes", "gen-key", "-h"}); code != 0 {
			t.Fatalf("aes gen-key -h returned code %d", code)
		}
	})
	if !strings.Contains(out, "vaulty-keeper aes gen-key") {
		t.Errorf("-h output missing syntax line:\n%s", out)
	}
	if strings.Contains(out, "SECRET_KEY") {
		t.Errorf("aes gen-key -h actually generated a key:\n%s", out)
	}

	// TTY-only commands must print help instead of being refused.
	out = captureStdout(t, func() {
		if code := Run([]string{"db", "show", "-h"}); code != 0 {
			t.Fatalf("db show -h returned code %d", code)
		}
	})
	if !strings.Contains(out, "vaulty-keeper db show") {
		t.Errorf("db show -h output missing syntax line:\n%s", out)
	}
}

// TestImportAppID verifies --appid works and --app-id stays as a
// compatibility alias.
func TestImportAppID(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("VAULTY_KEEPER_APOLLO_KEY", key)
	t.Setenv("VAULTY_KEEPER_SENSITIVE_KEY", key)
	dir := t.TempDir()
	snap := filepath.Join(dir, "snap")
	in := filepath.Join(dir, "paste.txt")
	os.WriteFile(in, []byte("FOO = 1\n"), 0o600)

	for _, flag := range []string{"--appid", "--app-id"} {
		if code := Run([]string{"apollo", "import", in, "--name", "prod-" + flag[2:], flag, "app-x", "--dir", snap}); code != 0 {
			t.Fatalf("import with %s failed with code %d", flag, code)
		}
	}
	if _, err := os.Stat(filepath.Join(snap, "prod-appid__app-x.json")); err != nil {
		t.Fatalf("import with --appid did not write the snapshot: %v", err)
	}
	if _, err := os.Stat(filepath.Join(snap, "prod-app-id__app-x.json")); err != nil {
		t.Fatalf("import with --app-id (alias) did not write the snapshot: %v", err)
	}
}

func TestApolloRm(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("VAULTY_KEEPER_APOLLO_KEY", key)
	t.Setenv("VAULTY_KEEPER_SENSITIVE_KEY", key)
	dir := t.TempDir()
	snap := filepath.Join(dir, "snap")
	in := filepath.Join(dir, "paste.txt")
	os.WriteFile(in, []byte("FOO = 1\n"), 0o600)

	if code := Run([]string{"apollo", "import", in, "--name", "prod", "--app-id", "app-x", "--dir", snap}); code != 0 {
		t.Fatalf("import failed with code %d", code)
	}
	// without --yes the snapshot must not be deleted (interactive or refused)
	Run([]string{"apollo", "rm", "prod", "--appid", "app-x", "--dir", snap})
	if _, err := os.Stat(filepath.Join(snap, "prod__app-x.json")); err != nil {
		t.Fatalf("snapshot removed without confirmation: %v", err)
	}
	// --yes deletes
	if code := Run([]string{"apollo", "rm", "prod", "--appid", "app-x", "--yes", "--dir", snap}); code != 0 {
		t.Fatalf("rm --yes failed with code %d", code)
	}
	if _, err := os.Stat(filepath.Join(snap, "prod__app-x.json")); !os.IsNotExist(err) {
		t.Fatal("snapshot still exists after rm --yes")
	}
	// removing a missing snapshot errors
	if code := Run([]string{"apollo", "rm", "prod", "--appid", "app-x", "--yes", "--dir", snap}); code != 1 {
		t.Fatalf("rm missing returned %d, want 1", code)
	}
	// missing --appid errors
	if code := Run([]string{"apollo", "rm", "prod", "--yes", "--dir", snap}); code != 1 {
		t.Fatalf("rm without --appid returned %d, want 1", code)
	}
}

func TestPlaintextCommandsRejectedWhenPiped(t *testing.T) {
	asPiped(t)
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("VAULTY_KEEPER_APOLLO_KEY", key)
	t.Setenv("VAULTY_KEEPER_SENSITIVE_KEY", key)
	dir := t.TempDir()
	snap := filepath.Join(dir, "snap")
	in := filepath.Join(dir, "paste.txt")
	os.WriteFile(in, []byte("SECRET_TOKEN = abc\nFOO = 1\n"), 0o600)
	if code := Run([]string{"apollo", "import", in, "--name", "prod", "--dir", snap, "--app-id", "app-x"}); code != 0 {
		t.Fatalf("import failed with code %d", code)
	}
	ct, err := aesx.Encrypt("0123456789abcdef", "abcdefghijklmnop", "hello")
	if err != nil {
		t.Fatal(err)
	}
	noopEditor := filepath.Join(dir, "noop-editor.sh")
	os.WriteFile(noopEditor, []byte("#!/bin/sh\nexit 0\n"), 0o700)

	// plaintext commands are refused in non-TTY contexts even with --yes
	cases := [][]string{
		{"apollo", "list", "prod", "--reveal", "--dir", snap, "--appid", "app-x"},
		{"apollo", "list", "prod", "--reveal", "--yes", "--dir", snap, "--appid", "app-x"},
		{"apollo", "compare", "prod", "prod", "--reveal", "--dir", snap, "--appid", "app-x", "--appid-to", "app-x"},
		{"apollo", "compare", "prod", "prod", "--reveal", "--yes", "--dir", snap, "--appid", "app-x", "--appid-to", "app-x"},
		{"apollo", "export", "prod", "--dir", snap, "--appid", "app-x"},
		{"apollo", "export", "prod", "--yes", "--dir", snap, "--appid", "app-x"},
		{"apollo", "edit", "prod", "--editor", noopEditor, "--dir", snap, "--appid", "app-x"},
		{"apollo", "edit", "prod", "--editor", noopEditor, "--yes", "--dir", snap, "--appid", "app-x"},
		{"aes", "decrypt", "--key", "0123456789abcdef", "--iv", "abcdefghijklmnop", ct},
		{"aes", "decrypt", "--key", "0123456789abcdef", "--iv", "abcdefghijklmnop", "--yes", ct},
	}
	for _, args := range cases {
		if code := Run(args); code != 1 {
			t.Fatalf("non-TTY plaintext %v returned %d, want 1", args, code)
		}
	}

	// reverse default: get on a non-safe key is masked when piped, never
	// plaintext, and never an error (the key exists, its value is hidden)
	for _, keyName := range []string{"SECRET_TOKEN", "FOO"} {
		out := captureStdout(t, func() {
			if code := Run([]string{"apollo", "get", "prod", keyName, "--dir", snap, "--appid", "app-x"}); code != 0 {
				t.Fatalf("get %s failed with code %d", keyName, code)
			}
		})
		if strings.TrimSpace(out) != "*** (3 chars)" && !strings.Contains(out, "***") {
			t.Fatalf("get %s leaked plaintext or unexpected output: %q", keyName, out)
		}
	}
	// a key explicitly marked safe (--plain) is readable when piped
	if code := Run([]string{"apollo", "set", "prod", "FOO", "1", "--plain", "--dir", snap, "--appid", "app-x"}); code != 0 {
		t.Fatalf("set --plain failed with code %d", code)
	}
	out := captureStdout(t, func() {
		if code := Run([]string{"apollo", "get", "prod", "FOO", "--dir", snap, "--appid", "app-x"}); code != 0 {
			t.Fatalf("get safe FOO failed with code %d", code)
		}
	})
	if strings.TrimSpace(out) != "1" {
		t.Fatalf("get safe FOO = %q, want 1", out)
	}
	// encrypt is not plaintext and works when piped
	if code := Run([]string{"aes", "encrypt", "--key", "0123456789abcdef", "--iv", "abcdefghijklmnop", "hello"}); code != 0 {
		t.Fatalf("aes encrypt failed with code %d", code)
	}
}

func TestPlaintextCommandsAvailableOnTTY(t *testing.T) {
	asTTY(t)
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("VAULTY_KEEPER_APOLLO_KEY", key)
	t.Setenv("VAULTY_KEEPER_SENSITIVE_KEY", key)
	dir := t.TempDir()
	snap := filepath.Join(dir, "snap")
	in := filepath.Join(dir, "paste.txt")
	os.WriteFile(in, []byte("SECRET_TOKEN = abc\nFOO = 1\n"), 0o600)
	if code := Run([]string{"apollo", "import", in, "--name", "prod", "--dir", snap, "--app-id", "app-x"}); code != 0 {
		t.Fatalf("import failed with code %d", code)
	}
	ct, err := aesx.Encrypt("0123456789abcdef", "abcdefghijklmnop", "hello")
	if err != nil {
		t.Fatal(err)
	}
	noopEditor := filepath.Join(dir, "noop-editor.sh")
	os.WriteFile(noopEditor, []byte("#!/bin/sh\nexit 0\n"), 0o700)

	// plaintext commands run in an interactive terminal
	cases := [][]string{
		{"apollo", "get", "prod", "SECRET_TOKEN", "--dir", snap, "--appid", "app-x"},
		{"apollo", "list", "prod", "--reveal", "--dir", snap, "--appid", "app-x"},
		{"apollo", "compare", "prod", "prod", "--reveal", "--dir", snap, "--appid", "app-x", "--appid-to", "app-x"},
		{"apollo", "export", "prod", "--dir", snap, "--appid", "app-x"},
		{"apollo", "edit", "prod", "--editor", noopEditor, "--dir", snap, "--appid", "app-x"},
		{"aes", "decrypt", "--key", "0123456789abcdef", "--iv", "abcdefghijklmnop", ct},
	}
	for _, args := range cases {
		if code := Run(args); code != 0 {
			t.Fatalf("TTY plaintext %v returned %d, want 0", args, code)
		}
	}
}

// asPiped makes commands run as if stdin were a pipe, so the non-TTY
// plaintext gates apply.
func asPiped(t *testing.T) {
	t.Helper()
	old := isTerminalFunc
	isTerminalFunc = func() bool { return false }
	t.Cleanup(func() { isTerminalFunc = old })
}

func TestReverseDefaultAndMark(t *testing.T) {
	asPiped(t)
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("VAULTY_KEEPER_APOLLO_KEY", key)
	t.Setenv("VAULTY_KEEPER_SENSITIVE_KEY", key)
	dir := t.TempDir()
	snap := filepath.Join(dir, "snap")
	in := filepath.Join(dir, "paste.txt")
	os.WriteFile(in, []byte("FOO = 1\nBAR = 2\nSECRET_TOKEN = abc\n"), 0o600)
	if code := Run([]string{"apollo", "import", in, "--name", "prod", "--dir", snap, "--app-id", "app-x"}); code != 0 {
		t.Fatalf("import failed with code %d", code)
	}

	// reverse default: every auto-detected value is masked when piped
	for _, k := range []string{"FOO", "BAR", "SECRET_TOKEN"} {
		out := captureStdout(t, func() {
			if code := Run([]string{"apollo", "get", "prod", k, "--dir", snap, "--appid", "app-x"}); code != 0 {
				t.Fatalf("get %s failed with code %d", k, code)
			}
		})
		if strings.TrimSpace(out) != "*** (1 chars)" && strings.TrimSpace(out) != "*** (3 chars)" {
			t.Fatalf("get %s = %q, want mask", k, out)
		}
	}

	// mark FOO safe -> piped get returns plaintext; others stay masked
	if code := Run([]string{"apollo", "mark", "prod", "FOO", "--plain", "--dir", snap, "--appid", "app-x"}); code != 0 {
		t.Fatalf("mark FOO --plain failed with code %d", code)
	}
	out := captureStdout(t, func() {
		if code := Run([]string{"apollo", "get", "prod", "FOO", "--dir", snap, "--appid", "app-x"}); code != 0 {
			t.Fatalf("get FOO failed with code %d", code)
		}
	})
	if strings.TrimSpace(out) != "1" {
		t.Fatalf("get safe FOO = %q, want 1", out)
	}
	out = captureStdout(t, func() {
		if code := Run([]string{"apollo", "get", "prod", "BAR", "--dir", snap, "--appid", "app-x"}); code != 0 {
			t.Fatalf("get BAR failed with code %d", code)
		}
	})
	if strings.TrimSpace(out) != "*** (1 chars)" {
		t.Fatalf("get BAR = %q, want mask", out)
	}

	// mark BACK to secret -> masked again
	if code := Run([]string{"apollo", "mark", "prod", "FOO", "--secret", "--dir", snap, "--appid", "app-x"}); code != 0 {
		t.Fatalf("mark FOO --secret failed with code %d", code)
	}
	out = captureStdout(t, func() {
		if code := Run([]string{"apollo", "get", "prod", "FOO", "--dir", snap, "--appid", "app-x"}); code != 0 {
			t.Fatalf("get FOO failed with code %d", code)
		}
	})
	if strings.TrimSpace(out) != "*** (1 chars)" {
		t.Fatalf("get FOO after --secret = %q, want mask", out)
	}

	// mark requires exactly one of --plain/--secret
	if code := Run([]string{"apollo", "mark", "prod", "FOO", "--dir", snap, "--appid", "app-x"}); code != 1 {
		t.Fatalf("mark without flag returned %d, want 1", code)
	}
	if code := Run([]string{"apollo", "mark", "prod", "FOO", "--plain", "--secret", "--dir", snap, "--appid", "app-x"}); code != 1 {
		t.Fatalf("mark with both flags returned %d, want 1", code)
	}
	if code := Run([]string{"apollo", "mark", "prod", "MISSING", "--plain", "--dir", snap, "--appid", "app-x"}); code != 1 {
		t.Fatalf("mark missing key returned %d, want 1", code)
	}

	// list --json also follows reverse default: only FOO-safe shows plaintext
	if code := Run([]string{"apollo", "mark", "prod", "FOO", "--plain", "--dir", snap, "--appid", "app-x"}); code != 0 {
		t.Fatalf("mark FOO --plain failed with code %d", code)
	}
	out = captureStdout(t, func() {
		if code := Run([]string{"apollo", "list", "prod", "--json", "--dir", snap, "--appid", "app-x"}); code != 0 {
			t.Fatalf("list failed with code %d", code)
		}
	})
	var lm struct {
		Items map[string]string `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &lm); err != nil {
		t.Fatalf("list json: %v\n%s", err, out)
	}
	if lm.Items["FOO"] != "1" {
		t.Errorf("list FOO = %q, want 1", lm.Items["FOO"])
	}
	if lm.Items["BAR"] != "*** (1 chars)" || lm.Items["SECRET_TOKEN"] != "*** (3 chars)" {
		t.Errorf("list masked items wrong: %#v", lm.Items)
	}
}

// TestAESListMasksKeysInNonTTY verifies that `aes list` does not dump stored
// AES key/iv values into non-interactive (script/AI) output.
func TestAESListMasksKeysInNonTTY(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VAULTY_KEEPER_AES_CONFIG", filepath.Join(dir, "aes.json"))
	if code := Run([]string{"aes", "add", "--name", "oss", "--key", "0123456789abcdef", "--iv", "abcdefghijklmnop"}); code != 0 {
		t.Fatalf("aes add failed with code %d", code)
	}
	old := isTerminalFunc
	isTerminalFunc = func() bool { return false }
	defer func() { isTerminalFunc = old }()

	oldOut := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	Run([]string{"aes", "list"})
	w.Close()
	os.Stdout = oldOut
	b, _ := io.ReadAll(r)
	out := string(b)

	if strings.Contains(out, "0123456789abcdef") || strings.Contains(out, "abcdefghijklmnop") {
		t.Fatalf("aes list leaked plaintext key/iv in non-TTY: %s", out)
	}
	if !strings.Contains(out, "oss") {
		t.Fatalf("aes list missing entry name: %s", out)
	}
	if !strings.Contains(out, "***") {
		t.Fatalf("aes list did not mask key/iv: %s", out)
	}
}
