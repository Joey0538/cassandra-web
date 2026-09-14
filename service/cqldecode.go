package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/gocql/gocql"
)

// CQLValue decodes any CQL value without asking gocql to build a Go type for
// it first.
//
// gocql's untyped row APIs (Iter.MapScan, Iter.SliceMap, both via RowData)
// derive a destination type from the column's CQL type in goType(). A UDT maps
// to map[string]interface{} and a map maps to reflect.MapOf(key, elem), so a
// map<frozen<udt>, ...> asks reflect for a map type whose key is itself a Go
// map. reflect.MapOf panics on that ("invalid key type"), and it panics rather
// than returning an error, so even RowData's error-returning NewWithError
// cannot catch it: the panic comes from inside reflect.
//
// Unmarshal checks for the Unmarshaler interface before its type switch and
// before any goType call, so implementing it here bypasses that path
// completely. Every CQL shape is then decoded from the wire bytes into
// JSON-friendly Go values, and unrepresentable map keys become strings rather
// than a crash.
type CQLValue struct {
	Value interface{}
}

// UnmarshalCQL implements gocql.Unmarshaler.
func (c *CQLValue) UnmarshalCQL(info gocql.TypeInfo, data []byte) error {
	v, err := decodeCQL(info, data)
	if err != nil {
		return err
	}
	c.Value = v
	return nil
}

// decodeCQL turns one CQL value's wire bytes into a JSON-encodable Go value.
func decodeCQL(info gocql.TypeInfo, data []byte) (interface{}, error) {
	if data == nil {
		return nil, nil
	}

	switch info.Type() {
	case gocql.TypeUDT:
		udt, ok := info.(gocql.UDTTypeInfo)
		if !ok {
			return nil, fmt.Errorf("decode udt: unexpected type info %T", info)
		}
		out := make(map[string]interface{}, len(udt.Elements))
		rest := data
		for _, field := range udt.Elements {
			// A UDT written before a field was added simply ends early; the
			// remaining fields are absent rather than null.
			if len(rest) == 0 {
				out[field.Name] = nil
				continue
			}
			chunk, remainder, err := readFrame(rest)
			if err != nil {
				return nil, fmt.Errorf("decode udt field %s: %w", field.Name, err)
			}
			rest = remainder
			val, err := decodeCQL(field.Type, chunk)
			if err != nil {
				return nil, fmt.Errorf("decode udt field %s: %w", field.Name, err)
			}
			out[field.Name] = val
		}
		return out, nil

	case gocql.TypeMap:
		coll, ok := info.(gocql.CollectionType)
		if !ok {
			return nil, fmt.Errorf("decode map: unexpected type info %T", info)
		}
		n, rest, err := readCount(data)
		if err != nil {
			return nil, fmt.Errorf("decode map: %w", err)
		}
		out := make(map[string]interface{}, n)
		for i := 0; i < n; i++ {
			keyBytes, remainder, err := readFrame(rest)
			if err != nil {
				return nil, fmt.Errorf("decode map key %d: %w", i, err)
			}
			valBytes, remainder2, err := readFrame(remainder)
			if err != nil {
				return nil, fmt.Errorf("decode map value %d: %w", i, err)
			}
			rest = remainder2

			key, err := decodeCQL(coll.Key, keyBytes)
			if err != nil {
				return nil, fmt.Errorf("decode map key %d: %w", i, err)
			}
			val, err := decodeCQL(coll.Elem, valBytes)
			if err != nil {
				return nil, fmt.Errorf("decode map value %d: %w", i, err)
			}
			out[mapKeyString(key)] = val
		}
		return out, nil

	case gocql.TypeList, gocql.TypeSet:
		coll, ok := info.(gocql.CollectionType)
		if !ok {
			return nil, fmt.Errorf("decode list: unexpected type info %T", info)
		}
		n, rest, err := readCount(data)
		if err != nil {
			return nil, fmt.Errorf("decode list: %w", err)
		}
		out := make([]interface{}, 0, n)
		for i := 0; i < n; i++ {
			chunk, remainder, err := readFrame(rest)
			if err != nil {
				return nil, fmt.Errorf("decode list element %d: %w", i, err)
			}
			rest = remainder
			val, err := decodeCQL(coll.Elem, chunk)
			if err != nil {
				return nil, fmt.Errorf("decode list element %d: %w", i, err)
			}
			out = append(out, val)
		}
		return out, nil

	case gocql.TypeTuple:
		tuple, ok := info.(gocql.TupleTypeInfo)
		if !ok {
			return nil, fmt.Errorf("decode tuple: unexpected type info %T", info)
		}
		out := make([]interface{}, 0, len(tuple.Elems))
		rest := data
		for i, elem := range tuple.Elems {
			if len(rest) == 0 {
				out = append(out, nil)
				continue
			}
			chunk, remainder, err := readFrame(rest)
			if err != nil {
				return nil, fmt.Errorf("decode tuple element %d: %w", i, err)
			}
			rest = remainder
			val, err := decodeCQL(elem, chunk)
			if err != nil {
				return nil, fmt.Errorf("decode tuple element %d: %w", i, err)
			}
			out = append(out, val)
		}
		return out, nil

	default:
		// Scalars have no composite key to build, so gocql's own type mapping
		// is safe here and keeps every native type's conversion consistent
		// with the rest of the driver.
		dest, err := info.NewWithError()
		if err != nil {
			return nil, err
		}
		if err := gocql.Unmarshal(info, data, dest); err != nil {
			return nil, err
		}
		return reflect.ValueOf(dest).Elem().Interface(), nil
	}
}

// readCount reads a collection's element count.
func readCount(data []byte) (int, []byte, error) {
	if len(data) < 4 {
		return 0, nil, fmt.Errorf("short collection header: %d bytes", len(data))
	}
	n := int(int32(binary.BigEndian.Uint32(data[:4])))
	if n < 0 {
		return 0, nil, fmt.Errorf("negative collection size %d", n)
	}
	return n, data[4:], nil
}

// readFrame reads one [int32 length][bytes] frame. A length of -1 is null.
func readFrame(data []byte) (chunk []byte, rest []byte, err error) {
	if len(data) < 4 {
		return nil, nil, fmt.Errorf("short element header: %d bytes", len(data))
	}
	size := int(int32(binary.BigEndian.Uint32(data[:4])))
	data = data[4:]
	if size < 0 {
		return nil, data, nil
	}
	if size > len(data) {
		return nil, nil, fmt.Errorf("element length %d exceeds remaining %d bytes", size, len(data))
	}
	return data[:size], data[size:], nil
}

// mapKeyString renders a decoded map key as a JSON object key. Composite keys
// (a UDT, tuple or nested collection) become their JSON encoding, which is
// what makes a map<frozen<udt>, ...> viewable at all.
func mapKeyString(key interface{}) string {
	switch k := key.(type) {
	case nil:
		return ""
	case string:
		return k
	case fmt.Stringer:
		return k.String()
	}

	switch reflect.ValueOf(key).Kind() {
	case reflect.Map, reflect.Slice, reflect.Array, reflect.Struct, reflect.Ptr:
		if b, err := json.Marshal(key); err == nil {
			return string(b)
		}
	}
	return fmt.Sprintf("%v", key)
}

// scanDestCount mirrors the expansion gocql's Iter.Scan expects: one
// destination per column, except a tuple column which takes one per element.
func scanDestCount(cols []gocql.ColumnInfo) int {
	n := 0
	for _, col := range cols {
		if tuple, ok := col.TypeInfo.(gocql.TupleTypeInfo); ok {
			n += len(tuple.Elems)
			continue
		}
		n++
	}
	return n
}

// SafeMapScan is a drop-in replacement for Iter.MapScan that cannot panic on
// an exotic column type. It returns false when the iterator is exhausted or
// errored, matching MapScan's contract.
func SafeMapScan(iter *gocql.Iter) (map[string]interface{}, bool) {
	cols := iter.Columns()
	if len(cols) == 0 {
		return nil, false
	}

	values := make([]CQLValue, scanDestCount(cols))
	dest := make([]interface{}, len(values))
	for i := range values {
		dest[i] = &values[i]
	}

	if !iter.Scan(dest...) {
		return nil, false
	}

	row := make(map[string]interface{}, len(cols))
	i := 0
	for _, col := range cols {
		if tuple, ok := col.TypeInfo.(gocql.TupleTypeInfo); ok {
			parts := make([]interface{}, 0, len(tuple.Elems))
			for range tuple.Elems {
				parts = append(parts, values[i].Value)
				i++
			}
			row[col.Name] = parts
			continue
		}
		row[col.Name] = values[i].Value
		i++
	}
	return row, true
}

// SafeSliceMap is a drop-in replacement for Iter.SliceMap.
func SafeSliceMap(iter *gocql.Iter) ([]map[string]interface{}, error) {
	rows := make([]map[string]interface{}, 0)
	for {
		row, ok := SafeMapScan(iter)
		if !ok {
			break
		}
		rows = append(rows, row)
	}
	return rows, iter.Close()
}
