package dbproxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeMySQL is a minimal MySQL server speaking mysql_native_password: it
// verifies the native scramble, replies OK, then answers one COM_QUERY with a
// fixed packet so the test can prove the spliced stream carries traffic.
type fakeMySQL struct {
	ln   net.Listener
	pass string
	salt []byte
}

func newFakeMySQL(t *testing.T, pass string) *fakeMySQL {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeMySQL{ln: ln, pass: pass, salt: []byte("0123456789abcdefghij")}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.handle(c)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return f
}

func (f *fakeMySQL) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *fakeMySQL) handle(c net.Conn) {
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if err := myWritePacket(c, 0, myHandshakeV10("8.0.0-fake", 7, f.salt, "mysql_native_password")); err != nil {
		return
	}
	_, payload, err := myReadPacket(c)
	if err != nil {
		return
	}
	user, auth, db, _, err := myParseResponse(payload)
	if err != nil || user != "app" || db != "appdb" {
		return
	}
	if !bytes.Equal(auth, myNativeScramble(f.pass, f.salt)) {
		return
	}
	if err := myWritePacket(c, 2, myOKPacket()); err != nil {
		return
	}
	seq, payload, err := myReadPacket(c)
	if err != nil {
		return
	}
	if len(payload) > 0 && payload[0] == 0x03 { // COM_QUERY
		// minimal OK-style reply; the client only checks bytes pass through
		myWritePacket(c, seq+1, myOKPacket())
	}
}

// myParseResponse extracts user/auth/db/plugin from a client handshake
// response (fixed 32-byte prefix, NUL-terminated user, 1-byte-length auth).
func myParseResponse(p []byte) (user string, auth []byte, db, plugin string, err error) {
	if len(p) < 32 {
		return "", nil, "", "", fmt.Errorf("short response")
	}
	caps := uint32(p[0]) | uint32(p[1])<<8 | uint32(p[2])<<16 | uint32(p[3])<<24
	i := 32
	end := bytes.IndexByte(p[i:], 0)
	if end < 0 {
		return "", nil, "", "", fmt.Errorf("no username")
	}
	user = string(p[i : i+end])
	i += end + 1
	if caps&myClientSecureConnection != 0 {
		if i >= len(p) {
			return "", nil, "", "", fmt.Errorf("no auth length")
		}
		n := int(p[i])
		i++
		if i+n > len(p) {
			return "", nil, "", "", fmt.Errorf("short auth")
		}
		auth = p[i : i+n]
		i += n
	}
	if caps&myClientConnectWithDB != 0 && i < len(p) {
		end = bytes.IndexByte(p[i:], 0)
		if end >= 0 {
			db = string(p[i : i+end])
			i += end + 1
		}
	}
	if caps&myClientPluginAuth != 0 && i < len(p) {
		end = bytes.IndexByte(p[i:], 0)
		if end >= 0 {
			plugin = string(p[i : i+end])
		}
	}
	return user, auth, db, plugin, nil
}

func TestMySQLTunnel(t *testing.T) {
	fake := newFakeMySQL(t, "realpass")
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	key := testKey(t)
	tunnelPort := freePort(t)
	realURL := fmt.Sprintf("mysql://app:realpass@127.0.0.1:%d/appdb", fake.port())
	if err := Add(path, key, "orders", realURL, tunnelPort); err != nil {
		t.Fatal(err)
	}
	const token = "mysql-tok"
	startTunnel(t, path, key, tunnelPort, token)

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tunnelPort))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// handshake against the tunnel's fake server
	_, hs, err := myReadPacket(conn)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := myParseHandshakeV10(hs); err != nil {
		t.Fatal(err)
	}
	caps := uint32(myClientProtocol41 | myClientSecureConnection | myClientPluginAuth | myClientConnectWithDB)
	resp := append(myHandshakeResponsePrefix(caps),
		myHandshakeResponseBody(caps, token, []byte("whatever"), "ignored", "mysql_native_password")...)
	if err := myWritePacket(conn, 1, resp); err != nil {
		t.Fatal(err)
	}
	seq, p, err := myReadPacket(conn)
	if err != nil {
		t.Fatal(err)
	}
	if len(p) == 0 || p[0] != 0x00 {
		t.Fatalf("expected OK from tunnel, got %x", p)
	}
	if !bytes.Equal(p, myOKPacket()) {
		t.Fatalf("OK packet malformed: %x (want %x)", p, myOKPacket())
	}

	// COM_QUERY flows through the spliced tunnel to the real server
	query := append([]byte{0x03}, []byte("SELECT 1")...)
	if err := myWritePacket(conn, seq+1, query); err != nil {
		t.Fatal(err)
	}
	_, reply, err := myReadPacket(conn)
	if err != nil {
		t.Fatal(err)
	}
	if len(reply) == 0 || reply[0] != 0x00 {
		t.Fatalf("expected query reply, got %x", reply)
	}
}

func TestMySQLTunnelRejectsBadToken(t *testing.T) {
	fake := newFakeMySQL(t, "realpass")
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	key := testKey(t)
	tunnelPort := freePort(t)
	realURL := fmt.Sprintf("mysql://app:realpass@127.0.0.1:%d/appdb", fake.port())
	if err := Add(path, key, "orders", realURL, tunnelPort); err != nil {
		t.Fatal(err)
	}
	startTunnel(t, path, key, tunnelPort, "good-tok")

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tunnelPort))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, _, err := myReadPacket(conn); err != nil {
		t.Fatal(err)
	}
	caps := uint32(myClientProtocol41 | myClientSecureConnection | myClientPluginAuth)
	resp := append(myHandshakeResponsePrefix(caps),
		myHandshakeResponseBody(caps, "wrong", []byte("x"), "", "mysql_native_password")...)
	if err := myWritePacket(conn, 1, resp); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := myReadPacket(conn); err == nil {
		t.Fatal("bad token connection got a reply, want closure")
	}
}

// ---- Independent reference implementations (intentionally separate from the
// code under test, so a self-consistent bug in both would be caught here). ----

func refNativeScramble(pass string, salt []byte) []byte {
	h1 := sha1.Sum([]byte(pass))
	h2 := sha1.Sum(h1[:])
	h := sha1.New()
	h.Write(salt)
	h.Write(h2[:])
	tok := h.Sum(nil)
	out := make([]byte, len(h1))
	for i := range out {
		out[i] = h1[i] ^ tok[i]
	}
	return out
}

func refSHA2Fast(pass string, salt []byte) []byte {
	s1 := sha256.Sum256([]byte(pass))
	s2 := sha256.Sum256(s1[:])
	h := sha256.New()
	h.Write(s2[:])
	h.Write(salt)
	tok := h.Sum(nil)
	out := make([]byte, len(s1))
	for i := range out {
		out[i] = s1[i] ^ tok[i]
	}
	return out
}

// fakeMySQLAuth is a configurable fake MySQL server that advertises
// caching_sha2_password and performs the full-auth (RSA) exchange using its own
// RSA key, verifying the client's password independently.
type fakeMySQLAuth struct {
	ln        net.Listener
	pass      string
	salt      []byte
	priv      *rsa.PrivateKey
	pubKeyPEM []byte
	gotQuery  string
}

func newFakeMySQLAuth(t *testing.T, pass string) *fakeMySQLAuth {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	priv, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	pkix, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeMySQLAuth{
		ln:        ln,
		pass:      pass,
		salt:      []byte("0123456789abcdefghij"),
		priv:      priv,
		pubKeyPEM: pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pkix}),
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.handle(c)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return f
}

func (f *fakeMySQLAuth) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *fakeMySQLAuth) handle(c net.Conn) {
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if err := myWritePacket(c, 0, myHandshakeV10("8.0.0-fake", 7, f.salt, "caching_sha2_password")); err != nil {
		return
	}
	_, payload, err := myReadPacket(c)
	if err != nil {
		return
	}
	user, auth, _, _, err := myParseResponse(payload)
	if err != nil || user != "app" {
		return
	}
	// verify fast scramble with the reference implementation
	if !bytes.Equal(auth, refSHA2Fast(f.pass, f.salt)) {
		return // treat as auth failure
	}
	// always demand full auth (0x01 0x04) to exercise the RSA path
	if err := myWritePacket(c, 2, []byte{0x01, 0x04}); err != nil {
		return
	}
	seq, p, err := myReadPacket(c)
	if err != nil || len(p) != 1 || p[0] != 0x02 {
		return // expect public key request
	}
	// send RSA public key (0x01 + PEM)
	if err := myWritePacket(c, seq+1, append([]byte{0x01}, f.pubKeyPEM...)); err != nil {
		return
	}
	seq, enc, err := myReadPacket(c)
	if err != nil {
		return
	}
	plain, err := rsa.DecryptOAEP(sha1.New(), rand.Reader, f.priv, enc, nil)
	if err != nil {
		return
	}
	// undo the XOR scramble with the salt
	for i := range plain {
		plain[i] ^= f.salt[i%len(f.salt)]
	}
	want := append([]byte(f.pass), 0)
	if !bytes.Equal(plain, want) {
		return
	}
	if err := myWritePacket(c, seq+1, myOKPacket()); err != nil {
		return
	}
	seq, payload, err = myReadPacket(c)
	if err != nil || len(payload) == 0 || payload[0] != 0x03 {
		return
	}
	f.gotQuery = string(payload[1:])
	myWritePacket(c, seq+1, myOKPacket())
}

// TestMySQLTunnelCachingSHA2 exercises the caching_sha2 full-auth (RSA) path
// end to end through the tunnel against an independently-verified fake server.
func TestMySQLTunnelCachingSHA2(t *testing.T) {
	fake := newFakeMySQLAuth(t, "sha2pass")
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	key := testKey(t)
	tunnelPort := freePort(t)
	realURL := fmt.Sprintf("mysql://app:sha2pass@127.0.0.1:%d/shop", fake.port())
	if err := Add(path, key, "m", realURL, tunnelPort); err != nil {
		t.Fatal(err)
	}
	tun := &Tunnel{Path: path, Key: key, Host: "127.0.0.1", Token: "tok", Log: os.Stderr}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- tun.Start(ctx) }()
	waitPort(t, tunnelPort)
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tunnelPort))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, hs, err := myReadPacket(conn)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := myParseHandshakeV10(hs); err != nil {
		t.Fatal(err)
	}
	caps := uint32(myClientProtocol41 | myClientSecureConnection | myClientPluginAuth | myClientConnectWithDB)
	resp := append(myHandshakeResponsePrefix(caps),
		myHandshakeResponseBody(caps, "tok", []byte("x"), "shop", "caching_sha2_password")...)
	if err := myWritePacket(conn, 1, resp); err != nil {
		t.Fatal(err)
	}
	seq, p, err := myReadPacket(conn)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(p, myOKPacket()) {
		t.Fatalf("expected OK from tunnel, got %x", p)
	}
	// COM_QUERY flows through
	if err := myWritePacket(conn, seq+1, []byte{0x03, 'S', 'E', 'L', 'E', 'C', 'T', ' ', '1'}); err != nil {
		t.Fatal(err)
	}
	_, reply, err := myReadPacket(conn)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reply, myOKPacket()) {
		t.Fatalf("expected query reply, got %x", reply)
	}
	if fake.gotQuery != "SELECT 1" {
		t.Fatalf("fake server got query %q", fake.gotQuery)
	}
}

// fakeMySQLSwitch advertises caching_sha2_password but then sends an
// AuthSwitchRequest to mysql_native_password, verifying the client's native
// scramble with an independent reference implementation.
type fakeMySQLSwitch struct {
	ln   net.Listener
	pass string
	salt []byte
}

func newFakeMySQLSwitch(t *testing.T, pass string) *fakeMySQLSwitch {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeMySQLSwitch{ln: ln, pass: pass, salt: []byte("abcdefghijklmnopqrst")}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.handle(c)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return f
}

func (f *fakeMySQLSwitch) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *fakeMySQLSwitch) handle(c net.Conn) {
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if err := myWritePacket(c, 0, myHandshakeV10("8.0.0-fake", 7, f.salt, "caching_sha2_password")); err != nil {
		return
	}
	_, payload, err := myReadPacket(c)
	if err != nil {
		return
	}
	user, _, _, _, err := myParseResponse(payload)
	if err != nil || user != "app" {
		return
	}
	// AuthSwitchRequest: 0xfe + "mysql_native_password\0" + new salt
	sw := append([]byte{0xfe}, []byte("mysql_native_password\x00")...)
	sw = append(sw, f.salt...)
	if err := myWritePacket(c, 2, sw); err != nil {
		return
	}
	seq, auth, err := myReadPacket(c)
	if err != nil {
		return
	}
	if !bytes.Equal(auth, refNativeScramble(f.pass, f.salt)) {
		return
	}
	if err := myWritePacket(c, seq+1, myOKPacket()); err != nil {
		return
	}
	seq, _, err = myReadPacket(c)
	if err != nil {
		return
	}
	myWritePacket(c, seq+1, myOKPacket())
}

func TestMySQLTunnelAuthSwitch(t *testing.T) {
	fake := newFakeMySQLSwitch(t, "nativepass")
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	key := testKey(t)
	tunnelPort := freePort(t)
	realURL := fmt.Sprintf("mysql://app:nativepass@127.0.0.1:%d/shop", fake.port())
	if err := Add(path, key, "m", realURL, tunnelPort); err != nil {
		t.Fatal(err)
	}
	startTunnel(t, path, key, tunnelPort, "tok")
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tunnelPort))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, hs, err := myReadPacket(conn)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := myParseHandshakeV10(hs); err != nil {
		t.Fatal(err)
	}
	caps := uint32(myClientProtocol41 | myClientSecureConnection | myClientPluginAuth | myClientConnectWithDB)
	resp := append(myHandshakeResponsePrefix(caps),
		myHandshakeResponseBody(caps, "tok", []byte("x"), "shop", "caching_sha2_password")...)
	if err := myWritePacket(conn, 1, resp); err != nil {
		t.Fatal(err)
	}
	seq, p, err := myReadPacket(conn)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(p, myOKPacket()) {
		t.Fatalf("expected OK from tunnel, got %x", p)
	}
	if err := myWritePacket(conn, seq+1, []byte{0x03, 'S', 'E', 'L', 'E', 'C', 'T', ' ', '1'}); err != nil {
		t.Fatal(err)
	}
	_, reply, err := myReadPacket(conn)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reply, myOKPacket()) {
		t.Fatalf("expected query reply, got %x", reply)
	}
}
