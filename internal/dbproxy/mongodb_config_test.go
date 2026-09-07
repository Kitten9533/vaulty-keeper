package dbproxy

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestMongoConfigDefaults(t *testing.T) {
	for _, tc := range []struct{ uri, address, db, auth, user, pass string }{
		{"mongodb://alice:synthetic@localhost", "localhost:27017", "test", "admin", "alice", "synthetic"},
		{"mongodb://a%40b:p%3Ass@[::1]:27018/business?authSource=users", "[::1]:27018", "business", "users", "a@b", "p:ss"},
		{"mongodb://alice:synthetic@host/business", "host:27017", "business", "business", "alice", "synthetic"},
	} {
		u, err := url.Parse(tc.uri)
		if err != nil {
			t.Fatal(err)
		}
		c, err := mongoConfigFromURL(u)
		if err != nil || c.Address != tc.address || c.Database != tc.db || c.AuthSource != tc.auth || c.Username != tc.user || c.Password != tc.pass || c.Timeout != 5*time.Second || c.TLS {
			t.Fatalf("unexpected config for synthetic case: %v", err)
		}
	}
	u, _ := url.Parse("mongodb://alice:synthetic@host/db?tls=true&ssl=true&tlsCAFile=%2Ffake%2Fca.pem&connectTimeoutMS=120000&directConnection=true&replicaSet=rs0&authMechanism=SCRAM-SHA-256")
	c, err := mongoConfigFromURL(u)
	if err != nil || !c.TLS || c.CAFile != "/fake/ca.pem" || c.Timeout != 120*time.Second || c.ReplicaSet != "rs0" || c.AuthMechanism != "SCRAM-SHA-256" {
		t.Fatalf("options: %v", err)
	}
}

func TestMongoConfigRejects(t *testing.T) {
	cases := []string{
		"mongodb://alice:synthetic@host/%ff",
		"mongodb+srv://alice:synthetic@host/db", "mongodb:opaque", "mongodb://host/db", "mongodb://alice@host/db", "mongodb://:synthetic@host/db", "mongodb://alice:@host/db", "mongodb://alice:synthetic@/db",
		"mongodb://alice:synthetic@host:0/db", "mongodb://alice:synthetic@host:65536/db", "mongodb://alice:synthetic@host:/db", "mongodb://alice:synthetic@host,other/db", "mongodb://alice:synthetic@::1/db", "mongodb://alice:synthetic@host/db#fragment",
	}
	for _, db := range []string{"a%2Fb", "a%5Cb", "a.b", "a%20b", "a%22b", "a%24b", "a%00b", "a%3Ab", "a%3Fb", "a%2Ab", "a%3Cb", "a%3Eb", "a%7Cb", strings.Repeat("a", 64)} {
		cases = append(cases, "mongodb://alice:synthetic@host/"+db)
	}
	for _, q := range []string{"authSource=", "authSource=a%2Fb", "authMechanism=PLAIN", "authMechanism=", "tls=yes", "tls=true&ssl=false", "tls=true&tls=true", "tlsCAFile=/fake", "tls=true&tlsCAFile=", "connectTimeoutMS=0", "connectTimeoutMS=-1", "connectTimeoutMS=120001", "connectTimeoutMS=NaN", "directConnection=false", "replicaSet=", "tlsInsecure=true", "appName=synthetic", "unknown=synthetic", "authSource=%zz", "tls=true;ssl=false"} {
		cases = append(cases, "mongodb://alice:synthetic@host/db?"+q)
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			u, err := url.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			_, err = mongoConfigFromURL(u)
			if err == nil {
				t.Fatal("accepted invalid config")
			}
			for _, s := range []string{"alice", "synthetic", "host", "%zz"} {
				if strings.Contains(err.Error(), s) {
					t.Fatalf("unsafe error: %v", err)
				}
			}
		})
	}
	if _, err := mongoConfigFromURL(nil); err == nil {
		t.Fatal("accepted nil URL")
	}
	for _, host := range []string{"[not-an-ip]", "[127.0.0.1]"} {
		u := &url.URL{Scheme: "mongodb", Host: host, User: url.UserPassword("alice", "synthetic")}
		if _, err := mongoConfigFromURL(u); err == nil {
			t.Error("accepted invalid IP literal")
		}
	}
}
