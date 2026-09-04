// Package dbproxy stores encrypted database connection URLs on the host and
// exposes them through credential-injecting TCP tunnels (see tunnel.go).
//
// A connection is a name plus the raw URL (DSN) the user registered with
// `vaulty-keeper db add`. The URL is encrypted at rest with AES-256-GCM using the
// dedicated database key (apollo.DBKey / VAULTY_KEEPER_DB_KEY) and only decrypted
// inside the host process — it never appears in db.json, logs, or any reply
// to a tunnel client. The tunnel binds a local TCP port per connection; a
// client connecting there authenticates with the bridge token (PG/MySQL
// username field, Redis AUTH) and the proxy substitutes the real credentials
// from the registered URL before relaying bytes.
package dbproxy

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

const (
	// DefaultPortBase is where per-connection tunnel ports start when the
	// user does not pin one with --port.
	DefaultPortBase = 15432

	// FileName is the store file inside the vaulty-keeper home directory.
	FileName = "db.json"
)

var connNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// Conn is one decrypted connection. URL is in memory only and must never be
// serialized, logged, or returned to a tunnel client (json:"-" is a hard
// guarantee it never appears in any JSON output). Token is the connection's
// dedicated tunnel token (empty for legacy entries, which then use the global
// bridge token). Disabled marks a connection whose tunnel is turned off with
// `db off`; its port is not listened on until `db on`. Broken marks an entry
// whose ciphertext cannot be decrypted with the current key (stale key); it is
// still listed so it can be removed, but has no usable URL.
type Conn struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	URL      string `json:"-"`
	Port     int    `json:"port"`
	Token    string `json:"-"`
	Disabled bool   `json:"disabled,omitempty"`
	Broken   bool   `json:"broken,omitempty"`
}

// storedConn is the on-disk representation: ciphertext plus the allocated
// tunnel port. No plaintext ever hits the file. KeyID is a plaintext
// fingerprint of the encryption key, so a key mismatch can be diagnosed
// precisely instead of surfacing as a cryptic "message authentication failed".
// Type is stored in plaintext so listing needs no decryption (a stale-key
// entry cannot block the whole list). TokenEnc/TokenNonce hold the
// connection's dedicated tunnel token, sealed with the same DB key. Disabled
// persists the `db off` state; the zero value (absent in old files) means the
// tunnel is on, so legacy connections keep working after an upgrade.
type storedConn struct {
	URLEnc     string `json:"url_cipher"`
	Nonce      string `json:"nonce"`
	KeyID      string `json:"key_id,omitempty"`
	Type       string `json:"type,omitempty"`
	Port       int    `json:"port,omitempty"`
	TokenEnc   string `json:"token_cipher,omitempty"`
	TokenNonce string `json:"token_nonce,omitempty"`
	Disabled   bool   `json:"disabled,omitempty"`
}

type store struct {
	Conns map[string]storedConn `json:"connections"`
}

// DefaultPath returns the store file under <home>/.vaulty.
func DefaultPath(home string) string {
	return filepath.Join(home, ".vaulty", FileName)
}

// ValidateConnName checks a connection name: same charset as snapshot names.
func ValidateConnName(name string) error {
	if !connNameRe.MatchString(name) {
		return errors.New("connection name must start with a letter or digit and contain only letters, digits, dots, dashes or underscores")
	}
	return nil
}

// ConnTypeFromURL detects the database type from the URL scheme.
func ConnTypeFromURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("cannot parse database URL: %w", err)
	}
	switch u.Scheme {
	case "postgres", "postgresql":
		return "postgres", nil
	case "mysql":
		return "mysql", nil
	case "redis", "rediss":
		return "redis", nil
	default:
		return "", fmt.Errorf("unsupported database URL scheme %q (supported: postgres/postgresql, mysql, redis/rediss)", u.Scheme)
	}
}

// keyID returns a short fingerprint of an encryption key for mismatch
// diagnostics. It is not secret: it is stored in plaintext beside the
// ciphertext so callers can tell which key a store was written with.
func keyID(key []byte) string {
	h := sha256.Sum256(key)
	return hex.EncodeToString(h[:8])
}

// seal encrypts a small string (URL or tunnel token) with AES-256-GCM under
// the database key and returns base64 ciphertext + nonce.
func seal(key []byte, raw string) (ctB64, nonceB64 string, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", "", err
	}
	ct := g.Seal(nil, nonce, []byte(raw), nil)
	return base64.StdEncoding.EncodeToString(ct), base64.StdEncoding.EncodeToString(nonce), nil
}

// open reverses seal.
func open(key []byte, ctB64, nonceB64 string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce, err := base64.StdEncoding.DecodeString(nonceB64)
	if err != nil {
		return "", err
	}
	ct, err := base64.StdEncoding.DecodeString(ctB64)
	if err != nil {
		return "", err
	}
	pt, err := g.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// encryptConn seals a URL with the database key.
func encryptConn(key []byte, raw string) (storedConn, error) {
	ct, nonce, err := seal(key, raw)
	if err != nil {
		return storedConn{}, err
	}
	return storedConn{
		URLEnc: ct,
		Nonce:  nonce,
		KeyID:  keyID(key),
	}, nil
}

func (s storedConn) decryptConn(key []byte) (string, error) {
	pt, err := open(key, s.URLEnc, s.Nonce)
	if err != nil {
		return "", fmt.Errorf("cannot decrypt connection: %w", err)
	}
	return pt, nil
}

// newToken generates a fresh 128-bit tunnel token (hex), matching the length
// of the global bridge token.
func newToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func load(path string, key []byte) (*store, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &store{Conns: map[string]storedConn{}}, nil
		}
		return nil, err
	}
	var s store
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	if s.Conns == nil {
		s.Conns = map[string]storedConn{}
	}
	return &s, nil
}

func (s *store) save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

// allocPort picks the tunnel port for a connection. requested==0 means
// auto-assign from base upward, skipping ports already used by other
// connections. A requested port that collides is an error.
func allocPort(s *store, requested, base int) (int, error) {
	used := map[int]bool{}
	for _, c := range s.Conns {
		if c.Port != 0 {
			used[c.Port] = true
		}
	}
	if requested != 0 {
		if used[requested] {
			return 0, fmt.Errorf("port %d is already used by another connection", requested)
		}
		return requested, nil
	}
	p := base
	for used[p] {
		p++
	}
	return p, nil
}

// Add creates or updates a named connection, encrypting rawURL with key.
// port==0 keeps the connection's existing tunnel port when re-adding the same
// name (so a URL fix does not silently change the port), and auto-allocates a
// fresh stable port for a new connection.
func Add(path string, key []byte, name, rawURL string, port int) error {
	if err := ValidateConnName(name); err != nil {
		return err
	}
	typ, err := ConnTypeFromURL(rawURL)
	if err != nil {
		return err
	}
	_ = typ
	s, err := load(path, key)
	if err != nil {
		return err
	}
	if port == 0 {
		if existing, ok := s.Conns[name]; ok && existing.Port != 0 {
			port = existing.Port // keep the tunnel port on overwrite
		}
	}
	delete(s.Conns, name) // exclude this connection from the used-port check
	allocated, err := allocPort(s, port, DefaultPortBase)
	if err != nil {
		return err
	}
	sc, err := encryptConn(key, rawURL)
	if err != nil {
		return err
	}
	tok, err := newToken()
	if err != nil {
		return err
	}
	if sc.TokenEnc, sc.TokenNonce, err = seal(key, tok); err != nil {
		return err
	}
	sc.Type = typ
	sc.Port = allocated
	s.Conns[name] = sc
	return s.save(path)
}

// RegenToken replaces the dedicated tunnel token of one connection and
// returns the new token. The old token stops working immediately (tunnels
// re-check on every connection); the global bridge token is unaffected.
func RegenToken(path string, key []byte, name string) (string, error) {
	if err := ValidateConnName(name); err != nil {
		return "", err
	}
	s, err := load(path, key)
	if err != nil {
		return "", err
	}
	sc, ok := s.Conns[name]
	if !ok {
		return "", fmt.Errorf("connection %q does not exist", name)
	}
	tok, err := newToken()
	if err != nil {
		return "", err
	}
	if sc.TokenEnc, sc.TokenNonce, err = seal(key, tok); err != nil {
		return "", err
	}
	s.Conns[name] = sc
	if err := s.save(path); err != nil {
		return "", err
	}
	return tok, nil
}

// RegenTokenAll replaces the dedicated tunnel token of every connection and
// returns the connection names that were updated.
func RegenTokenAll(path string, key []byte) ([]string, error) {
	s, err := load(path, key)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(s.Conns))
	for n, sc := range s.Conns {
		tok, err := newToken()
		if err != nil {
			return nil, err
		}
		if sc.TokenEnc, sc.TokenNonce, err = seal(key, tok); err != nil {
			return nil, err
		}
		s.Conns[n] = sc
		names = append(names, n)
	}
	if len(names) == 0 {
		return names, nil
	}
	sort.Strings(names)
	if err := s.save(path); err != nil {
		return nil, err
	}
	return names, nil
}

// Remove deletes a named connection.
func Remove(path string, key []byte, name string) error {
	if err := ValidateConnName(name); err != nil {
		return err
	}
	s, err := load(path, key)
	if err != nil {
		return err
	}
	if _, ok := s.Conns[name]; !ok {
		return fmt.Errorf("connection %q does not exist", name)
	}
	delete(s.Conns, name)
	return s.save(path)
}

// SetTunnel turns a connection's tunnel on (disabled=false) or off
// (disabled=true). It only touches the Disabled flag — it never needs to
// decrypt the URL, so even a broken (stale-key) entry can be disabled. The
// running serve picks the change up on its next db.json sync (≤2s).
func SetTunnel(path string, key []byte, name string, disabled bool) error {
	if err := ValidateConnName(name); err != nil {
		return err
	}
	s, err := load(path, key)
	if err != nil {
		return err
	}
	sc, ok := s.Conns[name]
	if !ok {
		return fmt.Errorf("connection %q does not exist", name)
	}
	sc.Disabled = disabled
	s.Conns[name] = sc
	return s.save(path)
}

// SetTunnelAll applies the tunnel state to every connection and returns the
// names that were updated.
func SetTunnelAll(path string, key []byte, disabled bool) ([]string, error) {
	s, err := load(path, key)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(s.Conns))
	for n, sc := range s.Conns {
		if sc.Disabled == disabled {
			continue
		}
		sc.Disabled = disabled
		s.Conns[n] = sc
		names = append(names, n)
	}
	if len(names) == 0 {
		return names, nil
	}
	sort.Strings(names)
	if err := s.save(path); err != nil {
		return nil, err
	}
	return names, nil
}

// List returns connection metadata (never the URL), sorted by name. A single
// undecryptable entry (e.g. written with a stale key) is reported as
// Broken=true instead of failing the whole list, so it can still be seen and
// removed.
func List(path string, key []byte) ([]Conn, error) {
	s, err := load(path, key)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(s.Conns))
	for n := range s.Conns {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]Conn, 0, len(names))
	for _, n := range names {
		sc := s.Conns[n]
		c := Conn{Name: n, Type: sc.Type, Port: sc.Port, Disabled: sc.Disabled}
		raw, derr := sc.decryptConn(key)
		if derr != nil {
			c.Broken = true // stale key / corrupt entry: still listed so it can be removed
			if c.Type == "" {
				c.Type = "?"
			}
		} else if c.Type == "" {
			// legacy entry without a stored type: derive from the URL
			c.Type, _ = ConnTypeFromURL(raw)
		}
		out = append(out, c)
	}
	return out, nil
}

// Resolve returns a fully decrypted connection, including the URL.
func Resolve(path string, key []byte, name string) (Conn, error) {
	if err := ValidateConnName(name); err != nil {
		return Conn{}, err
	}
	s, err := load(path, key)
	if err != nil {
		return Conn{}, err
	}
	sc, ok := s.Conns[name]
	if !ok {
		return Conn{}, fmt.Errorf("connection %q does not exist (register it with 'vaulty-keeper db add')", name)
	}
	raw, err := sc.decryptConn(key)
	if err != nil {
		return Conn{}, keyMismatchErr(name, sc, key, err)
	}
	typ, err := ConnTypeFromURL(raw)
	if err != nil {
		return Conn{}, err
	}
	tok := ""
	if sc.TokenEnc != "" {
		tok, err = open(key, sc.TokenEnc, sc.TokenNonce)
		if err != nil {
			return Conn{}, fmt.Errorf("cannot decrypt the tunnel token of connection %q (%v); regenerate it with 'vaulty-keeper db regen %s'", name, err, name)
		}
	}
	return Conn{Name: name, Type: typ, URL: raw, Port: sc.Port, Token: tok, Disabled: sc.Disabled}, nil
}

// keyMismatchErr rewrites a decrypt failure into a precise diagnosis when the
// store records which key it was written with (KeyID) and it differs from the
// key being used now. Legacy entries without a KeyID still get an actionable
// message.
func keyMismatchErr(name string, sc storedConn, key []byte, err error) error {
	if sc.KeyID != "" && sc.KeyID != keyID(key) {
		return fmt.Errorf("key mismatch: connection %q was encrypted with key_id=%s but the current key is key_id=%s (the Keychain key may have been replaced; re-register with 'vaulty-keeper db add %s')", name, sc.KeyID, keyID(key), name)
	}
	return fmt.Errorf("cannot decrypt connection %q (%v); it may have been registered with an old key — re-register it with 'vaulty-keeper db add %s' or remove it with 'vaulty-keeper db rm %s --yes'", name, err, name, name)
}
