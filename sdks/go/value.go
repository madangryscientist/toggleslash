package toggleslash

import (
	"math"
	"sort"
	"unicode/utf8"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wjson"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// Value is a JSON value: null, boolean, number (a float64), string, array or
// object. Its zero value is null. A string may hold a lone surrogate, which
// Go's own string type cannot say but JSON can.
type Value = wjson.Value

// Object is a JSON object that keeps the order its keys were set in.
type Object = wjson.Object

// The kinds of a Value.
const (
	KindNull   = wjson.KindNull
	KindBool   = wjson.KindBool
	KindNumber = wjson.KindNumber
	KindString = wjson.KindString
	KindArray  = wjson.KindArray
	KindObject = wjson.KindObject
)

// NullValue is the JSON null.
func NullValue() Value { return wjson.Null() }

// BoolValue is a JSON boolean.
func BoolValue(b bool) Value { return wjson.Bool(b) }

// NumberValue is a JSON number.
func NumberValue(n float64) Value { return wjson.Number(n) }

// StringValue is a JSON string. Bytes that are not UTF-8 become U+FFFD.
func StringValue(s string) Value { return wjson.String(cleanString(s)) }

// ArrayValue is a JSON array.
func ArrayValue(items ...Value) Value { return wjson.Array(items) }

// NewObject makes an empty JSON object; Set fills it.
func NewObject() *Object { return wjson.NewObject() }

// ObjectValue wraps an object as a Value.
func ObjectValue(o *Object) Value { return wjson.ObjectValue(o) }

// ParseValue reads JSON the way the server's rulesets are read: a leading byte
// order mark is skipped, invalid UTF-8 becomes U+FFFD, a duplicate key keeps
// the last value, 1e999 is infinity and a lone surrogate escape is kept.
func ParseValue(data []byte) (Value, error) { return wjson.Parse(data) }

// MarshalValue writes v as JSON. A number that is not finite is written as null.
func MarshalValue(v Value) []byte { return wjson.Marshal(v) }

// cleanString makes a Go string safe to hold in a Value: valid UTF-8 as it is,
// anything else with U+FFFD for the bad bytes.
func cleanString(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return wtf8.DecodeUTF8([]byte(s))
}

// valueOf converts a Go value to a Value: nil, bool, every integer and float
// type, string, Value, *Object, []any, []string, []Value and map[string]any.
// It reports false for anything else.
func valueOf(x any) (Value, bool) {
	switch v := x.(type) {
	case nil:
		return NullValue(), true
	case Value:
		return v, true
	case *Object:
		if v == nil {
			return NullValue(), true
		}
		return ObjectValue(v), true
	case bool:
		return BoolValue(v), true
	case string:
		return StringValue(v), true
	case int:
		return NumberValue(float64(v)), true
	case int8:
		return NumberValue(float64(v)), true
	case int16:
		return NumberValue(float64(v)), true
	case int32:
		return NumberValue(float64(v)), true
	case int64:
		return NumberValue(float64(v)), true
	case uint:
		return NumberValue(float64(v)), true
	case uint8:
		return NumberValue(float64(v)), true
	case uint16:
		return NumberValue(float64(v)), true
	case uint32:
		return NumberValue(float64(v)), true
	case uint64:
		return NumberValue(float64(v)), true
	case float32:
		return NumberValue(float64(v)), true
	case float64:
		return NumberValue(v), true
	case []Value:
		return ArrayValue(v...), true
	case []string:
		items := make([]Value, len(v))
		for i, s := range v {
			items[i] = StringValue(s)
		}
		return ArrayValue(items...), true
	case []any:
		items := make([]Value, len(v))
		for i, item := range v {
			converted, ok := valueOf(item)
			if !ok {
				return Value{}, false
			}
			items[i] = converted
		}
		return ArrayValue(items...), true
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		o := NewObject()
		for _, k := range keys {
			converted, ok := valueOf(v[k])
			if !ok {
				return Value{}, false
			}
			o.Set(cleanString(k), converted)
		}
		return ObjectValue(o), true
	}
	return Value{}, false
}

// integerOf reads a served number as an int64: it must be finite, have no
// fractional part and sit inside the signed 64-bit range. 2.0 is 2.
func integerOf(n float64) (int64, bool) {
	if math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) {
		return 0, false
	}
	if n < -9223372036854775808 || n >= 9223372036854775808 {
		return 0, false
	}
	return int64(n), true
}
