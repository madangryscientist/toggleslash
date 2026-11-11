// Package jsonvalue is the SDK's own JSON value, reader and writer.
//
// encoding/json cannot carry what docs/evaluation.md requires: it reports an error for 1e999 and replaces
// a lone surrogate with U+FFFD. Here numbers are binary64 (an overflow is an infinity, not an error) and a
// string is held as WTF-8: ordinary UTF-8, except that a lone surrogate is its own three-byte encoding.
// A pair of escapes that spells a valid pair is always one four-byte code point, so the form is canonical
// and two strings are equal exactly when their bytes are.
package jsonvalue

import (
	"math"
)

// Kind is the JSON type of a Value.
type Kind uint8

// The six JSON types. The zero Kind is null, so the zero Value is null.
const (
	KindNull Kind = iota
	KindBool
	KindNumber
	KindString
	KindArray
	KindObject
)

// smallObject is the size up to which an Object finds a key by scanning its keys instead of a map.
const smallObject = 8

// Value is one JSON value. The zero Value is null. A Value is immutable once built: nothing in the SDK
// changes one after it is shared.
type Value struct {
	kind Kind
	b    bool
	n    float64
	s    string
	a    []Value
	o    *Object
}

// Null is the JSON null.
func Null() Value { return Value{} }

// Bool is a JSON boolean.
func Bool(b bool) Value { return Value{kind: KindBool, b: b} }

// Number is a JSON number. Any float64 is allowed, including an infinity: the reader makes one from 1e999.
func Number(n float64) Value { return Value{kind: KindNumber, n: n} }

// String is a JSON string. The text is taken as it is, so it must already be WTF-8 (ordinary UTF-8 from
// a Go string literal always is).
func String(s string) Value { return Value{kind: KindString, s: s} }

// Array is a JSON array. The slice is kept, not copied.
func Array(items []Value) Value { return Value{kind: KindArray, a: items} }

// ObjectValue is a JSON object. The object is kept, not copied.
func ObjectValue(o *Object) Value { return Value{kind: KindObject, o: o} }

// Kind reports the JSON type.
func (v Value) Kind() Kind { return v.kind }

// IsNull reports whether the value is null (the zero Value is).
func (v Value) IsNull() bool { return v.kind == KindNull }

// IsString reports whether the value is a string.
func (v Value) IsString() bool { return v.kind == KindString }

// IsNumber reports whether the value is a number, finite or not.
func (v Value) IsNumber() bool { return v.kind == KindNumber }

// IsBool reports whether the value is a boolean.
func (v Value) IsBool() bool { return v.kind == KindBool }

// IsArray reports whether the value is an array.
func (v Value) IsArray() bool { return v.kind == KindArray }

// IsObject reports whether the value is an object.
func (v Value) IsObject() bool { return v.kind == KindObject }

// IsFiniteNumber reports whether the value is a number that is neither an infinity nor NaN.
func (v Value) IsFiniteNumber() bool {
	return v.kind == KindNumber && !math.IsInf(v.n, 0) && !math.IsNaN(v.n)
}

// BoolValue is the boolean, false for any other kind.
func (v Value) BoolValue() bool { return v.kind == KindBool && v.b }

// NumberValue is the number, 0 for any other kind.
func (v Value) NumberValue() float64 {
	if v.kind != KindNumber {
		return 0
	}
	return v.n
}

// StringValue is the string, empty for any other kind. It is WTF-8.
func (v Value) StringValue() string {
	if v.kind != KindString {
		return ""
	}
	return v.s
}

// Items is the elements of an array, nil for any other kind. Callers must not change the slice.
func (v Value) Items() []Value {
	if v.kind != KindArray {
		return nil
	}
	return v.a
}

// Object is the object, nil for any other kind.
func (v Value) Object() *Object {
	if v.kind != KindObject {
		return nil
	}
	return v.o
}

// Get reads an own property of an object. The second result is false for any other kind and for a
// missing property. A property whose value is null is present.
func (v Value) Get(name string) (Value, bool) {
	if v.kind != KindObject {
		return Value{}, false
	}
	return v.o.Get(name)
}

// Object is a JSON object: its keys in the order each first appeared, the last value written for each.
type Object struct {
	keys  []string
	vals  []Value
	index map[string]int
}

// NewObject returns an empty object.
func NewObject() *Object { return &Object{} }

// Len is the number of distinct keys.
func (o *Object) Len() int {
	if o == nil {
		return 0
	}
	return len(o.keys)
}

// At is the i-th key and value, in the order each key first appeared.
func (o *Object) At(i int) (string, Value) { return o.keys[i], o.vals[i] }

func (o *Object) find(name string) (int, bool) {
	if o.index != nil {
		i, ok := o.index[name]
		return i, ok
	}
	for i, k := range o.keys {
		if k == name {
			return i, true
		}
	}
	return 0, false
}

// Get reads an own property.
func (o *Object) Get(name string) (Value, bool) {
	if o == nil {
		return Value{}, false
	}
	i, ok := o.find(name)
	if !ok {
		return Value{}, false
	}
	return o.vals[i], true
}

// Set writes a property. A key already present keeps its place and takes the new value, the way
// JSON.parse reads a duplicate key.
func (o *Object) Set(name string, v Value) {
	if i, ok := o.find(name); ok {
		o.vals[i] = v
		return
	}
	o.keys = append(o.keys, name)
	o.vals = append(o.vals, v)
	if o.index != nil {
		o.index[name] = len(o.keys) - 1
		return
	}
	if len(o.keys) > smallObject {
		o.index = make(map[string]int, len(o.keys)*2)
		for i, k := range o.keys {
			o.index[k] = i
		}
	}
}

// Equal compares two values deeply by the spec's rules: numbers by IEEE-754 (so -0 equals 0 and NaN equals
// nothing), strings by their WTF-8 bytes, objects as sets of properties.
func Equal(a, b Value) bool {
	if a.kind != b.kind {
		return false
	}
	switch a.kind {
	case KindNull:
		return true
	case KindBool:
		return a.b == b.b
	case KindNumber:
		return a.n == b.n
	case KindString:
		return a.s == b.s
	case KindArray:
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
		for i := 0; i < a.o.Len(); i++ {
			k, av := a.o.At(i)
			bv, ok := b.o.Get(k)
			if !ok || !Equal(av, bv) {
				return false
			}
		}
		return true
	}
}
