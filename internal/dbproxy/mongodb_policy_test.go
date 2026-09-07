package dbproxy

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func mongoPolicyCommand(name string, arg any, fields ...bson.E) bson.D {
	doc := bson.D{{Key: name, Value: arg}, {Key: "$db", Value: "shop"}}
	return append(doc, fields...)
}

func TestMongoPolicyCommands(t *testing.T) {
	uuid := bson.Binary{Subtype: 4, Data: make([]byte, 16)}
	valid := []bson.D{
		mongoPolicyCommand("ping", 1),
		{{Key: "ping", Value: 1}, {Key: "$db", Value: "admin"}},
		mongoPolicyCommand("buildInfo", 1), mongoPolicyCommand("buildinfo", 1),
		mongoPolicyCommand("find", "orders", bson.E{Key: "filter", Value: bson.D{{Key: "host", Value: "secret business host"}, {Key: "price", Value: bson.D{{Key: "$gte", Value: 5}}}}}, bson.E{Key: "lsid", Value: bson.D{{Key: "id", Value: uuid}}}),
		mongoPolicyCommand("count", "orders", bson.E{Key: "query", Value: bson.D{{Key: "$or", Value: bson.A{bson.D{{Key: "x", Value: 1}}, bson.D{{Key: "x", Value: 2}}}}}}),
		mongoPolicyCommand("distinct", "orders", bson.E{Key: "key", Value: "user"}),
		mongoPolicyCommand("getMore", int64(12), bson.E{Key: "collection", Value: "orders"}, bson.E{Key: "batchSize", Value: int32(10)}),
		mongoPolicyCommand("killCursors", "orders", bson.E{Key: "cursors", Value: bson.A{int64(12)}}),
		mongoPolicyCommand("insert", "orders", bson.E{Key: "documents", Value: bson.A{bson.D{{Key: "host", Value: "$$USER_ROLES"}, {Key: "password", Value: "$function"}}}}),
		mongoPolicyCommand("update", "orders", bson.E{Key: "updates", Value: bson.A{bson.D{{Key: "q", Value: bson.D{}}, {Key: "u", Value: bson.D{{Key: "$set", Value: bson.D{{Key: "host", Value: "$$USER_ROLES"}, {Key: "password", Value: "$where"}}}}}, {Key: "multi", Value: true}}}}),
		mongoPolicyCommand("update", "orders", bson.E{Key: "updates", Value: bson.A{bson.D{{Key: "q", Value: bson.D{}}, {Key: "u", Value: bson.D{{Key: "host", Value: "$$USER_ROLES"}}}}}}),
		mongoPolicyCommand("delete", "orders", bson.E{Key: "deletes", Value: bson.A{bson.D{{Key: "q", Value: bson.D{}}, {Key: "limit", Value: 1}}}}),
		mongoPolicyCommand("findAndModify", "orders", bson.E{Key: "query", Value: bson.D{}}, bson.E{Key: "update", Value: bson.D{{Key: "$inc", Value: bson.D{{Key: "n", Value: 1}}}}}, bson.E{Key: "new", Value: true}),
		{{Key: "listDatabases", Value: 1}, {Key: "$db", Value: "admin"}, {Key: "nameOnly", Value: true}},
		mongoPolicyCommand("listCollections", 1, bson.E{Key: "nameOnly", Value: true}),
		mongoPolicyCommand("listIndexes", "orders"),
		{{Key: "endSessions", Value: bson.A{bson.D{{Key: "id", Value: uuid}}}}, {Key: "$db", Value: "admin"}},
	}
	for i, doc := range valid {
		if err := mongoCheckCommand(doc); err != nil {
			t.Errorf("valid %d (%s): %v", i, doc[0].Key, err)
		}
	}

	invalid := []bson.D{
		{}, {{Key: "find", Value: "orders"}}, {{Key: "$db", Value: "shop"}, {Key: "find", Value: "orders"}},
		mongoPolicyCommand("Find", "orders"), mongoPolicyCommand("hello", 1), mongoPolicyCommand("connectionStatus", 1),
		mongoPolicyCommand("explain", bson.D{{Key: "find", Value: "orders"}}), mongoPolicyCommand("serverStatus", 1),
		mongoPolicyCommand("find", "orders", bson.E{Key: "$db", Value: "shop"}),
		mongoPolicyCommand("find", "orders", bson.E{Key: "filter", Value: bson.D{}}, bson.E{Key: "filter", Value: bson.D{}}),
		mongoPolicyCommand("find", "orders", bson.E{Key: "unknown", Value: true}),
		mongoPolicyCommand("find", "orders", bson.E{Key: "filter", Value: "bad"}),
		mongoPolicyCommand("find", "orders", bson.E{Key: "filter", Value: bson.D{{Key: "$where", Value: "return true"}}}),
		mongoPolicyCommand("find", "orders", bson.E{Key: "filter", Value: bson.D{{Key: "x", Value: bson.D{{Key: "$unknown", Value: 1}}}}}),
		mongoPolicyCommand("find", "orders", bson.E{Key: "filter", Value: bson.D{{Key: "$expr", Value: "$$USER_ROLES"}}}),
		mongoPolicyCommand("find", "orders", bson.E{Key: "projection", Value: bson.D{{Key: "roles", Value: "$$USER_ROLES"}}}),
		mongoPolicyCommand("find", "orders", bson.E{Key: "let", Value: bson.D{{Key: "roles", Value: "$$USER_ROLES"}}}),
		mongoPolicyCommand("find", "orders", bson.E{Key: "txnNumber", Value: int64(1)}),
		mongoPolicyCommand("find", "orders", bson.E{Key: "startTransaction", Value: true}),
		mongoPolicyCommand("find", "orders", bson.E{Key: "autocommit", Value: false}),
		mongoPolicyCommand("find", "orders", bson.E{Key: "recoveryToken", Value: bson.D{}}),
		mongoPolicyCommand("find", "orders", bson.E{Key: "apiVersion", Value: "1"}),
		mongoPolicyCommand("find", "orders", bson.E{Key: "$readPreference", Value: bson.D{{Key: "mode", Value: "nearest"}, {Key: "tags", Value: bson.A{}}}}),
		mongoPolicyCommand("find", "orders", bson.E{Key: "maxTimeMS", Value: -1}),
		mongoPolicyCommand("find", "orders", bson.E{Key: "lsid", Value: bson.D{{Key: "id", Value: "not-uuid"}}}),
		mongoPolicyCommand("insert", "orders", bson.E{Key: "documents", Value: bson.A{bson.D{}}}, bson.E{Key: "writeConcern", Value: bson.D{{Key: "w", Value: 0}}}),
		mongoPolicyCommand("getMore", "12", bson.E{Key: "collection", Value: "orders"}),
		mongoPolicyCommand("getMore", int64(12)),
		mongoPolicyCommand("find", uuid),
	}
	for _, db := range []string{"admin", "local", "config", "", "a\x00b", "a.b", "a/b"} {
		invalid = append(invalid, bson.D{{Key: "find", Value: "orders"}, {Key: "$db", Value: db}})
	}
	for _, collection := range []string{"", "system.users", "a$b", "a\x00b", ".a", "a."} {
		invalid = append(invalid, mongoPolicyCommand("find", collection))
	}
	for i, doc := range invalid {
		if err := mongoCheckCommand(doc); err == nil {
			t.Errorf("invalid %d accepted: %v", i, doc)
		} else if err.Error() != "MongoDB command rejected by proxy policy" {
			t.Errorf("non-local error: %v", err)
		}
	}
}

func TestMongoPolicyAggregation(t *testing.T) {
	literal := bson.D{{Key: "$literal", Value: bson.D{{Key: "host", Value: "$$USER_ROLES"}, {Key: "$function", Value: "$currentOp"}}}}
	pipeline := bson.A{
		bson.D{{Key: "$match", Value: bson.D{{Key: "active", Value: true}}}},
		bson.D{{Key: "$lookup", Value: bson.D{{Key: "from", Value: "users"}, {Key: "as", Value: "users"}, {Key: "let", Value: bson.D{{Key: "id", Value: "$_id"}}}, {Key: "pipeline", Value: bson.A{bson.D{{Key: "$match", Value: bson.D{{Key: "$expr", Value: bson.D{{Key: "$eq", Value: bson.A{"$_id", "$$id"}}}}}}}}}}}},
		bson.D{{Key: "$facet", Value: bson.D{{Key: "part", Value: bson.A{bson.D{{Key: "$unionWith", Value: bson.D{{Key: "coll", Value: "archive"}, {Key: "pipeline", Value: bson.A{bson.D{{Key: "$project", Value: bson.D{{Key: "data", Value: literal}}}}}}}}}}}}}},
		bson.D{{Key: "$group", Value: bson.D{{Key: "_id", Value: "$user"}, {Key: "total", Value: bson.D{{Key: "$sum", Value: "$amount"}}}}}},
	}
	doc := mongoPolicyCommand("aggregate", "orders", bson.E{Key: "pipeline", Value: pipeline}, bson.E{Key: "cursor", Value: bson.D{}})
	if err := mongoCheckCommand(doc); err != nil {
		t.Fatal(err)
	}
	want := []mongoNamespace{{"shop", "orders"}, {"shop", "users"}, {"shop", "archive"}}
	if got := mongoCommandNamespaces(doc); !reflect.DeepEqual(got, want) {
		t.Fatalf("namespaces: %#v", got)
	}
	if got := mongoCommandNamespaces(mongoPolicyCommand("getMore", int64(8), bson.E{Key: "collection", Value: "orders"})); !reflect.DeepEqual(got, want[:1]) {
		t.Fatalf("getMore: %#v", got)
	}
	if got := mongoCommandNamespaces(mongoPolicyCommand("killCursors", "orders", bson.E{Key: "cursors", Value: bson.A{int64(8)}})); !reflect.DeepEqual(got, want[:1]) {
		t.Fatalf("killCursors: %#v", got)
	}
	for _, stage := range []string{"$currentOp", "$collStats", "$out", "$merge", "$changeStream", "$search", "$unknown"} {
		bad := bson.A{bson.D{{Key: stage, Value: bson.D{}}}}
		pipelines := []bson.A{
			bad,
			{bson.D{{Key: "$lookup", Value: bson.D{{Key: "from", Value: "users"}, {Key: "as", Value: "u"}, {Key: "pipeline", Value: bad}}}}},
			{bson.D{{Key: "$unionWith", Value: bson.D{{Key: "coll", Value: "users"}, {Key: "pipeline", Value: bad}}}}},
			{bson.D{{Key: "$facet", Value: bson.D{{Key: "part", Value: bad}}}}},
		}
		for _, p := range pipelines {
			if mongoCheckCommand(mongoPolicyCommand("aggregate", "orders", bson.E{Key: "pipeline", Value: p}, bson.E{Key: "cursor", Value: bson.D{}})) == nil {
				t.Errorf("allowed %s", stage)
			}
		}
	}
	for _, expression := range []any{"$$USER_ROLES", bson.D{{Key: "$function", Value: bson.D{}}}, bson.D{{Key: "$unknown", Value: "$user"}}} {
		p := bson.A{bson.D{{Key: "$set", Value: bson.D{{Key: "x", Value: expression}}}}}
		for _, doc := range []bson.D{
			mongoPolicyCommand("aggregate", "orders", bson.E{Key: "pipeline", Value: p}, bson.E{Key: "cursor", Value: bson.D{}}),
			mongoPolicyCommand("update", "orders", bson.E{Key: "updates", Value: bson.A{bson.D{{Key: "q", Value: bson.D{}}, {Key: "u", Value: p}}}}),
		} {
			if mongoCheckCommand(doc) == nil {
				t.Errorf("expression accepted: %v", expression)
			}
		}
	}
	for _, extra := range []bson.E{{Key: "explain", Value: true}, {Key: "explain", Value: false}} {
		if mongoCheckCommand(append(doc, extra)) == nil {
			t.Error("aggregate.explain accepted")
		}
	}
	for _, source := range []any{"system.users", bson.D{{Key: "db", Value: "admin"}, {Key: "coll", Value: "users"}}, bson.Binary{Subtype: 4, Data: make([]byte, 16)}} {
		p := bson.A{bson.D{{Key: "$lookup", Value: bson.D{{Key: "from", Value: source}, {Key: "as", Value: "x"}, {Key: "pipeline", Value: bson.A{}}}}}}
		if mongoCheckCommand(mongoPolicyCommand("aggregate", "orders", bson.E{Key: "pipeline", Value: p}, bson.E{Key: "cursor", Value: bson.D{}})) == nil {
			t.Error("unsafe source accepted")
		}
	}
	for _, doc := range []bson.D{
		mongoPolicyCommand("find", "orders", bson.E{Key: "projection", Value: bson.D{{Key: "value", Value: literal}}}),
		mongoPolicyCommand("update", "orders", bson.E{Key: "updates", Value: bson.A{bson.D{{Key: "q", Value: bson.D{}}, {Key: "u", Value: bson.A{bson.D{{Key: "$set", Value: bson.D{{Key: "value", Value: literal}}}}}}}}}),
	} {
		if err := mongoCheckCommand(doc); err != nil {
			t.Errorf("literal rejected: %v", err)
		}
	}
}

func TestMongoPolicyReplyBusinessTypes(t *testing.T) {
	decimal, _ := bson.ParseDecimal128("123.456")
	data := bson.D{{Key: "host", Value: "business-host"}, {Key: "user", Value: "business-user"}, {Key: "password", Value: "business-password"}, {Key: "decimal", Value: decimal}, {Key: "binary", Value: bson.Binary{Subtype: 0, Data: []byte{1, 2}}}, {Key: "timestamp", Value: bson.Timestamp{T: 10, I: 1}}, {Key: "oid", Value: bson.NewObjectID()}}
	for _, command := range []string{"find", "aggregate", "getMore"} {
		reply := bson.D{{Key: "ok", Value: 1}, {Key: "cursor", Value: bson.D{{Key: "id", Value: int64(2)}, {Key: "ns", Value: "shop.orders"}, {Key: "firstBatch", Value: bson.A{data}}, {Key: "nextBatch", Value: bson.A{data}}, {Key: "host", Value: "UPSTREAM_SECRET"}}}, {Key: "$clusterTime", Value: bson.D{{Key: "signature", Value: "UPSTREAM_SECRET"}}}, {Key: "operationTime", Value: bson.Timestamp{T: 5, I: 1}}, {Key: "host", Value: "UPSTREAM_SECRET"}}
		got := mongoPublicReply(command, reply)
		if len(got) == 0 {
			t.Fatal("missing public reply")
		}
		encoded, err := bson.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(encoded, []byte("UPSTREAM_SECRET")) {
			t.Fatalf("leak: %v", got)
		}
		cursor := mongoPolicyTestGet(got, "cursor").(bson.D)
		if !reflect.DeepEqual(mongoPolicyTestGet(cursor, "firstBatch"), bson.A{data}) {
			t.Fatalf("business BSON changed: %v", got)
		}
	}
	for _, tc := range []struct {
		command, field string
		value          any
	}{
		{"findAndModify", "value", data}, {"distinct", "values", bson.A{data}}, {"update", "upserted", bson.A{bson.D{{Key: "index", Value: 0}, {Key: "_id", Value: data}}}},
	} {
		got := mongoPublicReply(tc.command, bson.D{{Key: "ok", Value: 1}, {Key: tc.field, Value: tc.value}})
		if !reflect.DeepEqual(mongoPolicyTestGet(got, tc.field), tc.value) {
			t.Fatalf("%s changed: %v", tc.field, got)
		}
	}
}

func mongoPolicyTestGet(doc bson.D, key string) any {
	for _, e := range doc {
		if e.Key == key {
			return e.Value
		}
	}
	return nil
}

func TestMongoPolicyReplyErrors(t *testing.T) {
	secret := "UPSTREAM_SECRET"
	errDoc := bson.D{{Key: "code", Value: int32(11000)}, {Key: "codeName", Value: secret}, {Key: "errmsg", Value: secret}, {Key: "index", Value: int32(0)}, {Key: "errInfo", Value: bson.D{{Key: "host", Value: secret}}}, {Key: "unknown", Value: secret}}
	for _, ok := range []any{0, 1, secret, true} {
		reply := append(bson.D{{Key: "ok", Value: ok}, {Key: "writeErrors", Value: bson.A{errDoc}}, {Key: "writeConcernError", Value: errDoc}, {Key: "errorLabels", Value: bson.A{"RetryableWriteError", secret}}, {Key: "n", Value: int32(2)}}, errDoc...)
		got := mongoPublicReply("update", reply)
		encoded, err := bson.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(encoded, []byte(secret)) {
			t.Fatalf("leak: %v", got)
		}
		if _, ok := mongoPolicyTestGet(got, "ok").(float64); !ok {
			t.Fatalf("not normalized: %v", got)
		}
	}
	got := mongoPublicReply("find", bson.D{{Key: "ok", Value: 0}, {Key: "code", Value: secret}, {Key: "errmsg", Value: secret}, {Key: "operationTime", Value: secret}})
	encoded, _ := bson.Marshal(got)
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("malformed error leaked: %v", got)
	}
}

func TestMongoPolicyReplyMetadata(t *testing.T) {
	secret := "UPSTREAM_SECRET"
	collections := bson.A{bson.D{{Key: "name", Value: "orders"}, {Key: "type", Value: "collection"}, {Key: "options", Value: bson.D{{Key: "host", Value: secret}}}, {Key: "info", Value: secret}}, bson.D{{Key: "name", Value: "view"}, {Key: "type", Value: "view"}, {Key: "options", Value: secret}}, bson.D{{Key: "name", Value: "system.users"}, {Key: "type", Value: "collection"}}}
	got := mongoPublicReply("listCollections", bson.D{{Key: "ok", Value: 1}, {Key: "cursor", Value: bson.D{{Key: "id", Value: int64(1)}, {Key: "ns", Value: "shop.$cmd.listCollections"}, {Key: "firstBatch", Value: collections}}}})
	if len(got) == 0 {
		t.Fatal("missing metadata reply")
	}
	cursor := mongoPolicyTestGet(got, "cursor").(bson.D)
	if want := (bson.A{bson.D{{Key: "name", Value: "orders"}, {Key: "type", Value: "collection"}}}); !reflect.DeepEqual(mongoPolicyTestGet(cursor, "firstBatch"), want) {
		t.Fatalf("collections: %v", got)
	}
	dbs := bson.A{}
	for _, name := range []string{"admin", "local", "config", "shop"} {
		dbs = append(dbs, bson.D{{Key: "name", Value: name}, {Key: "sizeOnDisk", Value: int64(42)}, {Key: "empty", Value: false}, {Key: "host", Value: secret}})
	}
	got = mongoPublicReply("listDatabases", bson.D{{Key: "ok", Value: 1}, {Key: "databases", Value: dbs}, {Key: "totalSize", Value: int64(999)}, {Key: "host", Value: secret}})
	if want := (bson.A{bson.D{{Key: "name", Value: "shop"}, {Key: "sizeOnDisk", Value: int64(42)}, {Key: "empty", Value: false}}}); !reflect.DeepEqual(mongoPolicyTestGet(got, "databases"), want) {
		t.Fatalf("databases: %v", got)
	}
	indexes := bson.A{bson.D{{Key: "v", Value: int32(2)}, {Key: "key", Value: bson.D{{Key: "user", Value: int32(1)}}}, {Key: "name", Value: "user_1"}, {Key: "unique", Value: true}, {Key: "ns", Value: secret}, {Key: "buildUUID", Value: secret}, {Key: "storageEngine", Value: bson.D{{Key: "host", Value: secret}}}}}
	got = mongoPublicReply("listIndexes", bson.D{{Key: "ok", Value: 1}, {Key: "cursor", Value: bson.D{{Key: "id", Value: int64(0)}, {Key: "ns", Value: "shop.orders"}, {Key: "firstBatch", Value: indexes}}}})
	encoded, _ := bson.Marshal(got)
	if bytes.Contains(encoded, []byte(secret)) {
		t.Fatalf("index leak: %v", got)
	}
	got = mongoPublicReply("buildInfo", bson.D{{Key: "ok", Value: 1}, {Key: "version", Value: "8.0.12"}, {Key: "versionArray", Value: bson.A{8, 0, 12, 0}}, {Key: "sysInfo", Value: secret}, {Key: "gitVersion", Value: secret}, {Key: "modules", Value: bson.A{secret}}})
	encoded, _ = bson.Marshal(got)
	if bytes.Contains(encoded, []byte(secret)) || mongoPolicyTestGet(got, "version") != "8.0.12" {
		t.Fatalf("buildInfo: %v", got)
	}
}

func TestMongoPolicyExpressionSchemas(t *testing.T) {
	valid := []any{
		bson.D{{Key: "$add", Value: bson.A{"$n", 1}}},
		bson.D{{Key: "$map", Value: bson.D{{Key: "input", Value: "$items"}, {Key: "as", Value: "item"}, {Key: "in", Value: bson.D{{Key: "$multiply", Value: bson.A{"$$item.price", 2}}}}}}},
		bson.D{{Key: "$filter", Value: bson.D{{Key: "input", Value: "$items"}, {Key: "cond", Value: bson.D{{Key: "$gt", Value: bson.A{"$$this", 0}}}}}}},
		bson.D{{Key: "$let", Value: bson.D{{Key: "vars", Value: bson.D{{Key: "n", Value: 1}}}, {Key: "in", Value: "$$n"}}}},
		bson.D{{Key: "$convert", Value: bson.D{{Key: "input", Value: "$n"}, {Key: "to", Value: "string"}, {Key: "onNull", Value: nil}}}},
		bson.D{{Key: "$dateToString", Value: bson.D{{Key: "date", Value: "$$NOW"}, {Key: "format", Value: "%Y-%m-%d"}}}},
		bson.D{{Key: "$cond", Value: bson.D{{Key: "if", Value: true}, {Key: "then", Value: "$host"}, {Key: "else", Value: nil}}}},
		bson.D{{Key: "$switch", Value: bson.D{{Key: "branches", Value: bson.A{bson.D{{Key: "case", Value: true}, {Key: "then", Value: 1}}}}, {Key: "default", Value: 0}}}},
	}
	for _, value := range valid {
		if err := mongoCheckCommand(mongoPolicyCommand("find", "orders", bson.E{Key: "projection", Value: bson.D{{Key: "x", Value: value}}})); err != nil {
			t.Errorf("valid expression rejected: %v: %v", value, err)
		}
	}
	invalid := []any{
		bson.D{{Key: "$map", Value: bson.D{{Key: "input", Value: "$items"}, {Key: "in", Value: 1}, {Key: "unknown", Value: true}}}},
		bson.D{{Key: "$map", Value: bson.D{{Key: "input", Value: "$items"}}}},
		bson.D{{Key: "$map", Value: bson.D{{Key: "input", Value: "$items"}, {Key: "in", Value: 1}, {Key: "as", Value: "USER_ROLES"}}}},
		bson.D{{Key: "$let", Value: bson.D{{Key: "vars", Value: bson.D{{Key: "USER_ROLES", Value: 1}}}, {Key: "in", Value: 1}}}},
		bson.D{{Key: "$convert", Value: bson.D{{Key: "input", Value: "$n"}, {Key: "to", Value: "string"}, {Key: "unknown", Value: true}}}},
		bson.D{{Key: "$eq", Value: bson.A{1}}}, bson.D{{Key: "$add", Value: "wrong"}},
		bson.D{{Key: "$cond", Value: bson.D{{Key: "if", Value: true}, {Key: "then", Value: 1}, {Key: "else", Value: 0}, {Key: "unknown", Value: 1}}}},
		bson.M{"$function": bson.D{}},
	}
	for _, value := range invalid {
		if mongoCheckCommand(mongoPolicyCommand("find", "orders", bson.E{Key: "projection", Value: bson.D{{Key: "x", Value: value}}})) == nil {
			t.Errorf("invalid expression accepted: %v", value)
		}
	}
	group := bson.A{bson.D{{Key: "$group", Value: bson.D{{Key: "_id", Value: nil}, {Key: "n", Value: bson.D{{Key: "$sum", Value: 1}}}}}}}
	if err := mongoCheckCommand(mongoPolicyCommand("aggregate", "orders", bson.E{Key: "pipeline", Value: group}, bson.E{Key: "cursor", Value: bson.D{}})); err != nil {
		t.Errorf("null group key rejected: %v", err)
	}
	for _, p := range []bson.A{
		{bson.D{{Key: "$lookup", Value: bson.D{{Key: "from", Value: "users"}, {Key: "as", Value: "users"}, {Key: "localField", Value: "id"}, {Key: "foreignField", Value: "id"}, {Key: "pipeline", Value: nil}}}}},
		{bson.D{{Key: "$unionWith", Value: bson.D{{Key: "coll", Value: "users"}, {Key: "pipeline", Value: nil}}}}},
		{bson.D{{Key: "$group", Value: bson.D{{Key: "_id", Value: 1}, {Key: "bad", Value: bson.D{{Key: "$add", Value: bson.A{1, 2}}}}}}}},
	} {
		if mongoCheckCommand(mongoPolicyCommand("aggregate", "orders", bson.E{Key: "pipeline", Value: p}, bson.E{Key: "cursor", Value: bson.D{}})) == nil {
			t.Errorf("invalid pipeline accepted: %v", p)
		}
	}
}

func TestMongoPolicyRejectsOpaqueSyntax(t *testing.T) {
	raw, err := bson.Marshal(bson.D{{Key: "$where", Value: "UPSTREAM_SECRET"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{bson.M{"$where": "UPSTREAM_SECRET"}, bson.Raw(raw), bson.RawValue{Type: bson.TypeEmbeddedDocument, Value: raw}} {
		for _, doc := range []bson.D{
			mongoPolicyCommand("find", "orders", bson.E{Key: "filter", Value: bson.D{{Key: "x", Value: value}}}),
			mongoPolicyCommand("update", "orders", bson.E{Key: "updates", Value: bson.A{bson.D{{Key: "q", Value: bson.D{}}, {Key: "u", Value: bson.D{{Key: "$pull", Value: bson.D{{Key: "items", Value: value}}}}}}}}),
		} {
			if mongoCheckCommand(doc) == nil {
				t.Errorf("opaque syntax accepted: %T", value)
			}
		}
	}
}

func TestMongoPolicyBusinessCommandRoundTrip(t *testing.T) {
	decimal, _ := bson.ParseDecimal128("1234.5678")
	data := bson.D{{Key: "decimal", Value: decimal}, {Key: "id", Value: bson.NewObjectID()}, {Key: "binary", Value: bson.Binary{Subtype: 0, Data: []byte{1, 2, 3}}}, {Key: "timestamp", Value: bson.Timestamp{T: 123, I: 456}}, {Key: "host", Value: "$$USER_ROLES"}, {Key: "user", Value: "$where"}, {Key: "password", Value: "literal password"}, {Key: "object", Value: bson.D{{Key: "$function", Value: "literal"}}}}
	for _, doc := range []bson.D{
		mongoPolicyCommand("insert", "orders", bson.E{Key: "documents", Value: bson.A{data}}),
		mongoPolicyCommand("update", "orders", bson.E{Key: "updates", Value: bson.A{bson.D{{Key: "q", Value: bson.D{}}, {Key: "u", Value: data}}}}),
		mongoPolicyCommand("update", "orders", bson.E{Key: "updates", Value: bson.A{bson.D{{Key: "q", Value: bson.D{}}, {Key: "u", Value: bson.D{{Key: "$set", Value: data}}}}}}),
		mongoPolicyCommand("find", "orders", bson.E{Key: "filter", Value: bson.D{{Key: "data", Value: bson.D{{Key: "$eq", Value: data}}}}}),
	} {
		before, err := bson.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		var decoded bson.D
		if err := bson.Unmarshal(before, &decoded); err != nil {
			t.Fatal(err)
		}
		if err := mongoCheckCommand(decoded); err != nil {
			t.Fatalf("business data rejected: %v", err)
		}
		after, err := bson.Marshal(decoded)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Fatal("policy mutated business BSON")
		}
	}
}

func TestMongoPolicyProjectionContexts(t *testing.T) {
	for _, slice := range []any{5, -5, bson.A{2, 5}} {
		if err := mongoCheckCommand(mongoPolicyCommand("find", "orders", bson.E{Key: "projection", Value: bson.D{{Key: "items", Value: bson.D{{Key: "$slice", Value: slice}}}}})); err != nil {
			t.Errorf("find slice rejected: %v", err)
		}
	}
	p := bson.A{bson.D{{Key: "$project", Value: bson.D{{Key: "items", Value: bson.D{{Key: "$elemMatch", Value: bson.D{{Key: "x", Value: 1}}}}}}}}}
	if mongoCheckCommand(mongoPolicyCommand("aggregate", "orders", bson.E{Key: "pipeline", Value: p}, bson.E{Key: "cursor", Value: bson.D{}})) == nil {
		t.Error("find-only projection accepted as expression")
	}
}

func TestMongoPolicyAdminBuildInfo(t *testing.T) {
	for _, command := range []string{"buildInfo", "buildinfo"} {
		doc := bson.D{{Key: command, Value: int32(1)}, {Key: "$db", Value: "admin"}}
		if err := mongoCheckCommand(doc); err != nil {
			t.Errorf("admin %s rejected: %v", command, err)
		}
		if namespaces := mongoCommandNamespaces(doc); len(namespaces) != 0 {
			t.Errorf("buildInfo has data namespaces: %v", namespaces)
		}
		reply := bson.D{{Key: "ok", Value: 1}, {Key: "version", Value: "8.0.12"}, {Key: "versionArray", Value: bson.A{8, 0, 12, 0}}, {Key: "sysInfo", Value: "UPSTREAM_SECRET"}, {Key: "buildEnvironment", Value: bson.D{{Key: "host", Value: "UPSTREAM_SECRET"}}}}
		want := bson.D{{Key: "ok", Value: float64(1)}, {Key: "version", Value: "8.0.12"}, {Key: "versionArray", Value: bson.A{8, 0, 12, 0}}}
		if got := mongoPublicReply(command, reply); !reflect.DeepEqual(got, want) {
			t.Errorf("unexpected buildInfo schema: %v", got)
		}
	}
	for _, doc := range []bson.D{
		{{Key: "find", Value: "orders"}, {Key: "$db", Value: "admin"}},
		mongoPolicyCommand("getMore", int64(1), bson.E{Key: "collection", Value: "$cmd.listCollections"}),
		mongoPolicyCommand("killCursors", "$cmd.listCollections", bson.E{Key: "cursors", Value: bson.A{int64(1)}}),
	} {
		if mongoCheckCommand(doc) == nil {
			t.Errorf("data namespace restriction loosened: %v", doc)
		}
	}
}

func TestMongoPolicyOpaqueAllOperands(t *testing.T) {
	operator := bson.D{{Key: "$elemMatch", Value: bson.D{{Key: "$expr", Value: "$$USER_ROLES"}}}}
	raw, err := bson.Marshal(operator)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{bson.M{"$elemMatch": operator[0].Value}, bson.Raw(raw), bson.RawValue{Type: bson.TypeEmbeddedDocument, Value: raw}} {
		doc := mongoPolicyCommand("find", "orders", bson.E{Key: "filter", Value: bson.D{{Key: "items", Value: bson.D{{Key: "$all", Value: bson.A{value}}}}}})
		if mongoCheckCommand(doc) == nil {
			t.Errorf("opaque $all syntax accepted: %T", value)
		}
	}
}

func TestMongoPolicyUntrustedControlValues(t *testing.T) {
	values := []any{nil, bson.D{}, bson.A{}, bson.M{}, []byte{1}, bson.Binary{}, bson.RawValue{}, bson.Regex{}, bson.CodeWithScope{Scope: bson.D{}}}
	for _, value := range values {
		doc := mongoPolicyCommand("insert", "orders", bson.E{Key: "documents", Value: bson.A{bson.D{}}}, bson.E{Key: "writeConcern", Value: bson.D{{Key: "w", Value: value}}})
		if mongoCheckCommand(doc) == nil {
			t.Errorf("invalid write concern accepted: %T", value)
		}
		for _, key := range []string{"maxTimeMS", "$db", "lsid", "$readPreference"} {
			doc := mongoPolicyCommand("find", "orders")
			if key == "$db" {
				doc[1].Value = value
			} else {
				doc = append(doc, bson.E{Key: key, Value: value})
			}
			if mongoCheckCommand(doc) == nil {
				t.Errorf("invalid %s accepted: %T", key, value)
			}
		}
		reply := mongoPublicReply("update", bson.D{{Key: "ok", Value: value}, {Key: "code", Value: value}, {Key: "writeConcernError", Value: bson.D{{Key: "code", Value: value}}}})
		if mongoPolicyTestGet(reply, "ok") != float64(0) {
			t.Errorf("invalid ok accepted: %T", value)
		}
	}
}

func TestMongoPolicyRuntimeMetadataExpressions(t *testing.T) {
	computedName := bson.D{{Key: "$concat", Value: bson.A{bson.D{{Key: "$literal", Value: "$$"}}, "USER_", "ROLES"}}}
	raw, err := bson.Marshal(bson.D{{Key: "$function", Value: bson.D{{Key: "body", Value: "$$USER_ROLES"}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		value   any
		allowed bool
	}{
		{name: "roles", value: "$$USER_ROLES"},
		{name: "roles path", value: "$$USER_ROLES.role"},
		{name: "getField metadata input", value: bson.D{{Key: "$getField", Value: bson.D{{Key: "field", Value: "role"}, {Key: "input", Value: "$$USER_ROLES"}}}}},
		{name: "getField computed field", value: bson.D{{Key: "$getField", Value: bson.D{{Key: "field", Value: computedName}, {Key: "input", Value: "$$ROOT"}}}}},
		{name: "computed function body", value: bson.D{{Key: "$function", Value: bson.D{{Key: "body", Value: computedName}, {Key: "args", Value: bson.A{}}, {Key: "lang", Value: "js"}}}}},
		{name: "now", value: "$$NOW", allowed: true},
		{name: "root", value: "$$ROOT", allowed: true},
		{name: "root user data", value: "$$ROOT.USER_ROLES", allowed: true},
		{name: "computed string is not eval", value: computedName, allowed: true},
		{name: "raw literal", value: bson.D{{Key: "$literal", Value: bson.Raw(raw)}}, allowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, doc := range []bson.D{
				mongoPolicyCommand("find", "orders", bson.E{Key: "projection", Value: bson.D{{Key: "value", Value: tc.value}}}),
				mongoPolicyCommand("find", "orders", bson.E{Key: "filter", Value: bson.D{{Key: "$expr", Value: bson.D{{Key: "$eq", Value: bson.A{tc.value, 1}}}}}}),
				mongoPolicyCommand("find", "orders", bson.E{Key: "let", Value: bson.D{{Key: "value", Value: tc.value}}}),
				mongoPolicyCommand("update", "orders", bson.E{Key: "updates", Value: bson.A{bson.D{{Key: "q", Value: bson.D{}}, {Key: "u", Value: bson.A{bson.D{{Key: "$set", Value: bson.D{{Key: "value", Value: tc.value}}}}}}}}}),
			} {
				if accepted := mongoCheckCommand(doc) == nil; accepted != tc.allowed {
					t.Errorf("%s accepted=%v, want %v", doc[0].Key, accepted, tc.allowed)
				}
			}
		})
	}
}

func TestMongoPolicyMetadataFilters(t *testing.T) {
	oracle := bson.D{
		{Key: "type", Value: "view"},
		{Key: "name", Value: "v"},
		{Key: "options.viewOn", Value: bson.D{{Key: "$regex", Value: "^sec"}}},
	}
	for _, command := range []string{"listCollections", "listDatabases"} {
		t.Run(command, func(t *testing.T) {
			valid := []bson.D{
				{},
				{{Key: "name", Value: "orders"}},
				{{Key: "name", Value: bson.Regex{Pattern: "^orders", Options: "i"}}},
				{{Key: "name", Value: bson.D{{Key: "$regex", Value: "^orders"}, {Key: "$options", Value: "i"}}}},
				{{Key: "name", Value: bson.D{{Key: "$eq", Value: "orders"}}}},
				{{Key: "name", Value: bson.D{{Key: "$in", Value: bson.A{"orders", bson.Regex{Pattern: "^archive"}}}}}},
				{{Key: "$and", Value: bson.A{bson.D{{Key: "name", Value: bson.Regex{Pattern: "^orders"}}}, bson.D{{Key: "$or", Value: bson.A{bson.D{{Key: "name", Value: "orders"}}, bson.D{{Key: "name", Value: "orders_archive"}}}}}}}},
			}
			if command == "listCollections" {
				valid = append(valid,
					bson.D{{Key: "type", Value: "collection"}, {Key: "name", Value: bson.Regex{Pattern: "^orders"}}},
					bson.D{{Key: "$and", Value: bson.A{bson.D{{Key: "type", Value: "collection"}}, bson.D{{Key: "name", Value: "orders"}}}}},
				)
			}
			for _, filter := range valid {
				doc := mongoPolicyCommand(command, 1, bson.E{Key: "filter", Value: filter})
				before, err := bson.Marshal(doc)
				if err != nil {
					t.Fatal(err)
				}
				if err := mongoCheckCommand(doc); err != nil {
					t.Errorf("public filter rejected: %v: %v", filter, err)
				}
				after, err := bson.Marshal(doc)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(before, after) {
					t.Error("metadata filter mutated")
				}
			}
			invalid := []bson.D{
				oracle,
				{{Key: "options", Value: bson.D{{Key: "viewOn", Value: "secret"}}}},
				{{Key: "info.uuid", Value: bson.Binary{Subtype: 4, Data: make([]byte, 16)}}},
				{{Key: "roles", Value: "admin"}},
				{{Key: "name.hidden", Value: "secret"}},
				{{Key: "$expr", Value: bson.D{{Key: "$eq", Value: bson.A{"$options.viewOn", "secret"}}}}},
				{{Key: "$expr", Value: true}},
				{{Key: "name", Value: bson.D{{Key: "$exists", Value: true}}}},
				{{Key: "name", Value: bson.D{{Key: "$not", Value: bson.Regex{Pattern: "^system"}}}}},
				{{Key: "name", Value: bson.D{{Key: "$unknown", Value: "orders"}}}},
				{{Key: "name", Value: bson.D{{Key: "$in", Value: bson.A{bson.D{{Key: "$expr", Value: true}}}}}}},
				{{Key: "name", Value: bson.D{{Key: "$eq", Value: bson.D{{Key: "options", Value: "secret"}}}}}},
				{{Key: "name", Value: nil}},
				{{Key: "name", Value: 1}},
				{{Key: "name", Value: bson.D{}}},
				{{Key: "name", Value: "orders"}, {Key: "name", Value: "other"}},
				{{Key: "name", Value: bson.D{{Key: "$regex", Value: "a"}, {Key: "$regex", Value: "b"}}}},
				{{Key: "name", Value: bson.D{{Key: "$options", Value: "i"}}}},
				{{Key: "name", Value: bson.Regex{Pattern: "orders", Options: "unknown"}}},
				{{Key: "$and", Value: bson.A{}}},
				{{Key: "$or", Value: bson.D{{Key: "name", Value: "orders"}}}},
				{{Key: "$and", Value: bson.A{bson.M{"name": "orders"}}}},
			}
			if command == "listDatabases" {
				invalid = append(invalid, bson.D{{Key: "type", Value: "collection"}}, bson.D{{Key: "sizeOnDisk", Value: 1}})
			}
			for _, filter := range invalid {
				nestedFilter := bson.D{{Key: "$and", Value: bson.A{
					bson.D{{Key: "name", Value: "orders"}},
					bson.D{{Key: "$or", Value: bson.A{filter}}},
				}}}
				for _, nested := range []bson.D{filter, nestedFilter} {
					if err := mongoCheckCommand(mongoPolicyCommand(command, 1, bson.E{Key: "filter", Value: nested})); err == nil {
						t.Errorf("private/unsupported filter accepted: %v", nested)
					}
				}
			}
		})
	}
	for _, filter := range []bson.D{oracle, {{Key: "$expr", Value: bson.D{{Key: "$eq", Value: bson.A{"$options.viewOn", "secret"}}}}}} {
		if err := mongoCheckCommand(mongoPolicyCommand("find", "orders", bson.E{Key: "filter", Value: filter})); err != nil {
			t.Errorf("ordinary business filter restricted: %v", err)
		}
	}
}
