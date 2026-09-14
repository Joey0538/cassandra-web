package main

import (
	"encoding/binary"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/gocql/gocql"
)

// frame encodes one [int32 length][bytes] element.
func frame(b []byte) []byte {
	out := make([]byte, 4, 4+len(b))
	binary.BigEndian.PutUint32(out, uint32(int32(len(b))))
	return append(out, b...)
}

// nullFrame encodes an element with length -1.
func nullFrame() []byte {
	out := make([]byte, 4)
	binary.BigEndian.PutUint32(out, ^uint32(0)) // int32(-1)
	return out
}

// count encodes a collection element count.
func count(n int) []byte {
	out := make([]byte, 4)
	binary.BigEndian.PutUint32(out, uint32(int32(n)))
	return out
}

func text() gocql.NativeType {
	return gocql.NewNativeType(4, gocql.TypeVarchar, "")
}

func intType() gocql.NativeType {
	return gocql.NewNativeType(4, gocql.TypeInt, "")
}

// reactionKey mirrors chat.reaction_key: two text fields.
func reactionKey() gocql.UDTTypeInfo {
	return gocql.UDTTypeInfo{
		NativeType: gocql.NewNativeType(4, gocql.TypeUDT, ""),
		KeySpace:   "chat",
		Name:       "reaction_key",
		Elements: []gocql.UDTField{
			{Name: "emoji", Type: text()},
			{Name: "user_account", Type: text()},
		},
	}
}

func encodeInt(v int32) []byte {
	out := make([]byte, 4)
	binary.BigEndian.PutUint32(out, uint32(v))
	return out
}

func TestDecodeCQL_UDT(t *testing.T) {
	data := append(frame([]byte("thumbsup")), frame([]byte("alice"))...)

	got, err := decodeCQL(reactionKey(), data)
	if err != nil {
		t.Fatalf("decodeCQL: %v", err)
	}

	want := map[string]interface{}{"emoji": "thumbsup", "user_account": "alice"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

// A UDT value written before a field was added simply ends early.
func TestDecodeCQL_UDT_TruncatedTail(t *testing.T) {
	data := frame([]byte("thumbsup"))

	got, err := decodeCQL(reactionKey(), data)
	if err != nil {
		t.Fatalf("decodeCQL: %v", err)
	}

	want := map[string]interface{}{"emoji": "thumbsup", "user_account": nil}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestDecodeCQL_UDT_NullField(t *testing.T) {
	data := append(frame([]byte("heart")), nullFrame()...)

	got, err := decodeCQL(reactionKey(), data)
	if err != nil {
		t.Fatalf("decodeCQL: %v", err)
	}

	m := got.(map[string]interface{})
	if m["user_account"] != nil {
		t.Fatalf("null field decoded to %#v, want nil", m["user_account"])
	}
}

// The case the whole change exists for: map<frozen<udt>, frozen<udt>> is what
// gocql's own row decoding cannot build a Go type for.
func TestDecodeCQL_MapWithUDTKey(t *testing.T) {
	info := gocql.CollectionType{
		NativeType: gocql.NewNativeType(4, gocql.TypeMap, ""),
		Key:        reactionKey(),
		Elem:       text(),
	}

	key := append(frame([]byte("thumbsup")), frame([]byte("alice"))...)
	data := count(1)
	data = append(data, frame(key)...)
	data = append(data, frame([]byte("u1"))...)

	got, err := decodeCQL(info, data)
	if err != nil {
		t.Fatalf("decodeCQL: %v", err)
	}

	m, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("got %T, want map[string]interface{}", got)
	}

	const wantKey = `{"emoji":"thumbsup","user_account":"alice"}`
	if m[wantKey] != "u1" {
		t.Fatalf("got %#v, want key %s -> u1", m, wantKey)
	}

	// The rendered key must be valid JSON so a viewer can parse it back.
	var round map[string]string
	if err := json.Unmarshal([]byte(wantKey), &round); err != nil {
		t.Fatalf("rendered key is not valid JSON: %v", err)
	}
	if round["emoji"] != "thumbsup" {
		t.Fatalf("round-tripped key = %#v", round)
	}
}

func TestDecodeCQL_MapWithScalarKey(t *testing.T) {
	info := gocql.CollectionType{
		NativeType: gocql.NewNativeType(4, gocql.TypeMap, ""),
		Key:        text(),
		Elem:       intType(),
	}

	data := count(2)
	data = append(data, frame([]byte("a"))...)
	data = append(data, frame(encodeInt(1))...)
	data = append(data, frame([]byte("b"))...)
	data = append(data, frame(encodeInt(2))...)

	got, err := decodeCQL(info, data)
	if err != nil {
		t.Fatalf("decodeCQL: %v", err)
	}

	want := map[string]interface{}{"a": 1, "b": 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestDecodeCQL_ListAndSet(t *testing.T) {
	for _, typ := range []gocql.Type{gocql.TypeList, gocql.TypeSet} {
		info := gocql.CollectionType{
			NativeType: gocql.NewNativeType(4, typ, ""),
			Elem:       text(),
		}

		data := count(2)
		data = append(data, frame([]byte("x"))...)
		data = append(data, frame([]byte("y"))...)

		got, err := decodeCQL(info, data)
		if err != nil {
			t.Fatalf("%v: decodeCQL: %v", typ, err)
		}

		want := []interface{}{"x", "y"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%v: got %#v, want %#v", typ, got, want)
		}
	}
}

func TestDecodeCQL_ListOfUDT(t *testing.T) {
	info := gocql.CollectionType{
		NativeType: gocql.NewNativeType(4, gocql.TypeList, ""),
		Elem:       reactionKey(),
	}

	elem := append(frame([]byte("tada")), frame([]byte("carol"))...)
	data := append(count(1), frame(elem)...)

	got, err := decodeCQL(info, data)
	if err != nil {
		t.Fatalf("decodeCQL: %v", err)
	}

	want := []interface{}{map[string]interface{}{"emoji": "tada", "user_account": "carol"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestDecodeCQL_Tuple(t *testing.T) {
	info := gocql.TupleTypeInfo{
		NativeType: gocql.NewNativeType(4, gocql.TypeTuple, ""),
		Elems:      []gocql.TypeInfo{text(), intType()},
	}

	data := append(frame([]byte("a")), frame(encodeInt(7))...)

	got, err := decodeCQL(info, data)
	if err != nil {
		t.Fatalf("decodeCQL: %v", err)
	}

	want := []interface{}{"a", 7}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestDecodeCQL_NilAndScalar(t *testing.T) {
	got, err := decodeCQL(text(), nil)
	if err != nil {
		t.Fatalf("decodeCQL(nil): %v", err)
	}
	if got != nil {
		t.Fatalf("nil data decoded to %#v, want nil", got)
	}

	got, err = decodeCQL(text(), []byte("hello"))
	if err != nil {
		t.Fatalf("decodeCQL(text): %v", err)
	}
	if got != "hello" {
		t.Fatalf("got %#v, want \"hello\"", got)
	}
}

// Malformed input must return an error, never panic.
func TestDecodeCQL_Malformed(t *testing.T) {
	cases := []struct {
		name string
		info gocql.TypeInfo
		data []byte
	}{
		{
			name: "short collection header",
			info: gocql.CollectionType{
				NativeType: gocql.NewNativeType(4, gocql.TypeMap, ""),
				Key:        text(),
				Elem:       text(),
			},
			data: []byte{0, 1},
		},
		{
			name: "element length past end",
			info: gocql.CollectionType{
				NativeType: gocql.NewNativeType(4, gocql.TypeList, ""),
				Elem:       text(),
			},
			data: append(count(1), 0, 0, 0, 99),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panicked instead of erroring: %v", r)
				}
			}()
			if _, err := decodeCQL(tc.info, tc.data); err == nil {
				t.Fatal("expected an error, got nil")
			}
		})
	}
}

func TestMapKeyString(t *testing.T) {
	cases := []struct {
		in   interface{}
		want string
	}{
		{"plain", "plain"},
		{nil, ""},
		{42, "42"},
		{map[string]interface{}{"a": "b"}, `{"a":"b"}`},
		{[]interface{}{"a", "b"}, `["a","b"]`},
	}

	for _, tc := range cases {
		if got := mapKeyString(tc.in); got != tc.want {
			t.Errorf("mapKeyString(%#v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestScanDestCount(t *testing.T) {
	cols := []gocql.ColumnInfo{
		{Name: "a", TypeInfo: text()},
		{Name: "b", TypeInfo: gocql.TupleTypeInfo{
			NativeType: gocql.NewNativeType(4, gocql.TypeTuple, ""),
			Elems:      []gocql.TypeInfo{text(), intType()},
		}},
		{Name: "c", TypeInfo: text()},
	}

	if got := scanDestCount(cols); got != 4 {
		t.Fatalf("scanDestCount = %d, want 4 (a + 2 tuple elements + c)", got)
	}
}
