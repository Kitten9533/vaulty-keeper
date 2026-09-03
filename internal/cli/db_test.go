package cli

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// withStdin feeds data as the process stdin (a pipe, so isTTY is false).
func withStdin(t *testing.T, data string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	w.Close()
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old })
}

func dbTestEnv(t *testing.T) (dir string) {
	t.Helper()
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("VAULTY_KEEPER_DB_KEY", key)
	return t.TempDir()
}

func TestDBAddAndList(t *testing.T) {
	dir := dbTestEnv(t)
	db := filepath.Join(dir, "db")

	// add with DSN from stdin (non-TTY pipe)
	withStdin(t, "postgres://app:topsecret@db.example.com:5432/orders\n")
	if code := Run([]string{"db", "add", "prod", "--dir", db}); code != 0 {
		t.Fatalf("db add failed with code %d", code)
	}

	// add without stdin input must fail with a hint
	withStdin(t, "")
	if code := Run([]string{"db", "add", "empty", "--dir", db}); code == 0 {
		t.Fatal("db add with empty stdin should fail")
	}

	out := captureStdout(t, func() {
		if code := Run([]string{"db", "list", "--dir", db}); code != 0 {
			t.Fatalf("db list failed with code %d", code)
		}
	})
	if !strings.Contains(out, "prod") || !strings.Contains(out, "postgres") || !strings.Contains(out, "15432") {
		t.Fatalf("db list output missing info: %q", out)
	}
	if strings.Contains(out, "topsecret") || strings.Contains(out, "db.example.com") {
		t.Fatalf("db list leaked URL: %q", out)
	}
}

func TestDBAddRejectsUnknownScheme(t *testing.T) {
	dir := dbTestEnv(t)
	withStdin(t, "mongodb://u:p@h/db\n")
	if code := Run([]string{"db", "add", "m", "--dir", filepath.Join(dir, "db")}); code == 0 {
		t.Fatal("db add accepted unsupported scheme")
	}
}

func TestDBRMNeedsYesWhenPiped(t *testing.T) {
	dir := dbTestEnv(t)
	db := filepath.Join(dir, "db")
	withStdin(t, "redis://h:6379\n")
	if code := Run([]string{"db", "add", "cache", "--dir", db}); code != 0 {
		t.Fatalf("db add failed with code %d", code)
	}
	if code := Run([]string{"db", "rm", "cache", "--dir", db}); code == 0 {
		t.Fatal("db rm without --yes should fail when piped")
	}
	if code := Run([]string{"db", "rm", "cache", "--yes", "--dir", db}); code != 0 {
		t.Fatalf("db rm --yes failed with code %d", code)
	}
}

func TestDBShellRejectedWhenPiped(t *testing.T) {
	dir := dbTestEnv(t)
	db := filepath.Join(dir, "db")
	withStdin(t, "postgres://u:p@h/db\n")
	if code := Run([]string{"db", "add", "prod", "--dir", db}); code != 0 {
		t.Fatalf("db add failed with code %d", code)
	}
	if code := Run([]string{"db", "shell", "prod", "--dir", db}); code == 0 {
		t.Fatal("db shell should be rejected when not on a TTY")
	}
}

func TestDBConnectPrintsReadyCommand(t *testing.T) {
	dir := dbTestEnv(t)
	db := filepath.Join(dir, "db")
	// The serve-time global bridge token still exists; a fresh connection gets
	// its own dedicated token and db connect must use that, not the global one.
	t.Setenv("VAULTY_KEEPER_BRIDGE_TOKEN", "tok-abc123")
	withStdin(t, "postgres://app:pgpass@db.example.com:5432/appdb\n")
	if code := Run([]string{"db", "add", "pgdb", "--dir", db}); code != 0 {
		t.Fatalf("db add failed: %d", code)
	}
	out := captureStdout(t, func() {
		if code := Run([]string{"db", "connect", "pgdb", "--cmd", "--dir", db}); code != 0 {
			t.Fatalf("db connect failed: %d", code)
		}
	})
	tokRe := regexp.MustCompile(`postgresql://([0-9a-f]{32})@`)
	m := tokRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("db connect --cmd has no 32-hex per-connection token: %q", out)
	}
	if m[1] == "tok-abc123" {
		t.Fatalf("db connect should use the per-connection token, not the global bridge token: %q", out)
	}
	want := `psql "postgresql://` + m[1] + `@127.0.0.1:15432/appdb"`
	if strings.TrimSpace(out) != want {
		t.Fatalf("db connect --cmd = %q, want %q", out, want)
	}
	// 不允许泄露真实密码
	if strings.Contains(out, "pgpass") || strings.Contains(out, "db.example.com") {
		t.Fatalf("db connect leaked real credentials: %q", out)
	}

	// 丰富视图：原始隧道链接 + 各客户端链接
	out = captureStdout(t, func() {
		if code := Run([]string{"db", "connect", "pgdb", "--dir", db}); code != 0 {
			t.Fatalf("db connect failed: %d", code)
		}
	})
	for _, wantSub := range []string{
		"原始隧道链接",
		"postgresql://" + m[1] + "@127.0.0.1:15432/appdb",
		"jdbc:postgresql://127.0.0.1:15432/appdb?user=" + m[1],
		"pgAdmin4",
	} {
		if !strings.Contains(out, wantSub) {
			t.Fatalf("db connect rich output missing %q: %q", wantSub, out)
		}
	}
	if strings.Contains(out, "pgpass") {
		t.Fatalf("db connect rich output leaked real password: %q", out)
	}

	out = captureStdout(t, func() {
		if code := Run([]string{"db", "connect", "pgdb", "--container", "--dir", db}); code != 0 {
			t.Fatalf("db connect --container failed: %d", code)
		}
	})
	if !strings.Contains(out, "host.docker.internal") {
		t.Fatalf("db connect --container should use host.docker.internal: %q", out)
	}
}

func TestDBRegenToken(t *testing.T) {
	dir := dbTestEnv(t)
	db := filepath.Join(dir, "db")
	add := func(name, dsn string) {
		t.Helper()
		withStdin(t, dsn+"\n")
		if code := Run([]string{"db", "add", name, "--dir", db}); code != 0 {
			t.Fatalf("db add %s failed: %d", name, code)
		}
	}
	connTok := func(name string) string {
		t.Helper()
		out := captureStdout(t, func() {
			if code := Run([]string{"db", "connect", name, "--cmd", "--dir", db}); code != 0 {
				t.Fatalf("db connect %s failed: %d", name, code)
			}
		})
		m := regexp.MustCompile(`[0-9a-f]{32}`).FindString(out)
		if m == "" {
			t.Fatalf("no token in connect output for %s: %q", name, out)
		}
		return m
	}
	add("pg", "postgres://app:pgpass@db.example.com:5432/appdb")
	add("rd", "redis://:pw@db.example.com:6379/0")

	oldPG, oldRD := connTok("pg"), connTok("rd")
	out := captureStdout(t, func() {
		if code := Run([]string{"db", "regen", "pg", "--dir", db}); code != 0 {
			t.Fatalf("db regen pg failed: %d", code)
		}
	})
	if strings.Contains(out, "pgpass") || strings.Contains(out, "db.example.com") {
		t.Fatalf("db regen leaked real credentials: %q", out)
	}
	if newPG := connTok("pg"); newPG == oldPG {
		t.Fatalf("db regen did not rotate the token")
	}
	if connTok("rd") != oldRD {
		t.Fatalf("db regen of one connection must not affect another")
	}

	out = captureStdout(t, func() {
		if code := Run([]string{"db", "regen", "--all", "--dir", db}); code != 0 {
			t.Fatalf("db regen --all failed: %d", code)
		}
	})
	if !strings.Contains(out, "已为 2 个连接") {
		t.Fatalf("db regen --all summary missing: %q", out)
	}
}

func TestDBShowRejectedWhenPiped(t *testing.T) {
	dir := dbTestEnv(t)
	db := filepath.Join(dir, "db")
	withStdin(t, "redis://:pw@127.0.0.1:6379/0\n")
	if code := Run([]string{"db", "add", "cache", "--dir", db}); code != 0 {
		t.Fatalf("db add failed: %d", code)
	}
	// non-TTY: must refuse to print the real URL
	out := captureStdout(t, func() {
		if code := Run([]string{"db", "show", "cache", "--dir", db}); code == 0 {
			t.Fatal("db show should fail when not on a TTY")
		}
	})
	if strings.Contains(out, "redis://") {
		t.Fatalf("db show leaked URL when piped: %q", out)
	}
}

func TestDBTestDispatch(t *testing.T) {
	dir := dbTestEnv(t)
	db := filepath.Join(dir, "db")
	// register a connection to an unreachable port (dial fails -> sanitized FAIL)
	withStdin(t, "redis://:pw@127.0.0.1:1/0\n")
	if code := Run([]string{"db", "add", "bad", "--dir", db}); code != 0 {
		t.Fatalf("db add failed: %d", code)
	}
	out := captureStderr(t, func() {
		if code := Run([]string{"db", "test", "bad", "--dir", db}); code == 0 {
			t.Fatal("db test should fail for unreachable db")
		}
	})
	if strings.Contains(out, "127.0.0.1") {
		t.Fatalf("db test leaked address: %q", out)
	}
	if !strings.Contains(out, "db add bad") {
		t.Fatalf("db test should print fix hint: %q", out)
	}
}

func TestDBListHint(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope", "db.json")
	if !strings.Contains(dbListHint(missing, errors.New("x")), "db add") {
		t.Fatal("missing store hint should suggest db add")
	}
	mismatch := filepath.Join(dir, "db.json")
	os.WriteFile(mismatch, []byte("{}"), 0o600)
	h := dbListHint(mismatch, errors.New("解密连接失败：cipher: message authentication failed"))
	if !strings.Contains(h, "密钥不匹配") {
		t.Fatalf("mismatch hint missing: %s", h)
	}
	nokey := dbListHint(mismatch, errors.New("未找到数据库密钥"))
	if !strings.Contains(nokey, "db init") {
		t.Fatalf("no-key hint missing: %s", nokey)
	}
}

func TestDBInitForceNeedsYesWhenPiped(t *testing.T) {
	dir := dbTestEnv(t)
	_ = dir
	// non-TTY: --force without --yes must refuse (protects existing connections)
	out := captureStderr(t, func() {
		if code := Run([]string{"db", "init", "--force"}); code == 0 {
			t.Fatal("db init --force should need --yes when piped")
		}
	})
	if !strings.Contains(out, "重新 db add") {
		t.Fatalf("db init --force should warn about losing connections: %q", out)
	}
}
