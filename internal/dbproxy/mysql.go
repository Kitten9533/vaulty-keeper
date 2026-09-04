package dbproxy

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
)

// MySQL capability flags used by the proxy.
const (
	myClientConnectWithDB    = 1 << 3
	myClientProtocol41       = 1 << 9
	myClientSSL              = 1 << 11
	myClientSecureConnection = 1 << 15
	myClientPluginAuth       = 1 << 19
)

const myServerVersion = "5.7.0-proxy"

// handleMySQL implements the MySQL tunnel.
//
// Client side (fake server): it sends a HandshakeV10 advertising
// mysql_native_password (no SSL), reads the client's HandshakeResponse41,
// validates the username field as the bridge token, and replies OK — the
// client never needs a real password.
//
// Server side (real client): it reads the real server's HandshakeV10, computes
// the auth response for the registered password (mysql_native_password,
// caching_sha2_password fast/full auth) and completes the handshake, including
// the TLS upgrade when the URL has tls=true. Then the raw byte streams are
// spliced.
func handleMySQL(client net.Conn, u *url.URL, globalToken, connToken string) error {
	// ---- client side: fake server ----
	clientBR := bufio.NewReader(client)
	salt := make([]byte, 20)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	if err := myWritePacket(client, 0, myHandshakeV10(myServerVersion, 1, salt, "mysql_native_password")); err != nil {
		return err
	}
	_, payload, err := myReadPacket(clientBR)
	if err != nil {
		return fmt.Errorf("client handshake response: %w", err)
	}
	user, err := myParseResponseUser(payload)
	if err != nil {
		return err
	}
	if !tokenOKAny(user, globalToken, connToken) {
		return errors.New("invalid bridge token in username field")
	}
	if err := myWritePacket(client, 2, myOKPacket()); err != nil {
		return err
	}

	// ---- server side: real client ----
	server, err := net.Dial("tcp", hostPort(u.Host, 3306))
	if err != nil {
		return fmt.Errorf("dial %s: %w", hostPort(u.Host, 3306), err)
	}
	defer server.Close()

	if err := myAuthenticate(server, u); err != nil {
		return err
	}

	splice(clientBR, client, server)
	return nil
}

// myAuthenticate completes the MySQL client-side handshake against the real
// server using the credentials in u: native/caching_sha2 auth responses,
// full-auth (RSA/TLS) exchange and auth switches. The connection must already
// be established. Returns nil once the server sends OK/EOF.
func myAuthenticate(server net.Conn, u *url.URL) error {
	user, pass := "", ""
	if u.User != nil {
		user = u.User.Username()
		pass, _ = u.User.Password()
	}
	db := strings.TrimPrefix(u.Path, "/")

	seq, hs, err := myReadPacket(server)
	if err != nil {
		return fmt.Errorf("server handshake: %w", err)
	}
	serverSalt, plugin, err := myParseHandshakeV10(hs)
	if err != nil {
		return err
	}
	authResp, err := myAuthResponse(plugin, pass, serverSalt)
	if err != nil {
		return err
	}

	useTLS := strings.EqualFold(u.Query().Get("tls"), "true")
	caps := uint32(myClientProtocol41 | myClientSecureConnection | myClientPluginAuth)
	if db != "" {
		caps |= myClientConnectWithDB
	}
	prefix := myHandshakeResponsePrefix(caps)
	body := myHandshakeResponseBody(caps, user, authResp, db, plugin)

	if useTLS {
		// 1) SSLRequest (prefix only) over plaintext, 2) TLS handshake,
		// 3) full handshake response inside TLS.
		if err := myWritePacket(server, seq+1, prefix); err != nil {
			return err
		}
		tlsConn := tls.Client(server, &tls.Config{ServerName: hostOnly(u.Host)})
		if err := tlsConn.Handshake(); err != nil {
			return fmt.Errorf("tls handshake: %w", err)
		}
		server = tlsConn
		if err := myWritePacket(server, seq+2, append(prefix, body...)); err != nil {
			return err
		}
	} else {
		if err := myWritePacket(server, seq+1, append(prefix, body...)); err != nil {
			return err
		}
	}

	// Read auth result; handle plugin auth data / auth switches.
	authSeq := seq + 2
	for {
		s, p, err := myReadPacket(server)
		if err != nil {
			return fmt.Errorf("server auth result: %w", err)
		}
		authSeq = s
		if len(p) == 0 {
			continue
		}
		switch p[0] {
		case 0x00: // OK
			return nil
		case 0xff: // ERR
			return fmt.Errorf("server authentication failed: %s", myParseErr(p))
		case 0x01: // AuthMoreData (plugin extra data)
			if len(p) < 2 {
				continue
			}
			switch p[1] {
			case 0x04: // caching_sha2_password: full authentication required
				if _, err := myFullAuth(server, pass, serverSalt, useTLS, authSeq); err != nil {
					return err
				}
			case 0x03: // caching_sha2_password: fast auth success (OK follows)
			}
			continue
		case 0xfe:
			if len(p) == 1 { // EOF (old protocol): authenticated
				return nil
			}
			// AuthSwitchRequest: 0xfe + plugin name (NUL-terminated) + new salt.
			rest := p[1:]
			nul := bytes.IndexByte(rest, 0)
			if nul < 0 {
				return errors.New("malformed auth switch request")
			}
			newPlugin := string(rest[:nul])
			newSalt := rest[nul+1:]
			newSalt = bytes.TrimSuffix(newSalt, []byte{0})
			authResp, err := myAuthResponse(newPlugin, pass, newSalt)
			if err != nil {
				return err
			}
			if err := myWritePacket(server, authSeq+1, authResp); err != nil {
				return err
			}
			authSeq++
			continue
		}
	}
}

// myFullAuth completes caching_sha2/sha256 full authentication: cleartext
// password over TLS, or an RSA-OAEP(SHA1) encrypted password over plaintext
// (with the password XOR-scrambled by seed, per the caching_sha2 protocol).
// It returns the sequence number of the last packet written.
func myFullAuth(server net.Conn, pass string, seed []byte, useTLS bool, seq byte) (byte, error) {
	plain := append([]byte(pass), 0)
	if useTLS {
		if err := myWritePacket(server, seq+1, plain); err != nil {
			return seq, err
		}
		return seq + 1, nil
	}
	if err := myWritePacket(server, seq+1, []byte{0x02}); err != nil { // request public key
		return seq, err
	}
	keySeq, keyPayload, err := myReadPacket(server)
	if err != nil {
		return seq, err
	}
	keyData := bytes.TrimSpace(bytes.TrimPrefix(keyPayload, []byte{0x01}))
	pub, err := parseRSAPublicKey(keyData)
	if err != nil {
		return seq, fmt.Errorf("cannot parse server RSA public key: %w", err)
	}
	for i := range plain {
		plain[i] ^= seed[i%len(seed)]
	}
	enc, err := rsa.EncryptOAEP(sha1.New(), rand.Reader, pub, plain, nil)
	if err != nil {
		return seq, err
	}
	if err := myWritePacket(server, keySeq+1, enc); err != nil {
		return seq, err
	}
	return keySeq + 1, nil
}

func parseRSAPublicKey(data []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("no PEM block")
	}
	if bytes.Contains(data, []byte("RSA PUBLIC KEY")) {
		k, err := x509.ParsePKCS1PublicKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		return k, nil
	}
	k, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	pub, ok := k.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("not an RSA key")
	}
	return pub, nil
}

// myAuthResponse computes the auth response for a plugin.
func myAuthResponse(plugin, pass string, salt []byte) ([]byte, error) {
	switch plugin {
	case "mysql_native_password":
		return myNativeScramble(pass, salt), nil
	case "caching_sha2_password":
		return mySHA2Fast(pass, salt), nil
	case "sha256_password":
		return []byte{0x01}, nil // full auth follows via TLS/RSA exchange
	default:
		return nil, fmt.Errorf("unsupported authentication plugin %q (supported: mysql_native_password / caching_sha2_password)", plugin)
	}
}

func myNativeScramble(pass string, salt []byte) []byte {
	// token = SHA1(password) XOR SHA1(salt + SHA1(SHA1(password)))
	h1 := sha1.Sum([]byte(pass)) // SHA1(password)
	h2 := sha1.Sum(h1[:])        // SHA1(SHA1(password))
	h := sha1.New()
	h.Write(salt)
	h.Write(h2[:])
	token := h.Sum(nil)
	out := make([]byte, len(h1))
	for i := range out {
		out[i] = h1[i] ^ token[i]
	}
	return out
}

func mySHA2Fast(pass string, salt []byte) []byte {
	// scramble = SHA256(password) XOR SHA256(SHA256(SHA256(password)) + salt)
	stage1 := sha256.Sum256([]byte(pass)) // SHA256(password)
	stage2 := sha256.Sum256(stage1[:])    // SHA256(SHA256(password))
	h := sha256.New()
	h.Write(stage2[:])
	h.Write(salt) // SHA256(SHA256(SHA256(password)) + salt)
	token := h.Sum(nil)
	out := make([]byte, len(stage1))
	for i := range out {
		out[i] = stage1[i] ^ token[i]
	}
	return out
}

func myParseErr(p []byte) string {
	if len(p) > 7 {
		return string(p[7:])
	}
	return "mysql error"
}

// myOKPacket builds a valid protocol-41 OK packet: header, affected rows,
// last insert id, 2-byte status flags, 2-byte warnings.
func myOKPacket() []byte {
	return []byte{0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00}
}

// ---- MySQL packet framing ----

func myWritePacket(w io.Writer, seq byte, payload []byte) error {
	hdr := make([]byte, 4)
	hdr[0] = byte(len(payload))
	hdr[1] = byte(len(payload) >> 8)
	hdr[2] = byte(len(payload) >> 16)
	hdr[3] = seq
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func myReadPacket(r io.Reader) (seq byte, payload []byte, err error) {
	hdr := make([]byte, 4)
	if _, err = io.ReadFull(r, hdr); err != nil {
		return 0, nil, err
	}
	n := int(hdr[0]) | int(hdr[1])<<8 | int(hdr[2])<<16
	payload = make([]byte, n)
	if _, err = io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	return hdr[3], payload, nil
}

// myHandshakeV10 builds the server greeting.
func myHandshakeV10(serverVersion string, connID uint32, salt []byte, plugin string) []byte {
	caps := uint32(myClientProtocol41 | myClientSecureConnection | myClientPluginAuth | myClientConnectWithDB)
	var b bytes.Buffer
	b.WriteByte(0x0a)
	b.WriteString(serverVersion)
	b.WriteByte(0)
	var id [4]byte
	binary.LittleEndian.PutUint32(id[:], connID)
	b.Write(id[:])
	b.Write(salt[:8])
	b.WriteByte(0)
	var c16 [2]byte
	binary.LittleEndian.PutUint16(c16[:], uint16(caps&0xffff))
	b.Write(c16[:])
	b.WriteByte(0x21) // utf8_general_ci
	var st [2]byte
	binary.LittleEndian.PutUint16(st[:], 2) // SERVER_STATUS_AUTOCOMMIT
	b.Write(st[:])
	binary.LittleEndian.PutUint16(c16[:], uint16(caps>>16))
	b.Write(c16[:])
	b.WriteByte(21)
	b.Write(make([]byte, 10))
	b.Write(salt[8:])
	b.WriteByte(0)
	b.WriteString(plugin)
	b.WriteByte(0)
	return b.Bytes()
}

// myParseHandshakeV10 extracts the 20-byte salt and auth plugin from the
// server greeting. The salt is the 8-byte part1 plus the first 12 bytes of
// part2 (part2's trailing NUL is not part of the salt).
func myParseHandshakeV10(p []byte) (salt []byte, plugin string, err error) {
	if len(p) < 32 || p[0] != 0x0a {
		return nil, "", errors.New("invalid MySQL handshake packet")
	}
	i := 1
	for i < len(p) && p[i] != 0 {
		i++
	}
	i++ // server version NUL
	if i+4+8+1+2+1+2+2+1 > len(p) {
		return nil, "", errors.New("MySQL handshake packet too short")
	}
	i += 4 // connection id
	part1Start := i
	i += 8 // auth-plugin-data-part-1
	i += 1 // filler
	i += 2 // capability lower
	i += 1 // charset
	i += 2 // status
	i += 2 // capability upper
	authLen := int(p[i])
	i += 1
	i += 10 // reserved
	if authLen == 0 {
		authLen = 21
	}
	part2Len := authLen - 8
	if part2Len > 13 {
		part2Len = 13
	}
	if i+part2Len > len(p) {
		return nil, "", errors.New("MySQL handshake packet has insufficient salt")
	}
	salt = append(salt, p[part1Start:part1Start+8]...)
	part2 := p[i : i+part2Len]
	if n := bytes.IndexByte(part2, 0); n >= 0 {
		part2 = part2[:n]
	}
	salt = append(salt, part2...)
	i += part2Len
	if i < len(p) && p[i] == 0 {
		i++
	}
	if i < len(p) {
		end := bytes.IndexByte(p[i:], 0)
		if end < 0 {
			end = len(p) - i
		}
		plugin = string(p[i : i+end])
	}
	if plugin == "" {
		plugin = "mysql_native_password"
	}
	return salt, plugin, nil
}

// myHandshakeResponsePrefix builds the fixed 32-byte prefix shared by the
// SSLRequest packet and the full handshake response.
func myHandshakeResponsePrefix(caps uint32) []byte {
	b := make([]byte, 32)
	binary.LittleEndian.PutUint32(b[0:], caps)
	binary.LittleEndian.PutUint32(b[4:], 16*1024*1024) // max packet size
	b[8] = 0x21                                        // utf8_general_ci
	return b
}

// myHandshakeResponseBody builds the variable part after the prefix.
func myHandshakeResponseBody(caps uint32, user string, authResp []byte, db, plugin string) []byte {
	var b bytes.Buffer
	b.WriteString(user)
	b.WriteByte(0)
	b.WriteByte(byte(len(authResp)))
	b.Write(authResp)
	if caps&myClientConnectWithDB != 0 {
		b.WriteString(db)
		b.WriteByte(0)
	}
	if caps&myClientPluginAuth != 0 {
		b.WriteString(plugin)
		b.WriteByte(0)
	}
	return b.Bytes()
}

// myParseResponseUser extracts the username from the client's handshake
// response (fixed 32-byte header, then a NUL-terminated username).
func myParseResponseUser(p []byte) (string, error) {
	if len(p) < 32 {
		return "", errors.New("client handshake response too short")
	}
	end := bytes.IndexByte(p[32:], 0)
	if end < 0 {
		return "", errors.New("client handshake response is missing a username")
	}
	return string(p[32 : 32+end]), nil
}
