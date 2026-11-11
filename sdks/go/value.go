package toggleslash

import (
	"math"
	"slices"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// Value is a JSON value: null, a boolean, a number (an IEEE-754 double), a string, an array or an object.
// Strings may hold a lone surrogate, which a JSON escape such as "\ud800" can produce and a Go string
// cannot normally carry; the client keeps it as it came.
type Value = jsonv.Value

// Context is the context a flag is evaluated for: a single context or a multi context, as a Value
// (docs/evaluation.md, "Context"). A context with no "kind" member is a user.
type Context = Value

// Member is one key and value of an object.
type Member struct {
	Key   string
	Value Value
}

// Null is the JSON null.
func Null() Value { return jsonv.NullValue() }

// Bool is a JSON boolean.
func Bool(b bool) Value { return jsonv.BoolValue(b) }

// Number is a JSON number.
func Number(n float64) Value { return jsonv.NumberValue(n) }

// String is a JSON string. Invalid UTF-8 in s becomes U+FFFD.
func String(s string) Value { return jsonv.StringValue(s) }

// Array is a JSON array.
func Array(items ...Value) Value { return jsonv.ArrayValue(items...) }

// Object is a JSON object with its members in the order given; a repeated key keeps the last value.
func Object(members ...Member) Value {
	m := jsonv.NewMembers()
	for _, member := range members {
		m.Set(jsonv.SanitizeWTF8(member.Key), member.Value)
	}
	return jsonv.ObjectValue(m)
}

// ParseJSON reads one JSON text. It keeps what encoding/json cannot: a number that overflows (1e999 is
// +Inf) and a lone surrogate in an escape. Bytes that are not valid UTF-8 become U+FFFD.
func ParseJSON(data []byte) (Value, error) { return jsonv.Parse(data) }

// ValueOf converts the usual Go types to a Value: nil, bool, string, every integer and float type, Value,
// []any, []string, []Value, map[string]any and map[string]Value. Map keys are ordered by key, since a Go
// map has no order. Anything else becomes null.
func ValueOf(x any) Value {
	switch v := x.(type) {
	case nil:
		return Null()
	case Value:
		return v
	case bool:
		return Bool(v)
	case string:
		return String(v)
	case int:
		return Number(float64(v))
	case int8:
		return Number(float64(v))
	case int16:
		return Number(float64(v))
	case int32:
		return Number(float64(v))
	case int64:
		return Number(float64(v))
	case uint:
		return Number(float64(v))
	case uint8:
		return Number(float64(v))
	case uint16:
		return Number(float64(v))
	case uint32:
		return Number(float64(v))
	case uint64:
		return Number(float64(v))
	case float32:
		return Number(float64(v))
	case float64:
		return Number(v)
	case []any:
		items := make([]Value, len(v))
		for i, item := range v {
			items[i] = ValueOf(item)
		}
		return Array(items...)
	case []string:
		items := make([]Value, len(v))
		for i, item := range v {
			items[i] = String(item)
		}
		return Array(items...)
	case []Value:
		return Array(v...)
	case map[string]any:
		m := make(map[string]Value, len(v))
		for key, item := range v {
			m[key] = ValueOf(item)
		}
		return objectOf(m)
	case map[string]Value:
		return objectOf(v)
	}
	return Null()
}

func objectOf(m map[string]Value) Value {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	members := make([]Member, len(keys))
	for i, key := range keys {
		members[i] = Member{Key: key, Value: m[key]}
	}
	return Object(members...)
}

// intFromValue is the integer accessor rule of docs/sdk-contract.md section 4: a served number is an
// integer when it is finite, has no fractional part (2.0 is 2) and lies in [-2^63, 2^63).
func intFromValue(v Value) (int64, bool) {
	f, ok := v.AsNumber()
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) || math.Trunc(f) != f {
		return 0, false
	}
	if f < -9223372036854775808 || f >= 9223372036854775808 {
		return 0, false
	}
	return int64(f), true
}
