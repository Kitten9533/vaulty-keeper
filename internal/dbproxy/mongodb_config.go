package dbproxy

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type mongoConfig struct {
	Address, Username, Password, Database, AuthSource, AuthMechanism, CAFile, ReplicaSet string
	TLS                                                                                  bool
	Timeout                                                                              time.Duration
}

func mongoConfigFromURL(u *url.URL) (mongoConfig, error) {
	c := mongoConfig{Database: "test", AuthSource: "admin", Timeout: 5 * time.Second}
	invalid := errors.New("invalid MongoDB connection configuration")
	if u == nil || u.Scheme != "mongodb" || u.Opaque != "" || u.Fragment != "" || u.User == nil {
		return mongoConfig{}, invalid
	}
	c.Username = u.User.Username()
	var hasPass bool
	c.Password, hasPass = u.User.Password()
	if c.Username == "" || !hasPass || c.Password == "" || strings.ContainsRune(c.Username, 0) || strings.ContainsRune(c.Password, 0) {
		return mongoConfig{}, invalid
	}
	host := u.Hostname()
	if host == "" || strings.ContainsAny(u.Host, ",/\\ \t\r\n\x00") || strings.HasSuffix(u.Host, ":") {
		return mongoConfig{}, invalid
	}
	if strings.Contains(host, ":") {
		if !strings.HasPrefix(u.Host, "[") || net.ParseIP(host) == nil {
			return mongoConfig{}, invalid
		}
	} else if strings.ContainsAny(host, "[]%") || strings.HasPrefix(u.Host, "[") {
		return mongoConfig{}, invalid
	}
	port := u.Port()
	if port == "" {
		port = "27017"
	}
	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 1 || portNum > 65535 {
		return mongoConfig{}, invalid
	}
	c.Address = net.JoinHostPort(host, port)
	validDB := func(s string) bool {
		return len(s) > 0 && len(s) < 64 && utf8.ValidString(s) && !strings.ContainsAny(s, "/\\. \"$\x00:*<>|?\t\r\n")
	}
	if u.Path != "" && u.Path != "/" {
		if !strings.HasPrefix(u.Path, "/") || !validDB(u.Path[1:]) {
			return mongoConfig{}, invalid
		}
		c.Database, c.AuthSource = u.Path[1:], u.Path[1:]
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return mongoConfig{}, invalid
	}
	var tlsValue *bool
	for key, values := range q {
		if len(values) != 1 {
			return mongoConfig{}, invalid
		}
		v := values[0]
		switch key {
		case "authSource":
			if !validDB(v) {
				return mongoConfig{}, invalid
			}
			c.AuthSource = v
		case "authMechanism":
			if v != "SCRAM-SHA-256" && v != "SCRAM-SHA-1" {
				return mongoConfig{}, invalid
			}
			c.AuthMechanism = v
		case "tls", "ssl":
			if v != "true" && v != "false" {
				return mongoConfig{}, invalid
			}
			value := v == "true"
			if tlsValue != nil && *tlsValue != value {
				return mongoConfig{}, invalid
			}
			tlsValue, c.TLS = &value, value
		case "tlsCAFile":
			if v == "" || strings.ContainsRune(v, 0) {
				return mongoConfig{}, invalid
			}
			c.CAFile = v
		case "connectTimeoutMS":
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 || n > 120000 {
				return mongoConfig{}, invalid
			}
			c.Timeout = time.Duration(n) * time.Millisecond
		case "directConnection":
			if v != "true" {
				return mongoConfig{}, invalid
			}
		case "replicaSet":
			if v == "" || strings.ContainsAny(v, "/\\, \t\r\n\x00") {
				return mongoConfig{}, invalid
			}
			c.ReplicaSet = v
		default:
			return mongoConfig{}, invalid
		}
	}
	if c.CAFile != "" && !c.TLS {
		return mongoConfig{}, invalid
	}
	return c, nil
}
