package main

import (
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"gopkg.in/inf.v0"
)

// The read path hands the UI whatever encoding/json makes of gocql's decoded
// value; these are the exact strings observed against Cassandra 5.0.9.
func TestToCQLValue_Timestamp(t *testing.T) {
	want := time.Date(2024, 1, 15, 10, 30, 45, 123000000, time.UTC).UnixMilli()

	cases := []struct {
		name string
		in   interface{}
		want int64
	}{
		{"rfc3339 millis as served by the read path", "2024-01-15T10:30:45.123Z", want},
		{"rfc3339 nanos", "2024-01-15T10:30:45.123000000Z", want},
		{"whole second", "2024-01-15T10:30:45Z", want - 123},
		{"non-utc offset", "2024-01-15T18:30:45.123+08:00", want},
		{"space separated", "2024-01-15 10:30:45.123", want},
		{"date only", "2024-01-15", time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC).UnixMilli()},
		{"epoch millis as int64", int64(want), want},
		{"epoch millis as string", "1705314645123", want},
		{"json.Number", json.Number("1705314645123"), want},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := toCQLValue(TimestampType, tc.in)
			if err != nil {
				t.Fatalf("toCQLValue(%v) error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// A null column must bind as NULL, not as the zero value and not as an error.
// This is the failure users hit editing any row with an empty timestamp.
func TestToCQLValue_NullAndEmpty(t *testing.T) {
	for _, typ := range []string{
		TimestampType, DateType, TimeType, IntType, TextType,
		DecimalType, FloatType, UuidType, BlobType, VarintType,
	} {
		t.Run(typ+"/nil", func(t *testing.T) {
			got, err := toCQLValue(typ, nil)
			if err != nil {
				t.Fatalf("nil %s: unexpected error: %v", typ, err)
			}
			if got != nil {
				t.Fatalf("nil %s: got %v, want nil", typ, got)
			}
		})
		t.Run(typ+"/empty string", func(t *testing.T) {
			got, err := toCQLValue(typ, "")
			if err != nil {
				t.Fatalf("empty %s: unexpected error: %v", typ, err)
			}
			if got != nil {
				t.Fatalf("empty %s: got %v, want nil", typ, got)
			}
		})
	}
}

func TestToCQLValue_Date(t *testing.T) {
	// gocql only accepts the bare date layout for a string.
	cases := map[string]string{
		"2024-01-15T00:00:00Z":     "2024-01-15", // what the read path serves
		"2024-01-15":               "2024-01-15",
		"2024-01-15T10:30:45.123Z": "2024-01-15",
	}
	for in, want := range cases {
		got, err := toCQLValue(DateType, in)
		if err != nil {
			t.Fatalf("date %q: %v", in, err)
		}
		if got != want {
			t.Fatalf("date %q: got %v, want %v", in, got, want)
		}
	}
}

func TestToCQLValue_Time(t *testing.T) {
	const nanos = int64(37845123456789)
	for _, in := range []interface{}{nanos, "37845123456789", "10:30:45.123456789", json.Number("37845123456789")} {
		got, err := toCQLValue(TimeType, in)
		if err != nil {
			t.Fatalf("time %v: %v", in, err)
		}
		if got != nanos {
			t.Fatalf("time %v: got %v, want %v", in, got, nanos)
		}
	}
}

// Blob arrives base64-encoded from the read path; writing the raw text back
// silently created a second row keyed by the ASCII bytes of the base64.
func TestToCQLValue_Blob(t *testing.T) {
	got, err := toCQLValue(BlobType, "yv66vg==")
	if err != nil {
		t.Fatalf("blob: %v", err)
	}
	b, ok := got.([]byte)
	if !ok {
		t.Fatalf("blob: got %T, want []byte", got)
	}
	want := []byte{0xca, 0xfe, 0xba, 0xbe}
	if string(b) != string(want) {
		t.Fatalf("blob: got %x, want %x", b, want)
	}
}

func TestToCQLValue_NumericWidths(t *testing.T) {
	cases := []struct {
		typ  string
		in   interface{}
		want interface{}
	}{
		{TinyintType, int64(3), int8(3)},
		{SmallintType, int64(7), int16(7)},
		{IntType, int64(42), int32(42)},
		{BigintType, int64(9007199254740993), int64(9007199254740993)},
		{CounterType, int64(5), int64(5)},
		{DoubleType, "3.14159", float64(3.14159)},
		{FloatType, "2.5", float32(2.5)}, // gocql rejects float64 for a float column
		{BooleanType, true, true},
	}
	for _, tc := range cases {
		got, err := toCQLValue(tc.typ, tc.in)
		if err != nil {
			t.Fatalf("%s: %v", tc.typ, err)
		}
		if got != tc.want {
			t.Fatalf("%s: got %v (%T), want %v (%T)", tc.typ, got, got, tc.want, tc.want)
		}
	}
}

// decimal and varint must not round-trip through float64 - that silently
// rewrote large varints into a different row.
func TestToCQLValue_DecimalAndVarint(t *testing.T) {
	got, err := toCQLValue(DecimalType, "12345.6789")
	if err != nil {
		t.Fatalf("decimal: %v", err)
	}
	dec, ok := got.(*inf.Dec)
	if !ok {
		t.Fatalf("decimal: got %T, want *inf.Dec", got)
	}
	if dec.String() != "12345.6789" {
		t.Fatalf("decimal: got %s, want 12345.6789", dec)
	}

	const huge = "123456789012345678901234567890"
	got, err = toCQLValue(VarintType, json.Number(huge))
	if err != nil {
		t.Fatalf("varint: %v", err)
	}
	bi, ok := got.(*big.Int)
	if !ok {
		t.Fatalf("varint: got %T, want *big.Int", got)
	}
	if bi.String() != huge {
		t.Fatalf("varint: got %s, want %s", bi, huge)
	}
}

func TestToCQLValue_Collections(t *testing.T) {
	got, err := toCQLValue("list<timestamp>", []interface{}{"2024-01-15T10:30:45.123Z"})
	if err != nil {
		t.Fatalf("list<timestamp>: %v", err)
	}
	l, ok := got.([]interface{})
	if !ok || len(l) != 1 {
		t.Fatalf("list<timestamp>: got %#v", got)
	}
	want := time.Date(2024, 1, 15, 10, 30, 45, 123000000, time.UTC).UnixMilli()
	if l[0] != want {
		t.Fatalf("list<timestamp>: got %v, want %v", l[0], want)
	}

	got, err = toCQLValue("map<text, timestamp>", map[string]interface{}{"k": "2024-01-15T10:30:45.123Z"})
	if err != nil {
		t.Fatalf("map<text,timestamp>: %v", err)
	}
	m, ok := got.(map[interface{}]interface{})
	if !ok || len(m) != 1 {
		t.Fatalf("map<text,timestamp>: got %#v", got)
	}
	if m["k"] != want {
		t.Fatalf("map<text,timestamp>: got %v, want %v", m["k"], want)
	}

	// frozen<> is a storage hint, not a distinct type for binding.
	if _, err := toCQLValue("frozen<list<int>>", []interface{}{int64(1)}); err != nil {
		t.Fatalf("frozen<list<int>>: %v", err)
	}
}

func TestToCQLValue_UnparseableIsAnError(t *testing.T) {
	for _, tc := range []struct{ typ, in string }{
		{TimestampType, "not-a-time"},
		{DateType, "not-a-date"},
		{IntType, "not-a-number"},
		{BlobType, "not!base64"},
	} {
		if _, err := toCQLValue(tc.typ, tc.in); err == nil {
			t.Fatalf("%s(%q): expected an error, got nil", tc.typ, tc.in)
		}
	}
}

// json-bigint re-serializes a wide number in exponential form, so the exact
// digits arrive as something like 1.2345678901234567890123456789e+29.
func TestToCQLValue_ExponentialNotation(t *testing.T) {
	const huge = "123456789012345678901234567890"

	got, err := toCQLValue(VarintType, "1.2345678901234567890123456789e+29")
	if err != nil {
		t.Fatalf("varint exponential: %v", err)
	}
	bi, ok := got.(*big.Int)
	if !ok || bi.String() != huge {
		t.Fatalf("varint exponential: got %v (%T), want %s", got, got, huge)
	}

	got, err = toCQLValue(VarintType, "-1.2345678901234567890123456789e+29")
	if err != nil {
		t.Fatalf("negative varint exponential: %v", err)
	}
	if got.(*big.Int).String() != "-"+huge {
		t.Fatalf("negative varint exponential: got %v", got)
	}

	got, err = toCQLValue(DecimalType, "1.23456789e+5")
	if err != nil {
		t.Fatalf("decimal exponential: %v", err)
	}
	if got.(*inf.Dec).String() != "123456.789" {
		t.Fatalf("decimal exponential: got %v, want 123456.789", got)
	}

	// A fractional value is not a valid varint and must still be rejected.
	if _, err := toCQLValue(VarintType, "1.5e1"); err != nil {
		t.Fatalf("15 is integral: %v", err)
	}
	if _, err := toCQLValue(VarintType, "1.5e0"); err == nil {
		t.Fatal("1.5 is not a varint, expected an error")
	}
}
