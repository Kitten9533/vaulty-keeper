package dbproxy

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"go.mongodb.org/mongo-driver/v2/bson"
)

const mongoMaxMessage = 48_000_000

type mongoMessage struct {
	ID         int32
	ResponseTo int32
	OpCode     int32
	Flags      uint32
	Body       bson.D
}

var mongoReplyID atomic.Int32
var errMongoWire = errors.New("invalid MongoDB message")

func mongoGet(d bson.D, key string) any {
	for _, e := range d {
		if e.Key == key {
			return e.Value
		}
	}
	return nil
}

func mongoAsDoc(v any) (bson.D, bool) {
	d, ok := v.(bson.D)
	return d, ok
}

func mongoValidateBSON(d bson.Raw, depth int) error {
	if depth > 100 || len(d) < 5 || int(binary.LittleEndian.Uint32(d)) != len(d) || d[len(d)-1] != 0 {
		return errMongoWire
	}
	// In driver v2.9.0 Elements/ValueErr only inspect the current layer.
	// Bound every document, array and scope before allowing recursive decoding.
	elements, err := d.Elements()
	if err != nil {
		return errMongoWire
	}
	used := 5
	for _, element := range elements {
		used += len(element)
		value, err := element.ValueErr()
		if err != nil {
			return errMongoWire
		}
		switch value.Type {
		case bson.TypeEmbeddedDocument, bson.TypeArray:
			if mongoValidateBSON(bson.Raw(value.Value), depth+1) != nil {
				return errMongoWire
			}
		case bson.TypeCodeWithScope:
			v := value.Value
			if len(v) < 14 {
				return errMongoWire
			}
			codeLength := int(binary.LittleEndian.Uint32(v[4:]))
			if codeLength < 1 || codeLength > len(v)-8 || v[8+codeLength-1] != 0 {
				return errMongoWire
			}
			_, scope, ok := value.CodeWithScopeOK()
			if !ok || 8+codeLength+len(scope) != len(v) || mongoValidateBSON(scope, depth+1) != nil {
				return errMongoWire
			}
		}
	}
	if used != len(d) {
		return errMongoWire
	}
	return nil
}

func mongoReadDocument(p []byte) (bson.D, int, error) {
	if len(p) < 5 {
		return nil, 0, errMongoWire
	}
	n := int(binary.LittleEndian.Uint32(p))
	if n < 5 || n > len(p) || mongoValidateBSON(bson.Raw(p[:n]), 1) != nil {
		return nil, 0, errMongoWire
	}
	var d bson.D
	if bson.Unmarshal(p[:n], &d) != nil {
		return nil, 0, errMongoWire
	}
	seen := make(map[string]bool, len(d))
	for _, e := range d {
		if seen[e.Key] {
			return nil, 0, errMongoWire
		}
		seen[e.Key] = true
	}
	return d, n, nil
}

func mongoReadMessage(r io.Reader) (mongoMessage, error) {
	return mongoReadMessageLimit(r, mongoMaxMessage)
}

func mongoReadMessageLimit(r io.Reader, limit int) (mongoMessage, error) {
	var m mongoMessage
	if limit <= 0 || limit > mongoMaxMessage {
		return m, errMongoWire
	}
	header := make([]byte, 16)
	if _, err := io.ReadFull(r, header); err != nil {
		return m, errMongoWire
	}
	n := int(binary.LittleEndian.Uint32(header))
	if n < 16 || n > limit {
		return m, errMongoWire
	}
	m.ID = int32(binary.LittleEndian.Uint32(header[4:]))
	m.ResponseTo = int32(binary.LittleEndian.Uint32(header[8:]))
	m.OpCode = int32(binary.LittleEndian.Uint32(header[12:]))
	if m.OpCode != 2013 && m.OpCode != 2004 {
		return m, errMongoWire
	}
	p := make([]byte, n-16)
	if _, err := io.ReadFull(r, p); err != nil || len(p) < 4 {
		return m, errMongoWire
	}
	m.Flags = binary.LittleEndian.Uint32(p)
	if m.OpCode == 2004 {
		if m.Flags != 0 && m.Flags != 4 {
			return m, errMongoWire
		}
		p = p[4:]
		end := bytes.IndexByte(p, 0)
		if end < 0 || string(p[:end]) != "admin.$cmd" {
			return m, errMongoWire
		}
		p = p[end+1:]
		if len(p) < 8 || binary.LittleEndian.Uint32(p) != 0 {
			return m, errMongoWire
		}
		count := int32(binary.LittleEndian.Uint32(p[4:]))
		if count != -1 && count != 1 {
			return m, errMongoWire
		}
		d, used, err := mongoReadDocument(p[8:])
		if err != nil || used != len(p)-8 || len(d) == 0 {
			return m, errMongoWire
		}
		if d[0].Key != "hello" && d[0].Key != "isMaster" && d[0].Key != "ismaster" {
			return m, errMongoWire
		}
		m.Body = d
		return m, nil
	}
	// Reject fire-and-forget, exhaust and unknown flags before any forwarding.
	if m.Flags & ^uint32(1) != 0 {
		return m, errMongoWire
	}
	if m.Flags&1 != 0 {
		if len(p) < 8 {
			return m, errMongoWire
		}
		h := crc32.New(crc32.MakeTable(crc32.Castagnoli))
		_, _ = h.Write(header)
		_, _ = h.Write(p[:len(p)-4])
		if h.Sum32() != binary.LittleEndian.Uint32(p[len(p)-4:]) {
			return m, errMongoWire
		}
		p = p[:len(p)-4]
	}
	p = p[4:]
	var sequences bson.D
	bodyFound := false
	for len(p) > 0 {
		kind := p[0]
		p = p[1:]
		switch kind {
		case 0:
			if bodyFound {
				return m, errMongoWire
			}
			d, used, err := mongoReadDocument(p)
			if err != nil {
				return m, errMongoWire
			}
			m.Body, p, bodyFound = d, p[used:], true
		case 1:
			if len(p) < 5 {
				return m, errMongoWire
			}
			size := int(binary.LittleEndian.Uint32(p))
			if size < 5 || size > len(p) {
				return m, errMongoWire
			}
			section := p[4:size]
			end := bytes.IndexByte(section, 0)
			if end <= 0 {
				return m, errMongoWire
			}
			name := string(section[:end])
			// Only top-level arrays are supported; dotted paths are ambiguous to policy.
			if !utf8.ValidString(name) || strings.Contains(name, ".") {
				return m, errMongoWire
			}
			section = section[end+1:]
			docs := bson.A{}
			for len(section) > 0 {
				d, used, err := mongoReadDocument(section)
				if err != nil {
					return m, errMongoWire
				}
				docs = append(docs, d)
				section = section[used:]
			}
			sequences = append(sequences, bson.E{Key: name, Value: docs})
			p = p[size:]
		default:
			return m, errMongoWire
		}
	}
	if !bodyFound {
		return m, errMongoWire
	}
	seen := make(map[string]bool, len(m.Body)+len(sequences))
	for _, e := range m.Body {
		seen[e.Key] = true
	}
	for _, e := range sequences {
		if seen[e.Key] {
			return m, errMongoWire
		}
		seen[e.Key] = true
		m.Body = append(m.Body, e)
	}
	return m, nil
}

func mongoWriteReply(w io.Writer, req mongoMessage, body bson.D) error {
	if req.OpCode != 2004 && req.OpCode != 2013 {
		return errMongoWire
	}
	return mongoWriteMessage(w, mongoReplyID.Add(1), req.ID, req.OpCode == 2004, body, nil)
}

func mongoWriteCommand(w io.Writer, id int32, body bson.D) error {
	var field string
	if len(body) > 0 {
		switch body[0].Key {
		case "insert":
			field = "documents"
		case "update":
			field = "updates"
		case "delete":
			field = "deletes"
		}
	}
	if field == "" {
		return mongoWriteMessage(w, id, 0, false, body, nil)
	}
	control := make(bson.D, 0, len(body)-1)
	var docs bson.A
	found := false
	for _, element := range body {
		if element.Key != field {
			control = append(control, element)
			continue
		}
		var ok bool
		docs, ok = element.Value.(bson.A)
		if found || !ok {
			return errMongoWire
		}
		found = true
	}
	if !found {
		return errMongoWire
	}
	// Keep the batch in one message without making its array a single BSON document.
	sequence := append([]byte{1, 0, 0, 0, 0}, field...)
	sequence = append(sequence, 0)
	for _, item := range docs {
		doc, ok := mongoAsDoc(item)
		if !ok {
			return errMongoWire
		}
		encoded, err := bson.Marshal(doc)
		if err != nil || len(encoded) > 16<<20 || len(encoded) > mongoMaxMessage-21-len(sequence) || mongoValidateBSON(bson.Raw(encoded), 1) != nil {
			return errMongoWire
		}
		sequence = append(sequence, encoded...)
	}
	binary.LittleEndian.PutUint32(sequence[1:], uint32(len(sequence)-1))
	return mongoWriteMessage(w, id, 0, false, control, sequence)
}

func mongoWriteMessage(w io.Writer, id, responseTo int32, legacy bool, body bson.D, sequence []byte) error {
	d, err := bson.Marshal(body)
	if err != nil || (sequence != nil && len(d) > 16<<20) {
		return errMongoWire
	}
	prefix, op := 21, uint32(2013)
	if legacy {
		prefix, op = 36, 1
	}
	if len(d) > mongoMaxMessage-prefix-len(sequence) {
		return errMongoWire
	}
	p := make([]byte, prefix, prefix+len(d)+len(sequence))
	binary.LittleEndian.PutUint32(p, uint32(prefix+len(d)+len(sequence)))
	binary.LittleEndian.PutUint32(p[4:], uint32(id))
	binary.LittleEndian.PutUint32(p[8:], uint32(responseTo))
	binary.LittleEndian.PutUint32(p[12:], op)
	if legacy {
		binary.LittleEndian.PutUint32(p[32:], 1)
	}
	p = append(p, d...)
	p = append(p, sequence...)
	for len(p) > 0 {
		n, err := w.Write(p)
		if err != nil || n <= 0 || n > len(p) {
			return errors.New("MongoDB write failed")
		}
		p = p[n:]
	}
	return nil
}
