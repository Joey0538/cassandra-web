package main

import (
	"strings"
	"testing"
)

// gocql v1.3.1 cannot bind a tuple: the prepared metadata counts a tuple as one
// value per element but keeps one column entry, so the write path either
// reports the wrong count or indexes past its own slice. Writing the tuple as a
// CQL literal keeps it out of the bind path.
func TestCQLLiteral_TupleElements(t *testing.T) {
	cases := []struct {
		name string
		typ  string
		in   interface{}
		want string
	}{
		{"int", IntType, int64(1), "1"},
		{"bigint", BigintType, "9007199254740993", "9007199254740993"},
		{"text", TextType, "hello", "'hello'"},
		{"text with a quote", TextType, "it's", "'it''s'"},
		{"boolean", BooleanType, true, "true"},
		{"double", DoubleType, "3.5", "3.5"},
		{"uuid", UuidType, "123e4567-e89b-12d3-a456-426614174000", "123e4567-e89b-12d3-a456-426614174000"},
		{"timestamp", TimestampType, "2024-01-15T10:30:45.123Z", "1705314645123"},
		{"date", DateType, "2024-01-15T00:00:00Z", "'2024-01-15'"},
		{"blob", BlobType, "yv66vg==", "0xcafebabe"},
		{"null", TextType, nil, "null"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := (converter{}).literal(tc.typ, tc.in)
			if err != nil {
				t.Fatalf("literal(%s, %v): %v", tc.typ, tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("literal(%s, %v) = %s, want %s", tc.typ, tc.in, got, tc.want)
			}
		})
	}
}

func TestCQLLiteral_Tuple(t *testing.T) {
	got, err := (converter{}).literal("tuple<int, text>", []interface{}{int64(1), "t"})
	if err != nil {
		t.Fatalf("tuple: %v", err)
	}
	if got != "(1,'t')" {
		t.Fatalf("tuple = %s, want (1,'t')", got)
	}

	if _, err = (converter{}).literal("tuple<int, text>", []interface{}{int64(1)}); err == nil {
		t.Fatal("a tuple with too few elements should error")
	}

	// An injection attempt must end up quoted, not executed.
	got, err = (converter{}).literal("tuple<int, text>", []interface{}{int64(1), "'); DROP TABLE x --"})
	if err != nil {
		t.Fatalf("quoting: %v", err)
	}
	if got != "(1,'''); DROP TABLE x --')" {
		t.Fatalf("quoting = %s", got)
	}
}

// A tuple whose elements are themselves collections is not supported and must
// say so rather than emit a malformed literal.
func TestCQLLiteral_RejectsNonScalarElements(t *testing.T) {
	if _, err := (converter{}).literal("tuple<int, list<text>>", []interface{}{int64(1), []interface{}{"a"}}); err == nil {
		t.Fatal("expected an error for a collection inside a tuple")
	}
}

// Tuple elements are the only place a value reaches the statement as text
// rather than a bind parameter, so a hostile string must always come back as
// one balanced, quoted literal and never as CQL.
func TestCQLLiteral_HostileStringsStayQuoted(t *testing.T) {
	hostile := []string{
		`'`,
		`''`,
		`\'`,
		`'); DROP TABLE users --`,
		`' OR '1'='1`,
		`a'); INSERT INTO x (y) VALUES ('z`,
		"line\nbreak",
		"tab\there",
		`back\slash`,
		`"double"`,
		`%s`,
		`?`,
		`0x00`,
		`);`,
	}

	for _, in := range hostile {
		got, err := (converter{}).literal(TextType, in)
		if err != nil {
			t.Fatalf("literal(%q): %v", in, err)
		}

		if !strings.HasPrefix(got, "'") || !strings.HasSuffix(got, "'") || len(got) < 2 {
			t.Fatalf("literal(%q) = %s, want a quoted literal", in, got)
		}

		// Doubling is CQL's only string escape, so every quote inside the body
		// must appear as an even-length run for the literal to stay closed.
		body := got[1 : len(got)-1]
		for i := 0; i < len(body); {
			if body[i] != '\'' {
				i++
				continue
			}

			run := 0
			for i < len(body) && body[i] == '\'' {
				run++
				i++
			}
			if run%2 != 0 {
				t.Fatalf("literal(%q) = %s has an unbalanced quote run of %d", in, got, run)
			}
		}

		// Round-tripping the literal must give back exactly the input.
		if unquoteCQLString(got) != in {
			t.Fatalf("literal(%q) = %s does not round-trip", in, got)
		}
	}
}

// unquoteCQLString reverses quoteCQLString, for the round-trip assertion above.
func unquoteCQLString(s string) string {
	return strings.ReplaceAll(s[1:len(s)-1], "''", "'")
}

// inet reaches the literal as free text, so it is validated rather than trusted
// to be harmless once quoted.
func TestCQLLiteral_InetIsValidated(t *testing.T) {
	got, err := (converter{}).literal(InetType, "10.0.0.1")
	if err != nil || got != "'10.0.0.1'" {
		t.Fatalf("valid inet: got %s, err %v", got, err)
	}

	if _, err := (converter{}).literal(InetType, "'); DROP TABLE x --"); err == nil {
		t.Fatal("expected an error for an inet that is not an address")
	}
}
