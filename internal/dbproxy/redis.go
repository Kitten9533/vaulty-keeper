package dbproxy

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// handleRedis implements the Redis tunnel: the client's first command must be
// `AUTH <token>` (redis-cli -a "$TOKEN"); the proxy then authenticates to the
// real server with the registered password (and SELECTs the URL's database
// index) and relays raw RESP afterwards.
//
// The client never learns the real password: it authenticates with the bridge
// token, and the real AUTH is sent host-side.
func handleRedis(client net.Conn, u *url.URL, globalToken, connToken string) error {
	br := bufio.NewReader(client)

	args, err := readRESPCommand(br)
	if err != nil {
		client.Write([]byte("-ERR invalid request\r\n"))
		return errors.New("client did not send a valid RESP command")
	}
	// Accept both AUTH forms real clients use:
	//   AUTH <token>            (2 args, requirepass style)
	//   AUTH <user> <token>     (3 args, ACL style — GUI/URL clients send
	//                            an empty username like AUTH "" <token>)
	presented := ""
	if len(args) >= 2 && strings.EqualFold(string(args[0]), "AUTH") {
		presented = string(args[len(args)-1])
	}
	if !tokenOKAny(presented, globalToken, connToken) {
		client.Write([]byte("-ERR authentication required\r\n"))
		return errors.New("invalid bridge token")
	}
	if _, err := client.Write([]byte("+OK\r\n")); err != nil {
		return err
	}

	addr := hostPort(u.Host, 6379)
	var server net.Conn
	if u.Scheme == "rediss" {
		cfg := &tls.Config{ServerName: hostOnly(u.Host)}
		server, err = tls.Dial("tcp", addr, cfg)
	} else {
		server, err = net.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	defer server.Close()

	if err := redisAuthenticate(server, u); err != nil {
		return err
	}

	splice(br, client, server)
	return nil
}

// redisAuthenticate authenticates to the real Redis server using the password
// in u (and SELECTs the database index from the URL path).
func redisAuthenticate(server net.Conn, u *url.URL) error {
	if pass, ok := u.User.Password(); ok && pass != "" {
		if _, err := server.Write(respBulk("AUTH", pass)); err != nil {
			return err
		}
		reply, err := readRESPLine(server)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(reply, "+") {
			return fmt.Errorf("real server refused auth: %s", reply)
		}
	}
	if db := strings.TrimPrefix(u.Path, "/"); db != "" && db != "0" {
		if _, err := server.Write(respBulk("SELECT", db)); err != nil {
			return err
		}
		if reply, err := readRESPLine(server); err != nil {
			return err
		} else if !strings.HasPrefix(reply, "+") {
			return fmt.Errorf("real server refused SELECT: %s", reply)
		}
	}
	return nil
}

// readRESPCommand reads one RESP array-of-bulk-strings command (the only form
// redis-cli and drivers send).
func readRESPCommand(br *bufio.Reader) ([][]byte, error) {
	line, err := br.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	if !strings.HasPrefix(line, "*") {
		return nil, errors.New("not a RESP array")
	}
	n, err := strconv.Atoi(line[1:])
	if err != nil || n <= 0 {
		return nil, errors.New("bad array length")
	}
	args := make([][]byte, 0, n)
	for i := 0; i < n; i++ {
		hdr, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		hdr = strings.TrimSuffix(strings.TrimSuffix(hdr, "\n"), "\r")
		if !strings.HasPrefix(hdr, "$") {
			return nil, errors.New("not a bulk string")
		}
		sz, err := strconv.Atoi(hdr[1:])
		if err != nil || sz < 0 {
			return nil, errors.New("bad bulk length")
		}
		buf := make([]byte, sz+2)
		if _, err := io.ReadFull(br, buf); err != nil {
			return nil, err
		}
		args = append(args, buf[:sz])
	}
	return args, nil
}

// readRESPLine reads a single RESP reply line (e.g. "+OK" or "-ERR ...").
func readRESPLine(r io.Reader) (string, error) {
	var sb strings.Builder
	buf := make([]byte, 1)
	for {
		if _, err := r.Read(buf); err != nil {
			return "", err
		}
		if buf[0] == '\n' {
			break
		}
		sb.WriteByte(buf[0])
	}
	return strings.TrimSuffix(sb.String(), "\r"), nil
}

// respBulk encodes a command as a RESP array of bulk strings.
func respBulk(args ...string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "*%d\r\n", len(args))
	for _, a := range args {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(a), a)
	}
	return []byte(b.String())
}

// hostPort joins host:port, defaulting when no port is present.
func hostPort(hostport string, def int) string {
	if _, _, err := net.SplitHostPort(hostport); err == nil {
		return hostport
	}
	return net.JoinHostPort(hostport, strconv.Itoa(def))
}

func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}
