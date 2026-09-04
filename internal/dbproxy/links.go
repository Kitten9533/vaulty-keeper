package dbproxy

import "fmt"

// ClientLink is one ready-to-run client link for a connection. Kind is a
// stable identifier ("psql", "dbeaver", "pgadmin", "workbench", "mysql",
// "insight", "rediscli") that callers map to a localized label; Value is the
// exact command or URL to run or paste.
type ClientLink struct {
	Kind  string
	Value string
}

// RawTunnelURL builds the canonical token-based tunnel URL for a connection.
// It carries both a user and a password so GUI tools / URL parsers that
// require both fields accept it. The token sits in the field the tunnel
// authenticates against (PG/MySQL username, Redis AUTH password); the other
// field is a fixed placeholder ("x") that the tunnel ignores.
func RawTunnelURL(typ, token, host string, port int, db string) string {
	switch typ {
	case "postgres":
		return fmt.Sprintf("postgresql://%s:x@%s:%d/%s", token, host, port, db)
	case "mysql":
		return fmt.Sprintf("mysql://%s:x@%s:%d/%s", token, host, port, db)
	case "redis":
		return fmt.Sprintf("redis://x:%s@%s:%d/%s", token, host, port, db)
	}
	return ""
}

// TunnelLinks builds the raw tunnel URL plus the ready-to-run client links for
// a connection. typ is "postgres", "mysql" or "redis"; host is 127.0.0.1 or
// host.docker.internal; db is the database name from the registered URL. tr
// localizes the two embedded password hints ("Leave the password empty" /
// "Password=any") — pass i18n.T, or a func returning a fixed English string.
// An unsupported type returns a non-nil error and empty results.
func TunnelLinks(typ, token, host string, port int, db string, tr func(key string, args ...any) string) (raw string, links []ClientLink, err error) {
	raw = RawTunnelURL(typ, token, host, port, db)
	switch typ {
	case "postgres":
		links = []ClientLink{
			{Kind: "psql", Value: raw},
			{Kind: "dbeaver", Value: fmt.Sprintf("jdbc:postgresql://%s:%d/%s?user=%s&password=x", host, port, db, token)},
			{Kind: "pgadmin", Value: fmt.Sprintf("Host=%s Port=%d Database=%s Username=%s %s", host, port, db, token, tr("db.password-empty"))},
		}
	case "mysql":
		links = []ClientLink{
			{Kind: "dbeaver", Value: fmt.Sprintf("jdbc:mysql://%s:%d/%s?user=%s&password=x", host, port, db, token)},
			{Kind: "workbench", Value: fmt.Sprintf("Hostname=%s Port=%d Default Schema=%s Username=%s %s", host, port, db, token, tr("db.password-any"))},
			{Kind: "mysql", Value: fmt.Sprintf("mysql -h %s -P %d -u %s -px --ssl-mode=DISABLED %s", host, port, token, db)},
		}
	case "redis":
		links = []ClientLink{
			{Kind: "insight", Value: raw},
			{Kind: "rediscli", Value: fmt.Sprintf("redis-cli -h %s -p %d -a %s --no-auth-warning", host, port, token)},
		}
	default:
		return "", nil, fmt.Errorf("unsupported database type %q", typ)
	}
	return raw, links, nil
}
