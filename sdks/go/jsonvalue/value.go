// Package jsonvalue is the JSON value type the ToggleSlash SDK takes contexts and fallbacks in and
// returns values as. It exists because encoding/json cannot hold what the evaluation spec requires: a
// number such as 1e999 (Infinity, not an error) and a lone surrogate in a string (kept, not replaced).
package jsonvalue

import (
	"math"
	"sort"
	"unicode/utf8"
)

// Kind is the JSON kind of a Value.
type Kind uint8

// The JSON kinds. The zero Value is null.
const (
	Null Kind = iota
	Bool
	Number
	String
	Array
	Object
)

// Member is one entry of an object.
type Member struct {
	Key   string
	Value Value
}

type object struct {
	members []Member
	index   map[string]int
}

// Value is one JSON value. The zero Value is null. A Value is immutable once built: nothing here
// changes one, and the SDK never retains a caller's slice or map without copying it.
type Value struct {
	kind Kind
	b    bool
	n    float64
	s    string
	arr  []Value
	obj  *object
}

// Kind reports the JSON kind.
func (v Value) Kind() Kind { return v.kind }

// IsNull reports whether v is null.
func (v Value) IsNull() bool { return v.kind == Null }

// AsBool returns the boolean and true when v is one.
func (v Value) AsBool() (bool, bool) { return v.b, v.kind == Bool }

// AsNumber returns the number and true when v is one. It is a binary64: it may be +Inf, -Inf or -0.
func (v Value) AsNumber() (float64, bool) { return v.n, v.kind == Number }

// AsString returns the string and true when v is one. The string is WTF-8, so it may hold a lone surrogate.
func (v Value) AsString() (string, bool) { return v.s, v.kind == String }

// AsArray returns the elements and true when v is an array. The slice is shared: do not change it.
func (v Value) AsArray() ([]Value, bool) { return v.arr, v.kind == Array }

// Members returns the entries of an object in order, or nil. The slice is shared: do not change it.
func (v Value) Members() []Member {
	if v.obj == nil {
		return nil
	}
	return v.obj.members
}

// Get returns the member named key of an object.
func (v Value) Get(key string) (Value, bool) {
	if v.obj == nil {
		return Value{}, false
	}
	i, ok := v.obj.index[key]
	if !ok {
		return Value{}, false
	}
	return v.obj.members[i].Value, true
}

// NullValue is the JSON null.
func NullValue() Value { return Value{} }

// FromBool builds a boolean.
func FromBool(b bool) Value { return Value{kind: Bool, b: b} }

// FromFloat builds a number. Any float64 is allowed; NaN is stored as is but never arises from JSON text.
func FromFloat(f float64) Value { return Value{kind: Number, n: f} }

// FromInt builds a number from an int64, rounded to the nearest binary64 as JSON text would be.
func FromInt(i int64) Value { return Value{kind: Number, n: float64(i)} }

// FromString builds a string from a Go string. Invalid UTF-8 in it is replaced as LossyUTF8 does.
// To build a string with a lone surrogate, use FromUTF16Units.
func FromString(s string) Value {
	if !utf8.ValidString(s) {
		s = LossyUTF8([]byte(s))
	}
	return Value{kind: String, s: s}
}

// FromUTF16Units builds a string from UTF-16 code units, keeping an unpaired surrogate.
func FromUTF16Units(units []uint16) Value { return Value{kind: String, s: FromUTF16(units)} }

// FromArray builds an array. The elements are copied.
func FromArray(elements ...Value) Value {
	return Value{kind: Array, arr: append([]Value{}, elements...)}
}

// FromObject builds an object from members in order. A repeated key keeps the last value, in the
// position of the first.
func FromObject(members ...Member) Value {
	o := &object{index: make(map[string]int, len(members))}
	for _, m := range members {
		o.add(m.Key, m.Value)
	}
	return Value{kind: Object, obj: o}
}

func (o *object) add(key string, v Value) {
	if i, ok := o.index[key]; ok {
		o.members[i].Value = v
		return
	}
	o.index[key] = len(o.members)
	o.members = append(o.members, Member{Key: key, Value: v})
}

// From converts a native Go value: nil, bool, every integer and float type, string, []any,
// map[string]any (keys in sorted order), []Value, map[string]Value and Value itself. Anything else is null.
func From(x any) Value {
	switch t := x.(type) {
	case nil:
		return Value{}
	case Value:
		return t
	case bool:
		return FromBool(t)
	case string:
		return FromString(t)
	case float64:
		return FromFloat(t)
	case float32:
		return FromFloat(float64(t))
	case int:
		return FromInt(int64(t))
	case int8:
		return FromInt(int64(t))
	case int16:
		return FromInt(int64(t))
	case int32:
		return FromInt(int64(t))
	case int64:
		return FromInt(t)
	case uint:
		return FromFloat(float64(t))
	case uint8:
		return FromInt(int64(t))
	case uint16:
		return FromInt(int64(t))
	case uint32:
		return FromInt(int64(t))
	case uint64:
		return FromFloat(float64(t))
	case []Value:
		return FromArray(t...)
	case []any:
		out := make([]Value, len(t))
		for i, e := range t {
			out[i] = From(e)
		}
		return Value{kind: Array, arr: out}
	case map[string]Value:
		return fromMap(len(t), func(yield func(string, Value)) {
			for k, v := range t {
				yield(k, v)
			}
		})
	case map[string]any:
		return fromMap(len(t), func(yield func(string, Value)) {
			for k, v := range t {
				yield(k, From(v))
			}
		})
	}
	return Value{}
}

func fromMap(n int, each func(func(string, Value))) Value {
	members := make([]Member, 0, n)
	each(func(k string, v Value) { members = append(members, Member{Key: FromString(k).s, Value: v}) })
	sort.Slice(members, func(i, j int) bool { return members[i].Key < members[j].Key })
	return FromObject(members...)
}

// Equal reports whether a and b are the same JSON value by the spec's equality: numbers by IEEE-754
// (so -0 equals 0), strings by code point, arrays element by element and objects member by member
// regardless of order.
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
		return a.n == b.n || (math.IsNaN(a.n) && math.IsNaN(b.n))
	case String:
		return a.s == b.s
	case Array:
		if len(a.arr) != len(b.arr) {
			return false
		}
		for i := range a.arr {
			if !Equal(a.arr[i], b.arr[i]) {
				return false
			}
		}
		return true
	case Object:
		am, bm := a.Members(), b.Members()
		if len(am) != len(bm) {
			return false
		}
		for _, m := range am {
			other, ok := b.Get(m.Key)
			if !ok || !Equal(m.Value, other) {
				return false
			}
		}
		return true
	}
	return false
}
