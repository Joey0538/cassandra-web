package main

import "testing"

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
