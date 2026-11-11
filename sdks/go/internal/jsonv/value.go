// Package jsonv is the SDK's JSON value, reader and writer. It exists because encoding/json cannot keep
// what the evaluation spec needs: 1e999 as +Inf, and a lone surrogate in a string.
package jsonv

import "math"

// Kind is the JSON kind of a Value.
type Kind uint8

// The kinds. The zero Value is Null.
const (
	Null Kind = iota
	Bool
	Number
	String
	Array
	Object
)

// String names the kind.
func (k Kind) String() string {
	switch k {
	case Bool:
		return "boolean"
	case Number:
		return "number"
	case String:
		return "string"
	case Array:
		return "array"
	case Object:
		return "object"
	}
	return "null"
}

// Value is one JSON value. The zero Value is null. A Value is immutable by convention: the slices and maps
// inside are shared between copies and nothing in this module writes to them after construction.
type Value struct {
	kind Kind
	b    bool
	n    float64
	s    string
	a    []Value
	o    map[string]Value
}

// Kind returns the kind.
func (v Value) Kind() Kind { return v.kind }

// IsNull reports whether v is null.
func (v Value) IsNull() bool { return v.kind == Null }

// NewBool returns a boolean.
func NewBool(b bool) Value { return Value{kind: Bool, b: b} }

// NewNumber returns a number.
func NewNumber(n float64) Value { return Value{kind: Number, n: n} }

// NewString returns a string. s is WTF-8: a lone surrogate may be present as its three byte form.
func NewString(s string) Value { return Value{kind: String, s: s} }

// NewArray returns an array. The slice is kept, not copied.
func NewArray(a []Value) Value { return Value{kind: Array, a: a} }

// NewObject returns an object. The map is kept, not copied.
func NewObject(o map[string]Value) Value { return Value{kind: Object, o: o} }

// Bool returns the boolean and whether v is one.
func (v Value) Bool() (bool, bool) { return v.b, v.kind == Bool }

// Number returns the number and whether v is one.
func (v Value) Number() (float64, bool) { return v.n, v.kind == Number }

// Str returns the string and whether v is one.
func (v Value) Str() (string, bool) { return v.s, v.kind == String }

// Array returns the elements and whether v is an array.
func (v Value) Array() ([]Value, bool) { return v.a, v.kind == Array }

// Object returns the members and whether v is an object.
func (v Value) Object() (map[string]Value, bool) { return v.o, v.kind == Object }

// Get returns the member of an object by key.
func (v Value) Get(key string) (Value, bool) {
	if v.kind != Object {
		return Value{}, false
	}
	m, ok := v.o[key]
	return m, ok
}

// Equal is the spec's equality: kinds must match, numbers compare by IEEE-754 (so -0 equals 0 and NaN
// equals nothing), strings by code points, arrays element by element, objects by key set and member.
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
	case Object:
		if len(a.o) != len(b.o) {
			return false
		}
		for k, av := range a.o {
			bv, ok := b.o[k]
			if !ok || !Equal(av, bv) {
				return false
			}
		}
		return true
	}
	return false
}

// IsFinite reports whether v is a number that is neither infinite nor NaN.
func (v Value) IsFinite() bool {
	return v.kind == Number && !math.IsInf(v.n, 0) && !math.IsNaN(v.n)
}
