//go:build mongointegration

package dbproxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/event"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// The fixture is explicitly opt-in and must not be a user's local database.
// scripts/mongotest.sh supplies a fresh root user with this reserved prefix.
func mongoIntegrationFixture(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	invalid := errors.New("expected a synthetic vaulty_mongotest_ fixture user on one loopback MongoDB endpoint")
	if err != nil || u.Scheme != "mongodb" || u.User == nil || u.Fragment != "" || u.Opaque != "" || strings.Contains(u.Host, ",") {
		return nil, invalid
	}
	password, ok := u.User.Password()
	if !ok || password == "" || !strings.HasPrefix(u.User.Username(), "vaulty_mongotest_") {
		return nil, invalid
	}
	host := u.Hostname()
	if host == "localhost" {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(host)
	port, err := strconv.Atoi(u.Port())
	if ip == nil || !ip.IsLoopback() || err != nil || port < 1 || port > 65535 {
		return nil, invalid
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, invalid
	}
	for key, values := range q {
		if len(values) != 1 {
			return nil, invalid
		}
		switch key {
		case "authSource":
			if values[0] != "admin" {
				return nil, invalid
			}
		case "directConnection":
			if values[0] != "true" {
				return nil, invalid
			}
		case "retryWrites":
			if values[0] != "false" {
				return nil, invalid
			}
		default:
			return nil, invalid
		}
	}
	u.Host, u.Path, u.RawPath = net.JoinHostPort(host, strconv.Itoa(port)), "/admin", ""
	u.RawQuery = "authSource=admin&directConnection=true&retryWrites=false"
	return u, nil
}

// Driver errors may embed a credential-bearing URI. Report the operation, type
// and server code, never the raw error or an unsanitized command reply.
func mongoIntegrationOK(t *testing.T, operation string, err error) {
	t.Helper()
	if err != nil {
		var ce mongo.CommandError
		if errors.As(err, &ce) {
			t.Fatalf("%s: %T, server code %d (raw details suppressed)", operation, err, ce.Code)
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			t.Fatalf("%s: subprocess exit code %d (output suppressed)", operation, exit.ExitCode())
		}
		t.Fatalf("%s: %T (raw details suppressed)", operation, err)
	}
}

func mongoIntegrationClient(t *testing.T, uri string) *mongo.Client {
	t.Helper()
	c, err := mongo.Connect(options.Client().ApplyURI(uri).SetDirect(true).SetRetryWrites(false).
		SetRetryReads(false).SetServerSelectionTimeout(2 * time.Second).SetConnectTimeout(2 * time.Second).
		SetTimeout(4 * time.Second).SetMaxPoolSize(1))
	mongoIntegrationOK(t, "construct driver client", err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		mongoIntegrationOK(t, "disconnect driver", c.Disconnect(ctx))
	})
	return c
}

func mongoIntegrationListener(t *testing.T, address string, open bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", address, 150*time.Millisecond)
		if c != nil {
			_ = c.Close()
		}
		if (err == nil) == open {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("listener did not reach open=%t within 8 seconds", open)
}

func TestMongoIntegrationDriverHello(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	mongoIntegrationOK(t, "create handshake fixture", err)
	defer ln.Close()
	received := make(chan mongoMessage, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		req, _ := mongoReadMessage(conn)
		received <- req
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	_ = mongoIntegrationClient(t, RawTunnelURL("mongodb", "synthetic", "127.0.0.1", port, "appdb"))
	select {
	case req := <-received:
		if !mongoHelloRequest(req.Body) {
			var keys []string
			for _, e := range req.Body {
				keys = append(keys, e.Key)
			}
			t.Fatalf("driver handshake rejected: opcode=%d flags=%d fields=%v", req.OpCode, req.Flags, keys)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("driver did not initiate handshake")
	}
}

func TestMongoIntegrationDriverSASL(t *testing.T) {
	for _, replicaSet := range []bool{false, true} {
		t.Run(fmt.Sprintf("replicaSet=%t", replicaSet), func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			mongoIntegrationOK(t, "create SASL fixture", err)
			defer ln.Close()
			received := make(chan mongoMessage, 8)
			go func() {
				for {
					conn, err := ln.Accept()
					if err != nil {
						return
					}
					go func() {
						defer conn.Close()
						_ = conn.SetDeadline(time.Now().Add(time.Second))
						hello, err := mongoReadMessage(conn)
						if err != nil {
							return
						}
						upstream := bson.D{{Key: "isWritablePrimary", Value: true}}
						if replicaSet {
							upstream = append(upstream, bson.E{Key: "setName", Value: "vaulty-fixture"})
						}
						_ = mongoWriteReply(conn, hello, mongoHelloReply(upstream, hello.Body))
						req, err := mongoReadMessage(conn)
						if err == nil && len(req.Body) > 0 && req.Body[0].Key == "saslStart" {
							received <- req
							_ = mongoWriteReply(conn, req, mongoError(18, "synthetic authentication fixture"))
						}
					}()
				}
			}()
			client := mongoIntegrationClient(t, RawTunnelURL("mongodb", "synthetic", "127.0.0.1", ln.Addr().(*net.TCPAddr).Port, "appdb"))
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = client.Ping(ctx, nil)
			select {
			case req := <-received:
				var keys []string
				for _, e := range req.Body {
					keys = append(keys, e.Key)
				}
				if !mongoPolicyFields(req.Body, "saslStart mechanism payload autoAuthorize options $db $readPreference maxTimeMS") {
					t.Fatalf("native SASL fields rejected: %v", keys)
				}
				if replicaSet && mongoGet(req.Body, "$readPreference") == nil {
					t.Fatal("native driver did not emit $readPreference during replica-set SASL")
				}
				if !mongoHandshakeReadPreference(req.Body) {
					t.Fatal("native SASL read preference rejected")
				}
			case <-ctx.Done():
				t.Fatal("driver did not initiate SASL")
			}
		})
	}
}

func TestMongoIntegration(t *testing.T) {
	raw := os.Getenv("VAULTY_MONGO_TEST_URI")
	if raw == "" {
		t.Skip("use bash scripts/mongotest.sh to create an isolated MongoDB 8 fixture")
	}
	t.Run("FixtureGuard", func(t *testing.T) {
		for _, raw := range []string{
			"mongodb://vaulty_mongotest_check:synthetic@example.com:27017/admin",
			"mongodb://vaulty_mongotest_check:synthetic@192.0.2.1:27017/admin",
			"mongodb://vaulty_mongotest_check:synthetic@127.0.0.1:27017,example.com:27017/admin",
			"mongodb+srv://vaulty_mongotest_check:synthetic@localhost/admin",
			"mongodb://root:synthetic@127.0.0.1:27017/admin",
			"mongodb://vaulty_mongotest_check:synthetic@127.0.0.1:27017/admin?tlsCAFile=/not-a-fixture",
		} {
			if _, err := mongoIntegrationFixture(raw); err == nil {
				t.Error("fixture validator accepted a non-fixture or nonloopback configuration")
			}
		}
	})
	fixture, err := mongoIntegrationFixture(raw)
	mongoIntegrationOK(t, "validate loopback fixture URI", err)
	replicaSet := os.Getenv("VAULTY_MONGO_TEST_REPLICA_SET")
	if replicaSet != "" && replicaSet != "vaulty-fixture" {
		t.Fatal("replica-set fixture must use the synthetic vaulty-fixture name")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 145*time.Second)
	defer cancel()
	root := mongoIntegrationClient(t, fixture.String())
	mongoIntegrationOK(t, "ping fixture root", root.Ping(ctx, nil))
	var build bson.M
	mongoIntegrationOK(t, "check MongoDB version", root.Database("admin").RunCommand(ctx, bson.D{{Key: "buildInfo", Value: 1}}).Decode(&build))
	version, _ := build["version"].(string)
	if !strings.HasPrefix(version, "8.0.") {
		t.Fatal("fixture must run MongoDB 8.0.x")
	}
	t.Run("FixtureTopology", func(t *testing.T) {
		var hello struct {
			SetName string   `bson:"setName"`
			Primary bool     `bson:"isWritablePrimary"`
			Hosts   []string `bson:"hosts"`
		}
		mongoIntegrationOK(t, "inspect direct fixture topology", root.Database("admin").RunCommand(ctx, bson.D{{Key: "hello", Value: 1}}).Decode(&hello))
		if hello.SetName != replicaSet || !hello.Primary {
			t.Fatal("fixture is not a writable primary in the requested topology mode")
		}
		if replicaSet != "" && len(hello.Hosts) != 1 {
			t.Fatal("replica-set fixture must contain exactly one member")
		}
	})
	id, err := newToken()
	mongoIntegrationOK(t, "generate fixture names", err)
	prefix := "vaulty_it_" + id
	collection, lookup, view := prefix+"_docs", prefix+"_lookup", prefix+"_view"
	fixtureDB := root.Database("appdb")
	for _, name := range []string{collection, lookup} {
		mongoIntegrationOK(t, "create fixture collection", fixtureDB.CreateCollection(ctx, name))
		name := name
		t.Cleanup(func() {
			cleanupCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
			defer stop()
			mongoIntegrationOK(t, "drop owned fixture collection", fixtureDB.Collection(name).Drop(cleanupCtx))
		})
	}
	mongoIntegrationOK(t, "create unaudited fixture view", fixtureDB.RunCommand(ctx, bson.D{
		{Key: "create", Value: view}, {Key: "viewOn", Value: collection},
		{Key: "pipeline", Value: bson.A{bson.D{{Key: "$project", Value: bson.D{{Key: "roles", Value: "$$USER_ROLES"}}}}}},
	}).Err())
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		mongoIntegrationOK(t, "drop owned fixture view", fixtureDB.Collection(view).Drop(cleanupCtx))
	})
	_, err = fixtureDB.Collection(lookup).InsertOne(ctx, bson.D{{Key: "join", Value: "yes"}})
	mongoIntegrationOK(t, "seed lookup collection", err)
	_, err = fixtureDB.Collection(collection).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "n", Value: 1}}}, {Keys: bson.D{{Key: "kind", Value: 1}}},
	})
	mongoIntegrationOK(t, "seed paginated indexes", err)

	for _, tc := range []struct{ mechanism, source string }{
		{"SCRAM-SHA-256", "admin"}, {"SCRAM-SHA-1", "admin"},
		{"SCRAM-SHA-256", "businessdb"}, {"SCRAM-SHA-1", "businessdb"},
	} {
		t.Run(tc.mechanism+"_"+tc.source, func(t *testing.T) {
			user := prefix + "_" + strings.ReplaceAll(tc.mechanism, "-", "") + "_" + tc.source
			password, err := newToken()
			mongoIntegrationOK(t, "generate backend fixture password", err)
			authDB := root.Database(tc.source)
			mongoIntegrationOK(t, "create mechanism-specific fixture user", authDB.RunCommand(ctx, bson.D{
				{Key: "createUser", Value: user}, {Key: "pwd", Value: password},
				{Key: "mechanisms", Value: bson.A{tc.mechanism}},
				{Key: "roles", Value: bson.A{bson.D{{Key: "role", Value: "readWrite"}, {Key: "db", Value: "appdb"}}}},
			}).Err())
			t.Cleanup(func() {
				cleanupCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
				defer stop()
				mongoIntegrationOK(t, "drop owned fixture user", authDB.RunCommand(cleanupCtx, bson.D{{Key: "dropUser", Value: user}}).Err())
			})
			backend := url.URL{Scheme: "mongodb", Host: fixture.Host, Path: "/appdb", User: url.UserPassword(user, password)}
			backendQuery := url.Values{"authSource": {tc.source}, "authMechanism": {tc.mechanism}, "directConnection": {"true"}}
			if replicaSet != "" {
				backendQuery.Set("replicaSet", replicaSet)
			}
			backend.RawQuery = backendQuery.Encode()
			mongoIntegrationOK(t, "probe synthetic backend", TestConn(Conn{Type: "mongodb", URL: backend.String()}))
			automatic := backend
			q := automatic.Query()
			q.Del("authMechanism")
			automatic.RawQuery = q.Encode()
			mongoIntegrationOK(t, "negotiate backend mechanism", TestConn(Conn{Type: "mongodb", URL: automatic.String()}))
			path, key, port := filepath.Join(t.TempDir(), FileName), testKey(t), freePort(t)
			mongoIntegrationOK(t, "add encrypted synthetic connection", Add(path, key, "mongo-fixture", backend.String(), port))
			conn, err := Resolve(path, key, "mongo-fixture")
			mongoIntegrationOK(t, "resolve dedicated token", err)
			if conn.Token == "" {
				t.Fatal("new connection lacks dedicated token")
			}
			stored, err := os.ReadFile(path)
			mongoIntegrationOK(t, "read temporary encrypted store", err)
			for _, secret := range []string{backend.String(), password, conn.Token} {
				if bytes.Contains(stored, []byte(secret)) {
					t.Fatal("temporary store contains plaintext credentials")
				}
			}
			host := "127.0.0.1"
			container := os.Getenv("VAULTY_MONGO_TEST_CONTAINER")
			if container != "" {
				host = "0.0.0.0"
			}
			tunnelCtx, stop := context.WithCancel(ctx)
			done := make(chan error, 1)
			tunnel := &Tunnel{Path: path, Key: key, Host: host, Log: io.Discard}
			go func() { done <- tunnel.Start(tunnelCtx) }()
			t.Cleanup(func() {
				stop()
				select {
				case err := <-done:
					mongoIntegrationOK(t, "join tunnel", err)
				case <-time.After(5 * time.Second):
					t.Error("tunnel did not stop within 5 seconds")
				}
			})
			address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
			mongoIntegrationListener(t, address, true)
			proxyURI := RawTunnelURL("mongodb", conn.Token, "localhost", port, "appdb")
			client := mongoIntegrationClient(t, proxyURI)
			mongoIntegrationOK(t, "ping through authenticated tunnel", client.Ping(ctx, nil))
			var found bson.M
			mongoIntegrationOK(t, "query using backend mechanism and authSource", client.Database("appdb").Collection(lookup).FindOne(ctx, bson.D{}).Decode(&found))
			if found["join"] != "yes" {
				t.Fatal("fixture query returned incorrect data")
			}
			if tc.mechanism != "SCRAM-SHA-256" || tc.source != "admin" {
				return
			}

			db, coll := client.Database("appdb"), client.Database("appdb").Collection(collection)
			t.Run("CRUD_BSON_Batches_Cursors", func(t *testing.T) {
				decimal, err := bson.ParseDecimal128("123.4500")
				mongoIntegrationOK(t, "parse decimal fixture", err)
				doc := bson.D{{Key: "_id", Value: bson.NewObjectID()}, {Key: "n", Value: int32(7)},
					{Key: "long", Value: int64(1 << 45)}, {Key: "decimal", Value: decimal},
					{Key: "binary", Value: bson.Binary{Subtype: 0x80, Data: []byte{0, 1, 255}}},
					{Key: "date", Value: bson.DateTime(1700000000123)}, {Key: "timestamp", Value: bson.Timestamp{T: 123, I: 4}},
					{Key: "regex", Value: bson.Regex{Pattern: "^sample", Options: "i"}},
					{Key: "nested", Value: bson.D{{Key: "array", Value: bson.A{int32(1), nil, "business"}}}},
					{Key: "literal", Value: "$$USER_ROLES"}, {Key: "kind", Value: "typed"}}
				_, err = coll.InsertOne(ctx, doc)
				mongoIntegrationOK(t, "insert typed BSON", err)
				var got bson.Raw
				mongoIntegrationOK(t, "find typed BSON", coll.FindOne(ctx, bson.D{{Key: "_id", Value: doc[0].Value}}).Decode(&got))
				want, err := bson.Marshal(doc)
				mongoIntegrationOK(t, "marshal expected BSON", err)
				if !bytes.Equal(got, want) {
					t.Fatal("business BSON types or values changed in transit")
				}
				batch := []any{}
				for i := 0; i < 5; i++ {
					batch = append(batch, bson.D{{Key: "_id", Value: int32(i)}, {Key: "n", Value: int32(i)}, {Key: "kind", Value: "batch"}})
				}
				_, err = coll.InsertMany(ctx, batch)
				mongoIntegrationOK(t, "insertMany OP_MSG type-1 sequence", err)
				writes, err := coll.BulkWrite(ctx, []mongo.WriteModel{
					mongo.NewUpdateOneModel().SetFilter(bson.D{{Key: "_id", Value: 0}}).SetUpdate(bson.D{{Key: "$inc", Value: bson.D{{Key: "n", Value: 10}}}}),
					mongo.NewUpdateOneModel().SetFilter(bson.D{{Key: "_id", Value: 1}}).SetUpdate(bson.D{{Key: "$inc", Value: bson.D{{Key: "n", Value: 10}}}}),
				})
				mongoIntegrationOK(t, "bulk update OP_MSG type-1 sequence", err)
				if writes.ModifiedCount != 2 {
					t.Fatal("bulk update count was not preserved")
				}
				var modified bson.M
				mongoIntegrationOK(t, "findAndModify", coll.FindOneAndUpdate(ctx, bson.D{{Key: "_id", Value: 2}}, bson.D{{Key: "$set", Value: bson.D{{Key: "n", Value: int32(22)}}}}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&modified))
				if modified["n"] != int32(22) {
					t.Fatal("findAndModify returned incorrect document")
				}
				cursor, err := coll.Find(ctx, bson.D{}, options.Find().SetBatchSize(1))
				mongoIntegrationOK(t, "find with small batch", err)
				if cursor.ID() == 0 {
					t.Error("find did not create a getMore cursor")
				}
				var docs []bson.Raw
				mongoIntegrationOK(t, "drain find/getMore", cursor.All(ctx, &docs))
				if len(docs) != 6 {
					t.Fatalf("find/getMore returned %d documents, want 6", len(docs))
				}
				var count struct {
					N int64 `bson:"n"`
				}
				mongoIntegrationOK(t, "count command", db.RunCommand(ctx, bson.D{{Key: "count", Value: collection}, {Key: "query", Value: bson.D{}}}).Decode(&count))
				if count.N != 6 {
					t.Fatal("count result was not preserved")
				}
				var distinct []string
				mongoIntegrationOK(t, "distinct", coll.Distinct(ctx, "kind", bson.D{}).Decode(&distinct))
				if len(distinct) != 2 {
					t.Fatal("distinct returned incorrect values")
				}
				pipeline := mongo.Pipeline{bson.D{{Key: "$match", Value: bson.D{{Key: "kind", Value: "batch"}}}}, bson.D{{Key: "$lookup", Value: bson.D{
					{Key: "from", Value: lookup}, {Key: "as", Value: "joined"}, {Key: "pipeline", Value: bson.A{bson.D{{Key: "$lookup", Value: bson.D{
						{Key: "from", Value: lookup}, {Key: "localField", Value: "join"}, {Key: "foreignField", Value: "join"}, {Key: "as", Value: "nested"},
					}}}}},
				}}}}
				cursor, err = coll.Aggregate(ctx, pipeline, options.Aggregate().SetBatchSize(1))
				mongoIntegrationOK(t, "read aggregate with nested lookup", err)
				var joined []bson.M
				mongoIntegrationOK(t, "drain aggregate/getMore", cursor.All(ctx, &joined))
				if len(joined) != 5 {
					t.Fatal("aggregate returned incorrect document count")
				}
				if a, ok := joined[0]["joined"].(bson.A); !ok || len(a) != 1 {
					t.Fatal("lookup results missing")
				}
				deleted, err := coll.BulkWrite(ctx, []mongo.WriteModel{
					mongo.NewDeleteOneModel().SetFilter(bson.D{{Key: "_id", Value: 3}}),
					mongo.NewDeleteOneModel().SetFilter(bson.D{{Key: "_id", Value: 4}}),
				})
				mongoIntegrationOK(t, "bulk delete OP_MSG type-1 sequence", err)
				if deleted.DeletedCount != 2 {
					t.Fatal("delete count was not preserved")
				}
			})

			t.Run("MetadataPagination", func(t *testing.T) {
				for _, nameOnly := range []bool{false, true} {
					opts := options.ListCollections().SetBatchSize(1)
					if nameOnly {
						opts.SetNameOnly(true)
					}
					cursor, err := db.ListCollections(ctx, bson.D{}, opts)
					mongoIntegrationOK(t, "listCollections", err)
					if cursor.ID() == 0 {
						t.Error("listCollections did not create a paging cursor")
					}
					var docs []bson.M
					mongoIntegrationOK(t, "listCollections getMore", cursor.All(ctx, &docs))
					seen := map[string]bool{}
					for _, doc := range docs {
						name, _ := doc["name"].(string)
						if name == view || strings.HasPrefix(name, "system.") {
							t.Error("restricted collection exposed by metadata")
						}
						for key := range doc {
							if key != "name" && key != "type" {
								t.Errorf("unexpected listCollections metadata field %q", key)
							}
						}
						seen[name] = true
					}
					if !seen[collection] || !seen[lookup] {
						t.Error("collection metadata pagination lost business collections")
					}
				}
				cursor, err := coll.Indexes().List(ctx, options.ListIndexes().SetBatchSize(1))
				mongoIntegrationOK(t, "listIndexes", err)
				if cursor.ID() == 0 {
					t.Error("listIndexes did not create a paging cursor")
				}
				var indexes []bson.M
				mongoIntegrationOK(t, "listIndexes getMore", cursor.All(ctx, &indexes))
				if len(indexes) != 3 {
					t.Fatal("index pagination lost entries")
				}
				for _, index := range indexes {
					for key := range index {
						if key != "v" && key != "key" && key != "name" {
							t.Errorf("unexpected listIndexes metadata field %q", key)
						}
					}
				}
			})

			t.Run("CursorAcrossPoolConnections", func(t *testing.T) {
				var created atomic.Int32
				pooled, err := mongo.Connect(options.Client().ApplyURI(proxyURI).
					SetDirect(true).SetRetryWrites(false).SetMaxPoolSize(1).
					SetMaxConnIdleTime(10 * time.Millisecond).SetTimeout(4 * time.Second).
					SetPoolMonitor(&event.PoolMonitor{Event: func(e *event.PoolEvent) {
						if e.Type == event.ConnectionCreated {
							created.Add(1)
						}
					}}))
				mongoIntegrationOK(t, "create expiring pool client", err)
				defer pooled.Disconnect(ctx)
				items := pooled.Database("appdb").Collection(collection)
				_, err = items.InsertMany(ctx, []any{
					bson.D{{Key: "pool_fixture", Value: true}}, bson.D{{Key: "pool_fixture", Value: true}},
					bson.D{{Key: "pool_fixture", Value: true}}, bson.D{{Key: "pool_fixture", Value: true}},
				})
				mongoIntegrationOK(t, "seed cross-connection cursor", err)
				cursor, err := items.Find(ctx, bson.D{{Key: "pool_fixture", Value: true}}, options.Find().SetBatchSize(1))
				mongoIntegrationOK(t, "open cross-connection cursor", err)
				if cursor.ID() == 0 {
					t.Fatal("fixture did not open server cursor")
				}
				before := created.Load()
				// Force checkout to expire the original socket while retaining lsid.
				time.Sleep(50 * time.Millisecond)
				var docs []bson.D
				mongoIntegrationOK(t, "getMore on replacement socket", cursor.All(ctx, &docs))
				if len(docs) != 4 || created.Load() <= before {
					t.Fatal("cursor did not survive replacement of its original socket")
				}
			})

			t.Run("VirtualIdentityAndSession", func(t *testing.T) {
				var status bson.D
				mongoIntegrationOK(t, "connectionStatus", client.Database("admin").RunCommand(ctx, bson.D{{Key: "connectionStatus", Value: 1}}).Decode(&status))
				encoded, err := bson.MarshalExtJSON(status, false, false)
				mongoIntegrationOK(t, "encode virtual identity", err)
				if bytes.Contains(encoded, []byte(user)) || bytes.Contains(encoded, []byte(password)) {
					t.Fatal("connectionStatus leaked backend identity")
				}
				info, ok := mongoAsDoc(mongoGet(status, "authInfo"))
				if !ok {
					t.Fatal("connectionStatus missing authInfo")
				}
				users, ok := mongoGet(info, "authenticatedUsers").(bson.A)
				if !ok || len(users) != 1 {
					t.Fatal("connectionStatus must expose one virtual user")
				}
				virtual, ok := mongoAsDoc(users[0])
				if !ok || mongoGet(virtual, "user") != "vaulty" {
					t.Fatal("connectionStatus did not return the virtual user")
				}
				var hello bson.M
				mongoIntegrationOK(t, "hello", client.Database("admin").RunCommand(ctx, bson.D{{Key: "hello", Value: 1}}).Decode(&hello))
				for _, key := range []string{"hosts", "passives", "arbiters", "primary", "me", "topologyVersion", "saslSupportedMechs"} {
					if _, exists := hello[key]; exists {
						t.Errorf("hello exposed backend metadata field %s", key)
					}
				}
				if replicaSet == "" {
					if _, exists := hello["setName"]; exists {
						t.Error("standalone hello must not expose setName")
					}
				} else if hello["setName"] != "vaulty" {
					t.Error("replica-set hello must expose only the virtual vaulty setName")
				}
				if hello["logicalSessionTimeoutMinutes"] == nil {
					t.Log("proxy does not advertise sessions; explicit session check skipped")
					return
				}
				session, err := client.StartSession(options.Session().SetCausalConsistency(false))
				mongoIntegrationOK(t, "start normal session", err)
				defer session.EndSession(ctx)
				mongoIntegrationOK(t, "normal session query without transaction", mongo.WithSession(ctx, session, func(sessionCtx context.Context) error {
					return coll.FindOne(sessionCtx, bson.D{}).Err()
				}))
			})

			t.Run("PolicyAndErrorSanitation", func(t *testing.T) {
				var hidden bson.D
				mongoIntegrationOK(t, "filter hidden views before cursor creation", db.RunCommand(ctx, bson.D{
					{Key: "listCollections", Value: 1}, {Key: "filter", Value: bson.D{{Key: "type", Value: "view"}, {Key: "name", Value: view}}},
					{Key: "cursor", Value: bson.D{{Key: "batchSize", Value: 0}}},
				}).Decode(&hidden))
				hiddenCursor, _ := mongoAsDoc(mongoGet(hidden, "cursor"))
				if id, valid := mongoAuthNumber(mongoGet(hiddenCursor, "id")); !valid || id != 0 {
					t.Fatal("hidden view existence exposed through cursor ID")
				}
				for _, blocked := range []struct {
					name, database string
					command        bson.D
				}{
					{"explain", "appdb", bson.D{{Key: "explain", Value: bson.D{{Key: "find", Value: collection}}}}},
					{"hidden_metadata_filter", "appdb", bson.D{{Key: "listCollections", Value: 1}, {Key: "filter", Value: bson.D{{Key: "options.viewOn", Value: bson.D{{Key: "$regex", Value: "^private"}}}}}, {Key: "cursor", Value: bson.D{{Key: "batchSize", Value: 0}}}}},
					{"nested_metadata", "appdb", bson.D{{Key: "aggregate", Value: collection}, {Key: "pipeline", Value: bson.A{bson.D{{Key: "$facet", Value: bson.D{{Key: "meta", Value: bson.A{bson.D{{Key: "$collStats", Value: bson.D{}}}}}}}}}}, {Key: "cursor", Value: bson.D{}}}},
					{"user_roles", "appdb", bson.D{{Key: "find", Value: collection}, {Key: "projection", Value: bson.D{{Key: "roles", Value: "$$USER_ROLES"}}}}},
					{"system_collection", "appdb", bson.D{{Key: "find", Value: "system.views"}}},
					{"admin_namespace", "admin", bson.D{{Key: "find", Value: "ordinary"}}},
					{"local_namespace", "local", bson.D{{Key: "find", Value: "ordinary"}}},
					{"config_namespace", "config", bson.D{{Key: "find", Value: "ordinary"}}},
					{"unaudited_view", "appdb", bson.D{{Key: "find", Value: view}}},
					{"view_lookup", "appdb", bson.D{{Key: "aggregate", Value: collection}, {Key: "pipeline", Value: bson.A{bson.D{{Key: "$lookup", Value: bson.D{{Key: "from", Value: view}, {Key: "as", Value: "leak"}, {Key: "pipeline", Value: bson.A{}}}}}}}, {Key: "cursor", Value: bson.D{}}}},
				} {
					t.Run(blocked.name, func(t *testing.T) {
						err := client.Database(blocked.database).RunCommand(ctx, blocked.command).Err()
						var ce mongo.CommandError
						if !errors.As(err, &ce) || ce.Code != 13 {
							t.Fatalf("restricted operation must return Unauthorized code 13, got %T", err)
						}
						if strings.Contains(err.Error(), user) || strings.Contains(err.Error(), password) || strings.Contains(err.Error(), fixture.Host) {
							t.Error("policy error leaked backend details")
						}
					})
				}
				_, err := coll.InsertOne(ctx, bson.D{{Key: "_id", Value: "duplicate-fixture"}})
				mongoIntegrationOK(t, "insert duplicate-key fixture", err)
				_, err = coll.InsertOne(ctx, bson.D{{Key: "_id", Value: "duplicate-fixture"}})
				if !mongo.IsDuplicateKeyError(err) {
					t.Fatalf("duplicate key must preserve code 11000, got %T", err)
				}
				if strings.Contains(err.Error(), "duplicate-fixture") || strings.Contains(err.Error(), collection) || strings.Contains(err.Error(), user) {
					t.Error("duplicate-key error included unsanitized upstream details")
				}
				err = client.Database("businessdb").RunCommand(ctx, bson.D{{Key: "find", Value: prefix + "_denied"}}).Err()
				var denied mongo.CommandError
				if !errors.As(err, &denied) || denied.Code != 13 {
					t.Fatalf("backend role denial must preserve code 13, got %T", err)
				}
				if strings.Contains(err.Error(), user) || strings.Contains(err.Error(), password) || strings.Contains(err.Error(), fixture.Host) || strings.Contains(err.Error(), "_denied") {
					t.Error("upstream role error was not sanitized")
				}
			})

			t.Run("WrongAndAbsentToken", func(t *testing.T) {
				for _, token := range []string{"wrong-fixture-token", ""} {
					uri := RawTunnelURL("mongodb", token, "localhost", port, "appdb")
					if token == "" {
						u, err := url.Parse(uri)
						mongoIntegrationOK(t, "parse proxy URI", err)
						u.User = nil
						u.RawQuery = "directConnection=true&retryWrites=false"
						uri = u.String()
					}
					bad := mongoIntegrationClient(t, uri)
					if bad.Database("appdb").Collection(lookup).FindOne(ctx, bson.D{}).Err() == nil {
						t.Error("query succeeded without a valid dedicated token")
					}
				}
			})

			t.Run("NativeMongosh", func(t *testing.T) {
				if container == "" {
					t.Skip("use scripts/mongotest.sh --mongosh for container-native client")
				}
				if !strings.HasPrefix(container, "vaulty-mongotest-") || !connNameRe.MatchString(container) {
					t.Fatal("native shell requires a recorded fixture container name")
				}
				shellCtx, stop := context.WithTimeout(ctx, 25*time.Second)
				defer stop()
				label, err := exec.CommandContext(shellCtx, "docker", "inspect", "--format", "{{ index .Config.Labels \"vaulty.mongotest\" }}", container).Output()
				mongoIntegrationOK(t, "verify native fixture ownership label", err)
				if strings.TrimSpace(string(label)) != strings.TrimPrefix(container, "vaulty-mongotest-") {
					t.Fatal("native shell container ownership label mismatch")
				}
				script := fmt.Sprintf(`try { const d = db.getSiblingDB("appdb").getCollection(%q).findOne({join:"yes"}); if (!d || d.join !== "yes") quit(2); print("VAULTY_MONGO_NATIVE_OK"); } catch (_) { quit(3); }`, lookup)
				uri := RawTunnelURL("mongodb", conn.Token, "host.docker.internal", port, "appdb")
				out, err := exec.CommandContext(shellCtx, "docker", "exec", container, "mongosh", uri, "--quiet", "--eval", script).CombinedOutput()
				mongoIntegrationOK(t, "native mongosh business query (startup admin commands may be unsupported)", err)
				if !strings.Contains(string(out), "VAULTY_MONGO_NATIVE_OK") {
					t.Fatal("native mongosh success marker missing; output suppressed")
				}
			})

			t.Run("TokenRegenAndListenerReload", func(t *testing.T) {
				mongoIntegrationOK(t, "warm existing authenticated client", client.Database("appdb").Collection(lookup).FindOne(ctx, bson.D{}).Err())
				freshToken, err := RegenToken(path, key, "mongo-fixture")
				mongoIntegrationOK(t, "rotate dedicated token", err)
				if freshToken == conn.Token || freshToken == "" {
					t.Fatal("token rotation failed")
				}
				mongoIntegrationOK(t, "existing authenticated client survives rotation", client.Database("appdb").Collection(lookup).FindOne(ctx, bson.D{}).Err())
				old := mongoIntegrationClient(t, proxyURI)
				if old.Database("appdb").Collection(lookup).FindOne(ctx, bson.D{}).Err() == nil {
					t.Error("old token accepted by a new client")
				}
				freshURI := RawTunnelURL("mongodb", freshToken, "localhost", port, "appdb")
				fresh := mongoIntegrationClient(t, freshURI)
				mongoIntegrationOK(t, "new token accepted", fresh.Database("appdb").Collection(lookup).FindOne(ctx, bson.D{}).Err())
				mongoIntegrationOK(t, "disable tunnel", SetTunnel(path, key, "mongo-fixture", true))
				mongoIntegrationListener(t, address, false)
				mongoIntegrationOK(t, "enable tunnel", SetTunnel(path, key, "mongo-fixture", false))
				mongoIntegrationListener(t, address, true)
				reopened := mongoIntegrationClient(t, freshURI)
				mongoIntegrationOK(t, "query after listener re-enabled", reopened.Database("appdb").Collection(lookup).FindOne(ctx, bson.D{}).Err())
			})
		})
	}
}
