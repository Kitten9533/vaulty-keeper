package dbproxy

import (
	"net/url"
	"os/exec"
	"strings"
	"testing"
)

func TestMongoRawLink(t *testing.T) {
	for _, tc := range []struct{ name, token, host, db, want string }{
		{"default", "tok", "127.0.0.1", "", "mongodb://vaulty:tok@127.0.0.1:27018/test"},
		{"ipv6", "tok", "::1", "orders", "mongodb://vaulty:tok@[::1]:27018/orders"},
		{"escaped", "a:@/?#%' $`", "localhost", "db space/a?#", "mongodb://vaulty:a%3A%40%2F%3F%23%25%27%20$%60@localhost:27018/db%20space%2Fa%3F%23"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := RawTunnelURL("mongodb", tc.token, tc.host, 27018, tc.db)
			want := tc.want + "?authSource=admin&authMechanism=SCRAM-SHA-256&directConnection=true&retryWrites=false"
			if got != want {
				t.Fatalf("raw = %q, want %q", got, want)
			}
			u, err := url.Parse(got)
			if err != nil {
				t.Fatal(err)
			}
			if pass, _ := u.User.Password(); pass != tc.token || u.User.Username() != "vaulty" {
				t.Fatal("tunnel identity did not round-trip")
			}
		})
	}
}

func TestMongoTunnelLinkShellQuoting(t *testing.T) {
	for _, host := range []string{"::1", "local'$(printf injected)`printf injected`host"} {
		raw, links, err := TunnelLinks("mongodb", "tok'$(printf injected)", host, 27018, "db space';printf injected;#", nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(links) != 1 || links[0].Kind != "mongosh" {
			t.Fatalf("expected only verified mongosh command: %#v", links)
		}
		// A shell function captures argv without invoking a database client.
		out, err := exec.Command("sh", "-c", "mongosh() { test \"$#\" = 1 || exit 9; printf '%s' \"$1\"; }; "+links[0].Value).Output()
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != raw || !strings.HasPrefix(links[0].Value, "mongosh '") {
			t.Fatalf("shell changed URI: %q != %q", out, raw)
		}
	}
}
