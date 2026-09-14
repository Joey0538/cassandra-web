package main

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"gopkg.in/inf.v0"
)

// Layouts accepted for a timestamp column, most specific first. The read path
// serves RFC3339 in UTC, but a hand-edited cell may carry an offset, a space
// separator or no time at all.
var timestampLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

var timeOfDayLayouts = []string{
	"15:04:05.999999999",
	"15:04:05",
}

// baseCQLType strips frozen<> wrappers and reduces a collection to its keyword,
// so callers can switch on the type without re-parsing the CQL spelling.
func baseCQLType(cqlType string) string {
	t := strings.TrimSpace(cqlType)
	for strings.HasPrefix(t, FrozenType+"<") && strings.HasSuffix(t, ">") {
		t = strings.TrimSpace(t[len(FrozenType)+1 : len(t)-1])
	}
	if i := strings.IndexByte(t, '<'); i >= 0 {
		return strings.TrimSpace(t[:i])
	}
	return t
}

// collectionArgs returns the type arguments of a list/set/map/tuple, split at
// the top level so nested generics survive.
func collectionArgs(cqlType string) []string {
	t := strings.TrimSpace(cqlType)
	for strings.HasPrefix(t, FrozenType+"<") && strings.HasSuffix(t, ">") {
		t = strings.TrimSpace(t[len(FrozenType)+1 : len(t)-1])
	}
	open := strings.IndexByte(t, '<')
	if open < 0 || !strings.HasSuffix(t, ">") {
		return nil
	}
	inner := t[open+1 : len(t)-1]

	var (
		args  []string
		depth int
		start int
	)
	for i, r := range inner {
		switch r {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				args = append(args, strings.TrimSpace(inner[start:i]))
				start = i + 1
			}
		}
	}
	return append(args, strings.TrimSpace(inner[start:]))
}

// isBlank reports whether a value should bind as NULL. The UI sends an empty
// string for a cleared cell and null for a column that was never set.
func isBlank(v interface{}) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && s == ""
}

// numericString renders a JSON-decoded number without going through float64,
// so a varint or decimal keeps every digit it arrived with.
func numericString(v interface{}) (string, bool) {
	switch n := v.(type) {
	case json.Number:
		return n.String(), true
	case string:
		return n, true
	case int:
		return strconv.FormatInt(int64(n), 10), true
	case int32:
		return strconv.FormatInt(int64(n), 10), true
	case int64:
		return strconv.FormatInt(n, 10), true
	case uint64:
		return strconv.FormatUint(n, 10), true
	case float32:
		return strconv.FormatFloat(float64(n), 'f', -1, 32), true
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64), true
	}
	return "", false
}

func toInt64(v interface{}) (int64, error) {
	s, ok := numericString(v)
	if !ok {
		return 0, fmt.Errorf("cannot read %T as a number", v)
	}
	return strconv.ParseInt(strings.TrimSpace(s), 10, 64)
}

func toFloat64(v interface{}) (float64, error) {
	s, ok := numericString(v)
	if !ok {
		return 0, fmt.Errorf("cannot read %T as a number", v)
	}
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}

// parseTimestampMillis accepts every spelling the UI can produce and returns
// the epoch milliseconds gocql binds to a timestamp column.
func parseTimestampMillis(v interface{}) (int64, error) {
	if t, ok := v.(time.Time); ok {
		return t.UnixMilli(), nil
	}
	s, ok := numericString(v)
	if !ok {
		return 0, fmt.Errorf("cannot read %T as a timestamp", v)
	}
	s = strings.TrimSpace(s)

	// A bare integer is already epoch milliseconds.
	if ms, err := strconv.ParseInt(s, 10, 64); err == nil {
		return ms, nil
	}
	for _, layout := range timestampLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().UnixMilli(), nil
		}
	}
	return 0, fmt.Errorf("cannot parse %q as a timestamp", s)
}

// parseDateString returns the bare date layout, the only string form gocql
// accepts for a date column.
func parseDateString(v interface{}) (string, error) {
	if t, ok := v.(time.Time); ok {
		return t.UTC().Format("2006-01-02"), nil
	}
	s, ok := numericString(v)
	if !ok {
		return "", fmt.Errorf("cannot read %T as a date", v)
	}
	s = strings.TrimSpace(s)

	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.Format("2006-01-02"), nil
	}
	for _, layout := range timestampLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format("2006-01-02"), nil
		}
	}
	return "", fmt.Errorf("cannot parse %q as a date", s)
}

// parseTimeNanos returns nanoseconds since midnight, which is how gocql binds
// a time column.
func parseTimeNanos(v interface{}) (int64, error) {
	s, ok := numericString(v)
	if !ok {
		return 0, fmt.Errorf("cannot read %T as a time", v)
	}
	s = strings.TrimSpace(s)

	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, nil
	}
	for _, layout := range timeOfDayLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			midnight := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
			return t.Sub(midnight).Nanoseconds(), nil
		}
	}
	return 0, fmt.Errorf("cannot parse %q as a time", s)
}

// parseBlob decodes the base64 the read path serves. Writing the text through
// unchanged stores the ASCII of the encoding instead of the bytes.
func parseBlob(v interface{}) ([]byte, error) {
	switch b := v.(type) {
	case []byte:
		return b, nil
	case string:
		if hexStr, ok := strings.CutPrefix(b, "0x"); ok {
			decoded, err := hex.DecodeString(hexStr)
			if err == nil {
				return decoded, nil
			}
		}
		decoded, err := base64.StdEncoding.DecodeString(b)
		if err != nil {
			return nil, fmt.Errorf("cannot decode %q as a blob: %w", b, err)
		}
		return decoded, nil
	}
	return nil, fmt.Errorf("cannot read %T as a blob", v)
}

// expandExponent rewrites scientific notation into plain decimal digits.
// json-bigint re-serializes a wide varint or decimal as 1.23...e+29, which
// neither big.Int nor inf.Dec will parse.
func expandExponent(s string) string {
	i := strings.IndexAny(s, "eE")
	if i < 0 {
		return s
	}

	mantissa, expPart := s[:i], s[i+1:]
	exp, err := strconv.Atoi(strings.TrimPrefix(expPart, "+"))
	if err != nil {
		return s
	}

	sign := ""
	if strings.HasPrefix(mantissa, "-") || strings.HasPrefix(mantissa, "+") {
		if mantissa[0] == '-' {
			sign = "-"
		}
		mantissa = mantissa[1:]
	}

	intPart, fracPart, _ := strings.Cut(mantissa, ".")
	digits := intPart + fracPart
	if digits == "" {
		return s
	}

	// The decimal point sits after pointPos digits once the exponent is applied.
	pointPos := len(intPart) + exp
	switch {
	case pointPos <= 0:
		return sign + "0." + strings.Repeat("0", -pointPos) + digits
	case pointPos >= len(digits):
		return sign + digits + strings.Repeat("0", pointPos-len(digits))
	default:
		return sign + digits[:pointPos] + "." + digits[pointPos:]
	}
}

// toCQLValue converts a JSON-decoded cell into the Go type gocql binds for
// cqlType. It is the single conversion used by every write path, so an edit and
// a delete can never disagree about what a column's value means.
func toCQLValue(cqlType string, v interface{}) (interface{}, error) {
	if isBlank(v) {
		return nil, nil
	}

	switch baseCQLType(cqlType) {
	case AsciiType, TextType, VarcharType, InetType, UuidType, TimeuuidType:
		s, ok := numericString(v)
		if !ok {
			return nil, fmt.Errorf("cannot read %T as %s", v, cqlType)
		}
		return s, nil

	case BooleanType:
		switch b := v.(type) {
		case bool:
			return b, nil
		case string:
			return strconv.ParseBool(b)
		}
		return nil, fmt.Errorf("cannot read %T as a boolean", v)

	case TinyintType:
		n, err := toInt64(v)
		if err != nil {
			return nil, err
		}
		return int8(n), nil

	case SmallintType:
		n, err := toInt64(v)
		if err != nil {
			return nil, err
		}
		return int16(n), nil

	case IntType:
		n, err := toInt64(v)
		if err != nil {
			return nil, err
		}
		return int32(n), nil

	case BigintType, CounterType:
		return toInt64(v)

	case DoubleType:
		return toFloat64(v)

	case FloatType:
		// gocql rejects a float64 for a 32-bit float column.
		f, err := toFloat64(v)
		if err != nil {
			return nil, err
		}
		return float32(f), nil

	case DecimalType:
		s, ok := numericString(v)
		if !ok {
			return nil, fmt.Errorf("cannot read %T as a decimal", v)
		}
		dec := new(inf.Dec)
		if _, ok := dec.SetString(expandExponent(strings.TrimSpace(s))); !ok {
			return nil, fmt.Errorf("cannot parse %q as a decimal", s)
		}
		return dec, nil

	case VarintType:
		s, ok := numericString(v)
		if !ok {
			return nil, fmt.Errorf("cannot read %T as a varint", v)
		}
		expanded := expandExponent(strings.TrimSpace(s))
		if intPart, frac, hasFrac := strings.Cut(expanded, "."); hasFrac {
			// An exponent can leave a zero fraction (1.5e1 is the varint 15);
			// anything else is not a whole number.
			if strings.Trim(frac, "0") != "" {
				return nil, fmt.Errorf("cannot parse %q as a varint: not a whole number", s)
			}
			expanded = intPart
		}

		bi := new(big.Int)
		if _, ok := bi.SetString(expanded, 10); !ok {
			return nil, fmt.Errorf("cannot parse %q as a varint", s)
		}
		return bi, nil

	case BlobType:
		return parseBlob(v)

	case TimestampType:
		return parseTimestampMillis(v)

	case DateType:
		return parseDateString(v)

	case TimeType:
		return parseTimeNanos(v)

	case ListType, SetType:
		args := collectionArgs(cqlType)
		if len(args) != 1 {
			return v, nil
		}
		return toCQLList(v, args[0])

	case MapType:
		args := collectionArgs(cqlType)
		if len(args) != 2 {
			return v, nil
		}
		return toCQLMap(v, args[0], args[1])
	}

	// UDTs and tuples keep their decoded shape; gocql handles them from there.
	return v, nil
}

func toCQLList(v interface{}, elemType string) (interface{}, error) {
	items, ok := v.([]interface{})
	if !ok {
		return v, nil
	}
	out := make([]interface{}, 0, len(items))
	for i, item := range items {
		converted, err := toCQLValue(elemType, item)
		if err != nil {
			return nil, fmt.Errorf("element %d: %w", i, err)
		}
		out = append(out, converted)
	}
	return out, nil
}

func toCQLMap(v interface{}, keyType, valType string) (interface{}, error) {
	out := map[interface{}]interface{}{}

	switch m := v.(type) {
	case map[string]interface{}:
		for k, val := range m {
			if err := putCQLMapEntry(out, k, val, keyType, valType); err != nil {
				return nil, err
			}
		}
	case map[interface{}]interface{}:
		for k, val := range m {
			if err := putCQLMapEntry(out, k, val, keyType, valType); err != nil {
				return nil, err
			}
		}
	default:
		return v, nil
	}
	return out, nil
}

func putCQLMapEntry(out map[interface{}]interface{}, k, val interface{}, keyType, valType string) error {
	key, err := toCQLValue(keyType, k)
	if err != nil {
		return fmt.Errorf("map key %v: %w", k, err)
	}
	value, err := toCQLValue(valType, val)
	if err != nil {
		return fmt.Errorf("map value for key %v: %w", k, err)
	}
	out[key] = value
	return nil
}
