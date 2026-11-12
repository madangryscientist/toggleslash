// Package jsonvalue is the SDK's JSON: a value type, a reader and a writer.
//
// encoding/json cannot be used. It fails on 1e999 and it replaces a lone surrogate with U+FFFD, and the
// evaluation spec forbids both. Numbers here are IEEE-754 binary64, and a string is WTF-8 (see internal/wtf8).
package jsonvalue

// Kind is the JSON type of a Value.
type Kind uint8

// The kinds. Null is the zero Kind, so the zero Value is null.
const (
	KindNull Kind = iota
	KindBool
	KindNumber
	KindString
	KindArray
	KindObject
)

// Value is one JSON value. It is immutable by convention: nothing in the SDK changes a Value it was given.
type Value struct {
	kind Kind
	b    bool
	n    float64
	s    string
	arr  []Value
	obj  *object
}

// object keeps the members in the order they were read, and a map for lookups.
type object struct {
	keys []string
	m    map[string]Value
}

// Null is the null value.
func Null() Value { return Value{} }

// Bool wraps a boolean.
func Bool(b bool) Value { return Value{kind: KindBool, b: b} }

// Number wraps a binary64 number, which may be infinite or NaN (a reader makes the first from 1e999).
func Number(n float64) Value { return Value{kind: KindNumber, n: n} }

// String wraps a WTF-8 string.
func String(s string) Value { return Value{kind: KindString, s: s} }

// Array wraps a slice, which the caller must not change afterwards.
func Array(items []Value) Value { return Value{kind: KindArray, arr: items} }

// Member is one property of an object.
type Member struct {
	Key   string
	Value Value
}

// Object builds an object from members in order. A repeated key keeps the last value, at the position of the first.
func Object(members ...Member) Value {
	o := &object{m: make(map[string]Value, len(members))}
	for _, member := range members {
		o.set(member.Key, member.Value)
	}
	return Value{kind: KindObject, obj: o}
}

func (o *object) set(key string, v Value) {
	if _, ok := o.m[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.m[key] = v
}

// Kind returns the JSON type.
func (v Value) Kind() Kind { return v.kind }

// IsNull reports whether v is null.
func (v Value) IsNull() bool { return v.kind == KindNull }

// AsBool returns the boolean, and false in the second result when v is not one.
func (v Value) AsBool() (bool, bool) { return v.b, v.kind == KindBool }

// AsNumber returns the number, and false in the second result when v is not one.
func (v Value) AsNumber() (float64, bool) { return v.n, v.kind == KindNumber }

// AsString returns the string, and false in the second result when v is not one.
func (v Value) AsString() (string, bool) { return v.s, v.kind == KindString }

// AsArray returns the items, and false in the second result when v is not an array. The caller must not change them.
func (v Value) AsArray() ([]Value, bool) { return v.arr, v.kind == KindArray }

// IsObject reports whether v is an object.
func (v Value) IsObject() bool { return v.kind == KindObject }

// Get returns the property named key. The second result is false when v is not an object or has no such
// property; a property that is present and null returns (null, true).
func (v Value) Get(key string) (Value, bool) {
	if v.kind != KindObject {
		return Value{}, false
	}
	got, ok := v.obj.m[key]
	return got, ok
}

// Len is the number of properties of an object, or the length of an array, or 0.
func (v Value) Len() int {
	switch v.kind {
	case KindObject:
		return len(v.obj.keys)
	case KindArray:
		return len(v.arr)
	}
	return 0
}

// Members returns an object's properties in order, or nil.
func (v Value) Members() []Member {
	if v.kind != KindObject {
		return nil
	}
	out := make([]Member, 0, len(v.obj.keys))
	for _, k := range v.obj.keys {
		out = append(out, Member{Key: k, Value: v.obj.m[k]})
	}
	return out
}

// Equal reports deep equality by IEEE comparison for numbers: 1 equals 1.0, -0 equals 0, NaN equals nothing.
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
	case KindObject:
		if len(a.obj.keys) != len(b.obj.keys) {
			return false
		}
		for k, av := range a.obj.m {
			bv, ok := b.obj.m[k]
			if !ok || !Equal(av, bv) {
				return false
			}
		}
		return true
	}
	return false
}
