package cli

import (
	"bufio"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func newIt(input string) *interactive {
	return &interactive{in: bufio.NewReader(strings.NewReader(input)), dir: ""}
}

func TestCustomKeyIv(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "aes.json")
	t.Setenv("AI_TOOLS_AES_CONFIG", cfg)

	// add an entry via the management menu: 1=新增, then name/key/iv, 0=返回
	it := newIt("1\nmykey\nMyAesSecretKey12\n0123456789abcdef\n0\n")
	it.cmdCustomKeyIv()
	if it.aesKey != "MyAesSecretKey12" || it.aesIV != "0123456789abcdef" {
		t.Fatalf("got key=%q iv=%q", it.aesKey, it.aesIV)
	}
	if _, err := os.Stat(cfg); err != nil {
		t.Fatalf("config not saved: %v", err)
	}

	// a fresh session loads the saved entries
	it2 := newIt("")
	it2.loadAesConfig()
	if len(it2.aesEntries) != 1 || it2.aesEntries[0].Name != "mykey" || it2.aesKey != it.aesKey || it2.aesIV != it.aesIV {
		t.Errorf("reload mismatch: %#v", it2.aesEntries)
	}

	// aesKeyIv uses the saved values as defaults on empty input
	it3 := newIt("\n\n\n")
	it3.loadAesConfig()
	k, iv, cancel := it3.aesKeyIv()
	if cancel || k != it.aesKey || iv != it.aesIV {
		t.Errorf("aesKeyIv defaults got %q %q cancel=%v", k, iv, cancel)
	}

	// empty key/iv input is rejected
	it4 := newIt("\nclear\n\n")
	k, iv, cancel = it4.aesKeyIv()
	if !cancel && k != "" {
		t.Errorf("unexpected: %q %q cancel=%v", k, iv, cancel)
	}
}

func TestDisplayWidth(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abc", 3},
		{"快照名", 6},
		{"快照名abc", 9},
		{"AES secret key", 14},
		{"中文 abc", 8},
	}
	for _, c := range cases {
		if got := displayWidth(c.in); got != c.want {
			t.Errorf("displayWidth(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestFilterOptions(t *testing.T) {
	opts := []string{"APP_NAME", "REDIS_HOST", "REDIS_PORT", "PASSWORD_SALT", "imile.fs.oss.secret-key"}
	cases := []struct {
		q    string
		want []string
	}{
		{"", opts},
		{"redis", []string{"REDIS_HOST", "REDIS_PORT"}},
		{"redis_p", []string{"REDIS_PORT"}},
		{"pass", []string{"PASSWORD_SALT"}},
		{"secret", []string{"imile.fs.oss.secret-key"}},
		{"fs.oss", []string{"imile.fs.oss.secret-key"}},
		{"zzz", nil},
	}
	for _, c := range cases {
		got := filterOptions(opts, c.q)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("filterOptions(%q) = %v, want %v", c.q, got, c.want)
		}
	}
}

// Non-TTY pickOrInput falls back to a plain prompt.
func TestIsTTY(t *testing.T) {
	// a regular file is never a TTY (nor is /dev/null, a char device but not
	// a terminal) — the ModeCharDevice check alone would misfire here
	f, err := os.CreateTemp(t.TempDir(), "tty")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if isTTY(f.Fd()) {
		t.Error("regular file reported as TTY")
	}
	dn, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer dn.Close()
	if isTTY(dn.Fd()) {
		t.Error("/dev/null reported as TTY")
	}
	// a pipe is never a TTY
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if isTTY(r.Fd()) || isTTY(w.Fd()) {
		t.Error("pipe reported as TTY")
	}
}

func TestPickOrInputFallback(t *testing.T) {
	it := newIt("my-value\n")
	v, cancel := it.pickOrInput("label", []string{"a", "b"})
	if cancel || v != "my-value" {
		t.Errorf("pickOrInput fallback got %q cancel=%v", v, cancel)
	}
}

func TestChoose(t *testing.T) {
	it := newIt("2\n")
	if got := it.choose([]string{"a", "b", "c"}, "退出"); got != 2 {
		t.Errorf("choose got %d want 2", got)
	}

	it = newIt("0\n")
	if got := it.choose([]string{"a", "b", "c"}, "退出"); got != 0 {
		t.Errorf("choose got %d want 0", got)
	}

	// invalid then valid
	it = newIt("x\n1\n")
	if got := it.choose([]string{"a", "b", "c"}, "退出"); got != 1 {
		t.Errorf("choose got %d want 1", got)
	}

	// cancel
	it = newIt("cancel\n")
	if got := it.choose([]string{"a", "b", "c"}, "退出"); got != -1 {
		t.Errorf("choose got %d want -1", got)
	}

	// EOF -> exit (0)
	it = newIt("")
	if got := it.choose([]string{"a", "b", "c"}, "退出"); got != 0 {
		t.Errorf("choose got %d want 0 on EOF", got)
	}
}

func TestPrompt(t *testing.T) {
	it := newIt("\n")
	v, cancel := it.prompt("name", "prod")
	if cancel || v != "prod" {
		t.Errorf("prompt default got %q cancel=%v", v, cancel)
	}

	it = newIt("\n")
	v, cancel = it.prompt("appid", "")
	if cancel || v != "" {
		t.Errorf("prompt optional-empty got %q cancel=%v", v, cancel)
	}

	it = newIt("my-app\n")
	v, cancel = it.prompt("appid", "")
	if cancel || v != "my-app" {
		t.Errorf("prompt value got %q cancel=%v", v, cancel)
	}

	it = newIt("cancel\n")
	_, cancel = it.prompt("name", "prod")
	if !cancel {
		t.Error("prompt should cancel on 'cancel'")
	}
}

func TestPromptMultiline(t *testing.T) {
	it := newIt("FOO = 1\nBAR = 2\nend\n")
	text, cancel := it.promptMultiline("paste:")
	if cancel {
		t.Fatal("unexpected cancel")
	}
	if text != "FOO = 1\nBAR = 2" {
		t.Errorf("multiline got %q", text)
	}

	// Ctrl-D (EOF) also terminates
	it = newIt("A = 1\n")
	text, cancel = it.promptMultiline("paste:")
	if cancel {
		t.Fatal("unexpected cancel")
	}
	if text != "A = 1" {
		t.Errorf("multiline EOF got %q", text)
	}

	// cancel
	it = newIt("x\ncancel\n")
	_, cancel = it.promptMultiline("paste:")
	if !cancel {
		t.Error("multiline should cancel")
	}
}
