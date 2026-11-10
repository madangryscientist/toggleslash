package toggleslash

import (
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// Value is a JSON value. It is the SDK's own type because encoding/json cannot carry what the
// evaluation spec needs: 1e999 is an error there, and a lone surrogate is replaced by U+FFFD. The zero
// Value is JSON null.
//
// A Value has these methods: Kind, IsNull, AsBool, AsNumber, AsString, IsArray, IsObject, Items, Get,
// Keys, Len and IsInteger.
type Value = jsonv.Value

// Context is the end user (or other subject) a flag is evaluated for: a single context
// ({"key": "u1", "attributes": {...}}) or a multi context ({"kind": "multi", "contexts": {...}}), as
// docs/evaluation.md defines them. A context that is not valid is not an error: the caller gets its
// fallback with ERROR INVALID_CONTEXT.
type Context = jsonv.Value

// The kinds a Value can be.
const (
	KindNull   = jsonv.KindNull
	KindBool   = jsonv.KindBool
	KindNumber = jsonv.KindNumber
	KindString = jsonv.KindString
	KindArray  = jsonv.KindArray
	KindObject = jsonv.KindObject
)

// ParseJSON reads JSON the way the evaluation spec does: numbers are binary64 (1e999 is +Inf), and a
// lone surrogate escape is kept.
func ParseJSON(data []byte) (Value, error) { return jsonv.Parse(data) }

// Null is JSON null.
func Null() Value { return jsonv.Null() }

// Bool is a JSON boolean.
func Bool(b bool) Value { return jsonv.Bool(b) }

// Number is a JSON number.
func Number(n float64) Value { return jsonv.Number(n) }

// String is a JSON string.
func String(s string) Value { return jsonv.String(s) }

// Array is a JSON array.
func Array(items ...Value) Value { return jsonv.Array(items) }

// Object is a JSON object.
func Object(members map[string]Value) Value { return jsonv.Object(members) }

// FromAny builds a Value from nil, bool, any integer or float type, string, []any, map[string]any or a
// Value. It returns an error for any other type.
func FromAny(x any) (Value, error) { return jsonv.FromAny(x) }

// MarshalJSON writes a Value as JSON. A number that is NaN or infinite has no JSON form and is written
// as null.
func MarshalJSON(v Value) []byte { return jsonv.Marshal(v) }
