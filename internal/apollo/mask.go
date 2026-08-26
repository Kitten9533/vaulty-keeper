package apollo

import (
	"fmt"
	"regexp"
)

var sensitiveRe = regexp.MustCompile(`(?i)(password|passwd|pwd|token|secret|salt|credential|private|access[_-]?key|secret[_-]?key|api[_-]?key)`)

// IsSensitive reports whether a key should be masked by default.
func IsSensitive(key string) bool {
	return sensitiveRe.MatchString(key)
}

// Mask replaces a value with a fixed placeholder.
func Mask() string {
	return "***"
}

// MaskWithLen masks a value but keeps its length, so diffs show whether a
// secret actually changed in size.
func MaskWithLen(n int) string {
	return fmt.Sprintf("*** (%d chars)", n)
}
