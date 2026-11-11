package toggleslash

import (
	"reflect"
	"sort"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// Value is one JSON value as the SDK reads it: null, a boolean, a number (binary64), a string, an array or
// an object. Flag values, fallbacks of JSONVariation, context attributes and hook events use it. It is
// immutable; share it freely. Its accessors are AsBool, AsNumber, AsString, AsArray, AsObject, IsNull and
// Field.
type Value = jsonvalue.Value

// ParseValue reads one JSON value. It keeps what encoding/json cannot: 1e999 is +Inf and a lone surrogate
// escape stays. The only error is a syntax error, and it carries no text of the input.
func ParseValue(data []byte) (Value, error) { return jsonvalue.Parse(data) }

// Null is the JSON null.
func Null() Value { return jsonvalue.Null() }

// Bool makes a JSON boolean.
func Bool(b bool) Value { return jsonvalue.Bool(b) }

// Number makes a JSON number.
func Number(n float64) Value { return jsonvalue.Number(n) }

// String makes a JSON string. Bytes of s that are not valid UTF-8 become U+FFFD.
func String(s string) Value { return jsonvalue.String(wtf8.FromGo(s)) }

// Array makes a JSON array.
func Array(items ...Value) Value { return jsonvalue.Array(append([]Value(nil), items...)) }

// Object makes a JSON object. Its keys are in sorted order.
func Object(members map[string]Value) Value {
	obj := jsonvalue.NewObject()
	for _, key := range sortedKeys(members) {
		obj.Set(wtf8.FromGo(key), members[key])
	}
	return jsonvalue.ObjectValue(obj)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Context is who a flag is evaluated for: a single context (a kind, a key and attributes) or a multi
// context. It holds a Value and is checked when it is evaluated: a context that is not valid does not
// fail here, it gives the fallback with ReasonError and ErrInvalidContext. The zero Context is not valid.
type Context struct {
	v Value
}

// Value returns the context as the JSON value the evaluator reads.
func (c Context) Value() Value { return c.v }

// ContextOf makes a Context from a Value that is already in the shape of docs/evaluation.md.
func ContextOf(v Value) Context { return Context{v: v} }

// UserContext makes a context of kind "user" (the default kind) with the key and attributes.
func UserContext(key string, attributes map[string]Value) Context {
	return Context{v: single("", key, attributes)}
}

// NewContext makes a single context of the given kind.
func NewContext(kind, key string, attributes map[string]Value) Context {
	return Context{v: single(kind, key, attributes)}
}

func single(kind, key string, attributes map[string]Value) Value {
	obj := jsonvalue.NewObject()
	if kind != "" {
		obj.Set("kind", String(kind))
	}
	obj.Set("key", String(key))
	if attributes != nil {
		obj.Set("attributes", Object(attributes))
	}
	return jsonvalue.ObjectValue(obj)
}

// MultiContext makes a multi context from single contexts by kind. The kind of each entry is the map key;
// a kind written inside an entry is dropped, because the spec refuses one there.
func MultiContext(contexts map[string]Context) Context {
	entries := jsonvalue.NewObject()
	for _, kind := range sortedKeys(contexts) {
		entries.Set(wtf8.FromGo(kind), withoutKind(contexts[kind].v))
	}
	obj := jsonvalue.NewObject()
	obj.Set("kind", String("multi"))
	obj.Set("contexts", jsonvalue.ObjectValue(entries))
	return Context{v: jsonvalue.ObjectValue(obj)}
}

func withoutKind(v Value) Value {
	src, ok := v.AsObject()
	if !ok {
		return v
	}
	out := jsonvalue.NewObject()
	for _, key := range src.Keys() {
		if key == "kind" {
			continue
		}
		value, _ := src.Get(key)
		out.Set(key, value)
	}
	return jsonvalue.ObjectValue(out)
}

// ContextFromMap makes a context from native Go values, in the shape of docs/evaluation.md:
// {"kind": "user", "key": "u1", "attributes": {"plan": "pro"}}. Values may be nil, bool, any integer or
// float type, string, Value, Context, a slice or array, or a map with string keys. Anything else becomes
// null, which a clause reads as an absent attribute.
func ContextFromMap(m map[string]any) Context {
	return Context{v: fromNative(m, 0)}
}

// ContextFromJSON makes a context from JSON text. Text that is not JSON makes a context that is not valid.
func ContextFromJSON(data []byte) Context {
	v, err := jsonvalue.Parse(data)
	if err != nil {
		return Context{}
	}
	return Context{v: v}
}

func fromNative(x any, depth int) Value {
	if depth > jsonvalue.MaxDepth {
		return Null()
	}
	switch t := x.(type) {
	case nil:
		return Null()
	case Value:
		return t
	case Context:
		return t.v
	case bool:
		return Bool(t)
	case string:
		return String(t)
	case float64:
		return Number(t)
	case float32:
		return Number(float64(t))
	case int:
		return Number(float64(t))
	case int64:
		return Number(float64(t))
	case []any:
		items := make([]Value, len(t))
		for i, item := range t {
			items[i] = fromNative(item, depth+1)
		}
		return jsonvalue.Array(items)
	case map[string]any:
		obj := jsonvalue.NewObject()
		for _, key := range sortedKeys(t) {
			obj.Set(wtf8.FromGo(key), fromNative(t[key], depth+1))
		}
		return jsonvalue.ObjectValue(obj)
	}
	return fromReflect(reflect.ValueOf(x), depth)
}

func fromReflect(rv reflect.Value, depth int) Value {
	switch rv.Kind() {
	case reflect.Bool:
		return Bool(rv.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return Number(float64(rv.Int()))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return Number(float64(rv.Uint()))
	case reflect.Float32, reflect.Float64:
		return Number(rv.Float())
	case reflect.String:
		return String(rv.String())
	case reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			return Null()
		}
		items := make([]Value, rv.Len())
		for i := range items {
			items[i] = fromNative(rv.Index(i).Interface(), depth+1)
		}
		return jsonvalue.Array(items)
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String || rv.IsNil() {
			return Null()
		}
		members := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			members[iter.Key().String()] = iter.Value().Interface()
		}
		return fromNative(members, depth)
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return Null()
		}
		return fromNative(rv.Elem().Interface(), depth+1)
	}
	return Null()
}
