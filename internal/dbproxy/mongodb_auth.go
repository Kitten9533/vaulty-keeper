package dbproxy

import (
	"crypto/md5"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"time"

	"github.com/xdg-go/scram"
	"github.com/xdg-go/stringprep"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type mongoBackend struct {
	conn   net.Conn
	hello  bson.D
	config mongoConfig
	nextID int32
}

func mongoDial(u *url.URL) (*mongoBackend, error) {
	cfg, err := mongoConfigFromURL(u)
	if err != nil {
		return nil, err
	}
	failure := errors.New("MongoDB backend connection failed")
	var tlsConfig *tls.Config
	if cfg.TLS {
		host, _, _ := net.SplitHostPort(cfg.Address)
		tlsConfig = &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
		if cfg.CAFile != "" {
			f, err := os.Open(cfg.CAFile)
			if err != nil {
				return nil, failure
			}
			info, err := f.Stat()
			if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
				_ = f.Close()
				return nil, failure
			}
			data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
			_ = f.Close()
			if err != nil || len(data) > 1<<20 {
				return nil, failure
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(data) {
				return nil, failure
			}
			tlsConfig.RootCAs = pool
		}
	}
	conn, err := net.DialTimeout("tcp", cfg.Address, cfg.Timeout)
	if err != nil {
		return nil, failure
	}
	success := false
	defer func() {
		if !success {
			_ = conn.Close()
		}
	}()
	if tlsConfig != nil {
		if conn.SetDeadline(time.Now().Add(cfg.Timeout)) != nil {
			return nil, failure
		}
		secure := tls.Client(conn, tlsConfig)
		if secure.Handshake() != nil {
			return nil, failure
		}
		conn = secure
	}
	b := &mongoBackend{conn: conn, config: cfg}
	hello := bson.D{{Key: "hello", Value: int32(1)}, {Key: "helloOk", Value: true}, {Key: "$db", Value: cfg.AuthSource}}
	if cfg.AuthMechanism == "" {
		hello = append(hello, bson.E{Key: "saslSupportedMechs", Value: cfg.AuthSource + "." + cfg.Username})
	}
	b.hello, err = b.command(hello)
	if err != nil || !mongoCommandOK(b.hello) {
		return nil, failure
	}
	minWire, minOK := mongoAuthNumber(mongoGet(b.hello, "minWireVersion"))
	maxWire, maxOK := mongoAuthNumber(mongoGet(b.hello, "maxWireVersion"))
	if !minOK || !maxOK || minWire < 0 || minWire > 25 || maxWire < 25 {
		return nil, errors.New("unsupported MongoDB wire version")
	}
	if cfg.ReplicaSet != "" && mongoGet(b.hello, "setName") != cfg.ReplicaSet {
		return nil, errors.New("MongoDB replica set mismatch")
	}
	if err = b.authenticate(); err != nil {
		return nil, err
	}
	success = true
	return b, nil
}

func (b *mongoBackend) command(body bson.D) (bson.D, error) {
	failure := errors.New("MongoDB backend command failed")
	if b.conn == nil || b.config.Timeout <= 0 {
		return nil, failure
	}
	if b.conn.SetDeadline(time.Now().Add(b.config.Timeout)) != nil {
		_ = b.close()
		return nil, failure
	}
	b.nextID++
	if b.nextID == 0 {
		b.nextID++
	}
	if mongoWriteCommand(b.conn, b.nextID, body) != nil {
		_ = b.close()
		return nil, failure
	}
	m, err := mongoReadMessage(b.conn)
	if err != nil || m.OpCode != 2013 || m.ResponseTo != b.nextID {
		_ = b.close()
		return nil, failure
	}
	return m.Body, nil
}

func (b *mongoBackend) close() error {
	if b.conn == nil {
		return nil
	}
	if b.conn.Close() != nil {
		return errors.New("MongoDB backend close failed")
	}
	return nil
}

func mongoAuthNumber(v any) (int64, bool) {
	switch n := v.(type) {
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		if n >= -1<<53 && n <= 1<<53 && n == float64(int64(n)) {
			return int64(n), true
		}
	}
	return 0, false
}

func mongoCommandOK(d bson.D) bool {
	n, ok := mongoAuthNumber(mongoGet(d, "ok"))
	return ok && n == 1
}

func (b *mongoBackend) authenticate() error {
	failure := errors.New("MongoDB authentication failed")
	mechanism := b.config.AuthMechanism
	if mechanism == "" {
		value := mongoGet(b.hello, "saslSupportedMechs")
		if value == nil {
			mechanism = "SCRAM-SHA-1"
		} else {
			mechs, ok := value.(bson.A)
			if !ok {
				return failure
			}
			for _, m := range mechs {
				s, ok := m.(string)
				if !ok {
					return failure
				}
				if s == "SCRAM-SHA-256" {
					mechanism = s
				} else if s == "SCRAM-SHA-1" && mechanism == "" {
					mechanism = s
				}
			}
		}
	}
	hash := scram.SHA256
	password := b.config.Password
	switch mechanism {
	case "SCRAM-SHA-256":
		var err error
		password, err = stringprep.SASLprep.Prepare(password)
		if err != nil {
			return failure
		}
	case "SCRAM-SHA-1":
		hash = scram.SHA1
		sum := md5.Sum([]byte(b.config.Username + ":mongo:" + password))
		password = hex.EncodeToString(sum[:])
	default:
		return errors.New("unsupported MongoDB authentication mechanism")
	}
	// MongoDB normalizes the SHA-256 password only, not the username.
	client, err := hash.NewClientUnprepped(b.config.Username, password, "")
	if err != nil {
		return failure
	}
	conv := client.WithMinIterations(4096).NewConversation()
	first, err := conv.Step("")
	if err != nil {
		return failure
	}
	request := bson.D{{Key: "saslStart", Value: int32(1)}, {Key: "mechanism", Value: mechanism}, {Key: "payload", Value: bson.Binary{Subtype: 0, Data: []byte(first)}}, {Key: "autoAuthorize", Value: int32(1)}, {Key: "$db", Value: b.config.AuthSource}}
	var conversationID int32
	// A normal exchange has two replies, with at most one empty final ACK.
	for step := 0; step < 3; step++ {
		reply, err := b.command(request)
		if err != nil || !mongoCommandOK(reply) {
			return failure
		}
		id, ok := mongoGet(reply, "conversationId").(int32)
		if !ok || (step > 0 && id != conversationID) {
			return failure
		}
		conversationID = id
		done, ok := mongoGet(reply, "done").(bool)
		if !ok {
			return failure
		}
		payload, ok := mongoGet(reply, "payload").(bson.Binary)
		if !ok || payload.Subtype != 0 || len(payload.Data) > 1<<20 {
			return failure
		}
		var next string
		if conv.Done() {
			if !done || len(payload.Data) != 0 || !conv.Valid() {
				return failure
			}
		} else {
			next, err = conv.Step(string(payload.Data))
			if err != nil {
				return failure
			}
		}
		if done {
			if !conv.Done() || !conv.Valid() {
				return failure
			}
			return nil
		}
		request = bson.D{{Key: "saslContinue", Value: int32(1)}, {Key: "conversationId", Value: conversationID}, {Key: "payload", Value: bson.Binary{Subtype: 0, Data: []byte(next)}}, {Key: "$db", Value: b.config.AuthSource}}
	}
	return failure
}
