package dbproxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeRedis is a minimal in-memory Redis server speaking RESP: it demands
// AUTH with a fixed password and answers PING/GET.
type fakeRedis struct {
	ln   net.Listener
	pass string
	vals map[string]string
}

func newFakeRedis(t *testing.T, pass string) *fakeRedis {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeRedis{ln: ln, pass: pass, vals: map[string]string{"foo": "bar"}}
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

func (f *fakeRedis) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *fakeRedis) handle(c net.Conn) {
	defer c.Close()
	br := bufio.NewReader(c)
	authed := f.pass == ""
	for {
		args, err := readRESPCommand(br)
		if err != nil {
			return
		}
		switch strings.ToUpper(string(args[0])) {
		case "AUTH":
			if len(args) == 2 && string(args[1]) == f.pass {
				authed = true
				c.Write([]byte("+OK\r\n"))
			} else {
				c.Write([]byte("-ERR invalid password\r\n"))
			}
		case "PING":
			if !authed {
				c.Write([]byte("-NOAUTH Authentication required.\r\n"))
				continue
			}
			c.Write([]byte("+PONG\r\n"))
		case "GET":
			if !authed {
				c.Write([]byte("-NOAUTH Authentication required.\r\n"))
				continue
			}
			v := f.vals[string(args[1])]
			c.Write([]byte(fmt.Sprintf("$%d\r\n%s\r\n", len(v), v)))
		default:
			c.Write([]byte("-ERR unknown command\r\n"))
		}
	}
}

// freePort returns an unused TCP port on 127.0.0.1.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func readRESPReplyLine(t *testing.T, conn net.Conn) string {
	t.Helper()
	var sb strings.Builder
	buf := make([]byte, 1)
	for {
		if _, err := conn.Read(buf); err != nil {
			t.Fatalf("read reply: %v", err)
		}
		if buf[0] == '\n' {
			break
		}
		sb.WriteByte(buf[0])
	}
	return strings.TrimSuffix(sb.String(), "\r")
}

// readRESPBulk reads a "$<n>\r\n<data>\r\n" reply and returns the data.
func readRESPBulk(t *testing.T, conn net.Conn) string {
	t.Helper()
	line := readRESPReplyLine(t, conn)
	if !strings.HasPrefix(line, "$") {
		t.Fatalf("expected bulk reply, got %q", line)
	}
	n := 0
	fmt.Sscanf(line[1:], "%d", &n)
	buf := make([]byte, n+2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read bulk body: %v", err)
	}
	return string(buf[:n])
}

func startTunnel(t *testing.T, path string, key []byte, port int, token string) {
	t.Helper()
	tun := &Tunnel{Path: path, Key: key, Host: "127.0.0.1", Token: token, Log: io.Discard}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- tun.Start(ctx) }()
	waitPort(t, port)
}

func waitPort(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
		if err == nil {
			c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("tunnel on port %d did not accept within timeout", port)
}

func TestRedisTunnel(t *testing.T) {
	fake := newFakeRedis(t, "realpass")
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	key := testKey(t)
	tunnelPort := freePort(t)
	realURL := fmt.Sprintf("redis://:realpass@127.0.0.1:%d/0", fake.port())
	if err := Add(path, key, "cache", realURL, tunnelPort); err != nil {
		t.Fatal(err)
	}
	const token = "tok-123"
	startTunnel(t, path, key, tunnelPort, token)

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tunnelPort))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// client authenticates with the bridge token, not the real password
	if _, err := conn.Write(respBulk("AUTH", token)); err != nil {
		t.Fatal(err)
	}
	if got := readRESPReplyLine(t, conn); got != "+OK" {
		t.Fatalf("token auth reply = %q", got)
	}
	// query flows through to the real server
	if _, err := conn.Write(respBulk("GET", "foo")); err != nil {
		t.Fatal(err)
	}
	if got := readRESPBulk(t, conn); got != "bar" {
		t.Fatalf("GET foo = %q, want bar", got)
	}
}

func TestRedisTunnelRejectsBadToken(t *testing.T) {
	fake := newFakeRedis(t, "realpass")
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	key := testKey(t)
	tunnelPort := freePort(t)
	realURL := fmt.Sprintf("redis://:realpass@127.0.0.1:%d/0", fake.port())
	if err := Add(path, key, "cache", realURL, tunnelPort); err != nil {
		t.Fatal(err)
	}
	startTunnel(t, path, key, tunnelPort, "tok-123")

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tunnelPort))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write(respBulk("AUTH", "wrong")); err != nil {
		t.Fatal(err)
	}
	if got := readRESPReplyLine(t, conn); !strings.HasPrefix(got, "-ERR") {
		t.Fatalf("bad token reply = %q, want -ERR", got)
	}
}

func TestRedisTunnelACLStyleAuth(t *testing.T) {
	fake := newFakeRedis(t, "realpass")
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	key := testKey(t)
	tunnelPort := freePort(t)
	realURL := fmt.Sprintf("redis://:realpass@127.0.0.1:%d/0", fake.port())
	if err := Add(path, key, "cache", realURL, tunnelPort); err != nil {
		t.Fatal(err)
	}
	const token = "tok-456"
	startTunnel(t, path, key, tunnelPort, token)

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tunnelPort))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// URL-style clients send AUTH "" <token> (3 args, empty username)
	if _, err := conn.Write(respBulk("AUTH", "", token)); err != nil {
		t.Fatal(err)
	}
	if got := readRESPReplyLine(t, conn); got != "+OK" {
		t.Fatalf("ACL-style auth reply = %q", got)
	}
	if _, err := conn.Write(respBulk("GET", "foo")); err != nil {
		t.Fatal(err)
	}
	if got := readRESPBulk(t, conn); got != "bar" {
		t.Fatalf("GET foo = %q, want bar", got)
	}
}

// TestTunnelHotReload verifies that tunnels start/stop when connections are
// added/removed while the tunnel server is already running (no restart).
func TestTunnelHotReload(t *testing.T) {
	fake := newFakeRedis(t, "realpass")
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	key := testKey(t)
	tunnelPort := freePort(t)

	tun := &Tunnel{Path: path, Key: key, Host: "127.0.0.1", Token: "tok", Log: io.Discard}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- tun.Start(ctx) }()

	// no connections yet -> port must be closed
	waitPortGone(t, tunnelPort)

	// add a connection while running -> tunnel appears
	realURL := fmt.Sprintf("redis://:realpass@127.0.0.1:%d/0", fake.port())
	if err := Add(path, key, "cache", realURL, tunnelPort); err != nil {
		t.Fatal(err)
	}
	waitPort(t, tunnelPort)

	// tunnel actually works
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tunnelPort))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(respBulk("AUTH", "tok")); err != nil {
		t.Fatal(err)
	}
	if got := readRESPReplyLine(t, conn); got != "+OK" {
		t.Fatalf("auth reply = %q", got)
	}
	conn.Close()

	// remove the connection while running -> tunnel disappears
	if err := Remove(path, key, "cache"); err != nil {
		t.Fatal(err)
	}
	waitPortGone(t, tunnelPort)
}

// waitPortGone waits until the port stops accepting connections.
func waitPortGone(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
		if err != nil {
			return
		}
		c.Close()
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("port %d still accepting after removal", port)
}
