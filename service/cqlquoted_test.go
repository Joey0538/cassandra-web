package main

import (
	"testing"
	"time"
)

// A case-sensitive UDT is declared quoted, and system_schema.columns reports
// the type with those quotes: frozen<"EncMeta">, set<frozen<"Participant">>.
// The lookup key is the bare name, so the quotes have to come off.
func TestBaseCQLType_StripsQuotes(t *testing.T) {
	cases := map[string]string{
		`frozen<"EncMeta">`:                               "EncMeta",
		`"EncMeta"`:                                       "EncMeta",
		`frozen<"QuotedParentMessage">`:                   "QuotedParentMessage",
		`frozen<reaction_key>`:                            "reaction_key",
		`set<frozen<"Participant">>`:                      SetType,
		`map<frozen<reaction_key>, frozen<reactor_info>>`: MapType,
		"timestamp":                                       TimestampType,
	}
	for in, want := range cases {
		if got := baseCQLType(in); got != want {
			t.Fatalf("baseCQLType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCollectionArgs_StripsQuotesOnElements(t *testing.T) {
	args := collectionArgs(`set<frozen<"Participant">>`)
	if len(args) != 1 {
		t.Fatalf("args = %#v", args)
	}
	if got := baseCQLType(args[0]); got != "Participant" {
		t.Fatalf("element base type = %q, want Participant", got)
	}
}

// A quoted UDT must resolve, or its timestamp fields stay strings and gocql
// rejects the row with "can not marshal string into timestamp".
func TestConverter_QuotedUDTResolves(t *testing.T) {
	c := converter{
		keyspace: "chat",
		udts: fakeUDTs{
			"chat.EncMeta": {{Name: "nonce", Type: "blob"}},
			"chat.QuotedParentMessage": {
				{Name: "message_id", Type: "text"},
				{Name: "created_at", Type: "timestamp"},
			},
		},
	}

	got, err := c.value(`frozen<"QuotedParentMessage">`, map[string]interface{}{
		"message_id": "p1",
		"created_at": "2024-01-15T10:30:45.123Z",
	})
	if err != nil {
		t.Fatalf("quoted udt: %v", err)
	}
	m := got.(map[string]interface{})
	want := time.Date(2024, 1, 15, 10, 30, 45, 123000000, time.UTC).UnixMilli()
	if m["created_at"] != want {
		t.Fatalf("created_at = %v (%T), want %v", m["created_at"], m["created_at"], want)
	}
}

// gocql cannot marshal an untyped nil into a UDT - it reports
// "cannot marshal <nil> into chat.EncMeta{nonce=blob}" - so a null UDT column
// has to bind as a typed nil pointer, which marshals to NULL.
func TestConverter_NullUDTBindsTypedNil(t *testing.T) {
	c := converter{
		keyspace: "chat",
		udts:     fakeUDTs{"chat.EncMeta": {{Name: "nonce", Type: "blob"}}},
	}

	for _, in := range []interface{}{nil, ""} {
		got, err := c.value(`frozen<"EncMeta">`, in)
		if err != nil {
			t.Fatalf("null udt %#v: %v", in, err)
		}
		if got == nil {
			t.Fatalf("null udt %#v: got untyped nil, which gocql rejects for a UDT", in)
		}
		if p, ok := got.(*map[string]interface{}); !ok || p != nil {
			t.Fatalf("null udt %#v: got %#v, want a nil *map[string]interface{}", in, got)
		}
	}
}
