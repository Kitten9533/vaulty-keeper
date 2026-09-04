package i18n

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultEnglish(t *testing.T) {
	t.Setenv(EnvLang, "")
	t.Setenv("HOME", t.TempDir())
	Init()
	if Lang != LangEn {
		t.Fatalf("default Lang = %q, want en", Lang)
	}
	if got := T("db.removed", "x"); got != `connection "x" removed` {
		t.Fatalf("T en = %q", got)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := WriteLang(LangZh); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvLang, LangEn)
	Init()
	if Lang != LangEn {
		t.Fatalf("env should win over the prefs file, got %q", Lang)
	}
}

func TestFileFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvLang, "")
	if err := WriteLang(LangZh); err != nil {
		t.Fatal(err)
	}
	Init()
	if Lang != LangZh {
		t.Fatalf("prefs file should set Lang, got %q", Lang)
	}
	if got := T("db.removed", "x"); got != `连接 "x" 已删除` {
		t.Fatalf("T zh = %q", got)
	}
	// file permissions 0600
	b, err := os.ReadFile(filepath.Join(home, ".vaulty", "prefs.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"lang":"zh"`) {
		t.Fatalf("prefs content = %s", b)
	}
}

func TestWriteLangRejectsInvalid(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := WriteLang("fr"); err == nil {
		t.Fatal("WriteLang(fr) should fail")
	}
	if err := WriteLang("ZH"); err != nil {
		t.Fatalf("WriteLang(ZH) should normalize to zh: %v", err)
	}
	if ReadLang() != LangZh {
		t.Fatalf("ReadLang = %q, want zh", ReadLang())
	}
}

func TestTArgsAndFallback(t *testing.T) {
	t.Setenv(EnvLang, "")
	t.Setenv("HOME", t.TempDir())
	Init()
	if got := T("db.regen-done", 3, "a, b"); got != "regenerated tokens for 3 connections: a, b" {
		t.Fatalf("T with args = %q", got)
	}
	if got := T("no.such.key"); got != "no.such.key" {
		t.Fatalf("unknown key should echo back, got %q", got)
	}
}
