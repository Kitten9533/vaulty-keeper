package dbproxy

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// TestConn verifies that a registered connection can actually authenticate to
// the real database, by dialing it with the decrypted URL and completing the
// same auth exchange the tunnel performs. It is safe to run from an AI/script:
// the returned error never contains the URL, the password, or the real
// host:port — dial failures collapse to a generic message, and server-side
// auth errors keep only what the server itself said (e.g. the username).
func TestConn(conn Conn) error {
	u, err := url.Parse(conn.URL)
	if err != nil {
		return errors.New("cannot parse the registered connection URL")
	}
	switch conn.Type {
	case "postgres":
		server, err := pgDial(hostPort(u.Host, 5432), u)
		if err != nil {
			return errDial("PostgreSQL")
		}
		defer server.Close()
		return sanitizeTestErr(pgAuthenticate(server, u), u)
	case "mysql":
		server, err := net.DialTimeout("tcp", hostPort(u.Host, 3306), testDialTimeout)
		if err != nil {
			return errDial("MySQL")
		}
		defer server.Close()
		return sanitizeTestErr(myAuthenticate(server, u), u)
	case "redis":
		addr := hostPort(u.Host, 6379)
		var server net.Conn
		if u.Scheme == "rediss" {
			d := &net.Dialer{Timeout: testDialTimeout}
			server, err = tls.DialWithDialer(d, "tcp", addr, &tls.Config{ServerName: hostOnly(u.Host)})
		} else {
			server, err = net.DialTimeout("tcp", addr, testDialTimeout)
		}
		if err != nil {
			return errDial("Redis")
		}
		defer server.Close()
		return sanitizeTestErr(redisAuthenticate(server, u), u)
	default:
		return fmt.Errorf("unsupported database type %q", conn.Type)
	}
}

// testDialTimeout bounds how long a connection test waits to reach the real
// database, so an unreachable host fails fast instead of hanging.
const testDialTimeout = 5 * time.Second

// errDial collapses a network-level failure into a message that carries no
// address, so the real database location never leaks to an AI/script.
func errDial(dbType string) error {
	return fmt.Errorf("connection failed: cannot reach %s (check the host/port in the URL)", dbType)
}

// sanitizeTestErr rewrites an error so it cannot leak the URL's credentials or
// host:port. Server-provided messages (e.g. "Access denied for user 'app'")
// are preserved for diagnosis.
func sanitizeTestErr(err error, u *url.URL) error {
	if err == nil {
		return nil
	}
	s := err.Error()
	if u.User != nil {
		if pw, ok := u.User.Password(); ok && pw != "" {
			s = strings.ReplaceAll(s, pw, "<password>")
		}
		if us := u.User.Username(); us != "" {
			s = strings.ReplaceAll(s, us+"@", "")
		}
	}
	s = strings.ReplaceAll(s, u.Host, "<host:port>")
	s = strings.ReplaceAll(s, hostOnly(u.Host), "<host>")
	return errors.New(s)
}
