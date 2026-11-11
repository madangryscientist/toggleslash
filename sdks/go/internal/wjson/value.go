// Package wjson reads and writes JSON the way evaluation.md needs it read:
// numbers are binary64 (1e999 is infinity, -0 is kept), a lone surrogate
// escape survives (strings are WTF-8, see package wtf8), a duplicate key keeps
// the last value, and nothing is converted to a Go interface{}.
package wjson

import (
	"math"
)

// Kind is the JSON kind of a Value.
type Kind uint8

// The six JSON kinds. The zero Value is KindNull.
const (
	KindNull Kind = iota
	KindBool
	KindNumber
	KindString
	KindArray
	KindObject
)

func (k Kind) String() string {
	switch k {
	case KindNull:
		return "null"
	case KindBool:
		return "boolean"
	case KindNumber:
		return "number"
	case KindString:
		return "string"
	case KindArray:
		return "array"
	default:
		return "object"
	}
}

// Value is an immutable JSON value. The zero Value is null.
type Value struct {
	kind Kind
	b    bool
	n    float64
	s    string
	a    []Value
	o    *Object
}

// Null returns the JSON null.
func Null() Value { return Value{} }

// Bool returns a JSON boolean.
func Bool(b bool) Value { return Value{kind: KindBool, b: b} }

// Number returns a JSON number. Infinity and NaN are allowed in memory.
func Number(n float64) Value { return Value{kind: KindNumber, n: n} }

// String returns a JSON string; s is WTF-8.
func String(s string) Value { return Value{kind: KindString, s: s} }

// Array returns a JSON array. The slice is not copied.
func Array(a []Value) Value { return Value{kind: KindArray, a: a} }

// ObjectValue returns a JSON object. The object is not copied.
func ObjectValue(o *Object) Value {
	if o == nil {
		o = NewObject()
	}
	return Value{kind: KindObject, o: o}
}

// Kind returns the kind.
func (v Value) Kind() Kind { return v.kind }

// IsNull reports whether v is null.
func (v Value) IsNull() bool { return v.kind == KindNull }

// Bool returns the boolean, or false if v is not one.
func (v Value) Bool() bool { return v.kind == KindBool && v.b }

// Number returns the number, or 0 if v is not one.
func (v Value) Number() float64 {
	if v.kind == KindNumber {
		return v.n
	}
	return 0
}

// String returns the string, or "" if v is not one.
func (v Value) String() string {
	if v.kind == KindString {
		return v.s
	}
	return ""
}

// Array returns the elements, or nil if v is not an array. Do not modify it.
func (v Value) Array() []Value {
	if v.kind == KindArray {
		return v.a
	}
	return nil
}

// Object returns the object, or nil if v is not one. Do not modify it.
func (v Value) Object() *Object {
	if v.kind == KindObject {
		return v.o
	}
	return nil
}

// IsFinite reports whether v is a number that is neither infinite nor NaN.
func (v Value) IsFinite() bool {
	return v.kind == KindNumber && !math.IsInf(v.n, 0) && !math.IsNaN(v.n)
}

// Equal is the JSON equality of the spec's vectors: numbers by IEEE-754 (so -0
// equals 0), objects by key set regardless of order.
func (v Value) Equal(w Value) bool {
	if v.kind != w.kind {
		return false
	}
	switch v.kind {
	case KindNull:
		return true
	case KindBool:
		return v.b == w.b
	case KindNumber:
		return v.n == w.n
	case KindString:
		return v.s == w.s
	case KindArray:
		if len(v.a) != len(w.a) {
			return false
		}
		for i := range v.a {
			if !v.a[i].Equal(w.a[i]) {
				return false
			}
		}
		return true
	default:
		if v.o.Len() != w.o.Len() {
			return false
		}
		for _, k := range v.o.keys {
			x, ok := w.o.Get(k)
			if !ok || !v.o.m[k].Equal(x) {
				return false
			}
		}
		return true
	}
}

// Object is a JSON object that remembers the order its keys first appeared in.
type Object struct {
	keys []string
	m    map[string]Value
}

// NewObject returns an empty object.
func NewObject() *Object { return &Object{m: map[string]Value{}} }

// Set sets key to v. An existing key keeps its position and takes the new value.
func (o *Object) Set(key string, v Value) {
	if _, ok := o.m[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.m[key] = v
}

// Get returns the value for key.
func (o *Object) Get(key string) (Value, bool) {
	if o == nil {
		return Value{}, false
	}
	v, ok := o.m[key]
	return v, ok
}

// Has reports whether key is present.
func (o *Object) Has(key string) bool {
	_, ok := o.Get(key)
	return ok
}

// Len is the number of keys.
func (o *Object) Len() int {
	if o == nil {
		return 0
	}
	return len(o.keys)
}

// Keys returns the keys in first-seen order. Do not modify the slice.
func (o *Object) Keys() []string {
	if o == nil {
		return nil
	}
	return o.keys
}

// Clone returns a shallow copy.
func (o *Object) Clone() *Object {
	c := &Object{keys: append([]string(nil), o.keys...), m: make(map[string]Value, len(o.m))}
	for k, v := range o.m {
		c.m[k] = v
	}
	return c
}
