package dbproxy

import (
	"context"
	"crypto/subtle"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Tunnel serves one TCP listener per registered connection. A client that
// connects must authenticate with the bridge token (protocol-specific: the
// PG/MySQL username field, or the first Redis AUTH command); the proxy then
// connects to the real database using the decrypted URL and substitutes the
// real credentials during the handshake before relaying raw bytes.
//
// The real URL and credentials never leave this process: no value derived
// from them is ever written back to the client or the log.
type Tunnel struct {
	Path  string
	Key   []byte
	Host  string
	Token string
	Log   io.Writer
}

// Start runs the per-connection listeners until ctx is done. It watches
// db.json and auto-starts/stops tunnels as connections are added or removed,
// so 'vaulty-keeper db add'/'db rm' take effect without restarting serve. It
// returns only when ctx is cancelled.
func (t *Tunnel) Start(ctx context.Context) error {
	type running struct {
		ln net.Listener
	}
	var (
		mu     sync.Mutex
		active = map[string]*running{}
	)
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		for _, r := range active {
			r.ln.Close()
		}
	}()

	// sync reconciles the running tunnels with the current db.json.
	sync := func() {
		conns, err := List(t.Path, t.Key)
		if err != nil {
			fmt.Fprintf(t.Log, "dbproxy: reload failed: %v\n", err)
			return
		}
		desired := make(map[string]Conn, len(conns))
		for _, c := range conns {
			desired[c.Name] = c
		}
		mu.Lock()
		defer mu.Unlock()
		for name, c := range desired {
			if _, ok := active[name]; ok {
				continue
			}
			addr := net.JoinHostPort(t.Host, strconv.Itoa(c.Port))
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				fmt.Fprintf(t.Log, "dbproxy: %s: 监听 %s 失败：%v\n", name, addr, err)
				continue
			}
			active[name] = &running{ln: ln}
			go t.acceptLoop(ctx, ln, name)
			fmt.Fprintf(t.Log, "dbproxy: %s (%s) tunnel on %s\n", name, c.Type, addr)
		}
		for name, r := range active {
			if _, ok := desired[name]; ok {
				continue
			}
			r.ln.Close()
			delete(active, name)
			fmt.Fprintf(t.Log, "dbproxy: %s: tunnel stopped\n", name)
		}
	}

	sync()
	if len(active) == 0 {
		fmt.Fprintln(t.Log, "dbproxy: no database connections yet (use 'vaulty-keeper db add <name>', tunnels auto-start)")
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			sync()
		}
	}
}

func (t *Tunnel) acceptLoop(ctx context.Context, ln net.Listener, name string) {
	for {
		client, err := ln.Accept()
		if err != nil {
			return
		}
		go t.handle(ctx, client, name)
	}
}

func (t *Tunnel) handle(ctx context.Context, client net.Conn, name string) {
	defer client.Close()
	conn, err := Resolve(t.Path, t.Key, name)
	if err != nil {
		fmt.Fprintf(t.Log, "dbproxy: %s: resolve: %v\n", name, err)
		return
	}
	u, err := url.Parse(conn.URL)
	if err != nil {
		fmt.Fprintf(t.Log, "dbproxy: %s: bad url: %v\n", name, err)
		return
	}
	clientAddr := client.RemoteAddr().String()
	switch conn.Type {
	case "redis":
		err = handleRedis(client, u, t.Token, conn.Token)
	case "postgres":
		err = handlePostgres(client, u, t.Token, conn.Token)
	case "mysql":
		err = handleMySQL(client, u, t.Token, conn.Token)
	default:
		err = fmt.Errorf("unsupported type %q", conn.Type)
	}
	if err != nil && !isClosedErr(err) {
		fmt.Fprintf(t.Log, "dbproxy: %s: %s: %v\n", name, clientAddr, err)
		return
	}
	if err == nil {
		fmt.Fprintf(t.Log, "dbproxy: %s: %s: authenticated, tunnel open\n", name, clientAddr)
	}
	_ = ctx
}

func isClosedErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "use of closed network connection") ||
		strings.Contains(s, "connection reset by peer") ||
		strings.Contains(s, "broken pipe")
}

// splice relays bytes both ways until either side closes. br, when non-nil, is
// the client-side reader (it may already hold buffered bytes).
func splice(br io.Reader, client io.Writer, server io.ReadWriteCloser) {
	done := make(chan struct{}, 2)
	go func() {
		io.Copy(server, br)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(client, server)
		done <- struct{}{}
	}()
	<-done
}

// tokenOKAny does constant-time comparisons of the presented token against
// every expected token (the connection's dedicated token and the global
// bridge token), accepting if any matches. Every expected token is always
// compared so timing does not reveal which one matched.
func tokenOKAny(presented string, expected ...string) bool {
	var ok byte = 0
	for _, e := range expected {
		ok |= byte(subtle.ConstantTimeCompare([]byte(presented), []byte(e)))
	}
	return ok == 1
}
