package dbproxy

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"io"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func mongoTestDoc(t *testing.T, d bson.D) []byte {
	t.Helper()
	b, err := bson.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mongoTestFrame(op int32, payload []byte) []byte {
	b := make([]byte, 16, 16+len(payload))
	binary.LittleEndian.PutUint32(b, uint32(16+len(payload)))
	binary.LittleEndian.PutUint32(b[4:], 42)
	binary.LittleEndian.PutUint32(b[12:], uint32(op))
	return append(b, payload...)
}

func mongoTestSequence(t *testing.T, name string, docs ...bson.D) []byte {
	b := append(make([]byte, 4), []byte(name)...)
	b = append(b, 0)
	for _, d := range docs {
		b = append(b, mongoTestDoc(t, d)...)
	}
	binary.LittleEndian.PutUint32(b, uint32(len(b)))
	return append([]byte{1}, b...)
}

func TestMongoWireRoundTrip(t *testing.T) {
	d := bson.D{{Key: "find", Value: "items"}, {Key: "filter", Value: bson.D{{Key: "n", Value: int64(9)}}}, {Key: "$db", Value: "test"}}
	var out bytes.Buffer
	if err := mongoWriteCommand(&out, 42, d); err != nil {
		t.Fatal(err)
	}
	m, err := mongoReadMessage(iotestReader{&out})
	if err != nil || m.ID != 42 || m.OpCode != 2013 || !reflect.DeepEqual(m.Body, d) {
		t.Fatalf("message=%#v err=%v", m, err)
	}
	if got, ok := mongoAsDoc(mongoGet(m.Body, "filter")); !ok || mongoGet(got, "n") != int64(9) {
		t.Fatal("document types lost")
	}
	if _, ok := mongoAsDoc("not a document"); ok {
		t.Fatal("accepted non-document")
	}
	if err := mongoWriteReply(&out, m, bson.D{{Key: "ok", Value: float64(1)}}); err != nil {
		t.Fatal(err)
	}
	reply, err := mongoReadMessage(&out)
	if err != nil || reply.ResponseTo != 42 || reply.ID == 0 {
		t.Fatalf("reply=%#v err=%v", reply, err)
	}
}

type iotestReader struct{ io.Reader }

func (r iotestReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.Reader.Read(p)
}

type mongoShortWriter struct{ bytes.Buffer }

func (w *mongoShortWriter) Write(p []byte) (int, error) {
	if len(p) > 3 {
		p = p[:3]
	}
	return w.Buffer.Write(p)
}

type mongoZeroWriter struct{}

func (mongoZeroWriter) Write([]byte) (int, error) { return 0, nil }

func TestMongoWireSections(t *testing.T) {
	d := bson.D{{Key: "insert", Value: "items"}, {Key: "$db", Value: "test"}}
	item := bson.D{{Key: "_id", Value: bson.NewObjectID()}, {Key: "n", Value: int64(7)}}
	payload := append([]byte{0, 0, 0, 0, 0}, mongoTestDoc(t, d)...)
	payload = append(payload, mongoTestSequence(t, "documents", item)...)
	frame := mongoTestFrame(2013, payload)
	m, err := mongoReadMessage(bytes.NewReader(frame))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(mongoGet(m.Body, "documents"), bson.A{item}) {
		t.Fatalf("sequence: %#v", m.Body)
	}
	// Sections may precede the command body without changing command-name order.
	reversed := append([]byte{0, 0, 0, 0}, mongoTestSequence(t, "documents", item)...)
	reversed = append(reversed, payload[4:4+1+len(mongoTestDoc(t, d))]...)
	if m, err = mongoReadMessage(bytes.NewReader(mongoTestFrame(2013, reversed))); err != nil || m.Body[0].Key != "insert" {
		t.Fatalf("reversed: %#v %v", m, err)
	}
	frame[16] = 1
	frame = append(frame, make([]byte, 4)...)
	binary.LittleEndian.PutUint32(frame, uint32(len(frame)))
	binary.LittleEndian.PutUint32(frame[len(frame)-4:], crc32.Checksum(frame[:len(frame)-4], crc32.MakeTable(crc32.Castagnoli)))
	if _, err = mongoReadMessage(bytes.NewReader(frame)); err != nil {
		t.Fatal(err)
	}
	frame[len(frame)-1] ^= 1
	if _, err = mongoReadMessage(bytes.NewReader(frame)); err == nil {
		t.Fatal("accepted corrupt checksum")
	}
}

func TestMongoWireRejects(t *testing.T) {
	doc := mongoTestDoc(t, bson.D{{Key: "ping", Value: int32(1)}})
	base := append([]byte{0, 0, 0, 0, 0}, doc...)
	cases := map[string][]byte{
		"invalid sequence UTF8": mongoTestFrame(2013, append(append([]byte{}, base...), mongoTestSequence(t, "\xff")...)),
		"compressed":            mongoTestFrame(2012, base), "unknown opcode": mongoTestFrame(123, base),
		"missing body":        mongoTestFrame(2013, []byte{0, 0, 0, 0}),
		"two bodies":          mongoTestFrame(2013, append(append(append([]byte{}, base...), 0), doc...)),
		"duplicate keys":      mongoTestFrame(2013, append([]byte{0, 0, 0, 0, 0}, mongoTestDoc(t, bson.D{{Key: "ping", Value: 1}, {Key: "ping", Value: 2}})...)),
		"collision":           mongoTestFrame(2013, append(append([]byte{}, base...), mongoTestSequence(t, "ping")...)),
		"duplicate sequences": mongoTestFrame(2013, append(append(append([]byte{}, base...), mongoTestSequence(t, "docs")...), mongoTestSequence(t, "docs")...)),
		"unknown section":     mongoTestFrame(2013, append(append([]byte{}, base...), 2)),
		"bad BSON":            mongoTestFrame(2013, []byte{0, 0, 0, 0, 0, 5, 0, 0, 0, 1}),
		"bad sequence length": mongoTestFrame(2013, append(append([]byte{}, base...), 1, 255, 255, 255, 127)),
	}
	for _, flag := range []uint32{2, 4, 65536, 1 << 20} {
		p := append([]byte{}, base...)
		binary.LittleEndian.PutUint32(p, flag)
		cases["flag"+string(rune(flag))] = mongoTestFrame(2013, p)
	}
	for i := 0; i < len(mongoTestFrame(2013, base)); i++ {
		cases["truncated"+string(rune(i))] = mongoTestFrame(2013, base)[:i]
	}
	for _, n := range []uint32{0, 15, 48_000_001, 0xffffffff} {
		p := make([]byte, 16)
		binary.LittleEndian.PutUint32(p, n)
		cases["size"+string(rune(n))] = p
	}
	for name, frame := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := mongoReadMessage(bytes.NewReader(frame)); err == nil {
				t.Fatal("accepted invalid frame")
			}
		})
	}
}

func TestMongoWireMoreBoundaries(t *testing.T) {
	doc := mongoTestDoc(t, bson.D{{Key: "hello", Value: int32(1)}})
	for _, tc := range []struct {
		name, namespace    string
		flags, skip, count uint32
		extra              []byte
	}{
		{name: "namespace", namespace: "business.$cmd", count: 1},
		{name: "skip", namespace: "admin.$cmd", skip: 1, count: 1},
		{name: "count", namespace: "admin.$cmd", count: 2},
		{name: "negative count", namespace: "admin.$cmd", count: 0xfffffffe},
		{name: "projection", namespace: "admin.$cmd", count: 1, extra: doc},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := make([]byte, 4)
			binary.LittleEndian.PutUint32(p, tc.flags)
			p = append(p, []byte(tc.namespace)...)
			p = append(p, 0)
			numbers := make([]byte, 8)
			binary.LittleEndian.PutUint32(numbers, tc.skip)
			binary.LittleEndian.PutUint32(numbers[4:], tc.count)
			p = append(p, numbers...)
			p = append(p, doc...)
			p = append(p, tc.extra...)
			if _, err := mongoReadMessage(bytes.NewReader(mongoTestFrame(2004, p))); err == nil {
				t.Fatal("accepted invalid query")
			}
		})
	}
	var out bytes.Buffer
	if err := mongoWriteCommand(&out, 1, bson.D{{Key: "oversize", Value: strings.Repeat("x", mongoMaxMessage)}}); err == nil || out.Len() != 0 {
		t.Fatal("wrote oversized command")
	}
	if err := mongoWriteReply(&out, mongoMessage{OpCode: 2004}, bson.D{{Key: "oversize", Value: strings.Repeat("x", mongoMaxMessage)}}); err == nil || out.Len() != 0 {
		t.Fatal("wrote oversized reply")
	}
	if err := mongoWriteCommand(&out, 1, bson.D{{Key: "invalid", Value: make(chan int)}}); err == nil {
		t.Fatal("accepted unencodable body")
	}
	if err := mongoWriteReply(&out, mongoMessage{OpCode: 2012}, bson.D{}); err == nil {
		t.Fatal("replied to compressed request")
	}
}

func TestMongoWireLegacyAndWrites(t *testing.T) {
	for _, name := range []string{"hello", "isMaster", "ismaster"} {
		p := append([]byte{4, 0, 0, 0}, []byte("admin.$cmd\x00")...)
		p = append(p, 0, 0, 0, 0, 255, 255, 255, 255)
		p = append(p, mongoTestDoc(t, bson.D{{Key: name, Value: int32(1)}})...)
		m, err := mongoReadMessage(bytes.NewReader(mongoTestFrame(2004, p)))
		if err != nil {
			t.Fatal(err)
		}
		var w mongoShortWriter
		if err = mongoWriteReply(&w, m, bson.D{{Key: "ok", Value: float64(1)}}); err != nil {
			t.Fatal(err)
		}
		b := w.Bytes()
		if int(binary.LittleEndian.Uint32(b)) != len(b) || binary.LittleEndian.Uint32(b[8:]) != 42 || binary.LittleEndian.Uint32(b[12:]) != 1 || binary.LittleEndian.Uint32(b[32:]) != 1 {
			t.Fatal("bad legacy reply")
		}
		for _, bad := range [][]byte{append(append([]byte{}, p...), 0), append([]byte{1}, p[1:]...)} {
			if _, err = mongoReadMessage(bytes.NewReader(mongoTestFrame(2004, bad))); err == nil {
				t.Fatal("accepted invalid legacy query")
			}
		}
	}
	p := append([]byte{0, 0, 0, 0}, []byte("admin.$cmd\x00")...)
	p = append(p, 0, 0, 0, 0, 1, 0, 0, 0)
	p = append(p, mongoTestDoc(t, bson.D{{Key: "find", Value: "items"}})...)
	if _, err := mongoReadMessage(bytes.NewReader(mongoTestFrame(2004, p))); err == nil {
		t.Fatal("accepted legacy non-handshake")
	}
	if err := mongoWriteCommand(mongoZeroWriter{}, 1, bson.D{{Key: "ping", Value: 1}}); err == nil {
		t.Fatal("accepted zero write")
	}
}

// Build modest-depth raw fixtures without involving the recursive BSON encoder.
func mongoTestNestedRaw(depth int, kind byte) []byte {
	d := []byte{5, 0, 0, 0, 0}
	for i := 1; i < depth; i++ {
		value := d
		if kind == byte(bson.TypeCodeWithScope) {
			value = append([]byte{0, 0, 0, 0, 2, 0, 0, 0, 'x', 0}, d...)
			binary.LittleEndian.PutUint32(value, uint32(len(value)))
		}
		outer := []byte{0, 0, 0, 0, kind, '0', 0}
		outer = append(outer, value...)
		outer = append(outer, 0)
		binary.LittleEndian.PutUint32(outer, uint32(len(outer)))
		d = outer
	}
	return d
}

func TestMongoWireDepth(t *testing.T) {
	for _, kind := range []byte{byte(bson.TypeEmbeddedDocument), byte(bson.TypeArray), byte(bson.TypeCodeWithScope)} {
		for _, depth := range []int{100, 101, 140} {
			t.Run(strconv.Itoa(int(kind))+"/"+strconv.Itoa(depth), func(t *testing.T) {
				d := mongoTestNestedRaw(depth, kind)
				bodyFrame := mongoTestFrame(2013, append([]byte{0, 0, 0, 0, 0}, d...))
				control := mongoTestDoc(t, bson.D{{Key: "insert", Value: "items"}})
				sequence := append([]byte{1, 0, 0, 0, 0}, []byte("documents\x00")...)
				sequence = append(sequence, d...)
				binary.LittleEndian.PutUint32(sequence[1:], uint32(len(sequence)-1))
				mixed := append(append([]byte{0, 0, 0, 0, 0}, control...), sequence...)
				reversed := append(append([]byte{0, 0, 0, 0}, sequence...), append([]byte{0}, control...)...)
				deepControl := append(append([]byte{0, 0, 0, 0, 0}, d...), mongoTestSequence(t, "documents", bson.D{})...)
				hello := mongoTestDoc(t, bson.D{{Key: "hello", Value: int32(1)}})
				legacyDoc := append(append(append([]byte{}, d[:4]...), hello[4:len(hello)-1]...), d[4:]...)
				binary.LittleEndian.PutUint32(legacyDoc, uint32(len(legacyDoc)))
				legacy := append([]byte{0, 0, 0, 0}, []byte("admin.$cmd\x00")...)
				legacy = append(legacy, 0, 0, 0, 0, 1, 0, 0, 0)
				legacy = append(legacy, legacyDoc...)
				for _, frame := range [][]byte{bodyFrame, mongoTestFrame(2013, mixed), mongoTestFrame(2013, reversed), mongoTestFrame(2013, deepControl), mongoTestFrame(2004, legacy)} {
					for _, limit := range []int{1 << 20, mongoMaxMessage} {
						_, err := mongoReadMessageLimit(bytes.NewReader(frame), limit)
						if depth <= 100 && err != nil {
							t.Fatalf("valid depth %d: %v", depth, err)
						}
						if depth > 100 && err == nil {
							t.Fatalf("accepted depth %d", depth)
						}
					}
				}
			})
		}
	}
}

func TestMongoWireNestedStructure(t *testing.T) {
	for _, kind := range []byte{byte(bson.TypeEmbeddedDocument), byte(bson.TypeArray), byte(bson.TypeCodeWithScope)} {
		base := mongoTestNestedRaw(2, kind)
		childOffset := 7
		if kind == byte(bson.TypeCodeWithScope) {
			childOffset += 10
		}
		for _, corruption := range []string{"short length", "long length", "negative length", "terminator", "early terminator"} {
			t.Run(strconv.Itoa(int(kind))+"/"+corruption, func(t *testing.T) {
				d := append([]byte(nil), base...)
				switch corruption {
				case "short length":
					binary.LittleEndian.PutUint32(d[childOffset:], 4)
				case "long length":
					binary.LittleEndian.PutUint32(d[childOffset:], 6)
				case "negative length":
					binary.LittleEndian.PutUint32(d[childOffset:], 0xffffffff)
				case "terminator":
					d[len(d)-2] = 1
				case "early terminator":
					d[4] = 0
				}
				if _, err := mongoReadMessage(bytes.NewReader(mongoTestFrame(2013, append([]byte{0, 0, 0, 0, 0}, d...)))); err == nil {
					t.Fatal("accepted malformed nested BSON")
				}
			})
		}
	}
	for _, corruption := range []string{"code terminator", "zero code length", "long code length", "short total", "long total", "scope trailing byte"} {
		t.Run(corruption, func(t *testing.T) {
			d := mongoTestNestedRaw(2, byte(bson.TypeCodeWithScope))
			switch corruption {
			case "code terminator":
				d[16] = 1
			case "zero code length":
				binary.LittleEndian.PutUint32(d[11:], 0)
			case "long code length":
				binary.LittleEndian.PutUint32(d[11:], 0x7fffffff)
			case "short total":
				binary.LittleEndian.PutUint32(d[7:], 14)
			case "long total":
				binary.LittleEndian.PutUint32(d[7:], 16)
			case "scope trailing byte":
				d = append(d, 0)
				binary.LittleEndian.PutUint32(d, uint32(len(d)))
				binary.LittleEndian.PutUint32(d[7:], 16)
			}
			if _, err := mongoReadMessage(bytes.NewReader(mongoTestFrame(2013, append([]byte{0, 0, 0, 0, 0}, d...)))); err == nil {
				t.Fatal("accepted malformed CodeWithScope")
			}
		})
	}
}

type mongoHeaderOnlyReader struct {
	header    []byte
	bodyReads int
}

func (r *mongoHeaderOnlyReader) Read(p []byte) (int, error) {
	if len(r.header) == 0 {
		r.bodyReads++
		return 0, io.EOF
	}
	n := copy(p, r.header)
	r.header = r.header[n:]
	return n, nil
}

func TestMongoWireReadLimit(t *testing.T) {
	d := mongoTestDoc(t, bson.D{{Key: "hello", Value: int32(1)}})
	frame := mongoTestFrame(2013, append([]byte{0, 0, 0, 0, 0}, d...))
	if _, err := mongoReadMessageLimit(iotestReader{bytes.NewReader(frame)}, len(frame)); err != nil {
		t.Fatal(err)
	}
	if _, err := mongoReadMessageLimit(bytes.NewReader(frame), len(frame)-1); err == nil {
		t.Fatal("accepted message above caller limit")
	}
	for _, limit := range []int{0, -1, mongoMaxMessage + 1} {
		r := bytes.NewReader(frame)
		if _, err := mongoReadMessageLimit(r, limit); err == nil || r.Len() != len(frame) {
			t.Fatal("invalid limit must fail without reading")
		}
	}
	for _, size := range []uint32{(1 << 20) + 1, mongoMaxMessage, mongoMaxMessage + 1, 0xffffffff} {
		header := append([]byte(nil), frame[:16]...)
		binary.LittleEndian.PutUint32(header, size)
		r := &mongoHeaderOnlyReader{header: header}
		if _, err := mongoReadMessageLimit(r, 1<<20); err == nil || r.bodyReads != 0 {
			t.Fatal("must reject size from header before reading body")
		}
	}
	large := mongoTestFrame(2013, append([]byte{0, 0, 0, 0, 0}, mongoTestDoc(t, bson.D{{Key: "blob", Value: strings.Repeat("x", 1<<20)}})...))
	if _, err := mongoReadMessage(bytes.NewReader(large)); err != nil {
		t.Fatal("default reader must retain 48 MB limit")
	}
	if _, err := mongoReadMessageLimit(bytes.NewReader(large), 1<<20); err == nil {
		t.Fatal("unauthenticated reader accepted large message")
	}
	for _, depth := range []int{100, 101} {
		deep := mongoTestFrame(2013, append([]byte{0, 0, 0, 0, 0}, mongoTestNestedRaw(depth, byte(bson.TypeArray))...))
		_, err := mongoReadMessageLimit(bytes.NewReader(deep), 1<<20)
		if (err == nil) != (depth == 100) {
			t.Fatalf("limited reader depth %d: %v", depth, err)
		}
	}
}

func TestMongoWireBulkSequences(t *testing.T) {
	for _, tc := range []struct{ command, field string }{
		{"insert", "documents"}, {"update", "updates"}, {"delete", "deletes"},
	} {
		t.Run(tc.command, func(t *testing.T) {
			docs := bson.A{
				bson.D{{Key: "q", Value: bson.D{{Key: "_id", Value: int64(2)}}}, {Key: "value", Value: bson.Binary{Subtype: 0, Data: []byte{1, 2, 3}}}},
				bson.D{{Key: "q", Value: bson.D{{Key: "_id", Value: int64(1)}}}, {Key: "value", Value: bson.A{int32(4), "last"}}},
			}
			body := bson.D{{Key: tc.command, Value: "items"}, {Key: tc.field, Value: docs}, {Key: "ordered", Value: false}, {Key: "$db", Value: "test"}}
			original := append(bson.D(nil), body...)
			var out mongoShortWriter
			if err := mongoWriteCommand(&out, 73, body); err != nil {
				t.Fatal(err)
			}
			frame := out.Bytes()
			if binary.LittleEndian.Uint32(frame[4:]) != 73 || binary.LittleEndian.Uint32(frame[8:]) != 0 || binary.LittleEndian.Uint32(frame[16:]) != 0 || frame[20] != 0 {
				t.Fatal("invalid bulk command header")
			}
			controlLength := int(binary.LittleEndian.Uint32(frame[21:]))
			control := bson.Raw(frame[21 : 21+controlLength])
			if _, err := control.LookupErr(tc.field); err == nil {
				t.Fatal("bulk array remains in type 0 body")
			}
			sequence := frame[21+controlLength:]
			if len(sequence) < 5 || sequence[0] != 1 || int(binary.LittleEndian.Uint32(sequence[1:])) != len(sequence)-1 || !bytes.HasPrefix(sequence[5:], []byte(tc.field+"\x00")) {
				t.Fatal("missing type 1 document sequence")
			}
			m, err := mongoReadMessage(bytes.NewReader(frame))
			want := bson.D{body[0], body[2], body[3], body[1]}
			if err != nil || !reflect.DeepEqual(m.Body, want) {
				t.Fatalf("bulk order or BSON values lost: %v", err)
			}
			if !reflect.DeepEqual(body, original) {
				t.Fatal("writer mutated input command")
			}
			if err := mongoWriteCommand(mongoZeroWriter{}, 73, body); err == nil {
				t.Fatal("accepted zero-length bulk write")
			}
		})
	}
	// The same field name on an unrelated command must remain ordinary BSON.
	other := bson.D{{Key: "custom", Value: 1}, {Key: "documents", Value: bson.A{bson.D{{Key: "n", Value: 1}}}}}
	var out bytes.Buffer
	if err := mongoWriteCommand(&out, 1, other); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 21+int(binary.LittleEndian.Uint32(out.Bytes()[21:])) {
		t.Fatal("rewrote unrelated command as type 1")
	}
}

func TestMongoWireBulkLarge(t *testing.T) {
	docs := bson.A{
		bson.D{{Key: "_id", Value: int64(2)}, {Key: "blob", Value: strings.Repeat("a", 9<<20)}},
		bson.D{{Key: "_id", Value: int64(1)}, {Key: "blob", Value: strings.Repeat("b", 9<<20)}},
	}
	body := bson.D{{Key: "insert", Value: "items"}, {Key: "documents", Value: docs}, {Key: "ordered", Value: true}, {Key: "$db", Value: "test"}}
	var out bytes.Buffer
	if err := mongoWriteCommand(&out, 81, body); err != nil {
		t.Fatal(err)
	}
	frame := out.Bytes()
	controlLength := int(binary.LittleEndian.Uint32(frame[21:]))
	if len(frame) <= 16<<20 || controlLength > 16<<20 || frame[21+controlLength] != 1 {
		t.Fatal("large batch encoded as oversized type 0 BSON")
	}
	m, err := mongoReadMessage(bytes.NewReader(frame))
	if err != nil || !reflect.DeepEqual(mongoGet(m.Body, "documents"), docs) || mongoGet(m.Body, "ordered") != true {
		t.Fatalf("large batch roundtrip: %v", err)
	}
	if _, err := mongoReadMessageLimit(bytes.NewReader(frame), 1<<20); err == nil {
		t.Fatal("type 1 message bypassed caller limit")
	}
}

func TestMongoWireBulkRejects(t *testing.T) {
	for _, tc := range []struct{ command, field string }{
		{"insert", "documents"}, {"update", "updates"}, {"delete", "deletes"},
	} {
		for i, value := range []any{nil, "not-array", bson.D{}, bson.A{nil}, bson.A{int32(1)}, bson.A{bson.A{}}, bson.A{bson.D{}, false}} {
			t.Run(tc.command+"/"+strconv.Itoa(i), func(t *testing.T) {
				var out bytes.Buffer
				body := bson.D{{Key: tc.command, Value: "items"}, {Key: tc.field, Value: value}}
				if err := mongoWriteCommand(&out, 1, body); err == nil || out.Len() != 0 {
					t.Fatal("wrote invalid bulk array")
				}
			})
		}
		for _, body := range []bson.D{
			{{Key: tc.command, Value: "items"}},
			{{Key: tc.command, Value: "items"}, {Key: tc.field, Value: bson.A{}}, {Key: tc.field, Value: bson.A{}}},
		} {
			var out bytes.Buffer
			if err := mongoWriteCommand(&out, 1, body); err == nil || out.Len() != 0 {
				t.Errorf("accepted absent or duplicate %s field", tc.field)
			}
		}
	}
	for _, size := range []int{16 << 20, (16 << 20) + 1} {
		doc := bson.D{{Key: "blob", Value: strings.Repeat("x", size-len(mongoTestDoc(t, bson.D{{Key: "blob", Value: ""}})))}}
		var out bytes.Buffer
		err := mongoWriteCommand(&out, 1, bson.D{{Key: "insert", Value: "items"}, {Key: "documents", Value: bson.A{doc}}})
		if size == 16<<20 && err != nil {
			t.Fatalf("rejected exact 16 MiB document: %v", err)
		}
		if size > 16<<20 && (err == nil || out.Len() != 0) {
			t.Error("wrote individual BSON document above 16 MiB")
		}
	}
	var out bytes.Buffer
	control := bson.D{{Key: "insert", Value: "items"}, {Key: "documents", Value: bson.A{}}, {Key: "comment", Value: strings.Repeat("x", 16<<20)}}
	if err := mongoWriteCommand(&out, 1, control); err == nil || out.Len() != 0 {
		t.Error("wrote oversized bulk control body")
	}
	out.Reset()
	doc := bson.D{{Key: "blob", Value: strings.Repeat("x", 8<<20)}}
	batch := bson.A{doc, doc, doc, doc, doc, doc}
	if err := mongoWriteCommand(&out, 1, bson.D{{Key: "insert", Value: "items"}, {Key: "documents", Value: batch}}); err == nil || out.Len() != 0 {
		t.Fatal("wrote batch above 48 MB")
	}
}
