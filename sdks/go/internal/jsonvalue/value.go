// Package jsonvalue is the SDK's own JSON reader and writer. encoding/json cannot be used for rulesets
// and contexts because it rejects 1e999 (the spec reads it as Infinity) and replaces a lone surrogate
// escape with U+FFFD (the spec keeps it). Strings here are WTF-8 Go strings; see internal/wtf8.
package jsonvalue

// Kind is the JSON type of a Value.
type Kind uint8

// The JSON types. The zero Kind is Null, so the zero Value is null, and an absent member and a null one
// read the same, which is what the spec asks of every optional field.
const (
	Null Kind = iota
	Bool
	Number
	String
	Array
	Object
)

// Value is one JSON value. Only the field that matches Kind is meaningful.
type Value struct {
	Kind Kind
	B    bool
	N    float64
	S    string
	A    []Value
	O    *Members
}

// Members is the content of a JSON object: the members by name, and the names in the order they first
// appeared. A duplicate name keeps its first place and its last value.
type Members struct {
	Keys []string
	M    map[string]Value
}

// Get returns the member called name. A missing member is the zero Value (null) with ok false.
func (v Value) Get(name string) (Value, bool) {
	if v.Kind != Object || v.O == nil {
		return Value{}, false
	}
	m, ok := v.O.M[name]
	return m, ok
}

// IsObject reports whether v is an object.
func (v Value) IsObject() bool { return v.Kind == Object }

// IsAbsent reports whether v is null, which also stands for an absent member.
func (v Value) IsAbsent() bool { return v.Kind == Null }

// NewObject returns an empty object value.
func NewObject() Value {
	return Value{Kind: Object, O: &Members{M: map[string]Value{}}}
}

// Set adds or replaces a member of an object value that was made by NewObject or the reader.
func (v Value) Set(name string, member Value) {
	if _, seen := v.O.M[name]; !seen {
		v.O.Keys = append(v.O.Keys, name)
	}
	v.O.M[name] = member
}

// Str, Num, Boolean and List build scalar and array values.
func Str(s string) Value       { return Value{Kind: String, S: s} }
func Num(n float64) Value      { return Value{Kind: Number, N: n} }
func Boolean(b bool) Value     { return Value{Kind: Bool, B: b} }
func List(items []Value) Value { return Value{Kind: Array, A: items} }

// Equal is deep equality as the conformance harness compares: numbers by binary64 value (so 1.0 equals 1
// and -0 equals 0), strings by bytes, objects by member sets.
func Equal(a, b Value) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case Null:
		return true
	case Bool:
		return a.B == b.B
	case Number:
		return a.N == b.N
	case String:
		return a.S == b.S
	case Array:
		if len(a.A) != len(b.A) {
			return false
		}
		for i := range a.A {
			if !Equal(a.A[i], b.A[i]) {
				return false
			}
		}
		return true
	}
	if len(a.O.Keys) != len(b.O.Keys) {
		return false
	}
	for k, av := range a.O.M {
		bv, ok := b.O.M[k]
		if !ok || !Equal(av, bv) {
			return false
		}
	}
	return true
}
