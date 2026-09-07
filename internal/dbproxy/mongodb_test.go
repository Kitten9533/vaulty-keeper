package dbproxy

import (
	"context"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoModernHello(t *testing.T) {
	doc := bson.D{{Key: "isMaster", Value: int32(1)}, {Key: "helloOk", Value: true}, {Key: "backpressure", Value: "2"}, {Key: "$db", Value: "admin"}}
	if !mongoHelloRequest(doc) {
		t.Fatal("current Go driver handshake rejected")
	}
	doc = append(doc, bson.E{Key: "lsid", Value: bson.D{{Key: "id", Value: bson.Binary{Subtype: 4, Data: make([]byte, 16)}}}})
	if !mongoHelloRequest(doc) {
		t.Fatal("authenticated hello with implicit session rejected")
	}
	doc[2].Value = "unknown"
	if mongoHelloRequest(doc) {
		t.Fatal("unknown backpressure version accepted")
	}
}

func TestMongoFrontendAuth(t *testing.T) {
	for _, password := range []string{"token-for-test", "wrong-token"} {
		t.Run(password, func(t *testing.T) {
			u := mongoFakeUpstream(t, mongoFakeOptions{mechanism: "SCRAM-SHA-256", username: "fixture", password: "fixturepass"})
			server, client := net.Pipe()
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() {
				defer server.Close()
				done <- handleMongo(ctx, server, u, "token-for-test", "fixture", newMongoTunnelState())
			}()
			t.Cleanup(func() { cancel(); _ = client.Close(); <-done })
			peer := &mongoBackend{conn: client, config: mongoConfig{Username: "vaulty", Password: password, AuthSource: "admin", AuthMechanism: "SCRAM-SHA-256", Timeout: time.Second}}
			var err error
			peer.hello, err = peer.command(bson.D{{Key: "hello", Value: int32(1)}, {Key: "$db", Value: "admin"}})
			if err != nil {
				t.Fatal(err)
			}
			denied, err := peer.command(bson.D{{Key: "ping", Value: int32(1)}, {Key: "$db", Value: "admin"}})
			if err != nil || mongoCommandOK(denied) {
				t.Fatal("unauthenticated command reached backend")
			}
			err = peer.authenticate()
			if password == "wrong-token" {
				if err == nil {
					t.Fatal("wrong token authenticated")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			status, err := peer.command(bson.D{{Key: "connectionStatus", Value: int32(1)}, {Key: "maxTimeMS", Value: int64(1000)}, {Key: "$db", Value: "admin"}})
			if err != nil {
				t.Fatal(err)
			}
			data, _ := bson.MarshalExtJSON(status, false, false)
			if !strings.Contains(string(data), "vaulty") || strings.Contains(string(data), "fixture") {
				t.Fatal("virtual identity missing or real identity leaked")
			}
		})
	}
}

func mongoResponseBackend(t *testing.T, response func(bson.D) bson.D) *mongoBackend {
	t.Helper()
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer server.Close()
		for {
			req, err := mongoReadMessage(server)
			if err != nil {
				return
			}
			if mongoWriteReply(server, req, response(req.Body)) != nil {
				return
			}
		}
	}()
	t.Cleanup(func() { _ = client.Close(); <-done })
	return &mongoBackend{conn: client, config: mongoConfig{Timeout: time.Second}}
}

func TestMongoCursorFailureDoesNotRenewOwnership(t *testing.T) {
	state, scope := newMongoTunnelState(), [32]byte{1}
	k := mongoCursorKey{scope, 91}
	created := time.Now().Add(-time.Minute)
	state.cursors[k] = mongoCursor{namespace: "appdb.$cmd.listCollections", command: "listCollections", used: created}
	if _, ok := state.cursor(scope, 91, "appdb.$cmd.listCollections", ""); !ok {
		t.Fatal("valid cursor not found")
	}
	if state.cursors[k].used != created {
		t.Fatal("lookup renews cursor before successful backend operation")
	}
	backend := mongoResponseBackend(t, func(doc bson.D) bson.D {
		return mongoError(43, "synthetic cursor expired")
	})
	_, err := state.execute(backend, scope, bson.D{{Key: "getMore", Value: int64(91)}, {Key: "collection", Value: "$cmd.listCollections"}, {Key: "$db", Value: "appdb"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := state.cursors[k]; ok {
		t.Fatal("CursorNotFound retained ownership")
	}
}

func TestMongoMetadataBackendFilter(t *testing.T) {
	state := newMongoTunnelState()
	backend := mongoResponseBackend(t, func(doc bson.D) bson.D {
		filter, _ := mongoAsDoc(mongoGet(doc, "filter"))
		and, _ := mongoGet(filter, "$and").(bson.A)
		if len(and) != 2 {
			t.Error("metadata filter not constrained before server execution")
		}
		if mongoGet(doc, "nameOnly") != false {
			t.Error("metadata type information unavailable to sanitizer")
		}
		return bson.D{{Key: "ok", Value: float64(1)}, {Key: "cursor", Value: bson.D{{Key: "id", Value: int64(0)}, {Key: "ns", Value: "appdb.$cmd.listCollections"}, {Key: "firstBatch", Value: bson.A{}}}}}
	})
	_, err := state.execute(backend, [32]byte{1}, bson.D{{Key: "listCollections", Value: int32(1)}, {Key: "nameOnly", Value: true}, {Key: "filter", Value: bson.D{{Key: "type", Value: "view"}}}, {Key: "cursor", Value: bson.D{{Key: "batchSize", Value: int32(0)}}}, {Key: "$db", Value: "appdb"}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMongoEndSessionsRemovesOwnedCursors(t *testing.T) {
	state, scope := newMongoTunnelState(), [32]byte{1}
	id := bson.Binary{Subtype: 4, Data: make([]byte, 16)}
	state.remember(scope, 7, mongoCursor{namespace: "appdb.items", command: "find", session: string(id.Data)})
	state.remember([32]byte{2}, 8, mongoCursor{namespace: "appdb.items", command: "find", session: string(id.Data)})
	backend := mongoResponseBackend(t, func(bson.D) bson.D { return bson.D{{Key: "ok", Value: float64(1)}} })
	_, err := state.execute(backend, scope, bson.D{{Key: "endSessions", Value: bson.A{bson.D{{Key: "id", Value: id}}}}, {Key: "$db", Value: "admin"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := state.cursors[mongoCursorKey{scope, 7}]; exists || len(state.cursors) != 1 {
		t.Fatal("endSessions failed to isolate and remove matching cursors")
	}
}

func TestMongoRegisteredConnection(t *testing.T) {
	raw := "mongodb://fixture:fixturepass@127.0.0.1:27017/appdb?authSource=admin"
	if typ, err := ConnTypeFromURL(raw); err != nil || typ != "mongodb" {
		t.Fatalf("MongoDB registration unsupported: type=%q err=%v", typ, err)
	}
	for _, bad := range []string{
		"mongodb://fixture:fixturepass@host1,host2/appdb",
		"mongodb://fixture:fixturepass@host/appdb?tlsAllowInvalidCertificates=true",
	} {
		if _, err := ConnTypeFromURL(bad); err == nil {
			t.Fatal("unsupported MongoDB configuration accepted")
		}
	}
}

func TestMongoMalformedURLDoesNotLeak(t *testing.T) {
	for _, scheme := range []string{"mongodb", "MongoDB", "mongodb+srv"} {
		_, err := ConnTypeFromURL(scheme + "://fixtureUser:fixturePassword@private.example:invalid/app")
		if err == nil {
			t.Fatal("malformed URI accepted")
		}
		for _, private := range []string{"fixtureUser", "fixturePassword", "private.example"} {
			if strings.Contains(err.Error(), private) {
				t.Fatal("malformed MongoDB URI leaked connection data")
			}
		}
	}
}

func TestMongoTestConnUsesRealAuth(t *testing.T) {
	u := mongoFakeUpstream(t, mongoFakeOptions{mechanism: "SCRAM-SHA-256", username: "fixture", password: "fixturepass"})
	if err := TestConn(Conn{Type: "mongodb", URL: u.String()}); err != nil {
		t.Fatal(err)
	}
	u = mongoFakeUpstream(t, mongoFakeOptions{mechanism: "SCRAM-SHA-256", username: "fixture", password: "fixturepass"})
	u.User = url.UserPassword("fixture", "WRONG")
	err := TestConn(Conn{Type: "mongodb", URL: u.String()})
	if err == nil {
		t.Fatal("invalid password accepted")
	}
	for _, forbidden := range []string{u.Host, "fixture", "WRONG"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatal("MongoDB probe leaked connection information")
		}
	}
}

func TestMongoHelloHidesTopology(t *testing.T) {
	raw := bson.D{
		{Key: "ok", Value: float64(1)}, {Key: "isWritablePrimary", Value: true},
		{Key: "minWireVersion", Value: int32(0)}, {Key: "maxWireVersion", Value: int32(25)},
		{Key: "hosts", Value: bson.A{"private.example:27017"}}, {Key: "me", Value: "private.example:27017"},
		{Key: "setName", Value: "private-replica"}, {Key: "primary", Value: "private.example:27017"},
		{Key: "topologyVersion", Value: bson.D{{Key: "processId", Value: bson.NewObjectID()}, {Key: "counter", Value: int64(1)}}},
		{Key: "logicalSessionTimeoutMinutes", Value: int32(30)},
		{Key: "saslSupportedMechs", Value: bson.A{"SCRAM-SHA-1"}},
	}
	public := mongoHelloReply(raw, nil)
	data, err := bson.MarshalExtJSON(public, false, false)
	if err != nil || strings.Contains(string(data), "private") || mongoGet(public, "topologyVersion") != nil {
		t.Fatal("hello leaked topology")
	}
	if mongoGet(public, "logicalSessionTimeoutMinutes") != int32(30) || mongoGet(public, "setName") != "vaulty" {
		t.Fatal("hello lost safe session/replica capabilities")
	}
}
