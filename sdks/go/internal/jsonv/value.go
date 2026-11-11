// Package jsonv is the SDK's own JSON value, reader and writer.
//
// encoding/json cannot be used: it fails on 1e999 (which the spec reads as Infinity) and replaces a lone
// surrogate escape with U+FFFD (which the spec keeps). Strings here are held as WTF-8: UTF-8 in which a
// lone surrogate is written as its three-byte form, which valid UTF-8 never contains.
package jsonv

import "unicode/utf8"

// Kind is the JSON type of a Value.
type Kind uint8

// The six JSON kinds. The zero Value is Null.
const (
	Null Kind = iota
	Bool
	Number
	String
	Array
	Object
)

// Value is one JSON value. The zero Value is null. A Value is immutable once built by this package.
type Value struct {
	kind Kind
	b    bool
	n    float64
	s    string
	a    []Value
	o    *Members
}

// Members is a JSON object: an insertion-ordered map. A duplicate key keeps the last value in the
// position of the first, as JSON.parse does.
type Members struct {
	keys []string
	m    map[string]Value
}

// NewMembers returns an empty object body.
func NewMembers() *Members {
	return &Members{m: map[string]Value{}}
}

// Set stores v under key, keeping the key's first position.
func (o *Members) Set(key string, v Value) {
	if _, ok := o.m[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.m[key] = v
}

// Get reads an own property.
func (o *Members) Get(key string) (Value, bool) {
	if o == nil {
		return Value{}, false
	}
	v, ok := o.m[key]
	return v, ok
}

// Len is the number of properties.
func (o *Members) Len() int {
	if o == nil {
		return 0
	}
	return len(o.keys)
}

// Keys returns the property names in insertion order. The slice must not be changed.
func (o *Members) Keys() []string {
	if o == nil {
		return nil
	}
	return o.keys
}

// NullValue is the JSON null.
func NullValue() Value { return Value{} }

// BoolValue is a JSON boolean.
func BoolValue(b bool) Value { return Value{kind: Bool, b: b} }

// NumberValue is a JSON number, held as binary64. Infinity and NaN are allowed values: a reader can
// produce Infinity from 1e999.
func NumberValue(n float64) Value { return Value{kind: Number, n: n} }

// StringValue is a JSON string. Bytes that are not WTF-8 are replaced as the WHATWG UTF-8 decoder would.
func StringValue(s string) Value { return Value{kind: String, s: SanitizeWTF8(s)} }

// ArrayValue is a JSON array of the given items.
func ArrayValue(items ...Value) Value {
	return Value{kind: Array, a: append([]Value(nil), items...)}
}

// ObjectValue is a JSON object over m. The caller must not change m afterwards.
func ObjectValue(m *Members) Value {
	if m == nil {
		m = NewMembers()
	}
	return Value{kind: Object, o: m}
}

// Kind reports the JSON type.
func (v Value) Kind() Kind { return v.kind }

// IsNull reports whether the value is null.
func (v Value) IsNull() bool { return v.kind == Null }

// AsBool returns the boolean and whether the value is one.
func (v Value) AsBool() (bool, bool) { return v.b, v.kind == Bool }

// AsNumber returns the number and whether the value is one.
func (v Value) AsNumber() (float64, bool) { return v.n, v.kind == Number }

// AsString returns the (WTF-8) string and whether the value is one.
func (v Value) AsString() (string, bool) { return v.s, v.kind == String }

// AsArray returns the items and whether the value is an array. The slice must not be changed.
func (v Value) AsArray() ([]Value, bool) { return v.a, v.kind == Array }

// AsObject returns the members and whether the value is an object.
func (v Value) AsObject() (*Members, bool) { return v.o, v.kind == Object }

// Get reads an own property of an object value; any other kind has none.
func (v Value) Get(key string) (Value, bool) {
	if v.kind != Object {
		return Value{}, false
	}
	return v.o.Get(key)
}

// Equal is deep equality by the spec's rules: numbers by IEEE-754 (so -0 equals 0, and NaN equals
// nothing), strings by their code units, objects by their property sets.
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
		return a.n == b.n
	case String:
		return a.s == b.s
	case Array:
		if len(a.a) != len(b.a) {
			return false
		}
		for i := range a.a {
			if !Equal(a.a[i], b.a[i]) {
				return false
			}
		}
		return true
	default:
		if a.o.Len() != b.o.Len() {
			return false
		}
		for _, k := range a.o.keys {
			other, ok := b.o.m[k]
			if !ok || !Equal(a.o.m[k], other) {
				return false
			}
		}
		return true
	}
}

// SanitizeWTF8 returns s unchanged when it is valid UTF-8, and otherwise s with each maximal invalid
// subpart replaced by U+FFFD, as the WHATWG UTF-8 decoder does. A surrogate written in its three-byte
// form is invalid UTF-8, so a caller's Go string cannot smuggle a lone surrogate in; only a JSON escape
// produces one.
func SanitizeWTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return string(DecodeUTF8([]byte(s)))
}
