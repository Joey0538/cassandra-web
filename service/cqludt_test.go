package main

import (
	"testing"
	"time"

	"github.com/gocql/gocql"
)

// fakeUDTs stands in for the system_schema.types lookup.
type fakeUDTs map[string][]udtField

func (f fakeUDTs) UDTFields(keyspace, typeName string) ([]udtField, bool) {
	fields, ok := f[keyspace+"."+typeName]
	return fields, ok
}

func testConverter() converter {
	return converter{
		keyspace: "chat",
		udts: fakeUDTs{
			"chat.rinfo": {{Name: "uid", Type: "text"}, {Name: "at", Type: "timestamp"}},
			"chat.rkey":  {{Name: "emoji", Type: "text"}, {Name: "ts", Type: "timestamp"}},
		},
	}
}

var wantMillis = time.Date(2024, 1, 15, 10, 30, 45, 123000000, time.UTC).UnixMilli()

// A UDT column arrives as an object whose timestamp fields are still strings;
// gocql marshals each field against its own type, so they must be converted.
func TestConverter_UDTColumn(t *testing.T) {
	got, err := testConverter().value("rinfo", map[string]interface{}{
		"uid": "u1",
		"at":  "2024-01-15T10:30:45.123Z",
	})
	if err != nil {
		t.Fatalf("udt column: %v", err)
	}
	m, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("udt column: got %T, want map[string]interface{}", got)
	}
	if m["uid"] != "u1" {
		t.Fatalf("uid: got %v", m["uid"])
	}
	if m["at"] != wantMillis {
		t.Fatalf("at: got %v (%T), want %v", m["at"], m["at"], wantMillis)
	}
}

func TestConverter_FrozenUDTAndListOfUDT(t *testing.T) {
	c := testConverter()

	if _, err := c.value("frozen<rinfo>", map[string]interface{}{"uid": "u", "at": nil}); err != nil {
		t.Fatalf("frozen<rinfo>: %v", err)
	}

	got, err := c.value("list<frozen<rinfo>>", []interface{}{
		map[string]interface{}{"uid": "u3", "at": "2024-01-15T10:30:45.123Z"},
	})
	if err != nil {
		t.Fatalf("list<frozen<rinfo>>: %v", err)
	}
	l := got.([]interface{})
	if l[0].(map[string]interface{})["at"] != wantMillis {
		t.Fatalf("list element at: got %v", l[0])
	}
}

// The read path renders a UDT map key as its JSON encoding, because a Go map
// cannot be keyed by a map. Writing it back has to parse that JSON.
func TestConverter_UDTKeyedMap(t *testing.T) {
	got, err := testConverter().value(
		"map<frozen<rkey>, frozen<rinfo>>",
		map[string]interface{}{
			`{"emoji":"x","ts":"2024-01-15T10:30:45.123Z"}`: map[string]interface{}{
				"uid": "u2", "at": "2024-01-15T10:30:45.123Z",
			},
		})
	if err != nil {
		t.Fatalf("udt-keyed map: %v", err)
	}
	m, ok := got.(map[interface{}]interface{})
	if !ok || len(m) != 1 {
		t.Fatalf("udt-keyed map: got %#v", got)
	}
	for k, v := range m {
		key, ok := k.(*udtMapKey)
		if !ok {
			t.Fatalf("key: got %T, want *udtMapKey", k)
		}
		if key.fields["emoji"] != "x" || key.fields["ts"] != wantMillis {
			t.Fatalf("key fields: %#v", key.fields)
		}
		if v.(map[string]interface{})["at"] != wantMillis {
			t.Fatalf("value fields: %#v", v)
		}
	}
}

// Without a resolver the value must pass through untouched rather than error,
// so the conversion stays usable outside a request.
func TestConverter_UnknownTypeWithoutResolver(t *testing.T) {
	in := map[string]interface{}{"uid": "u1"}
	got, err := converter{}.value("rinfo", in)
	if err != nil {
		t.Fatalf("no resolver: %v", err)
	}
	if _, ok := got.(map[string]interface{}); !ok {
		t.Fatalf("no resolver: got %T", got)
	}
}

func TestConverter_UDTFieldErrorNamesTheField(t *testing.T) {
	_, err := testConverter().value("rinfo", map[string]interface{}{"at": "not-a-time"})
	if err == nil {
		t.Fatal("expected an error for an unparseable UDT field")
	}
	if got := err.Error(); got == "" || !contains(got, "at") {
		t.Fatalf("error should name the field, got %q", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// gocql.Marshal dereferences a pointer before checking for UDTMarshaler, so the
// dereferenced value has to implement it or every field marshals as null.
func TestUDTMapKey_ValueImplementsUDTMarshaler(t *testing.T) {
	var v interface{} = udtMapKey{fields: map[string]interface{}{"a": "b"}}
	if _, ok := v.(gocql.UDTMarshaler); !ok {
		t.Fatal("udtMapKey value must implement gocql.UDTMarshaler")
	}
}
