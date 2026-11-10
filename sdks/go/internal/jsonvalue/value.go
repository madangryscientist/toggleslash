// Package jsonvalue is the JSON value type the SDK reads rulesets, contexts and served values with.
//
// encoding/json cannot be used for this: it returns an error for a number such as 1e999, where the spec
// reads +Infinity, and it turns a lone surrogate in a string into U+FFFD, where the spec keeps it. So this
// package has its own value and its own reader and writer. Numbers are float64. Strings are WTF-8 (see
// package wtf8): a lone surrogate is kept, a valid escaped pair becomes one four-byte sequence.
package jsonvalue

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// Kind is the JSON type of a Value.
type Kind uint8

// The kinds. The zero Value is Null.
const (
	KindNull Kind = iota
	KindBool
	KindNumber
	KindString
	KindArray
	KindObject
)

// Member is one key and value of an object.
type Member struct {
	Key   string
	Value Value
}

type object struct {
	keys []string
	vals map[string]Value
}

// Value is an immutable JSON value. The zero Value is null.
type Value struct {
	kind Kind
	b    bool
	n    float64
	s    string
	arr  []Value
	obj  *object
}

// Null is the JSON null.
func Null() Value { return Value{} }

// Bool is a JSON boolean.
func Bool(b bool) Value { return Value{kind: KindBool, b: b} }

// Number is a JSON number. +Inf, -Inf and negative zero are kept.
func Number(n float64) Value { return Value{kind: KindNumber, n: n} }

// String is a JSON string. Bytes of s that are not valid UTF-8 become U+FFFD.
func String(s string) Value { return Value{kind: KindString, s: wtf8.FromGo(s)} }

// Array is a JSON array of items.
func Array(items ...Value) Value {
	return Value{kind: KindArray, arr: append([]Value(nil), items...)}
}

// Object is a JSON object. A repeated key keeps its first position and its last value, as JSON.parse does.
func Object(members ...Member) Value {
	o := &object{vals: make(map[string]Value, len(members))}
	for _, m := range members {
		key := wtf8.FromGo(m.Key)
		if _, seen := o.vals[key]; !seen {
			o.keys = append(o.keys, key)
		}
		o.vals[key] = m.Value
	}
	return Value{kind: KindObject, obj: o}
}

// Kind returns the value's JSON type.
func (v Value) Kind() Kind { return v.kind }

// IsNull reports whether v is null.
func (v Value) IsNull() bool { return v.kind == KindNull }

// IsObject reports whether v is a JSON object.
func (v Value) IsObject() bool { return v.kind == KindObject }

// Bool returns the boolean and true when v is one.
func (v Value) Bool() (bool, bool) { return v.b, v.kind == KindBool }

// Number returns the number and true when v is one.
func (v Value) Number() (float64, bool) { return v.n, v.kind == KindNumber }

// Text returns the string and true when v is one. The string is WTF-8.
func (v Value) Text() (string, bool) { return v.s, v.kind == KindString }

// Items returns the elements and true when v is an array. The slice must not be modified.
func (v Value) Items() ([]Value, bool) { return v.arr, v.kind == KindArray }

// Get returns the member of an object by key. It is false for a missing key and for a value that is not
// an object. Only the object's own members are found.
func (v Value) Get(key string) (Value, bool) {
	if v.kind != KindObject {
		return Value{}, false
	}
	m, ok := v.obj.vals[key]
	return m, ok
}

// Field is Get without the flag: a missing member is null, which the spec treats the same way.
func (v Value) Field(key string) Value {
	m, _ := v.Get(key)
	return m
}

// Keys returns an object's keys in the order they first appeared. The slice must not be modified.
func (v Value) Keys() []string {
	if v.kind != KindObject {
		return nil
	}
	return v.obj.keys
}

// IsCount reports whether v is a non-negative integral number.
func (v Value) IsCount() bool {
	return v.kind == KindNumber && !math.IsInf(v.n, 0) && v.n == math.Trunc(v.n) && v.n >= 0
}

// Equal is deep equality with IEEE numbers (so -0 equals 0), for tests.
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
		if len(a.obj.keys) != len(b.obj.keys) {
			return false
		}
		for _, k := range a.obj.keys {
			other, ok := b.obj.vals[k]
			if !ok || !Equal(a.obj.vals[k], other) {
				return false
			}
		}
		return true
	}
}
