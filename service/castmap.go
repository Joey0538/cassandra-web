package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cast"
)

// fToStringMapStringE flattens any of CQL's map shapes to map[string]string.
//
// This lived as a local patch to vendor/github.com/spf13/cast (added as
// cast.FToStringMapStringE), which meant `go mod vendor` silently deleted it
// and broke the build. Keeping it here instead makes the vendor tree pristine,
// so dependency bumps are routine.

func fToStringMapStringE(i interface{}) (map[string]string, error) {
	var m = map[string]string{}

	switch v := i.(type) {
	case map[string]string:
		return v, nil
	case map[string]interface{}:
		for k, val := range v {
			m[cast.ToString(k)] = cast.ToString(val)
		}
		return m, nil

	case map[string]int64:
		for k, val := range v {
			m[cast.ToString(k)] = cast.ToString(val)
		}
		return m, nil
	case map[int64]int64:
		for k, val := range v {
			m[cast.ToString(k)] = cast.ToString(val)
		}
		return m, nil
	case map[int32]int64:
		for k, val := range v {
			m[cast.ToString(k)] = cast.ToString(val)
		}
		return m, nil
	case map[int16]int64:
		for k, val := range v {
			m[cast.ToString(k)] = cast.ToString(val)
		}
		return m, nil
	case map[int8]int64:
		for k, val := range v {
			m[cast.ToString(k)] = cast.ToString(val)
		}
		return m, nil
	case map[float64]int64:
		for k, val := range v {
			m[cast.ToString(k)] = cast.ToString(val)
		}
		return m, nil
	case map[float32]int64:
		for k, val := range v {
			m[cast.ToString(k)] = cast.ToString(val)
		}
		return m, nil
	case map[bool]int64:
		for k, val := range v {
			m[cast.ToString(k)] = cast.ToString(val)
		}
		return m, nil
	case map[interface{}]string:
		for k, val := range v {
			m[cast.ToString(k)] = cast.ToString(val)
		}
		return m, nil
	case map[interface{}]interface{}:
		for k, val := range v {
			m[cast.ToString(k)] = cast.ToString(val)
		}
		return m, nil
	case string:
		// cast's own unexported helper, inlined.
		err := json.Unmarshal([]byte(v), &m)
		return m, err
	default:
		return m, fmt.Errorf("unable to cast %#v of type %T to map[string]string", i, i)
	}
}
