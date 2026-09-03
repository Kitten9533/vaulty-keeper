package dbproxy

import (
	"crypto/md5"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/xdg-go/scram"
)

// handlePostgres implements the PostgreSQL tunnel.
//
// Client side (fake server): it answers the client's startup message with
// AuthenticationOk (trust-style), so the client needs no password at all —
// the bridge token travels in the startup message's "user" field, which is
// the only gate. ParameterStatus/BackendKeyData/ReadyForQuery are synthesized
// so the client believes the session is established.
//
// Server side (real client): it dials the real database from the registered
// URL and completes authentication with the real credentials (cleartext, md5
// or SCRAM-SHA-256). After both sides are ready the connections are spliced.
//
// Because the auth messages on both sides are consumed by pgproto3 and the
// real server's ParameterStatus/ReadyForQuery are re-synthesized for the
// client, the raw byte splice carries only the session traffic that follows.
func handlePostgres(client net.Conn, u *url.URL, token string) error {
	// ---- client side: fake server ----
	backend := pgproto3.NewBackend(client, client)
	var startup *pgproto3.StartupMessage
	for {
		msg, err := backend.ReceiveStartupMessage()
		if err != nil {
			return fmt.Errorf("client startup: %w", err)
		}
		switch m := msg.(type) {
		case *pgproto3.SSLRequest:
			if _, err := client.Write([]byte{'N'}); err != nil {
				return err
			}
		case *pgproto3.StartupMessage:
			if !tokenOK(m.Parameters["user"], token) {
				return errors.New("invalid bridge token in user field")
			}
			startup = m
		default:
			return fmt.Errorf("unexpected startup message %T", msg)
		}
		if startup != nil {
			break
		}
	}

	backend.Send(&pgproto3.AuthenticationOk{})
	for _, ps := range []pgproto3.ParameterStatus{
		{Name: "server_version", Value: "16.0"},
		{Name: "client_encoding", Value: "UTF8"},
		{Name: "DateStyle", Value: "ISO, MDY"},
		{Name: "standard_conforming_strings", Value: "on"},
	} {
		p := ps
		backend.Send(&p)
	}
	backend.Send(&pgproto3.BackendKeyData{ProcessID: 1, SecretKey: []byte{1, 2, 3, 4}})
	backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	if err := backend.Flush(); err != nil {
		return err
	}

	// ---- server side: real client ----
	server, err := pgDial(hostPort(u.Host, 5432), u)
	if err != nil {
		return err
	}
	defer server.Close()

	if err := pgAuthenticate(server, u); err != nil {
		return err
	}

	splice(client, client, server)
	return nil
}

// pgAuthenticate connects to the real PostgreSQL server and completes
// authentication (cleartext / md5 / SCRAM-SHA-256) using the credentials in u.
// The connection must already be established (including any TLS from sslmode).
func pgAuthenticate(server net.Conn, u *url.URL) error {
	user, pass := "", ""
	if u.User != nil {
		user = u.User.Username()
		pass, _ = u.User.Password()
	}
	db := strings.TrimPrefix(u.Path, "/")
	if user == "" {
		return errors.New("URL 缺少数据库用户")
	}
	if db == "" {
		return errors.New("URL 缺少数据库名")
	}

	frontend := pgproto3.NewFrontend(server, server)
	frontend.Send(&pgproto3.StartupMessage{
		ProtocolVersion: 3 << 16,
		Parameters:      map[string]string{"user": user, "database": db},
	})
	if err := frontend.Flush(); err != nil {
		return err
	}

	var conv *scram.ClientConversation
authLoop:
	for {
		msg, err := frontend.Receive()
		if err != nil {
			return fmt.Errorf("server auth: %w", err)
		}
		switch m := msg.(type) {
		case *pgproto3.AuthenticationOk:
			break authLoop
		case *pgproto3.AuthenticationCleartextPassword:
			frontend.Send(&pgproto3.PasswordMessage{Password: pass})
			if err := frontend.Flush(); err != nil {
				return err
			}
		case *pgproto3.AuthenticationMD5Password:
			frontend.Send(&pgproto3.PasswordMessage{Password: pgMD5(pass, user, m.Salt)})
			if err := frontend.Flush(); err != nil {
				return err
			}
		case *pgproto3.AuthenticationSASL:
			mech := pickSCRAM(m.AuthMechanisms)
			if mech == "" {
				return errors.New("服务端要求的 SASL 机制不支持（仅支持 SCRAM-SHA-256）")
			}
			c, err := scram.SHA256.NewClient(user, pass, "")
			if err != nil {
				return err
			}
			conv = c.NewConversation()
			clientFirst, err := conv.Step("")
			if err != nil {
				return fmt.Errorf("scram client-first: %w", err)
			}
			frontend.Send(&pgproto3.SASLInitialResponse{AuthMechanism: mech, Data: []byte(clientFirst)})
			if err := frontend.Flush(); err != nil {
				return err
			}
		case *pgproto3.AuthenticationSASLContinue:
			clientFinal, err := conv.Step(string(m.Data))
			if err != nil {
				return fmt.Errorf("scram continue: %w", err)
			}
			frontend.Send(&pgproto3.SASLResponse{Data: []byte(clientFinal)})
			if err := frontend.Flush(); err != nil {
				return err
			}
		case *pgproto3.AuthenticationSASLFinal:
			if _, err := conv.Step(string(m.Data)); err != nil {
				return fmt.Errorf("scram server verify: %w", err)
			}
		case *pgproto3.ErrorResponse:
			return fmt.Errorf("服务端认证失败：%s", m.Message)
		case *pgproto3.ParameterStatus, *pgproto3.BackendKeyData, *pgproto3.ReadyForQuery:
			// Consumed here; the client already received synthesized equivalents.
		}
	}
	return nil
}

// pgDial connects to the real PostgreSQL server, honoring sslmode from the
// URL query (require/verify-ca/verify-full force TLS, prefer/allow try TLS
// first, disable/absent use plaintext).
func pgDial(addr string, u *url.URL) (net.Conn, error) {
	mode := strings.ToLower(u.Query().Get("sslmode"))
	required := mode == "require" || mode == "verify-ca" || mode == "verify-full"
	preferred := mode == "prefer" || mode == "allow"
	if required || preferred {
		dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: testDialTimeout}, Config: &tls.Config{ServerName: hostOnly(u.Host)}}
		conn, err := dialer.Dial("tcp", addr)
		if err == nil {
			return conn, nil
		}
		if required {
			return nil, fmt.Errorf("dial %s (tls): %w", addr, err)
		}
	}
	conn, err := net.DialTimeout("tcp", addr, testDialTimeout)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	return conn, nil
}

// pgMD5 computes the PostgreSQL md5 password response.
func pgMD5(password, user string, salt [4]byte) string {
	h := md5.New()
	h.Write([]byte(password))
	h.Write([]byte(user))
	inner := hex.EncodeToString(h.Sum(nil))
	h2 := md5.New()
	h2.Write([]byte(inner))
	h2.Write(salt[:])
	return "md5" + hex.EncodeToString(h2.Sum(nil))
}

// pickSCRAM selects a non-channel-binding SCRAM mechanism.
func pickSCRAM(mechs []string) string {
	for _, m := range mechs {
		if m == "SCRAM-SHA-256" {
			return m
		}
	}
	return ""
}
