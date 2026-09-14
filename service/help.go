package main

import (
	"fmt"
	"os"
	"regexp"

	"github.com/spf13/cast"
)

const (
	AsciiType     = "ascii"
	BigintType    = "bigint"
	BlobType      = "blob"
	BooleanType   = "boolean"
	CounterType   = "counter"
	DateType      = "date"
	DecimalType   = "decimal"
	DoubleType    = "double"
	FloatType     = "float"
	FrozenType    = "frozen"
	InetType      = "inet"
	IntType       = "int"
	ListType      = "list"
	MapType       = "map"
	SetType       = "set"
	SmallintType  = "smallint"
	TextType      = "text"
	TimeType      = "time"
	TimestampType = "timestamp"
	TimeuuidType  = "timeuuid"
	TinyintType   = "tinyint"
	TupleType     = "tuple"
	UuidType      = "uuid"
	VarcharType   = "varchar"
	VarintType    = "varint"
)

// OutputTransformType Map Value 數值轉字串
func OutputTransformType(row map[string]interface{}) map[string]interface{} {
	for k, v := range row {
		switch v.(type) {
		case int64, float64, float32:
			row[k] = cast.ToString(v)
		case []int64:
			row[k] = cast.ToStringSlice(v)
		case map[string]int64:
			val, err := fToStringMapStringE(v)

			if err == nil {
				row[k] = val
			}
		case map[int64]int64:
			val, err := fToStringMapStringE(v)

			if err == nil {
				row[k] = val
			}
		case map[int32]int64:
			val, err := fToStringMapStringE(v)

			if err == nil {
				row[k] = val
			}
		case map[int16]int64:
			val, err := fToStringMapStringE(v)

			if err == nil {
				row[k] = val
			}
		case map[int8]int64:
			val, err := fToStringMapStringE(v)

			if err == nil {
				row[k] = val
			}
		case map[float64]int64:
			val, err := fToStringMapStringE(v)

			if err == nil {
				row[k] = val
			}
		case map[float32]int64:
			val, err := fToStringMapStringE(v)

			if err == nil {
				row[k] = val
			}
		case map[bool]int64:
			val, err := fToStringMapStringE(v)

			if err == nil {
				row[k] = val
			}
		}

	}

	return row
}

// cqlFormatValue converts a filter or primary-key value for binding.
func cqlFormatValue(columnType string, columnVal interface{}) (interface{}, error) {
	return toCQLValue(columnType, columnVal)
}

func cqlFormatWhere(columnName string, operator string) string {
	return fmt.Sprintf("%s %s ?", columnName, operator)
}

var mapReg = regexp.MustCompile(`(?U)^map\<(.+),\s(.+)\>`)
var listReg = regexp.MustCompile(`(?U)^list\<(.+)>`)

// InputTransformType 對應table schema型別作轉換
func InputTransformType(item map[string]interface{}, schema map[string]string) ([]string, []interface{}, []string, error) {
	var (
		itemKey         []string
		itemData        []interface{}
		itemPlaceholder []string
	)

	for k, v := range item {
		val, err := toCQLValue(schema[k], v)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%s: %w", k, err)
		}

		itemData = append(itemData, val)
		itemKey = append(itemKey, k)
		itemPlaceholder = append(itemPlaceholder, "?")
	}

	return itemKey, itemData, itemPlaceholder, nil
}

// ListToCassandraListType list對應cassandra list的型別
func ListToCassandraListType(i interface{}, valType string) (interface{}, error) {
	var l = []interface{}{}

	switch v := i.(type) {
	case []interface{}:
		for _, val := range v {
			valRet, err := CassandraTypeToGoType(val, valType)

			if err != nil {
				return nil, err
			}

			l = append(l, valRet)
		}

		return l, nil
	case string:
		err := JsonStringToObject(v, &l)
		return l, err
	case nil:
		return l, nil
	default:
		return l, fmt.Errorf("unable to cast %#v of type %T to []interface{}", i, i)
	}
}

// MapToCassandraMapType map對應cassandra map的型別
func MapToCassandraMapType(i interface{}, keyType string, valType string) (interface{}, error) {
	var m = map[interface{}]interface{}{}

	switch v := i.(type) {
	case map[string]interface{}:
		for k, val := range v {
			kRet, err := CassandraTypeToGoType(k, keyType)

			if err != nil {
				return nil, err
			}

			valRet, err := CassandraTypeToGoType(val, valType)

			if err != nil {
				return nil, err
			}

			m[kRet] = valRet
		}

		return m, nil
	case string:
		err := JsonStringToObject(v, &m)
		return m, err
	case nil:
		return m, nil
	default:
		return m, fmt.Errorf("unable to cast %#v of type %T to map[string]interface{}", i, i)
	}
}

// CassandraTypeToGoType cassandra的型別轉Go型別
func CassandraTypeToGoType(i interface{}, t string) (interface{}, error) {
	return toCQLValue(t, i)
}

// JsonStringToObject json轉obj
func JsonStringToObject(s string, v interface{}) error {
	data := []byte(s)
	return jsoni.Unmarshal(data, v)
}

// CreateTmpFile 建立暫存檔案
func CreateTmpFile(fpath string) error {
	f, err := os.Create(fpath)

	if err != nil {
		return err
	}

	f.Close()

	if err := os.Chmod(fpath, 0666); err != nil {
		return err
	}

	return nil
}
