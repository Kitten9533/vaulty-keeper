package apollo

import (
	"fmt"
	"regexp"
	"strings"
)

// Parses KV text pasted from the Apollo portal. Rules:
//
//   - "KEY = value" per line, split on the FIRST '=', both sides trimmed.
//   - blank lines and full-line "#" comments (single or multi-line blocks)
//     are skipped.
//   - a single line holding several merged "KEY = " entries (all-caps keys
//     glued to the previous value, e.g. "A = 1B = 2") is auto-split and
//     reported in warnings.
//   - lines whose key fails validation are skipped and reported in warnings.

type KV struct {
	Key   string
	Value string
}

// Warning describes a non-fatal parse issue (auto-split / skipped line).
// Line is 1-based, Content is the offending raw line/segment so the user can
// locate it in their paste.
type Warning struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
	Content string `json:"content"`
}

func (w Warning) String() string {
	s := fmt.Sprintf("line %d: %s", w.Line, w.Message)
	if w.Content != "" {
		s += fmt.Sprintf(": %q", w.Content)
	}
	return s
}

var (
	keyRe    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)
	splitRe  = regexp.MustCompile(`[A-Z][A-Z0-9_]*\s*=`)
	isGlueRe = regexp.MustCompile(`[A-Za-z0-9_.-]`)
)

// ParseKV returns parsed items plus non-fatal warnings (auto-split / skipped).
func ParseKV(text string) ([]KV, []Warning) {
	var (
		items    []KV
		warnings []Warning
	)
	for i, raw := range strings.Split(text, "\n") {
		lineNo := i + 1
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		segs := splitSegments(line)
		if len(segs) > 1 {
			warnings = append(warnings, Warning{
				Line:    lineNo,
				Message: fmt.Sprintf("自动拆分 %d 个粘连条目，请核对", len(segs)),
				Content: line,
			})
		}
		for _, seg := range segs {
			key, value, ok := parseOne(seg)
			if !ok {
				warnings = append(warnings, Warning{
					Line:    lineNo,
					Message: "已跳过（缺少 '=' 或 key 非法）",
					Content: strings.TrimSpace(seg),
				})
				continue
			}
			items = append(items, KV{Key: key, Value: value})
		}
	}
	return items, warnings
}

// ValidateKey reports whether key is a valid Apollo key name.
func ValidateKey(key string) error {
	if !keyRe.MatchString(key) {
		return fmt.Errorf("非法的 key %q", key)
	}
	return nil
}

// NormValue strips one pair of matching surrounding quotes so that
// `name="merdi"` and `name=merdi` compare equal. Unbalanced quotes are left
// untouched.
func NormValue(value string) string {
	if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
		return value[1 : len(value)-1]
	}
	return value
}

func parseOne(seg string) (key, value string, ok bool) {
	idx := strings.Index(seg, "=")
	if idx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(seg[:idx])
	value = NormValue(strings.TrimSpace(seg[idx+1:]))
	if err := ValidateKey(key); err != nil {
		return "", "", false
	}
	return key, value, true
}

// splitSegments splits a line into "KEY = value" segments when several
// all-caps keys are glued together without a separator (e.g.
// "MONGODB_NAME = merdi_portalREDIS_URI = r-bp..."). A candidate is only a
// split point if it is glued to the preceding content (previous value's last
// char is alnum/_/./-), which avoids splitting URL query params like
// "...?TOKEN=1" (preceded by '?').
func splitSegments(line string) []string {
	idxs := splitRe.FindAllStringIndex(line, -1)
	if len(idxs) <= 1 {
		return []string{line}
	}
	var cuts []int
	for _, m := range idxs {
		if m[0] == 0 {
			continue
		}
		if !isGlueRe.MatchString(line[m[0]-1 : m[0]]) {
			continue
		}
		cuts = append(cuts, m[0])
	}
	if len(cuts) == 0 {
		return []string{line}
	}
	segs := make([]string, 0, len(cuts)+1)
	start := 0
	for _, c := range cuts {
		segs = append(segs, line[start:c])
		start = c
	}
	return append(segs, line[start:])
}
