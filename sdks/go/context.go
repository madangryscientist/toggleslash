package toggleslash

import (
	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

// Context is an evaluation context. The zero Context is invalid: an evaluation with it answers the fallback
// with ERROR INVALID_CONTEXT. A Context is immutable and safe to share.
type Context struct {
	value jsonvalue.Value
	set   bool
	// parsed is the evaluator's reading of value, nil when the value is not a valid context.
	parsed *evaluator.Context
}

func newContext(v jsonvalue.Value) Context {
	return Context{value: v, set: true, parsed: evaluator.NewContext(v)}
}

// ContextFromValue wraps a JSON value as a context. It is not checked here: an invalid one is reported by
// the evaluation that uses it.
func ContextFromValue(v jsonvalue.Value) Context { return newContext(v) }

// ContextFromJSON reads a context from JSON text. Text that is not JSON gives an invalid context.
func ContextFromJSON(data []byte) Context {
	v, err := jsonvalue.Parse(data)
	if err != nil {
		return Context{}
	}
	return newContext(v)
}

// NewContext is a single context of the default kind "user" with the given key and attributes. The
// attributes are Go values jsonvalue.From accepts (nil for none); one it does not accept makes the context
// invalid.
func NewContext(key string, attributes map[string]any) Context {
	return NewKindContext("user", key, attributes)
}

// NewKindContext is a single context of the given kind.
func NewKindContext(kind, key string, attributes map[string]any) Context {
	members := []jsonvalue.Member{
		{Key: "kind", Value: jsonvalue.FromString(kind)},
		{Key: "key", Value: jsonvalue.FromString(key)},
	}
	if attributes != nil {
		a, ok := jsonvalue.From(attributes)
		if !ok {
			return Context{}
		}
		members = append(members, jsonvalue.Member{Key: "attributes", Value: a})
	}
	return newContext(jsonvalue.FromObject(members...))
}

// NewMultiContext combines contexts of different kinds. A context that is itself multi, or invalid, or one
// that repeats a kind, makes the result invalid.
func NewMultiContext(contexts ...Context) Context {
	members := make([]jsonvalue.Member, 0, len(contexts))
	seen := map[string]bool{}
	for _, c := range contexts {
		if !c.set || c.parsed == nil {
			return Context{}
		}
		kind := "user"
		if k, ok := c.value.Get("kind"); ok {
			kind, _ = k.AsString()
		}
		if kind == "multi" || seen[kind] {
			return Context{}
		}
		seen[kind] = true
		inner := make([]jsonvalue.Member, 0, 2)
		for _, m := range c.value.Members() {
			if m.Key == "key" || m.Key == "attributes" {
				inner = append(inner, m)
			}
		}
		members = append(members, jsonvalue.Member{Key: kind, Value: jsonvalue.FromObject(inner...)})
	}
	return newContext(jsonvalue.FromObject(
		jsonvalue.Member{Key: "kind", Value: jsonvalue.FromString("multi")},
		jsonvalue.Member{Key: "contexts", Value: jsonvalue.FromObject(members...)},
	))
}

// Valid reports whether the context is a valid context under docs/evaluation.md.
func (c Context) Valid() bool { return c.parsed != nil }

// Value returns the context as JSON (null for the zero Context).
func (c Context) Value() jsonvalue.Value { return c.value }
