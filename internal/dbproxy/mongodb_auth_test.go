package dbproxy

import (
	"crypto/md5"
	"crypto/tls"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xdg-go/scram"
	"github.com/xdg-go/stringprep"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type mongoFakeOptions struct {
	mechanism, mode, username, password string
	tlsConfig                           *tls.Config
}

func mongoFakeUpstream(t *testing.T, opts mongoFakeOptions) *url.URL {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if opts.tlsConfig != nil {
		ln = tls.NewListener(ln, opts.tlsConfig)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(4 * time.Second))
		req, err := mongoReadMessage(conn)
		if err != nil {
			return
		}
		if len(req.Body) == 0 || req.Body[0].Key != "hello" || mongoGet(req.Body, "helloOk") != true || mongoGet(req.Body, "$db") != "users" {
			t.Error("invalid hello command")
			return
		}
		if opts.mode == "auto" || opts.mode == "unsupported" {
			if mongoGet(req.Body, "saslSupportedMechs") != "users."+opts.username {
				t.Error("mechanism discovery did not use authSource")
			}
		}
		wire := int32(25)
		if opts.mode == "old" {
			wire = 24
		}
		minWire := int32(0)
		if opts.mode == "future" {
			minWire = 26
		}
		mechs := bson.A{opts.mechanism}
		if opts.mode == "unsupported" {
			mechs = bson.A{"PLAIN"}
		}
		hello := bson.D{{Key: "ok", Value: float64(1)}, {Key: "minWireVersion", Value: minWire}, {Key: "maxWireVersion", Value: wire}, {Key: "saslSupportedMechs", Value: mechs}, {Key: "setName", Value: "rs0"}, {Key: "hosts", Value: bson.A{"synthetic-internal:27017"}}}
		if opts.mode == "hello error" {
			hello = bson.D{{Key: "ok", Value: float64(0)}, {Key: "errmsg", Value: "synthetic-private-error"}}
		}
		if opts.mode == "wrong response" {
			req.ID++
		}
		if err = mongoWriteReply(conn, req, hello); err != nil {
			return
		}
		hash := scram.SHA256
		pass := opts.password
		if opts.mechanism == "SCRAM-SHA-1" {
			hash = scram.SHA1
			sum := md5.Sum([]byte(opts.username + ":mongo:" + pass))
			pass = hex.EncodeToString(sum[:])
		} else {
			pass, err = stringprep.SASLprep.Prepare(pass)
			if err != nil {
				t.Error(err)
				return
			}
		}
		client, err := hash.NewClientUnprepped(opts.username, pass, "")
		if err != nil {
			t.Error(err)
			return
		}
		iters := 4096
		if opts.mode == "low iterations" {
			iters = 1024
		}
		stored := client.GetStoredCredentials(scram.KeyFactors{Salt: "synthetic-salt", Iters: iters})
		server, err := hash.NewServer(func(user string) (scram.StoredCredentials, error) {
			if user != opts.username {
				return scram.StoredCredentials{}, errors.New("unknown synthetic user")
			}
			return stored, nil
		})
		if err != nil {
			t.Error(err)
			return
		}
		conv := server.NewConversation()
		lastID := req.ID
		step := 0
		for {
			req, err = mongoReadMessage(conn)
			if err != nil {
				return
			}
			if req.ID == lastID {
				t.Error("request ID reused")
				return
			}
			lastID = req.ID
			if len(req.Body) == 0 {
				t.Error("empty command")
				return
			}
			name := req.Body[0].Key
			if name == "ping" {
				if !conv.Valid() {
					t.Error("command before authentication")
					return
				}
				_ = mongoWriteReply(conn, req, bson.D{{Key: "ok", Value: float64(0)}, {Key: "errmsg", Value: "synthetic-private-error"}, {Key: "n", Value: int64(99)}})
				continue
			}
			if mongoGet(req.Body, "$db") != "users" {
				t.Error("SASL used business db instead of authSource")
				return
			}
			if step == 0 {
				if name != "saslStart" || mongoGet(req.Body, "mechanism") != opts.mechanism {
					t.Error("wrong saslStart mechanism")
					return
				}
			} else if name != "saslContinue" || mongoGet(req.Body, "conversationId") != int32(23) {
				t.Error("wrong saslContinue conversation")
				return
			}
			payload, ok := mongoGet(req.Body, "payload").(bson.Binary)
			if !ok || payload.Subtype != 0 {
				t.Error("non-binary SCRAM payload")
				return
			}
			var response string
			if !conv.Done() {
				response, err = conv.Step(string(payload.Data))
			} else if len(payload.Data) != 0 {
				t.Error("nonempty final ACK")
				return
			}
			if err != nil {
				_ = mongoWriteReply(conn, req, bson.D{{Key: "ok", Value: float64(0)}, {Key: "errmsg", Value: "synthetic-private-error"}})
				return
			}
			finished := conv.Done()
			if opts.mode == "extra ack" && step == 1 {
				finished = false
			}
			if opts.mode == "never done" {
				finished = false
			}
			if opts.mode == "early done" && step == 0 {
				finished = true
			}
			if opts.mode == "bad proof" && step == 1 {
				response = "v=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
			}
			conversationID := int32(23)
			if opts.mode == "bad conversation" && step == 1 {
				conversationID = 24
			}
			body := bson.D{{Key: "ok", Value: float64(1)}, {Key: "conversationId", Value: conversationID}, {Key: "done", Value: finished}, {Key: "payload", Value: bson.Binary{Subtype: 0, Data: []byte(response)}}}
			if opts.mode == "bad payload" {
				body[3].Value = "synthetic-invalid-payload"
			}
			if opts.mode == "bad subtype" {
				body[3].Value = bson.Binary{Subtype: 1, Data: []byte(response)}
			}
			if opts.mode == "missing done" {
				body = append(body[:2], body[3:]...)
			}
			if opts.mode == "bad conversation type" {
				body[1].Value = "23"
			}
			if opts.mode == "bad ack" && step == 2 {
				body[3].Value = bson.Binary{Data: []byte("unexpected")}
			}
			if opts.mode == "bad ack" && step == 1 {
				body[2].Value = false
			}
			if err = mongoWriteReply(conn, req, body); err != nil {
				return
			}
			step++
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("fake Mongo server did not stop")
		}
	})
	u := &url.URL{Scheme: "mongodb", Host: ln.Addr().String(), Path: "/business", User: url.UserPassword(opts.username, opts.password)}
	q := url.Values{"authSource": {"users"}, "connectTimeoutMS": {"1500"}, "authMechanism": {opts.mechanism}}
	if opts.mode == "auto" || opts.mode == "unsupported" {
		q.Del("authMechanism")
	}
	u.RawQuery = q.Encode()
	return u
}

func TestMongoAuthSuccess(t *testing.T) {
	for _, mechanism := range []string{"SCRAM-SHA-256", "SCRAM-SHA-1"} {
		for _, mode := range []string{"explicit", "auto", "extra ack"} {
			t.Run(mechanism+"/"+mode, func(t *testing.T) {
				u := mongoFakeUpstream(t, mongoFakeOptions{mechanism: mechanism, mode: mode, username: "synthetic-user", password: "synthetic-pass"})
				b, err := mongoDial(u)
				if err != nil {
					t.Fatal(err)
				}
				defer b.close()
				if mongoGet(b.hello, "setName") != "rs0" || mongoGet(b.hello, "hosts") == nil {
					t.Fatal("actual hello was not preserved")
				}
				d, err := b.command(bson.D{{Key: "ping", Value: int32(1)}, {Key: "$db", Value: "business"}})
				if err != nil || mongoGet(d, "ok") != float64(0) || mongoGet(d, "n") != int64(99) || mongoGet(d, "errmsg") != "synthetic-private-error" {
					t.Fatalf("command must preserve even ok:0 documents: %v", err)
				}
			})
		}
	}
	// Only the SHA-256 password is SASLprepped, never the username.
	u := mongoFakeUpstream(t, mongoFakeOptions{mechanism: "SCRAM-SHA-256", username: "user\u00adname", password: "pass\u00adword"})
	b, err := mongoDial(u)
	if err != nil {
		t.Fatal(err)
	}
	_ = b.close()
}

func TestMongoAuthRejects(t *testing.T) {
	for _, mode := range []string{"wrong password", "old", "future", "unsupported", "hello error", "wrong response", "bad proof", "bad conversation", "early done", "never done", "bad payload", "bad subtype", "missing done", "bad conversation type", "low iterations", "replica mismatch", "bad ack"} {
		t.Run(mode, func(t *testing.T) {
			u := mongoFakeUpstream(t, mongoFakeOptions{mechanism: "SCRAM-SHA-256", mode: mode, username: "synthetic-user", password: "synthetic-pass"})
			if mode == "wrong password" {
				u.User = url.UserPassword("synthetic-user", "synthetic-wrong")
			}
			if mode == "replica mismatch" {
				u.RawQuery += "&replicaSet=other"
			}
			b, err := mongoDial(u)
			if err == nil {
				_ = b.close()
				t.Fatal("accepted invalid backend authentication")
			}
			for _, secret := range []string{"synthetic", "127.0.0.1", "business", "users", "other"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("unsafe error: %v", err)
				}
			}
		})
	}
}

func TestMongoAuthNetworkAndTLS(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	u, _ := url.Parse("mongodb://synthetic-user:synthetic-pass@" + addr + "/db?connectTimeoutMS=50")
	if b, err := mongoDial(u); err == nil {
		_ = b.close()
		t.Fatal("dialed closed port")
	} else if strings.Contains(err.Error(), addr) {
		t.Fatal("address leaked")
	}
	certServer := httptest.NewTLSServer(nil)
	cert := certServer.TLS.Certificates[0]
	certServer.Close()
	for _, mode := range []string{"untrusted", "trusted", "wrong host", "bad CA", "oversize CA"} {
		t.Run(mode, func(t *testing.T) {
			tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
			u := mongoFakeUpstream(t, mongoFakeOptions{mechanism: "SCRAM-SHA-256", username: "synthetic-user", password: "synthetic-pass", tlsConfig: tlsConfig})
			u.RawQuery += "&tls=true"
			if mode != "untrusted" {
				data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]})
				if mode == "bad CA" {
					data = []byte("invalid synthetic cert")
				}
				if mode == "oversize CA" {
					data = make([]byte, 1<<20+1)
				}
				path := filepath.Join(t.TempDir(), "ca.pem")
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				u.RawQuery += "&tlsCAFile=" + url.QueryEscape(path)
			}
			if mode == "wrong host" {
				u.Host = net.JoinHostPort("localhost", u.Port())
			}
			b, err := mongoDial(u)
			if mode == "trusted" {
				if err != nil {
					t.Fatal(err)
				}
				_ = b.close()
			} else if err == nil {
				_ = b.close()
				t.Fatal("accepted invalid TLS")
			} else if strings.Contains(err.Error(), "synthetic") || strings.Contains(err.Error(), "127.0.0.1") {
				t.Fatal("TLS error leaked input")
			}
		})
	}
}

func TestMongoCommandDeadlineAndResponse(t *testing.T) {
	for _, mode := range []string{"timeout", "responseTo", "moreToCome", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			client, server := net.Pipe()
			defer client.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer server.Close()
				m, err := mongoReadMessage(server)
				if err != nil {
					return
				}
				switch mode {
				case "timeout":
					_, _ = io.Copy(io.Discard, server)
				case "responseTo":
					m.ID++
					_ = mongoWriteReply(server, m, bson.D{{Key: "ok", Value: 1}})
				case "legacy":
					m.OpCode = 2004
					_ = mongoWriteReply(server, m, bson.D{{Key: "ok", Value: 1}})
				case "moreToCome":
					p := append([]byte{2, 0, 0, 0, 0}, mongoTestDoc(t, bson.D{{Key: "ok", Value: 1}})...)
					_, _ = server.Write(mongoTestFrame(2013, p))
				}
			}()
			b := mongoBackend{conn: client, config: mongoConfig{Timeout: 30 * time.Millisecond}}
			if _, err := b.command(bson.D{{Key: "ping", Value: 1}}); err == nil {
				t.Fatal("accepted invalid response")
			}
			_ = b.close()
			<-done
		})
	}
}
