package main

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// isTupleType reports whether cqlType is a tuple, which cannot be bound.
func isTupleType(cqlType string) bool {
	return baseCQLType(cqlType) == TupleType
}

// quoteCQLString renders a CQL string literal. A single quote is escaped by
// doubling it, which is the only escape CQL string literals have.
func quoteCQLString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// literal renders a value as CQL source text instead of a bind parameter.
// It exists for tuples: gocql v1.3.1 counts a tuple as one bind value per
// element but keeps a single column entry for it, so binding one either reports
// the wrong value count or indexes past its own column slice and panics.
// Inlining the tuple keeps the statement's remaining columns on the bind path.
func (c converter) literal(cqlType string, v interface{}) (string, error) {
	if isTupleType(cqlType) {
		return c.tupleLiteral(cqlType, v)
	}

	converted, err := c.value(cqlType, v)
	if err != nil {
		return "", err
	}
	if converted == nil {
		return "null", nil
	}

	switch baseCQLType(cqlType) {
	case TextType, VarcharType, AsciiType, InetType, DateType:
		s, ok := converted.(string)
		if !ok {
			return "", fmt.Errorf("cannot render %T as %s", converted, cqlType)
		}
		return quoteCQLString(s), nil

	case UuidType, TimeuuidType:
		// UUID literals are unquoted in CQL, so the text has to be a UUID and
		// nothing else or it would be pasted into the statement verbatim.
		s, _ := converted.(string)
		if !isUUID(s) {
			return "", fmt.Errorf("%q is not a valid uuid", s)
		}
		return s, nil

	case BlobType:
		b, ok := converted.([]byte)
		if !ok {
			return "", fmt.Errorf("cannot render %T as a blob", converted)
		}
		return "0x" + hex.EncodeToString(b), nil

	case BooleanType, TinyintType, SmallintType, IntType, BigintType, CounterType,
		FloatType, DoubleType, DecimalType, VarintType, TimestampType, TimeType:
		return fmt.Sprintf("%v", converted), nil
	}

	return "", fmt.Errorf("cannot render %s as a literal", cqlType)
}

func (c converter) tupleLiteral(cqlType string, v interface{}) (string, error) {
	args := collectionArgs(cqlType)
	elems, ok := v.([]interface{})
	if !ok {
		return "", fmt.Errorf("cannot read %T as a tuple", v)
	}
	if len(elems) != len(args) {
		return "", fmt.Errorf("tuple needs %d elements, got %d", len(args), len(elems))
	}

	parts := make([]string, 0, len(elems))
	for i, elem := range elems {
		switch baseCQLType(args[i]) {
		case ListType, SetType, MapType, TupleType:
			return "", fmt.Errorf("element %d: a %s inside a tuple is not supported", i, baseCQLType(args[i]))
		}

		part, err := c.literal(args[i], elem)
		if err != nil {
			return "", fmt.Errorf("element %d: %w", i, err)
		}
		parts = append(parts, part)
	}
	return "(" + strings.Join(parts, ",") + ")", nil
}

// isUUID reports the canonical 8-4-4-4-12 hexadecimal form.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
}
