package dbproxy

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestTestConnSuccessAllTypes(t *testing.T) {
	pg := newFakePG(t, "app", "pgpass")
	my := newFakeMySQL(t, "mysqlpass")
	rd := newFakeRedis(t, "redispass")

	cases := []Conn{
		{Type: "postgres", URL: fmt.Sprintf("postgres://app:pgpass@127.0.0.1:%d/appdb", pg.port())},
		{Type: "mysql", URL: fmt.Sprintf("mysql://app:mysqlpass@127.0.0.1:%d/appdb", my.port())},
		{Type: "redis", URL: fmt.Sprintf("redis://:redispass@127.0.0.1:%d/0", rd.port())},
	}
	for _, c := range cases {
		if err := TestConn(c); err != nil {
			t.Errorf("%s: TestConn failed: %v", c.Type, err)
		}
	}
}

func TestTestConnWrongPasswordSanitized(t *testing.T) {
	pg := newFakePG(t, "app", "pgpass")
	my := newFakeMySQL(t, "mysqlpass")
	rd := newFakeRedis(t, "redispass")

	cases := []Conn{
		{Type: "postgres", URL: fmt.Sprintf("postgres://app:WRONG@127.0.0.1:%d/appdb", pg.port())},
		{Type: "mysql", URL: fmt.Sprintf("mysql://app:WRONG@127.0.0.1:%d/shop", my.port())},
		{Type: "redis", URL: fmt.Sprintf("redis://:WRONG@127.0.0.1:%d/0", rd.port())},
	}
	for _, c := range cases {
		err := TestConn(c)
		if err == nil {
			t.Errorf("%s: TestConn with wrong password succeeded", c.Type)
			continue
		}
		// error must not leak the password or the address
		u, _ := url.Parse(c.URL)
		for _, secret := range []string{"WRONG", u.Host} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("%s: error leaked %q: %v", c.Type, secret, err)
			}
		}
	}
}

func TestTestConnDialFailureSanitized(t *testing.T) {
	// unreachable port
	c := Conn{Type: "redis", URL: "redis://:pw@127.0.0.1:1/0"}
	err := TestConn(c)
	if err == nil {
		t.Fatal("TestConn to unreachable port succeeded")
	}
	if strings.Contains(err.Error(), "127.0.0.1") || strings.Contains(err.Error(), ":1") {
		t.Fatalf("dial error leaked address: %v", err)
	}
}
