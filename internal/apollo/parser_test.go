package apollo

import (
	"reflect"
	"strings"
	"testing"
)

// The user's real pasted sample (first line contains two merged entries).
const sample = `MONGODB_NAME = merdi_portalREDIS_URI = r-bp1vt3z0q58742w17y.redis.rds.aliyuncs.com
REDIS_PORT = 6379
CMS_JWT_EXPIRES_IN = 1h
COOKIE_MAX_AGE = 3600
PASSWORD_SALT = 10
APP_NAME = merdi
`

func TestParseSample(t *testing.T) {
	items, warnings := ParseKV(sample)
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning for merged line, got %v", warnings)
	}
	want := []KV{
		{"MONGODB_NAME", "merdi_portal"},
		{"REDIS_URI", "r-bp1vt3z0q58742w17y.redis.rds.aliyuncs.com"},
		{"REDIS_PORT", "6379"},
		{"CMS_JWT_EXPIRES_IN", "1h"},
		{"COOKIE_MAX_AGE", "3600"},
		{"PASSWORD_SALT", "10"},
		{"APP_NAME", "merdi"},
	}
	if !reflect.DeepEqual(items, want) {
		t.Errorf("got %v want %v", items, want)
	}
}

func TestCommentsAndBlankLines(t *testing.T) {
	in := "# single line comment\n\n# multi\n# line\nFOO = 1\n   # indented comment\nBAR=2\n"
	items, warnings := ParseKV(in)
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	want := []KV{{"FOO", "1"}, {"BAR", "2"}}
	if !reflect.DeepEqual(items, want) {
		t.Errorf("got %v want %v", items, want)
	}
}

func TestSplitOnFirstEquals(t *testing.T) {
	items, _ := ParseKV("URL = http://host/path?a=1&b=2\nSECRET = abc=def=ghi\n")
	want := []KV{
		{"URL", "http://host/path?a=1&b=2"},
		{"SECRET", "abc=def=ghi"},
	}
	if !reflect.DeepEqual(items, want) {
		t.Errorf("got %v want %v", items, want)
	}
}

func TestNoFalseSplitOnUrlQuery(t *testing.T) {
	// Lowercase and '?'-preceded params must NOT be split.
	items, warnings := ParseKV("URL = http://x?a=1&b=2\nS3_URL = https://s3?token=abc\n")
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2: %v", len(items), items)
	}
	if items[0].Value != "http://x?a=1&b=2" {
		t.Errorf("URL value mangled: %q", items[0].Value)
	}
	if items[1].Value != "https://s3?token=abc" {
		t.Errorf("S3_URL value mangled: %q", items[1].Value)
	}
}

func TestInvalidLinesSkipped(t *testing.T) {
	items, warnings := ParseKV("GOOD = 1\nno-equals-here\n123BAD = x\n-GOOD = y\n")
	if len(items) != 1 || items[0].Key != "GOOD" {
		t.Fatalf("got %v", items)
	}
	if len(warnings) != 3 {
		t.Errorf("expected 3 warnings, got %v", warnings)
	}
}

func TestMultipleMergedEntriesOnLine(t *testing.T) {
	items, warnings := ParseKV("A = 1B = 2C = 3\n")
	want := []KV{{"A", "1"}, {"B", "2"}, {"C", "3"}}
	if !reflect.DeepEqual(items, want) {
		t.Errorf("got %v want %v", items, want)
	}
	if len(warnings) != 1 {
		t.Errorf("expected 1 warning, got %v", warnings)
	}
}

func TestValueWithTrailingSpacesTrimmed(t *testing.T) {
	items, _ := ParseKV("KEY =   padded value   \n")
	if items[0].Value != "padded value" {
		t.Errorf("got %q", items[0].Value)
	}
}

func TestValidateKey(t *testing.T) {
	for _, key := range []string{"APP_NAME", "imile.fs.oss.secret-key", "SOME-KEY"} {
		if err := ValidateKey(key); err != nil {
			t.Errorf("ValidateKey(%q): %v", key, err)
		}
	}
	for _, key := range []string{"", "123BAD", "bad key", "A/B"} {
		if err := ValidateKey(key); err == nil {
			t.Errorf("ValidateKey(%q) succeeded, want error", key)
		}
	}
}

func TestDotAndHyphenKeys(t *testing.T) {
	items, warnings := ParseKV("imile.fs.oss.access-key-id = abc\nSOME-KEY = x\n")
	if len(warnings) != 0 || len(items) != 2 {
		t.Fatalf("got %v %v", items, warnings)
	}
	if strings.Join([]string{items[0].Key, items[1].Key}, ",") != "imile.fs.oss.access-key-id,SOME-KEY" {
		t.Errorf("got %v", items)
	}
}

func TestQuotedValuesStripped(t *testing.T) {
	in := `NAME = "merdi"
PATH = 'app/config'
URL = "https://a.com/x?q=1"
RAW = "unclosed
SINGLE = 'only-left
`
	items, warnings := ParseKV(in)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	got := map[string]string{}
	for _, kv := range items {
		got[kv.Key] = kv.Value
	}
	want := map[string]string{
		"NAME":   "merdi",
		"PATH":   "app/config",
		"URL":    "https://a.com/x?q=1",
		"RAW":    `"unclosed`,  // unbalanced quote left as-is
		"SINGLE": "'only-left", // unbalanced quote left as-is
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestNormValue(t *testing.T) {
	cases := []struct{ in, want string }{
		{`"6379"`, "6379"},
		{"'6379'", "6379"},
		{"6379", "6379"},
		{`"unclosed`, `"unclosed`},
		{`'unclosed`, `'unclosed`},
		{`""`, ""},
		{`"a"b"`, `a"b`},
		{"", ""},
		{`{"a":1}`, `{"a":1}`},
	}
	for _, c := range cases {
		if got := NormValue(c.in); got != c.want {
			t.Errorf("NormValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDiffIgnoresSurroundingQuotes(t *testing.T) {
	key := make([]byte, 32)
	a := NewSnapshot("a", "")
	b := NewSnapshot("b", "")
	for _, kv := range []struct{ k, v string }{
		{"REDIS_PORT", "6379"},
		{"QUOTED", `"hello"`},
		{"REAL_DIFF", "1"},
	} {
		if err := a.Set(key, kv.k, kv.v, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, kv := range []struct{ k, v string }{
		{"REDIS_PORT", `"6379"`},
		{"QUOTED", "'hello'"},
		{"REAL_DIFF", "2"},
	} {
		if err := b.Set(key, kv.k, kv.v, nil); err != nil {
			t.Fatal(err)
		}
	}
	changes, err := a.Diff(b, key)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, c := range changes {
		keys = append(keys, c.Key)
	}
	if len(keys) != 1 || keys[0] != "REAL_DIFF" {
		t.Fatalf("changes = %v, want only REAL_DIFF", keys)
	}
}
