// Package jsonv is the SDK's JSON value, reader and writer.
//
// It exists because encoding/json cannot carry what docs/evaluation.md requires: it errors on 1e999
// (the spec reads every number as binary64, so that is +Inf) and it replaces a lone surrogate with
// U+FFFD (the spec keeps it). Strings here are WTF-8 (see internal/wtf8).
package jsonv

import (
	"fmt"
	"math"
	"sort"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// Kind is the JSON kind of a Value.
type Kind uint8

// The JSON kinds. The zero Value is null.
const (
	KindNull Kind = iota
	KindBool
	KindNumber
	KindString
	KindArray
	KindObject
)

// String names a kind the way the spec does.
func (k Kind) String() string {
	switch k {
	case KindBool:
		return "boolean"
	case KindNumber:
		return "number"
	case KindString:
		return "string"
	case KindArray:
		return "array"
	case KindObject:
		return "object"
	}
	return "null"
}

// Value is one JSON value. The zero Value is null. A Value is immutable once built: nothing in this
// package changes a slice or map it was given or returned.
type Value struct {
	kind Kind
	b    bool
	n    float64
	s    string
	arr  []Value
	obj  *object
}

type object struct {
	keys []string
	m    map[string]Value
}

// Null is the JSON null.
func Null() Value { return Value{} }

// Bool is a JSON boolean.
func Bool(b bool) Value { return Value{kind: KindBool, b: b} }

// Number is a JSON number. Any float64 is accepted, infinities and negative zero included.
func Number(n float64) Value { return Value{kind: KindNumber, n: n} }

// String is a JSON string. Bytes that are not UTF-8 become U+FFFD; a lone surrogate encoded as WTF-8 stays.
func String(s string) Value { return Value{kind: KindString, s: wtf8.Sanitize(s)} }

// rawString is a string already known to be well-formed WTF-8.
func rawString(s string) Value { return Value{kind: KindString, s: s} }

// Array is a JSON array. The slice is not copied; the caller must not change it afterwards.
func Array(items []Value) Value { return Value{kind: KindArray, arr: items} }

// Object is a JSON object from a Go map. Keys are listed in sorted order.
func Object(members map[string]Value) Value {
	keys := make([]string, 0, len(members))
	m := make(map[string]Value, len(members))
	for k, v := range members {
		k = wtf8.Sanitize(k)
		if _, dup := m[k]; !dup {
			keys = append(keys, k)
		}
		m[k] = v
	}
	sort.Strings(keys)
	return Value{kind: KindObject, obj: &object{keys: keys, m: m}}
}

// FromAny builds a Value from nil, bool, any integer or float type, string, []any, map[string]any,
// []Value and Value. Any other type is an error.
func FromAny(x any) (Value, error) {
	switch t := x.(type) {
	case nil:
		return Null(), nil
	case Value:
		return t, nil
	case bool:
		return Bool(t), nil
	case string:
		return String(t), nil
	case float64:
		return Number(t), nil
	case float32:
		return Number(float64(t)), nil
	case int:
		return Number(float64(t)), nil
	case int8:
		return Number(float64(t)), nil
	case int16:
		return Number(float64(t)), nil
	case int32:
		return Number(float64(t)), nil
	case int64:
		return Number(float64(t)), nil
	case uint:
		return Number(float64(t)), nil
	case uint8:
		return Number(float64(t)), nil
	case uint16:
		return Number(float64(t)), nil
	case uint32:
		return Number(float64(t)), nil
	case uint64:
		return Number(float64(t)), nil
	case []Value:
		return Array(append([]Value(nil), t...)), nil
	case []any:
		out := make([]Value, len(t))
		for i, e := range t {
			v, err := FromAny(e)
			if err != nil {
				return Null(), err
			}
			out[i] = v
		}
		return Array(out), nil
	case map[string]any:
		m := make(map[string]Value, len(t))
		for k, e := range t {
			v, err := FromAny(e)
			if err != nil {
				return Null(), err
			}
			m[k] = v
		}
		return Object(m), nil
	}
	return Null(), fmt.Errorf("toggleslash: cannot make a JSON value from %T", x)
}

// Kind reports the JSON kind.
func (v Value) Kind() Kind { return v.kind }

// IsNull reports whether v is null.
func (v Value) IsNull() bool { return v.kind == KindNull }

// AsBool returns the boolean and true when v is one.
func (v Value) AsBool() (bool, bool) { return v.b, v.kind == KindBool }

// AsNumber returns the number and true when v is one.
func (v Value) AsNumber() (float64, bool) { return v.n, v.kind == KindNumber }

// AsString returns the WTF-8 string and true when v is one.
func (v Value) AsString() (string, bool) { return v.s, v.kind == KindString }

// Items returns the elements of an array, or nil. The slice must not be changed.
func (v Value) Items() []Value {
	if v.kind != KindArray {
		return nil
	}
	return v.arr
}

// IsArray reports whether v is an array.
func (v Value) IsArray() bool { return v.kind == KindArray }

// IsObject reports whether v is an object.
func (v Value) IsObject() bool { return v.kind == KindObject }

// Get returns the own member named key of an object.
func (v Value) Get(key string) (Value, bool) {
	if v.kind != KindObject {
		return Value{}, false
	}
	m, ok := v.obj.m[key]
	return m, ok
}

// Keys returns the member names of an object in the order they were read. The slice must not be changed.
func (v Value) Keys() []string {
	if v.kind != KindObject {
		return nil
	}
	return v.obj.keys
}

// Len is the number of elements of an array, members of an object, or zero.
func (v Value) Len() int {
	switch v.kind {
	case KindArray:
		return len(v.arr)
	case KindObject:
		return len(v.obj.keys)
	}
	return 0
}

// IsInteger reports whether v is a finite number with no fractional part.
func (v Value) IsInteger() bool {
	return v.kind == KindNumber && !math.IsInf(v.n, 0) && !math.IsNaN(v.n) && v.n == math.Trunc(v.n)
}
