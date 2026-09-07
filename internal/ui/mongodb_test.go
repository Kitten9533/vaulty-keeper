package ui

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"vaulty-keeper/internal/dbproxy"
	"vaulty-keeper/internal/i18n"
)

func TestMongoConnectLinksHideUpstream(t *testing.T) {
	oldLang := i18n.Lang
	t.Cleanup(func() { i18n.Lang = oldLang })
	for _, lang := range []string{"en", "zh"} {
		i18n.Lang = lang
		conn := dbproxy.Conn{Name: "mongo", Type: "mongodb", Port: 27018,
			URL: "mongodb://upstreamUser:upstreamPassword@private.example:27017/orders?authSource=privateAuth&tls=true&replicaSet=privateRS"}
		info, err := buildConnectInfo(conn, "tunnel-token", "::1")
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(info)
		for _, secret := range []string{"upstreamUser", "upstreamPassword", "private.example", "privateAuth", "tls=true", "privateRS"} {
			if strings.Contains(string(encoded), secret) {
				t.Fatalf("upstream field leaked: %s", secret)
			}
		}
		if len(info.Clients) != 1 || !strings.HasPrefix(info.Clients[0].Line, "mongosh '") || !strings.HasPrefix(info.Clients[0].Label, "mongosh ") {
			t.Fatalf("missing translated mongosh command (%s): %#v", lang, info.Clients)
		}
		if !strings.Contains(info.Raw, "@[::1]:27018/orders?") {
			t.Fatalf("incorrect tunnel endpoint: %q", info.Raw)
		}
	}
}

func TestMongoProbeHidesUsername(t *testing.T) {
	if got := dbUser("mongodb://upstreamUser:upstreamPassword@private.example/orders"); got != "" {
		t.Fatalf("Mongo probe exposes username: %q", got)
	}
	if got := dbUser("postgres://app:pw@host/orders"); got != "app" {
		t.Fatalf("changed other protocol metadata: %q", got)
	}
}

func TestMongoCreateExample(t *testing.T) {
	page, err := os.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "mongodb://user:pass@host:27017/dbname?authSource=admin") {
		t.Fatal("Mongo registration URI example missing")
	}
}
