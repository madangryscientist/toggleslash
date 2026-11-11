package jsonvalue

import (
	"fmt"
	"reflect"
	"slices"
	"unicode/utf8"
)

// FromGo converts a plain Go value to a Value: nil, bool, string, every integer and float type, []any,
// map[string]any, []string, []Value and Value itself. Anything else is an error that names the Go type
// and never shows the value. A string that is not valid UTF-8 has each invalid byte replaced by U+FFFD.
func FromGo(x any) (Value, error) {
	switch t := x.(type) {
	case nil:
		return Null(), nil
	case Value:
		return t, nil
	case bool:
		return Bool(t), nil
	case string:
		return String(cleanString(t)), nil
	case float64:
		return Number(t), nil
	case float32:
		return Number(float64(t)), nil
	case int:
		return Number(float64(t)), nil
	case int8:
		return Number(float64(t)), nil
	case int16:
		return Number(float64(t)), nil
	case int32:
		return Number(float64(t)), nil
	case int64:
		return Number(float64(t)), nil
	case uint:
		return Number(float64(t)), nil
	case uint8:
		return Number(float64(t)), nil
	case uint16:
		return Number(float64(t)), nil
	case uint32:
		return Number(float64(t)), nil
	case uint64:
		return Number(float64(t)), nil
	case []string:
		items := make([]Value, len(t))
		for i, s := range t {
			items[i] = String(cleanString(s))
		}
		return Array(items), nil
	case []Value:
		return Array(t), nil
	case []any:
		items := make([]Value, len(t))
		for i, e := range t {
			v, err := FromGo(e)
			if err != nil {
				return Value{}, err
			}
			items[i] = v
		}
		return Array(items), nil
	case map[string]any:
		o := NewObject()
		for _, k := range sortedKeys(t) {
			v, err := FromGo(t[k])
			if err != nil {
				return Value{}, err
			}
			o.Set(cleanString(k), v)
		}
		return ObjectValue(o), nil
	}
	return Value{}, fmt.Errorf("toggleslash: unsupported attribute type %s", reflect.TypeOf(x))
}

func cleanString(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return string(DecodeLossy([]byte(s)))
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
