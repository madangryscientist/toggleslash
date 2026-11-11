package toggleslash

import (
	"encoding/json"
	"reflect"
	"slices"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

// Context is a single or a multi context as docs/evaluation.md defines them. It is a value: its builders
// return a new Context and never change the one they were called on. The zero Context is invalid, and so is
// any Context that cannot be built; evaluating one gives ERROR INVALID_CONTEXT and the caller's fallback,
// never a panic.
type Context struct{ v jsonvalue.Value }

// UserContext is the single context of kind "user" with the given key.
func UserContext(key string) Context { return NewContext("user", key) }

// NewContext is a single context of the given kind and key.
func NewContext(kind, key string) Context {
	obj := jsonvalue.NewObject()
	obj.Set("kind", jsonvalue.Str(kind))
	obj.Set("key", jsonvalue.Str(key))
	return Context{obj}
}

// With returns a copy of a single context with one attribute set. On a multi or invalid context it returns
// the context unchanged.
func (c Context) With(attribute string, value Value) Context {
	if c.v.Kind != jsonvalue.Object {
		return c
	}
	if kind, _ := c.v.Get("kind"); kind.Kind == jsonvalue.String && kind.S == "multi" {
		return c
	}
	out := jsonvalue.NewObject()
	for _, k := range c.v.O.Keys {
		if k != "attributes" {
			out.Set(k, c.v.O.M[k])
		}
	}
	attrs := jsonvalue.NewObject()
	if old, ok := c.v.Get("attributes"); ok && old.Kind == jsonvalue.Object {
		for _, k := range old.O.Keys {
			attrs.Set(k, old.O.M[k])
		}
	}
	attrs.Set(attribute, value.v)
	out.Set("attributes", attrs)
	return Context{out}
}

// MultiContext combines single contexts of different kinds. A part that is not a valid single context, a
// repeated kind or no part at all makes the result invalid.
func MultiContext(parts ...Context) Context {
	contexts := jsonvalue.NewObject()
	for _, p := range parts {
		kind, _ := p.v.Get("kind")
		key, _ := p.v.Get("key")
		if p.v.Kind != jsonvalue.Object || kind.Kind != jsonvalue.String || key.Kind != jsonvalue.String || kind.S == "multi" {
			return Context{}
		}
		if _, dup := contexts.O.M[kind.S]; dup {
			return Context{}
		}
		entry := jsonvalue.NewObject()
		entry.Set("key", key)
		if attrs, ok := p.v.Get("attributes"); ok {
			entry.Set("attributes", attrs)
		}
		contexts.Set(kind.S, entry)
	}
	if len(contexts.O.Keys) == 0 {
		return Context{}
	}
	out := jsonvalue.NewObject()
	out.Set("kind", jsonvalue.Str("multi"))
	out.Set("contexts", contexts)
	return Context{out}
}

// ContextFromJSON reads a context from JSON text, through the SDK's own reader, so 1e999 and a lone surrogate
// escape survive. Text that is not JSON gives an invalid Context.
func ContextFromJSON(text []byte) Context {
	v, err := jsonvalue.Parse(string(text))
	if err != nil {
		return Context{}
	}
	return Context{v}
}

// ContextFromMap converts native Go values to a context. It takes nil, bool, every int and uint type, float32
// and float64, string, Value, []any, map[string]any and json.Number, and nested slices and maps of those. Any
// other type gives an invalid Context.
func ContextFromMap(m map[string]any) Context {
	v, ok := fromNative(m, 0)
	if !ok {
		return Context{}
	}
	return Context{v}
}

// ToValue returns the context as a Value, for a hook that wants to look inside it.
func (c Context) ToValue() Value { return Value(c) }

func fromNative(x any, depth int) (jsonvalue.Value, bool) {
	if depth > 200 {
		return jsonvalue.Value{}, false
	}
	switch t := x.(type) {
	case nil:
		return jsonvalue.Value{}, true
	case Value:
		return t.v, true
	case bool:
		return jsonvalue.Boolean(t), true
	case string:
		return jsonvalue.Str(t), true
	case float64:
		return jsonvalue.Num(t), true
	case float32:
		return jsonvalue.Num(float64(t)), true
	case json.Number:
		n, err := jsonvalue.Parse(string(t))
		if err != nil || n.Kind != jsonvalue.Number {
			return jsonvalue.Value{}, false
		}
		return n, true
	case []any:
		items := make([]jsonvalue.Value, len(t))
		for i, item := range t {
			v, ok := fromNative(item, depth+1)
			if !ok {
				return jsonvalue.Value{}, false
			}
			items[i] = v
		}
		return jsonvalue.List(items), true
	case map[string]any:
		obj := jsonvalue.NewObject()
		keys := sortedKeys(t)
		for _, k := range keys {
			v, ok := fromNative(t[k], depth+1)
			if !ok {
				return jsonvalue.Value{}, false
			}
			obj.Set(k, v)
		}
		return obj, true
	}
	rv := reflect.ValueOf(x)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return jsonvalue.Num(float64(rv.Int())), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return jsonvalue.Num(float64(rv.Uint())), true
	case reflect.Float32, reflect.Float64:
		return jsonvalue.Num(rv.Float()), true
	}
	return jsonvalue.Value{}, false
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// ContextFromValue turns a Value into a context. A Value that is not a valid context gives an invalid one.
func ContextFromValue(v Value) Context { return Context(v) }
