// Package jsonvalue is the SDK's JSON: a value type, a parser and a writer.
//
// encoding/json cannot be used: it errors on 1e999 and replaces a lone surrogate with U+FFFD, and the
// spec needs both kept. Numbers here are binary64, and strings are WTF-8 (see package wtf8).
package jsonvalue

import (
	"math"
)

// Kind is the JSON type of a Value.
type Kind uint8

// The kinds. The zero Value is null.
const (
	KindNull Kind = iota
	KindBool
	KindNumber
	KindString
	KindArray
	KindObject
)

// Value is one JSON value. It is immutable once built; share it freely.
type Value struct {
	kind Kind
	b    bool
	n    float64
	s    string
	arr  []Value
	obj  *Object
}

// Object is a JSON object. A key given twice keeps its last value.
type Object struct {
	keys []string
	m    map[string]Value
}

// Null is the null value.
func Null() Value { return Value{} }

// Bool makes a boolean.
func Bool(b bool) Value { return Value{kind: KindBool, b: b} }

// Number makes a number.
func Number(n float64) Value { return Value{kind: KindNumber, n: n} }

// String makes a string. s must be WTF-8 (use wtf8.FromGo for a Go string of unknown origin).
func String(s string) Value { return Value{kind: KindString, s: s} }

// Array makes an array. The slice is not copied.
func Array(items []Value) Value { return Value{kind: KindArray, arr: items} }

// NewObject makes an empty object under construction.
func NewObject() *Object { return &Object{m: map[string]Value{}} }

// Set sets a key. A key set twice keeps the last value and its first position.
func (o *Object) Set(key string, v Value) {
	if _, ok := o.m[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.m[key] = v
}

// Get returns the value of an own key.
func (o *Object) Get(key string) (Value, bool) {
	v, ok := o.m[key]
	return v, ok
}

// Keys returns the keys in the order they were first set.
func (o *Object) Keys() []string { return o.keys }

// Len is the number of keys.
func (o *Object) Len() int { return len(o.keys) }

// ObjectValue wraps an object as a Value.
func ObjectValue(o *Object) Value { return Value{kind: KindObject, obj: o} }

// Kind returns the JSON type.
func (v Value) Kind() Kind { return v.kind }

// IsNull reports whether v is null.
func (v Value) IsNull() bool { return v.kind == KindNull }

// AsBool returns the boolean and whether v is one.
func (v Value) AsBool() (bool, bool) { return v.b, v.kind == KindBool }

// AsNumber returns the number and whether v is one.
func (v Value) AsNumber() (float64, bool) { return v.n, v.kind == KindNumber }

// AsString returns the string and whether v is one.
func (v Value) AsString() (string, bool) { return v.s, v.kind == KindString }

// AsArray returns the items and whether v is an array. The slice is shared: do not change it.
func (v Value) AsArray() ([]Value, bool) { return v.arr, v.kind == KindArray }

// AsObject returns the object and whether v is one.
func (v Value) AsObject() (*Object, bool) { return v.obj, v.kind == KindObject }

// IsFiniteNumber reports whether v is a number that is neither infinite nor NaN.
func (v Value) IsFiniteNumber() bool {
	return v.kind == KindNumber && !math.IsInf(v.n, 0) && !math.IsNaN(v.n)
}

// Field returns an own key of an object value; it is null and false when v is not an object or the
// key is absent.
func (v Value) Field(key string) (Value, bool) {
	if v.kind != KindObject {
		return Value{}, false
	}
	return v.obj.Get(key)
}

// Equal is the spec's equality: same kind and same content, numbers by IEEE-754 (so -0 equals 0, and
// NaN equals nothing), objects without regard to key order.
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
		if a.obj.Len() != b.obj.Len() {
			return false
		}
		for _, k := range a.obj.keys {
			bv, ok := b.obj.Get(k)
			if !ok || !Equal(a.obj.m[k], bv) {
				return false
			}
		}
		return true
	}
}
