// Package jsonval is the JSON value model and reader the SDK uses. It exists
// because encoding/json cannot keep what docs/evaluation.md needs: a lone
// surrogate escape survives the read here, and a number too large for binary64
// reads as an infinity instead of failing.
package jsonval

// Value is one JSON value: Null, Bool, Number, String, Array or Object. The
// interface is sealed, so a caller switches on its six cases and nothing else.
type Value interface {
	isValue()
}

// Null is the JSON literal null. A nil Value is never used: a typed nil in an
// interface is a footgun.
type Null struct{}

// Bool is true or false.
type Bool bool

// Number is an IEEE-754 binary64. It carries no integer flag, so 1.0 and 1 are
// the same value.
type Number float64

// String is UTF-8, or WTF-8 where the input held a lone surrogate escape.
type String string

// Array is a JSON array, in order.
type Array []Value

// Object is a JSON object. A duplicate key keeps the last value.
type Object map[string]Value

func (Null) isValue()   {}
func (Bool) isValue()   {}
func (Number) isValue() {}
func (String) isValue() {}
func (Array) isValue()  {}
func (Object) isValue() {}
