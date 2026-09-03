package dbproxy

import (
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgproto3"
)

// fakePG is a minimal PostgreSQL server for tests: it demands a cleartext
// password, authenticates against a fixed credential, then answers one
// "SELECT 1" style query before closing.
type fakePG struct {
	ln   net.Listener
	user string
	pass string
}

func newFakePG(t *testing.T, user, pass string) *fakePG {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakePG{ln: ln, user: user, pass: pass}
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

func (f *fakePG) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *fakePG) handle(c net.Conn) {
	defer c.Close()
	be := pgproto3.NewBackend(c, c)
	msg, err := be.ReceiveStartupMessage()
	if err != nil {
		return
	}
	sm, ok := msg.(*pgproto3.StartupMessage)
	if !ok {
		return
	}
	if sm.Parameters["user"] != f.user {
		be.Send(&pgproto3.ErrorResponse{Message: "unknown user"})
		be.Flush()
		return
	}
	// demand cleartext password
	be.Send(&pgproto3.AuthenticationCleartextPassword{})
	if err := be.Flush(); err != nil {
		return
	}
	pm, err := be.Receive()
	if err != nil {
		return
	}
	pw, ok := pm.(*pgproto3.PasswordMessage)
	if !ok || pw.Password != f.pass {
		be.Send(&pgproto3.ErrorResponse{Message: "bad password"})
		be.Flush()
		return
	}
	be.Send(&pgproto3.AuthenticationOk{})
	be.Send(&pgproto3.ParameterStatus{Name: "server_version", Value: "16.0"})
	be.Send(&pgproto3.BackendKeyData{ProcessID: 42, SecretKey: []byte{9, 9, 9, 9}})
	be.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	if err := be.Flush(); err != nil {
		return
	}

	// one query round-trip to prove the splice carries real traffic
	fe, err := be.Receive()
	if err != nil {
		return
	}
	q, ok := fe.(*pgproto3.Query)
	if !ok || q.String != "SELECT 1" {
		be.Send(&pgproto3.ErrorResponse{Message: "unexpected query"})
		be.Flush()
		return
	}
	be.Send(&pgproto3.RowDescription{Fields: []pgproto3.FieldDescription{
		{Name: []byte("?column?"), DataTypeOID: 23},
	}})
	be.Send(&pgproto3.DataRow{Values: [][]byte{[]byte("1")}})
	be.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
	be.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	be.Flush()
}

func TestPostgresTunnel(t *testing.T) {
	fake := newFakePG(t, "app", "realpass")
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	key := testKey(t)
	tunnelPort := freePort(t)
	realURL := fmt.Sprintf("postgres://app:realpass@127.0.0.1:%d/appdb", fake.port())
	if err := Add(path, key, "prod", realURL, tunnelPort); err != nil {
		t.Fatal(err)
	}
	const token = "pg-tok"
	startTunnel(t, path, key, tunnelPort, token)

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tunnelPort))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// client authenticates with the token in the user field, no password
	fe := pgproto3.NewFrontend(conn, conn)
	fe.Send(&pgproto3.StartupMessage{
		ProtocolVersion: 3 << 16,
		Parameters:      map[string]string{"user": token, "database": "whatever"},
	})
	if err := fe.Flush(); err != nil {
		t.Fatal(err)
	}

	// expect auth ok + parameter status + ready for query
	sawAuth := false
	sawReady := false
	for !sawReady {
		msg, err := fe.Receive()
		if err != nil {
			t.Fatalf("receive: %v", err)
		}
		switch msg.(type) {
		case *pgproto3.AuthenticationOk:
			sawAuth = true
		case *pgproto3.ReadyForQuery:
			sawReady = true
		}
	}
	if !sawAuth {
		t.Fatal("did not receive AuthenticationOk")
	}

	// issue a query through the spliced tunnel
	fe.Send(&pgproto3.Query{String: "SELECT 1"})
	if err := fe.Flush(); err != nil {
		t.Fatal(err)
	}
	gotRow := false
	for !gotRow {
		msg, err := fe.Receive()
		if err != nil {
			t.Fatalf("query receive: %v", err)
		}
		if dr, ok := msg.(*pgproto3.DataRow); ok && len(dr.Values) == 1 && string(dr.Values[0]) == "1" {
			gotRow = true
		}
		if _, ok := msg.(*pgproto3.ReadyForQuery); ok {
			break
		}
	}
	if !gotRow {
		t.Fatal("did not receive the query result row")
	}
}

func TestPostgresTunnelRejectsBadToken(t *testing.T) {
	fake := newFakePG(t, "app", "realpass")
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	key := testKey(t)
	tunnelPort := freePort(t)
	realURL := fmt.Sprintf("postgres://app:realpass@127.0.0.1:%d/appdb", fake.port())
	if err := Add(path, key, "prod", realURL, tunnelPort); err != nil {
		t.Fatal(err)
	}
	startTunnel(t, path, key, tunnelPort, "good-tok")

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tunnelPort))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fe := pgproto3.NewFrontend(conn, conn)
	fe.Send(&pgproto3.StartupMessage{
		ProtocolVersion: 3 << 16,
		Parameters:      map[string]string{"user": "wrong", "database": "x"},
	})
	if err := fe.Flush(); err != nil {
		t.Fatal(err)
	}
	// The proxy must not answer; reading should hit EOF/error quickly.
	conn.SetReadDeadline(nowAdd(2))
	if _, err := fe.Receive(); err == nil {
		t.Fatal("bad token connection got a reply, want closure")
	}
}

func nowAdd(ms int64) time.Time {
	return time.Now().Add(time.Duration(ms) * time.Millisecond)
}
