// Package jsonvalue is the SDK's own JSON value, reader and writer. The standard encoding/json
// cannot be used for flags and contexts: it errors on 1e999 (the spec reads it as Infinity) and it
// replaces a lone surrogate with U+FFFD (the spec keeps it). Strings here are WTF-8, so a lone
// surrogate survives; numbers are IEEE-754 binary64.
package jsonvalue

import (
	"math"
	"strconv"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// Kind is the JSON type of a Value.
type Kind uint8

// The kinds of a Value. The zero Value is Null.
const (
	Null Kind = iota
	Bool
	Number
	String
	Array
	Object
)

// Value is one JSON value. The zero Value is null. A Value is immutable by convention: nothing in
// the SDK changes one it was given.
type Value struct {
	kind Kind
	b    bool
	num  float64
	str  string
	arr  []Value
	obj  map[string]Value
}

// NewBool returns a boolean Value.
func NewBool(b bool) Value { return Value{kind: Bool, b: b} }

// NewNumber returns a number Value. Infinity is allowed: JSON spells it 1e999.
func NewNumber(f float64) Value { return Value{kind: Number, num: f} }

// NewString returns a string Value. s should be valid UTF-8 or WTF-8.
func NewString(s string) Value { return Value{kind: String, str: s} }

// NewArray returns an array Value over items.
func NewArray(items []Value) Value { return Value{kind: Array, arr: items} }

// NewObject returns an object Value over members.
func NewObject(members map[string]Value) Value { return Value{kind: Object, obj: members} }

// Kind reports the JSON type.
func (v Value) Kind() Kind { return v.kind }

// IsNull reports whether v is null (which is also what an absent member reads as).
func (v Value) IsNull() bool { return v.kind == Null }

// Bool returns the boolean and whether v is one.
func (v Value) Bool() (bool, bool) { return v.b, v.kind == Bool }

// Number returns the number and whether v is one.
func (v Value) Number() (float64, bool) { return v.num, v.kind == Number }

// String returns the string (WTF-8) and whether v is one.
func (v Value) String() (string, bool) { return v.str, v.kind == String }

// Array returns the items and whether v is an array. The slice is shared: do not change it.
func (v Value) Array() ([]Value, bool) { return v.arr, v.kind == Array }

// Object returns the members and whether v is an object. The map is shared: do not change it.
func (v Value) Object() (map[string]Value, bool) { return v.obj, v.kind == Object }

// Get returns the member of an object named key, or null when v is not an object or has no such
// member. Only the object's own members count.
func (v Value) Get(key string) Value {
	if v.kind != Object {
		return Value{}
	}
	return v.obj[key]
}

// Equal is deep equality as the spec means it: numbers by IEEE-754 (so -0 equals 0), strings by
// code unit, objects by member set.
func Equal(a, b Value) bool {
	if a.kind != b.kind {
		return false
	}
	switch a.kind {
	case Null:
		return true
	case Bool:
		return a.b == b.b
	case Number:
		return a.num == b.num
	case String:
		return a.str == b.str
	case Array:
		if len(a.arr) != len(b.arr) {
			return false
		}
		for i := range a.arr {
			if !Equal(a.arr[i], b.arr[i]) {
				return false
			}
		}
		return true
	default:
		if len(a.obj) != len(b.obj) {
			return false
		}
		for k, av := range a.obj {
			bv, ok := b.obj[k]
			if !ok || !Equal(av, bv) {
				return false
			}
		}
		return true
	}
}

// From builds a Value from native Go values: nil, bool, every integer and float type, string,
// []any, []string, map[string]any, map[string]string and Value. Any other type becomes null.
func From(x any) Value {
	switch t := x.(type) {
	case nil:
		return Value{}
	case Value:
		return t
	case bool:
		return NewBool(t)
	case string:
		return NewString(t)
	case float64:
		return NewNumber(t)
	case float32:
		return NewNumber(float64(t))
	case int:
		return NewNumber(float64(t))
	case int8:
		return NewNumber(float64(t))
	case int16:
		return NewNumber(float64(t))
	case int32:
		return NewNumber(float64(t))
	case int64:
		return NewNumber(float64(t))
	case uint:
		return NewNumber(float64(t))
	case uint8:
		return NewNumber(float64(t))
	case uint16:
		return NewNumber(float64(t))
	case uint32:
		return NewNumber(float64(t))
	case uint64:
		return NewNumber(float64(t))
	case []any:
		items := make([]Value, len(t))
		for i, item := range t {
			items[i] = From(item)
		}
		return NewArray(items)
	case []string:
		items := make([]Value, len(t))
		for i, item := range t {
			items[i] = NewString(item)
		}
		return NewArray(items)
	case map[string]any:
		members := make(map[string]Value, len(t))
		for k, item := range t {
			members[k] = From(item)
		}
		return NewObject(members)
	case map[string]string:
		members := make(map[string]Value, len(t))
		for k, item := range t {
			members[k] = NewString(item)
		}
		return NewObject(members)
	}
	return Value{}
}

// Marshal writes v as JSON text that is valid UTF-8: a lone surrogate is written as a \uXXXX
// escape, and an infinite number as 1e999 or -1e999, so Parse reads back what was written.
func Marshal(v Value) []byte { return Append(nil, v) }

// Append appends the JSON text of v to dst.
func Append(dst []byte, v Value) []byte {
	switch v.kind {
	case Bool:
		return strconv.AppendBool(dst, v.b)
	case Number:
		return appendNumber(dst, v.num)
	case String:
		return appendString(dst, v.str)
	case Array:
		dst = append(dst, '[')
		for i, item := range v.arr {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = Append(dst, item)
		}
		return append(dst, ']')
	case Object:
		dst = append(dst, '{')
		first := true
		for k, item := range v.obj {
			if !first {
				dst = append(dst, ',')
			}
			first = false
			dst = appendString(dst, k)
			dst = append(dst, ':')
			dst = Append(dst, item)
		}
		return append(dst, '}')
	}
	return append(dst, "null"...)
}

func appendNumber(dst []byte, f float64) []byte {
	switch {
	case math.IsInf(f, 1):
		return append(dst, "1e999"...)
	case math.IsInf(f, -1):
		return append(dst, "-1e999"...)
	case math.IsNaN(f):
		return append(dst, "null"...)
	}
	if f == 0 && math.Signbit(f) {
		return append(dst, "-0"...)
	}
	return strconv.AppendFloat(dst, f, 'g', -1, 64)
}

const hex = "0123456789abcdef"

func appendString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	for len(s) > 0 {
		r, size := wtf8.Decode(s)
		raw := s[:size]
		s = s[size:]
		switch {
		case r == '"':
			dst = append(dst, '\\', '"')
		case r == '\\':
			dst = append(dst, '\\', '\\')
		case r == '\n':
			dst = append(dst, '\\', 'n')
		case r == '\r':
			dst = append(dst, '\\', 'r')
		case r == '\t':
			dst = append(dst, '\\', 't')
		case r < 0x20 || (r >= 0xD800 && r <= 0xDFFF):
			dst = append(dst, '\\', 'u', hex[(r>>12)&0xF], hex[(r>>8)&0xF], hex[(r>>4)&0xF], hex[r&0xF])
		case r == 0xFFFD && size == 1:
			dst = append(dst, "\xEF\xBF\xBD"...)
		default:
			dst = append(dst, raw...)
		}
	}
	return append(dst, '"')
}
