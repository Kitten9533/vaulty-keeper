package apollo

import (
	"fmt"
	"regexp"
)

var sensitiveRe = regexp.MustCompile(`(?i)(password|passwd|pwd|token|secret|salt|credential|private|access[_-]?key|secret[_-]?key|api[_-]?key)`)

// credentialURINameRe matches keys that look like a connection URI / DSN.
var credentialURINameRe = regexp.MustCompile(`(?i)(uri|url|dsn|connection|endpoint|addr|address)`)

// credentialURIValueRe matches a scheme://user[:password]@host prefix; the
// "@" must sit right after the userinfo (no "/" between :// and @), so a "@"
// inside a URL path is not treated as a credential.
var credentialURIValueRe = regexp.MustCompile(`://[^/@]*@`)

// jwtRe matches a JWT: three base64url segments, header starting with "eyJ"
// (the base64url of `{"`), e.g. eyJhbGci...eyJpc3Mi...signature.
var jwtRe = regexp.MustCompile(`^eyJ[A-Za-z0-9_-]*\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$`)

// IsSensitive reports whether a key should be masked by default.
func IsSensitive(key string) bool {
	return sensitiveRe.MatchString(key)
}

// IsSensitiveKeyValue reports whether a key/value should be masked: either
// the key name matches (password/token/secret/...), or the value itself looks
// sensitive — a connection URI / DSN carrying inline credentials
// (scheme://user[:password]@host), or a JWT (eyJ... . ... . ...).
func IsSensitiveKeyValue(key, value string) bool {
	if IsSensitive(key) {
		return true
	}
	if isCredentialURI(key, value) {
		return true
	}
	return isJWT(value)
}

func isCredentialURI(key, value string) bool {
	if !credentialURINameRe.MatchString(key) {
		return false
	}
	return credentialURIValueRe.MatchString(value)
}

func isJWT(value string) bool {
	return jwtRe.MatchString(NormValue(value))
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
