// Package jsonv is the SDK's JSON value, reader and writer. The standard library cannot do what the
// spec needs: it fails on 1e999 and turns a lone surrogate into U+FFFD. Strings are WTF-8 (see
// package wtf8), numbers are binary64, and an object keeps its keys in order with the last duplicate
// winning.
package jsonv

import (
	"math"
	"sort"
	"strings"
)

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

// String names the kind in the contract's words.
func (k Kind) String() string {
	return [...]string{"null", "boolean", "number", "string", "array", "object"}[k]
}

// Obj is a JSON object: ordered keys and a lookup map. It is immutable once built.
type Obj struct {
	keys []string
	m    map[string]Value
}

// Value is one JSON value. The zero value is null. A Value is immutable by convention and safe to
// share between goroutines.
type Value struct {
	kind Kind
	b    bool
	n    float64
	s    string
	a    []Value
	o    *Obj
}

// NewBool returns a boolean.
func NewBool(b bool) Value { return Value{kind: Bool, b: b} }

// NewNumber returns a number. NaN is kept as it is; the writer prints non-finite numbers as null.
func NewNumber(n float64) Value { return Value{kind: Number, n: n} }

// NewString returns a string. The text must be valid WTF-8.
func NewString(s string) Value { return Value{kind: String, s: s} }

// NewArray returns an array holding items.
func NewArray(items []Value) Value { return Value{kind: Array, a: items} }

// NewObject returns an object with the given keys and values; a key given twice keeps the last
// value in the position of the first.
func NewObject(keys []string, values []Value) Value {
	o := &Obj{m: make(map[string]Value, len(keys))}
	for i, k := range keys {
		o.set(k, values[i])
	}
	return Value{kind: Object, o: o}
}

func (o *Obj) set(k string, v Value) {
	if _, dup := o.m[k]; !dup {
		o.keys = append(o.keys, k)
	}
	o.m[k] = v
}

// Kind returns the value's kind.
func (v Value) Kind() Kind { return v.kind }

// IsNull reports whether v is null.
func (v Value) IsNull() bool { return v.kind == Null }

// Bool returns the boolean, or false if v is not one.
func (v Value) Bool() bool { return v.b }

// Number returns the number, or 0 if v is not one.
func (v Value) Number() float64 { return v.n }

// String returns the string, or "" if v is not one. A lone surrogate is kept in WTF-8 form.
func (v Value) String() string { return v.s }

// Items returns the array's items, or nil if v is not an array. The caller must not change them.
func (v Value) Items() []Value { return v.a }

// Object returns the object, or nil if v is not one.
func (v Value) Object() *Obj { return v.o }

// Get returns the member named key and whether it exists.
func (o *Obj) Get(key string) (Value, bool) {
	if o == nil {
		return Value{}, false
	}
	v, ok := o.m[key]
	return v, ok
}

// Keys returns the keys in first-seen order. The caller must not change the slice.
func (o *Obj) Keys() []string {
	if o == nil {
		return nil
	}
	return o.keys
}

// Len is the number of members.
func (o *Obj) Len() int {
	if o == nil {
		return 0
	}
	return len(o.keys)
}

// Field returns the member of an object value, or null when v is not an object or has none.
func (v Value) Field(key string) Value {
	f, _ := v.o.Get(key)
	return f
}

// Has reports whether v is an object with a member named key.
func (v Value) Has(key string) bool {
	_, ok := v.o.Get(key)
	return ok
}

// SameKind is the contract's mismatch rule on kinds: array and object are distinct, every number
// is one kind.
func SameKind(a, b Value) bool { return a.kind == b.kind }

// Equal is deep equality by the spec: numbers by IEEE-754 (so -0 equals 0, NaN equals nothing),
// strings by code units (WTF-8 bytes), objects regardless of key order.
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
			bv, ok := b.o.m[k]
			if !ok || !Equal(a.o.m[k], bv) {
				return false
			}
		}
		return true
	}
}

// FromAny converts a Go value: nil, bool, the integer and float types, string, Value, []any,
// []string, map[string]any and map[string]string. Anything else becomes null. A map's keys are
// sorted so the result is the same every time.
func FromAny(x any) Value {
	switch t := x.(type) {
	case nil:
		return Value{}
	case Value:
		return t
	case bool:
		return NewBool(t)
	case string:
		return NewString(sanitize(t))
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
		for i, e := range t {
			items[i] = FromAny(e)
		}
		return NewArray(items)
	case []string:
		items := make([]Value, len(t))
		for i, e := range t {
			items[i] = NewString(sanitize(e))
		}
		return NewArray(items)
	case map[string]any:
		keys := sortedKeys(t)
		values := make([]Value, len(keys))
		for i, k := range keys {
			values[i] = FromAny(t[k])
		}
		return NewObject(sanitizeAll(keys), values)
	case map[string]string:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		values := make([]Value, len(keys))
		for i, k := range keys {
			values[i] = NewString(sanitize(t[k]))
		}
		return NewObject(sanitizeAll(keys), values)
	}
	return Value{}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sanitizeAll(keys []string) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = sanitize(k)
	}
	return out
}

// sanitize makes a Go string valid: invalid UTF-8 becomes U+FFFD. A lone surrogate already in
// WTF-8 form is kept.
func sanitize(s string) string {
	if strings.ToValidUTF8(s, "") == s {
		return s
	}
	return strings.ToValidUTF8(s, "�")
}

// Finite reports whether n is neither infinite nor NaN.
func Finite(n float64) bool { return !math.IsInf(n, 0) && !math.IsNaN(n) }
