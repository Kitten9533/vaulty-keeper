package dbproxy

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/xdg-go/scram"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type mongoCursorKey struct {
	scope [32]byte
	id    int64
}

type mongoCursor struct {
	namespace string
	command   string
	session   string
	used      time.Time
}

// Cursor ownership follows a registered target/token and logical session, not
// a socket: native drivers can check out another socket for getMore.
type mongoTunnelState struct {
	slots   chan struct{}
	mu      sync.Mutex
	cursors map[mongoCursorKey]mongoCursor
}

func newMongoTunnelState() *mongoTunnelState {
	return &mongoTunnelState{slots: make(chan struct{}, 128), cursors: make(map[mongoCursorKey]mongoCursor)}
}

func mongoSession(doc bson.D) string {
	lsid, _ := mongoAsDoc(mongoGet(doc, "lsid"))
	id, _ := mongoGet(lsid, "id").(bson.Binary)
	return string(id.Data)
}

func (s *mongoTunnelState) cursor(scope [32]byte, id int64, ns, session string) (mongoCursor, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := mongoCursorKey{scope, id}
	c, ok := s.cursors[k]
	if !ok || c.namespace != ns || c.session != session || time.Since(c.used) > 30*time.Minute {
		return mongoCursor{}, false
	}
	return c, true
}

func (s *mongoTunnelState) remember(scope [32]byte, id int64, c mongoCursor) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for k, old := range s.cursors {
		if time.Since(old.used) > 30*time.Minute {
			delete(s.cursors, k)
		} else if k.scope == scope {
			count++
		}
	}
	if _, exists := s.cursors[mongoCursorKey{scope, id}]; !exists && (count >= 1024 || len(s.cursors) >= 16384) {
		return false
	}
	c.used = time.Now()
	s.cursors[mongoCursorKey{scope, id}] = c
	return true
}

func (s *mongoTunnelState) forget(scope [32]byte, id int64) {
	s.mu.Lock()
	delete(s.cursors, mongoCursorKey{scope, id})
	s.mu.Unlock()
}

func mongoError(code int32, message string) bson.D {
	return bson.D{{Key: "ok", Value: float64(0)}, {Key: "code", Value: code}, {Key: "errmsg", Value: message}}
}

func mongoHelloReply(upstream, request bson.D) bson.D {
	primary, _ := mongoGet(upstream, "isWritablePrimary").(bool)
	reply := bson.D{
		{Key: "ok", Value: float64(1)}, {Key: "helloOk", Value: true},
		{Key: "isWritablePrimary", Value: primary}, {Key: "ismaster", Value: primary},
		{Key: "minWireVersion", Value: int32(0)}, {Key: "maxWireVersion", Value: int32(25)},
		{Key: "maxBsonObjectSize", Value: int32(16 * 1024 * 1024)},
		{Key: "maxMessageSizeBytes", Value: int32(mongoMaxMessage)},
		{Key: "maxWriteBatchSize", Value: int32(100000)},
	}
	if mongoGet(request, "saslSupportedMechs") != nil {
		reply = append(reply, bson.E{Key: "saslSupportedMechs", Value: bson.A{"SCRAM-SHA-256"}})
	}
	if _, ok := mongoGet(upstream, "setName").(string); ok {
		reply = append(reply, bson.E{Key: "setName", Value: "vaulty"})
		if secondary, ok := mongoGet(upstream, "secondary").(bool); ok {
			reply = append(reply, bson.E{Key: "secondary", Value: secondary})
		}
	}
	if mongoGet(upstream, "msg") == "isdbgrid" {
		reply = append(reply, bson.E{Key: "msg", Value: "isdbgrid"})
	}
	if minutes, ok := mongoAuthNumber(mongoGet(upstream, "logicalSessionTimeoutMinutes")); ok && minutes > 0 && minutes <= 1440 {
		reply = append(reply, bson.E{Key: "logicalSessionTimeoutMinutes", Value: int32(minutes)})
	}
	return reply
}

func mongoHelloRequest(doc bson.D) bool {
	if len(doc) == 0 || !mongoPolicyHas("hello isMaster ismaster", doc[0].Key) || !mongoPolicyRange(doc[0].Value, 1, 1) {
		return false
	}
	if !mongoPolicyFields(doc, doc[0].Key+" $db helloOk client compression saslSupportedMechs speculativeAuthenticate loadBalanced backpressure $readPreference maxTimeMS lsid") || !mongoHandshakeReadPreference(doc) {
		return false
	}
	if lsid := mongoGet(doc, "lsid"); lsid != nil && !mongoPolicySession(lsid) {
		return false
	}
	if timeout := mongoGet(doc, "maxTimeMS"); timeout != nil && !mongoPolicyRange(timeout, 0, 1<<31-1) {
		return false
	}
	if version := mongoGet(doc, "backpressure"); version != nil && version != "2" {
		return false
	}
	if db := mongoGet(doc, "$db"); db != nil && db != "admin" {
		return false
	}
	if balanced := mongoGet(doc, "loadBalanced"); balanced != nil && balanced != false {
		return false
	}
	return true
}

func mongoHandshakeReadPreference(doc bson.D) bool {
	v := mongoGet(doc, "$readPreference")
	if v == nil {
		return true
	}
	d, ok := mongoAsDoc(v)
	if !ok || len(d) != 1 || d[0].Key != "mode" {
		return false
	}
	mode, ok := d[0].Value.(string)
	return ok && mongoPolicyHas("primary primaryPreferred secondary secondaryPreferred nearest", mode)
}

func handleMongo(ctx context.Context, client net.Conn, u *url.URL, token, name string, state *mongoTunnelState) error {
	if token == "" {
		return errors.New("MongoDB requires a dedicated connection token")
	}
	select {
	case state.slots <- struct{}{}:
		defer func() { <-state.slots }()
	default:
		return errors.New("MongoDB tunnel connection limit reached")
	}
	stopClient := context.AfterFunc(ctx, func() { _ = client.Close() })
	defer stopClient()
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	first, err := mongoReadMessageLimit(client, 1<<20)
	if err != nil || first.ResponseTo != 0 || !mongoHelloRequest(first.Body) {
		return errors.New("invalid MongoDB client handshake")
	}
	// Monitoring sockets never perform client authentication. Their upstream
	// connection is private and may only service the whitelisted hello below.
	backend, err := mongoDial(u)
	if err != nil {
		_ = mongoWriteReply(client, first, mongoError(6, "MongoDB backend unavailable"))
		return err
	}
	defer backend.close()
	stopBackend := context.AfterFunc(ctx, func() { _ = backend.close() })
	defer stopBackend()
	if err = mongoWriteReply(client, first, mongoHelloReply(backend.hello, first.Body)); err != nil {
		return err
	}
	scope := sha256.Sum256([]byte(name + "\x00" + u.String() + "\x00" + token))
	var conversation *scram.ServerConversation
	var conversationID int32
	authed, awaitingAck, skipEmpty := false, false, false
	for {
		idle := 30 * time.Minute
		if !authed {
			idle = 45 * time.Second
			if conversation != nil {
				idle = 5 * time.Second
			}
		}
		_ = client.SetReadDeadline(time.Now().Add(idle))
		limit := mongoMaxMessage
		if !authed {
			limit = 1 << 20
		}
		req, err := mongoReadMessageLimit(client, limit)
		if err != nil {
			return nil
		}
		if req.ResponseTo != 0 || len(req.Body) == 0 {
			return errors.New("invalid MongoDB client command")
		}
		_ = client.SetWriteDeadline(time.Now().Add(backend.config.Timeout))
		command := req.Body[0].Key
		var reply bson.D
		switch command {
		case "hello", "isMaster", "ismaster":
			if !mongoHelloRequest(req.Body) {
				reply = mongoError(13, "MongoDB handshake option not supported")
				break
			}
			hello, err := backend.command(bson.D{{Key: "hello", Value: int32(1)}, {Key: "$db", Value: "admin"}})
			if err != nil || !mongoCommandOK(hello) {
				_ = mongoWriteReply(client, req, mongoError(6, "MongoDB backend unavailable"))
				return errors.New("MongoDB backend unavailable")
			}
			reply = mongoHelloReply(hello, req.Body)
		case "saslStart", "saslContinue":
			failure := func() error {
				_ = mongoWriteReply(client, req, mongoError(18, "MongoDB tunnel authentication failed"))
				return errors.New("MongoDB tunnel authentication failed")
			}
			fields := "saslContinue payload conversationId $db $readPreference maxTimeMS"
			if command == "saslStart" {
				fields = "saslStart mechanism payload autoAuthorize options $db $readPreference maxTimeMS"
			}
			if timeout := mongoGet(req.Body, "maxTimeMS"); timeout != nil && !mongoPolicyRange(timeout, 0, 1<<31-1) {
				return failure()
			}
			payload, binary := mongoGet(req.Body, "payload").(bson.Binary)
			if authed || !mongoPolicyFields(req.Body, fields) || !mongoHandshakeReadPreference(req.Body) || !mongoPolicyRange(req.Body[0].Value, 1, 1) || mongoGet(req.Body, "$db") != "admin" || !binary || payload.Subtype != 0 || len(payload.Data) > 16384 {
				return failure()
			}
			if command == "saslStart" {
				if conversation != nil || mongoGet(req.Body, "mechanism") != "SCRAM-SHA-256" {
					return failure()
				}
				if options := mongoGet(req.Body, "options"); options != nil {
					d, valid := mongoAsDoc(options)
					if !valid || !mongoPolicyFields(d, "skipEmptyExchange") {
						return failure()
					}
					if v := mongoGet(d, "skipEmptyExchange"); v != nil {
						var valid bool
						skipEmpty, valid = v.(bool)
						if !valid {
							return failure()
						}
					}
				}
				var salt [16]byte
				if _, err = rand.Read(salt[:]); err != nil {
					return failure()
				}
				credentials, err := scram.SHA256.NewClient("vaulty", token, "")
				if err != nil {
					return failure()
				}
				stored := credentials.GetStoredCredentials(scram.KeyFactors{Salt: string(salt[:]), Iters: 4096})
				server, err := scram.SHA256.NewServer(func(user string) (scram.StoredCredentials, error) {
					if user != "vaulty" {
						return scram.StoredCredentials{}, errors.New("unknown tunnel user")
					}
					return stored, nil
				})
				if err != nil {
					return failure()
				}
				conversation = server.NewConversation()
				conversationID = mongoReplyID.Add(1)
			} else if conversation == nil || mongoGet(req.Body, "conversationId") != conversationID {
				return failure()
			}
			response := ""
			if awaitingAck {
				if len(payload.Data) != 0 {
					return failure()
				}
				authed = true
			} else {
				response, err = conversation.Step(string(payload.Data))
				if err != nil || (conversation.Done() && !conversation.Valid()) {
					return failure()
				}
				if conversation.Valid() {
					authed, awaitingAck = skipEmpty, !skipEmpty
				}
			}
			reply = bson.D{{Key: "ok", Value: float64(1)}, {Key: "conversationId", Value: conversationID}, {Key: "done", Value: authed}, {Key: "payload", Value: bson.Binary{Subtype: 0, Data: []byte(response)}}}
		case "connectionStatus":
			if timeout := mongoGet(req.Body, "maxTimeMS"); timeout != nil && !mongoPolicyRange(timeout, 0, 1<<31-1) {
				reply = mongoError(13, "MongoDB identity option not supported")
				break
			}
			if !mongoPolicyFields(req.Body, "connectionStatus showPrivileges $db lsid maxTimeMS $readPreference") || !mongoHandshakeReadPreference(req.Body) || !mongoPolicyRange(req.Body[0].Value, 1, 1) || mongoGet(req.Body, "$db") != "admin" || (mongoGet(req.Body, "showPrivileges") != nil && mongoGet(req.Body, "showPrivileges") != false) {
				reply = mongoError(13, "MongoDB identity option not supported")
				break
			}
			users := bson.A{}
			if authed {
				users = append(users, bson.D{{Key: "user", Value: "vaulty"}, {Key: "db", Value: "admin"}})
			}
			reply = bson.D{{Key: "ok", Value: float64(1)}, {Key: "authInfo", Value: bson.D{{Key: "authenticatedUsers", Value: users}, {Key: "authenticatedUserRoles", Value: bson.A{}}}}}
		default:
			if !authed {
				reply = mongoError(13, "MongoDB tunnel authentication required")
			} else {
				reply, err = state.execute(backend, scope, req.Body)
				if err != nil {
					_ = mongoWriteReply(client, req, mongoError(6, "MongoDB backend operation failed"))
					return err
				}
			}
		}
		if err = mongoWriteReply(client, req, reply); err != nil {
			return err
		}
	}
}

func (s *mongoTunnelState) execute(backend *mongoBackend, scope [32]byte, doc bson.D) (bson.D, error) {
	denied := mongoError(13, "MongoDB command, option or namespace not supported by this tunnel")
	command, publicCommand := doc[0].Key, doc[0].Key
	session := mongoSession(doc)
	check := append(bson.D(nil), doc...)
	var oldIDs []int64
	var expectedNS string
	metadataCursor := false
	if command == "getMore" || command == "killCursors" {
		db, _ := mongoGet(doc, "$db").(string)
		collection, _ := doc[0].Value.(string)
		if command == "getMore" {
			collection, _ = mongoGet(doc, "collection").(string)
			id, valid := mongoAuthNumber(doc[0].Value)
			if !valid {
				return denied, nil
			}
			oldIDs = []int64{id}
		} else {
			ids, valid := mongoGet(doc, "cursors").(bson.A)
			if !valid || len(ids) == 0 {
				return denied, nil
			}
			for _, v := range ids {
				id, valid := mongoAuthNumber(v)
				if !valid {
					return denied, nil
				}
				oldIDs = append(oldIDs, id)
			}
		}
		expectedNS = db + "." + collection
		for _, id := range oldIDs {
			owner, valid := s.cursor(scope, id, expectedNS, session)
			if !valid {
				return denied, nil
			}
			if command == "getMore" {
				publicCommand = owner.command
			}
			metadataCursor = metadataCursor || owner.command == "listCollections" || owner.command == "listIndexes"
		}
		// Only registry-authorized metadata cursors may use a $cmd namespace.
		// Validate every other field with the ordinary business-command policy.
		if metadataCursor {
			for i := range check {
				if check[i].Key == "collection" || (i == 0 && command == "killCursors") {
					check[i].Value = "vaulty_metadata"
				}
			}
		}
	}
	if mongoCheckCommand(check) != nil {
		return denied, nil
	}
	if !metadataCursor {
		for _, ns := range mongoCommandNamespaces(doc) {
			allowed, err := backend.collectionAllowed(ns)
			if err != nil {
				return nil, err
			}
			if !allowed {
				return denied, nil
			}
		}
	}
	forward := append(bson.D(nil), doc...)
	// nameOnly replies omit collection type, which is needed to filter views.
	if command == "listCollections" {
		originalFilter := bson.D{}
		filtered := forward[:0]
		for i := range forward {
			if forward[i].Key == "nameOnly" {
				forward[i].Value = false
			}
			if forward[i].Key == "filter" {
				originalFilter, _ = mongoAsDoc(forward[i].Value)
				continue
			}
			filtered = append(filtered, forward[i])
		}
		visible := bson.D{{Key: "type", Value: "collection"}, {Key: "name", Value: bson.D{{Key: "$not", Value: bson.Regex{Pattern: "^system\\.", Options: "i"}}}}}
		forward = append(filtered, bson.E{Key: "filter", Value: bson.D{{Key: "$and", Value: bson.A{originalFilter, visible}}}})
	}
	reply, err := backend.command(forward)
	if err != nil {
		return nil, err
	}
	if code, _ := mongoAuthNumber(mongoGet(reply, "code")); !mongoCommandOK(reply) && code == 43 {
		for _, id := range oldIDs {
			s.forget(scope, id)
		}
	}
	if cursor, valid := mongoAsDoc(mongoGet(reply, "cursor")); valid && mongoCommandOK(reply) {
		id, validID := mongoAuthNumber(mongoGet(cursor, "id"))
		ns, validNS := mongoGet(cursor, "ns").(string)
		if expectedNS == "" {
			db, _ := mongoGet(doc, "$db").(string)
			collection, _ := doc[0].Value.(string)
			if command == "listCollections" {
				collection = "$cmd.listCollections"
			}
			expectedNS = db + "." + collection
			if command == "listIndexes" && ns == db+".$cmd.listIndexes."+collection {
				expectedNS = ns
			}
		}
		if !validID || !validNS || ns != expectedNS {
			return nil, errors.New("invalid MongoDB cursor response")
		}
		if id != 0 && !s.remember(scope, id, mongoCursor{namespace: ns, command: publicCommand, session: session}) {
			return nil, errors.New("MongoDB cursor limit reached")
		}
		for _, old := range oldIDs {
			if old != id {
				s.forget(scope, old)
			}
		}
	}
	if command == "killCursors" && mongoCommandOK(reply) {
		for _, field := range []string{"cursorsKilled", "cursorsNotFound", "cursorsUnknown"} {
			ids, _ := mongoGet(reply, field).(bson.A)
			for _, v := range ids {
				id, _ := mongoAuthNumber(v)
				s.forget(scope, id)
			}
		}
	}
	if command == "endSessions" && mongoCommandOK(reply) {
		ids, _ := doc[0].Value.(bson.A)
		s.mu.Lock()
		for _, v := range ids {
			lsid, _ := mongoAsDoc(v)
			id, _ := mongoGet(lsid, "id").(bson.Binary)
			for k, cursor := range s.cursors {
				if k.scope == scope && cursor.session == string(id.Data) {
					delete(s.cursors, k)
				}
			}
		}
		s.mu.Unlock()
	}
	return mongoPublicReply(publicCommand, reply), nil
}

func (b *mongoBackend) collectionAllowed(ns mongoNamespace) (bool, error) {
	reply, err := b.command(bson.D{
		{Key: "listCollections", Value: int32(1)},
		{Key: "filter", Value: bson.D{{Key: "name", Value: ns.Collection}}},
		{Key: "cursor", Value: bson.D{{Key: "batchSize", Value: int32(2)}}},
		{Key: "$db", Value: ns.Database},
	})
	if err != nil {
		return false, err
	}
	if !mongoCommandOK(reply) {
		return false, nil
	}
	cursor, valid := mongoAsDoc(mongoGet(reply, "cursor"))
	batch, batchOK := mongoGet(cursor, "firstBatch").(bson.A)
	id, idOK := mongoAuthNumber(mongoGet(cursor, "id"))
	if !valid || !batchOK || !idOK || id != 0 {
		return false, errors.New("invalid MongoDB collection metadata")
	}
	for _, item := range batch {
		d, valid := mongoAsDoc(item)
		if !valid || mongoGet(d, "name") != ns.Collection || mongoGet(d, "type") != "collection" {
			return false, nil
		}
	}
	// Missing collections are safe to read or create through ordinary CRUD.
	// Trusted administrators remain responsible for concurrent DDL/view changes.
	return !strings.HasPrefix(ns.Collection, "system."), nil
}
