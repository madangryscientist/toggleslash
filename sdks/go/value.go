package toggleslash

import (
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// Value is a JSON value: what a flag serves, what a fallback is, and what a context is made of.
// It holds numbers as float64 (a read of 1e999 is +Inf, not an error) and strings that may hold a
// lone UTF-16 surrogate, which encoding/json cannot carry. Read one with Kind and the accessors;
// build one with the constructors below, FromAny or ParseJSON.
type Value = jsonv.Value

// ValueKind is the kind of a Value.
type ValueKind = jsonv.Kind

// The kinds of a Value.
const (
	KindNull   = jsonv.Null
	KindBool   = jsonv.Bool
	KindNumber = jsonv.Number
	KindString = jsonv.String
	KindArray  = jsonv.Array
	KindObject = jsonv.Object
)

// Null is the JSON null, which is also the zero Value.
func Null() Value { return Value{} }

// Bool is a JSON boolean.
func Bool(b bool) Value { return jsonv.NewBool(b) }

// Number is a JSON number.
func Number(n float64) Value { return jsonv.NewNumber(n) }

// String is a JSON string. Bytes that are not valid UTF-8 are not valid JSON text: the caller
// should pass text, and ParseJSON for anything with a lone surrogate.
func String(s string) Value { return jsonv.FromAny(s) }

// Array is a JSON array.
func Array(items ...Value) Value { return jsonv.NewArray(items) }

// Object is a JSON object from keys and the values at the same positions. A duplicate key keeps the
// last value.
func Object(keys []string, values []Value) Value { return jsonv.NewObject(keys, values) }

// FromAny converts a Go value to a Value: nil, bool, the integer and float types, string, Value,
// []any, []string, map[string]any and map[string]string. Anything else becomes null.
func FromAny(x any) Value { return jsonv.FromAny(x) }

// ParseJSON reads one JSON text, as the SDK reads a ruleset: 1e999 is +Inf and a lone surrogate
// written as an escape is kept. It returns an error for text that is not JSON.
func ParseJSON(text []byte) (Value, error) { return jsonv.Parse(text) }

// MarshalJSON writes v as JSON. A number that is not finite is written as null, as JSON.stringify
// does.
func MarshalJSON(v Value) []byte { return jsonv.Marshal(v) }

// Equal reports whether two values are equal by the evaluation spec: numbers by IEEE-754 (so -0
// equals 0) and objects without regard to key order.
func Equal(a, b Value) bool { return jsonv.Equal(a, b) }
