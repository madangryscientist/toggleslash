package toggleslash

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

// ValueKind is the JSON type of a Value.
type ValueKind string

// The JSON types.
const (
	KindNull    ValueKind = "null"
	KindBoolean ValueKind = "boolean"
	KindNumber  ValueKind = "number"
	KindString  ValueKind = "string"
	KindArray   ValueKind = "array"
	KindObject  ValueKind = "object"
)

// Value is one JSON value: a flag's served value, a JSON fallback, or a context attribute. The zero Value is
// null. Numbers are binary64, so 1e999 is +Inf and a number above 2^53 is rounded, as the spec says.
type Value struct{ v jsonvalue.Value }

// NullValue is JSON null.
func NullValue() Value { return Value{} }

// BoolValue is a JSON boolean.
func BoolValue(b bool) Value { return Value{jsonvalue.Boolean(b)} }

// NumberValue is a JSON number.
func NumberValue(n float64) Value { return Value{jsonvalue.Num(n)} }

// StringValue is a JSON string.
func StringValue(s string) Value { return Value{jsonvalue.Str(s)} }

// ArrayValue is a JSON array.
func ArrayValue(items ...Value) Value {
	inner := make([]jsonvalue.Value, len(items))
	for i, item := range items {
		inner[i] = item.v
	}
	return Value{jsonvalue.List(inner)}
}

// ObjectValue is a JSON object made from members, in the order of keys.
func ObjectValue(keys []string, members map[string]Value) Value {
	obj := jsonvalue.NewObject()
	for _, k := range keys {
		obj.Set(k, members[k].v)
	}
	return Value{obj}
}

// ParseValue reads JSON text. It accepts 1e999 and keeps a lone surrogate escape.
func ParseValue(text string) (Value, error) {
	v, err := jsonvalue.Parse(text)
	if err != nil {
		return Value{}, err
	}
	return Value{v}, nil
}

// Kind is the JSON type of v.
func (v Value) Kind() ValueKind {
	switch v.v.Kind {
	case jsonvalue.Bool:
		return KindBoolean
	case jsonvalue.Number:
		return KindNumber
	case jsonvalue.String:
		return KindString
	case jsonvalue.Array:
		return KindArray
	case jsonvalue.Object:
		return KindObject
	default:
		return KindNull
	}
}

// IsNull reports whether v is null.
func (v Value) IsNull() bool { return v.v.Kind == jsonvalue.Null }

// Bool returns the boolean and true when v is one.
func (v Value) Bool() (bool, bool) { return v.v.B, v.v.Kind == jsonvalue.Bool }

// Float returns the number and true when v is one.
func (v Value) Float() (float64, bool) { return v.v.N, v.v.Kind == jsonvalue.Number }

// String returns the string and true when v is one. A lone surrogate is returned as WTF-8 bytes.
func (v Value) String() (string, bool) { return v.v.S, v.v.Kind == jsonvalue.String }

// Len is the number of elements of an array or members of an object, else 0.
func (v Value) Len() int {
	switch v.v.Kind {
	case jsonvalue.Array:
		return len(v.v.A)
	case jsonvalue.Object:
		return len(v.v.O.Keys)
	default:
		return 0
	}
}

// Index returns element i of an array, and false when v is not an array or i is out of range.
func (v Value) Index(i int) (Value, bool) {
	if v.v.Kind != jsonvalue.Array || i < 0 || i >= len(v.v.A) {
		return Value{}, false
	}
	return Value{v.v.A[i]}, true
}

// Get returns the member of an object called name.
func (v Value) Get(name string) (Value, bool) {
	m, ok := v.v.Get(name)
	return Value{m}, ok
}

// Keys returns the member names of an object in the order they first appeared.
func (v Value) Keys() []string {
	if v.v.Kind != jsonvalue.Object {
		return nil
	}
	return append([]string(nil), v.v.O.Keys...)
}

// JSON writes v as JSON text. A number that is not finite is written as 1e999 or -1e999 so it reads back.
func (v Value) JSON() string { return jsonvalue.Write(v.v) }

// Equal reports whether two values are equal as the spec compares them (deeply; numbers by value).
func (v Value) Equal(other Value) bool { return jsonvalue.Equal(v.v, other.v) }

// wholeInt64 reads a number as an int64 when it has no fractional part and is inside the signed 64-bit range.
func wholeInt64(n float64) (int64, bool) {
	if math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) {
		return 0, false
	}
	if n < -9223372036854775808.0 || n >= 9223372036854775808.0 {
		return 0, false
	}
	return int64(n), true
}
