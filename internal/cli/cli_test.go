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

	"ai-tools/internal/aesx"
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
	t.Setenv("AI_TOOLS_APOLLO_KEY", key)
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
	// compare prod vs test: FOO changed 1->2
	if code := Run([]string{"apollo", "compare", "prod", "test", "--reveal", "--yes", "--dir", snap, "--appid", "app-x", "--appid-to", "app-x"}); code != 0 {
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

func TestImportAutoName(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("AI_TOOLS_APOLLO_KEY", key)
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
	t.Setenv("AI_TOOLS_APOLLO_KEY", key)
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
	// value unchanged
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
	t.Setenv("AI_TOOLS_APOLLO_KEY", key)
	dir := t.TempDir()
	snap := filepath.Join(dir, "snap")

	aesKey := "MyAesSecretKey12" // 16 bytes
	aesIV := "0123456789abcdef"  // 16 bytes
	ct, err := aesx.Encrypt(aesKey, aesIV, "LTAI5t-secret-SK")
	if err != nil {
		t.Fatal(err)
	}
	in := filepath.Join(dir, "paste.txt")
	os.WriteFile(in, []byte("imile.fs.aes.secret-key = "+aesKey+"\nimile.fs.aes.iv = "+aesIV+"\nimile.fs.oss.secret-key = "+ct+"\n"), 0o600)
	if code := Run([]string{"apollo", "import", in, "--name", "prod", "--dir", snap, "--app-id", "app-x"}); code != 0 {
		t.Fatalf("import failed with code %d", code)
	}

	// reveal using AES config from the same snapshot
	out := captureStdout(t, func() {
		if code := Run([]string{"apollo", "reveal", "prod", "imile.fs.oss.secret-key", "--dir", snap, "--appid", "app-x"}); code != 0 {
			t.Fatalf("reveal failed with code %d", code)
		}
	})
	if strings.TrimSpace(out) != "LTAI5t-secret-SK" {
		t.Errorf("reveal got %q", out)
	}

	// reveal with explicit --key/--iv override
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
		if code := Run([]string{"apollo", "reveal", "prod", "imile.fs.oss.secret-key", "imile.fs.oss.access-key-id", "--json", "--dir", snap, "--appid", "app-x"}); code != 0 {
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

func TestRevealMissingAesConfig(t *testing.T) {
	asTTY(t)
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("AI_TOOLS_APOLLO_KEY", key)
	dir := t.TempDir()
	snap := filepath.Join(dir, "snap")
	in := filepath.Join(dir, "paste.txt")
	os.WriteFile(in, []byte("FOO = bar\n"), 0o600)
	if code := Run([]string{"apollo", "import", in, "--name", "prod", "--dir", snap, "--app-id", "app-x"}); code != 0 {
		t.Fatalf("import failed with code %d", code)
	}
	// no aes config anywhere -> should fail with a helpful message (code 1)
	if code := Run([]string{"apollo", "reveal", "prod", "FOO", "--dir", snap, "--appid", "app-x"}); code != 1 {
		t.Fatalf("expected failure code 1, got %d", code)
	}
}

func TestEdit(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("AI_TOOLS_APOLLO_KEY", key)
	dir := t.TempDir()
	snap := filepath.Join(dir, "snap")
	in := filepath.Join(dir, "paste.txt")
	os.WriteFile(in, []byte("FOO = 1\nBAR = 2\n"), 0o600)
	if code := Run([]string{"apollo", "import", in, "--name", "prod", "--dir", snap, "--app-id", "app-x"}); code != 0 {
		t.Fatalf("import failed with code %d", code)
	}

	// fake editor: appends a new key and changes FOO
	editor := filepath.Join(dir, "editor.sh")
	os.WriteFile(editor, []byte("#!/bin/sh\nsed -i '' 's/FOO = 1/FOO = 9/' \"$1\"\nprintf 'NEW_KEY = 3\\n' >> \"$1\"\n"), 0o700)

	if code := Run([]string{"apollo", "edit", "prod", "--editor", editor, "--yes", "--dir", snap, "--appid", "app-x"}); code != 0 {
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
	t.Setenv("AI_TOOLS_APOLLO_KEY", key)
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
	t.Setenv("AI_TOOLS_APOLLO_KEY", key)
	err := captureStderr(t, func() {
		if code := Run([]string{"apollo", "list", "../outside", "--dir", t.TempDir(), "--appid", "app-x"}); code != 1 {
			t.Fatalf("list with unsafe name returned %d, want 1", code)
		}
	})
	if !strings.Contains(err, "snapshot name must start") {
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

func TestApolloRm(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("AI_TOOLS_APOLLO_KEY", key)
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

func TestPlaintextCommandsRequireYesWhenPiped(t *testing.T) {
	asPiped(t)
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("AI_TOOLS_APOLLO_KEY", key)
	dir := t.TempDir()
	snap := filepath.Join(dir, "snap")
	in := filepath.Join(dir, "paste.txt")
	os.WriteFile(in, []byte("SECRET_TOKEN = abc\nFOO = 1\n"), 0o600)
	if code := Run([]string{"apollo", "import", in, "--name", "prod", "--dir", snap, "--app-id", "app-x"}); code != 0 {
		t.Fatalf("import failed with code %d", code)
	}

	cases := []struct {
		name string
		args []string
	}{
		{"get sensitive key", []string{"apollo", "get", "prod", "SECRET_TOKEN", "--dir", snap, "--appid", "app-x"}},
		{"list --reveal", []string{"apollo", "list", "prod", "--reveal", "--dir", snap, "--appid", "app-x"}},
		{"compare --reveal", []string{"apollo", "compare", "prod", "prod", "--reveal", "--dir", snap, "--appid", "app-x", "--appid-to", "app-x"}},
		{"export", []string{"apollo", "export", "prod", "--dir", snap, "--appid", "app-x"}},
		{"edit", []string{"apollo", "edit", "prod", "--editor", "/bin/true", "--dir", snap, "--appid", "app-x"}},
		{"aes decrypt", []string{"aes", "decrypt", "--key", "0123456789abcdef", "--iv", "abcdefghijklmnop", "AAAA"}},
	}
	for _, c := range cases {
		if code := Run(c.args); code != 1 {
			t.Fatalf("%s without --yes returned %d, want 1", c.name, code)
		}
	}

	// non-sensitive get still works when piped
	if code := Run([]string{"apollo", "get", "prod", "FOO", "--dir", snap, "--appid", "app-x"}); code != 0 {
		t.Fatalf("get non-sensitive failed with code %d", code)
	}
	// encrypt is not plaintext and works when piped
	if code := Run([]string{"aes", "encrypt", "--key", "0123456789abcdef", "--iv", "abcdefghijklmnop", "hello"}); code != 0 {
		t.Fatalf("aes encrypt failed with code %d", code)
	}
	// each plaintext command works with --yes
	ct, err := aesx.Encrypt("0123456789abcdef", "abcdefghijklmnop", "hello")
	if err != nil {
		t.Fatal(err)
	}
	noopEditor := filepath.Join(dir, "noop-editor.sh")
	os.WriteFile(noopEditor, []byte("#!/bin/sh\nexit 0\n"), 0o700)
	withYes := [][]string{
		{"apollo", "get", "prod", "SECRET_TOKEN", "--yes", "--dir", snap, "--appid", "app-x"},
		{"apollo", "list", "prod", "--reveal", "--yes", "--dir", snap, "--appid", "app-x"},
		{"apollo", "compare", "prod", "prod", "--reveal", "--yes", "--dir", snap, "--appid", "app-x", "--appid-to", "app-x"},
		{"apollo", "export", "prod", "--yes", "--dir", snap, "--appid", "app-x"},
		{"apollo", "edit", "prod", "--editor", noopEditor, "--yes", "--dir", snap, "--appid", "app-x"},
		{"aes", "decrypt", "--key", "0123456789abcdef", "--iv", "abcdefghijklmnop", "--yes", ct},
	}
	for _, args := range withYes {
		if code := Run(args); code != 0 {
			t.Fatalf("with --yes returned %d: %v", code, args)
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
