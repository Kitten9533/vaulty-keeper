// Package dbproxy stores encrypted database connection URLs on the host and
// exposes them through credential-injecting TCP tunnels (see tunnel.go).
//
// A connection is a name plus the raw URL (DSN) the user registered with
// `ai-tools db add`. The URL is encrypted at rest with AES-256-GCM using the
// dedicated database key (apollo.DBKey / AI_TOOLS_DB_KEY) and only decrypted
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

	// FileName is the store file inside the ai-tools home directory.
	FileName = "db.json"
)

var connNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// Conn is one decrypted connection. URL is in memory only and must never be
// serialized, logged, or returned to a tunnel client (json:"-" is a hard
// guarantee it never appears in any JSON output). Broken marks an entry whose
// ciphertext cannot be decrypted with the current key (stale key); it is still
// listed so it can be removed, but has no usable URL.
type Conn struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	URL    string `json:"-"`
	Port   int    `json:"port"`
	Broken bool   `json:"broken,omitempty"`
}

// storedConn is the on-disk representation: ciphertext plus the allocated
// tunnel port. No plaintext ever hits the file. KeyID is a plaintext
// fingerprint of the encryption key, so a key mismatch can be diagnosed
// precisely instead of surfacing as a cryptic "message authentication failed".
// Type is stored in plaintext so listing needs no decryption (a stale-key
// entry cannot block the whole list).
type storedConn struct {
	URLEnc string `json:"url_cipher"`
	Nonce  string `json:"nonce"`
	KeyID  string `json:"key_id,omitempty"`
	Type   string `json:"type,omitempty"`
	Port   int    `json:"port,omitempty"`
}

type store struct {
	Conns map[string]storedConn `json:"connections"`
}

// DefaultPath returns the store file under <home>/.ai-tools.
func DefaultPath(home string) string {
	return filepath.Join(home, ".ai-tools", FileName)
}

// ValidateConnName checks a connection name: same charset as snapshot names.
func ValidateConnName(name string) error {
	if !connNameRe.MatchString(name) {
		return errors.New("连接名必须以字母或数字开头，且只能包含字母、数字、点、横线或下划线")
	}
	return nil
}

// ConnTypeFromURL detects the database type from the URL scheme.
func ConnTypeFromURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("无法解析数据库 URL：%w", err)
	}
	switch u.Scheme {
	case "postgres", "postgresql":
		return "postgres", nil
	case "mysql":
		return "mysql", nil
	case "redis", "rediss":
		return "redis", nil
	default:
		return "", fmt.Errorf("不支持的数据库 URL scheme %q（支持 postgres/postgresql、mysql、redis/rediss）", u.Scheme)
	}
}

// keyID returns a short fingerprint of an encryption key for mismatch
// diagnostics. It is not secret: it is stored in plaintext beside the
// ciphertext so callers can tell which key a store was written with.
func keyID(key []byte) string {
	h := sha256.Sum256(key)
	return hex.EncodeToString(h[:8])
}

// encryptConn seals a URL with the database key.
func encryptConn(key []byte, raw string) (storedConn, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return storedConn{}, err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return storedConn{}, err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return storedConn{}, err
	}
	ct := g.Seal(nil, nonce, []byte(raw), nil)
	return storedConn{
		URLEnc: base64.StdEncoding.EncodeToString(ct),
		Nonce:  base64.StdEncoding.EncodeToString(nonce),
		KeyID:  keyID(key),
	}, nil
}

func (s storedConn) decryptConn(key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce, err := base64.StdEncoding.DecodeString(s.Nonce)
	if err != nil {
		return "", err
	}
	ct, err := base64.StdEncoding.DecodeString(s.URLEnc)
	if err != nil {
		return "", err
	}
	pt, err := g.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("解密连接失败：%w", err)
	}
	return string(pt), nil
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
		return nil, fmt.Errorf("读取 %s 失败：%w", path, err)
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
			return 0, fmt.Errorf("端口 %d 已被其他连接占用", requested)
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
	sc.Type = typ
	sc.Port = allocated
	s.Conns[name] = sc
	return s.save(path)
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
		return fmt.Errorf("连接 %q 不存在", name)
	}
	delete(s.Conns, name)
	return s.save(path)
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
		c := Conn{Name: n, Type: sc.Type, Port: sc.Port}
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
		return Conn{}, fmt.Errorf("连接 %q 不存在（用 'ai-tools db add' 注册）", name)
	}
	raw, err := sc.decryptConn(key)
	if err != nil {
		return Conn{}, keyMismatchErr(name, sc, key, err)
	}
	typ, err := ConnTypeFromURL(raw)
	if err != nil {
		return Conn{}, err
	}
	return Conn{Name: name, Type: typ, URL: raw, Port: sc.Port}, nil
}

// keyMismatchErr rewrites a decrypt failure into a precise diagnosis when the
// store records which key it was written with (KeyID) and it differs from the
// key being used now. Legacy entries without a KeyID still get an actionable
// message.
func keyMismatchErr(name string, sc storedConn, key []byte, err error) error {
	if sc.KeyID != "" && sc.KeyID != keyID(key) {
		return fmt.Errorf("密钥不匹配：连接 %q 是用 key_id=%s 加密的，当前密钥 key_id=%s（Keychain 密钥可能被换过；用 'ai-tools db add %s' 重新注册即可）", name, sc.KeyID, keyID(key), name)
	}
	return fmt.Errorf("连接 %q 无法解密（%v）；可能用旧密钥注册——重新注册：'ai-tools db add %s'，或删除：'ai-tools db rm %s --yes'", name, err, name, name)
}
