// Package jsonv is the SDK's JSON value, reader and writer.
//
// encoding/json cannot carry what docs/evaluation.md asks of a ruleset: it fails on 1e999 and replaces a
// lone surrogate with U+FFFD. This package reads numbers as binary64 (1e999 is +Inf), holds every string as
// WTF-8 (a lone surrogate is its generalised three-byte form, so a Go string can hold it) and keeps null and
// absent the same: the zero Value is null.
package jsonv

import "math"

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

// Value is one JSON value. It is immutable once built: nothing in the SDK changes a Value it was given.
type Value struct {
	kind Kind
	b    bool
	n    float64
	s    string
	a    []Value
	o    *Object
}

// Object is a JSON object. A duplicate name keeps its last value, at the position of the first.
type Object struct {
	keys []string
	m    map[string]Value
}

// Null is the null Value (also the zero Value).
func Null() Value { return Value{} }

// Bool makes a boolean Value.
func Bool(b bool) Value { return Value{kind: KindBool, b: b} }

// Number makes a number Value.
func Number(n float64) Value { return Value{kind: KindNumber, n: n} }

// String makes a string Value. The text is held as it is, so it may be WTF-8 (a lone surrogate).
func String(s string) Value { return Value{kind: KindString, s: s} }

// Array makes an array Value. The slice is not copied.
func Array(items []Value) Value { return Value{kind: KindArray, a: items} }

// ObjectValue makes an object Value.
func ObjectValue(o *Object) Value {
	if o == nil {
		o = NewObject()
	}
	return Value{kind: KindObject, o: o}
}

// NewObject returns an empty Object.
func NewObject() *Object { return &Object{m: map[string]Value{}} }

// Set adds or replaces a name.
func (o *Object) Set(name string, v Value) {
	if _, ok := o.m[name]; !ok {
		o.keys = append(o.keys, name)
	}
	o.m[name] = v
}

// Get returns the value of a name; ok is false when the name is not there.
func (o *Object) Get(name string) (Value, bool) {
	if o == nil {
		return Value{}, false
	}
	v, ok := o.m[name]
	return v, ok
}

// Len is the number of names.
func (o *Object) Len() int {
	if o == nil {
		return 0
	}
	return len(o.keys)
}

// Keys returns the names in the order they were first set. The slice must not be changed.
func (o *Object) Keys() []string {
	if o == nil {
		return nil
	}
	return o.keys
}

// Kind returns the JSON type.
func (v Value) Kind() Kind { return v.kind }

// IsNull reports a null Value.
func (v Value) IsNull() bool { return v.kind == KindNull }

// AsBool returns the boolean and whether v is one.
func (v Value) AsBool() (bool, bool) { return v.b, v.kind == KindBool }

// AsNumber returns the number and whether v is one.
func (v Value) AsNumber() (float64, bool) { return v.n, v.kind == KindNumber }

// AsString returns the string and whether v is one.
func (v Value) AsString() (string, bool) { return v.s, v.kind == KindString }

// AsArray returns the items and whether v is an array. The slice must not be changed.
func (v Value) AsArray() ([]Value, bool) { return v.a, v.kind == KindArray }

// AsObject returns the object and whether v is one.
func (v Value) AsObject() (*Object, bool) { return v.o, v.kind == KindObject }

// Get returns an object's value for a name, or null when v is not an object or the name is absent.
func (v Value) Get(name string) Value {
	if v.kind != KindObject {
		return Value{}
	}
	got, _ := v.o.Get(name)
	return got
}

// Equal is deep equality by the spec: numbers compare by IEEE-754 (so -0 equals 0), strings by bytes,
// objects by their names and values whatever the order.
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
	case KindObject:
		if a.o.Len() != b.o.Len() {
			return false
		}
		for _, k := range a.o.keys {
			other, ok := b.o.Get(k)
			if !ok || !Equal(a.o.m[k], other) {
				return false
			}
		}
		return true
	}
	return false
}

// IsFinite reports whether f is neither infinite nor NaN.
func IsFinite(f float64) bool { return !math.IsInf(f, 0) && !math.IsNaN(f) }
