package toggleslash

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"

// Context describes who a flag is evaluated for. It wraps a JSON Value in the shape docs/evaluation.md
// defines: a single kind is {"key": ..., "kind": ..., "attributes": {...}}, and several kinds are
// {"kind": "multi", "contexts": {"<kind>": {"key": ..., "attributes": ...}}}. The zero Context is invalid, so
// evaluating it gives ERROR INVALID_CONTEXT and the fallback.
type Context struct {
	v Value
}

// UserContext is a context of the default kind, "user", with a key.
func UserContext(key string) Context {
	return Context{v: jsonvalue.Object(jsonvalue.Member{Key: "key", Value: jsonvalue.String(key)})}
}

// NewContext is a context of one kind with a key.
func NewContext(kind, key string) Context {
	return Context{v: jsonvalue.Object(
		jsonvalue.Member{Key: "kind", Value: jsonvalue.String(kind)},
		jsonvalue.Member{Key: "key", Value: jsonvalue.String(key)},
	)}
}

// MultiContext combines contexts of different kinds into one. A context that is itself multi, or has
// no kind-specific key, makes the result invalid, which the evaluator answers with INVALID_CONTEXT.
func MultiContext(parts ...Context) Context {
	members := make([]jsonvalue.Member, 0, len(parts))
	for _, part := range parts {
		kind := "user"
		if k, ok := part.v.Field("kind").Text(); ok {
			kind = k
		}
		entry := []jsonvalue.Member{{Key: "key", Value: part.v.Field("key")}}
		if attrs := part.v.Field("attributes"); !attrs.IsNull() {
			entry = append(entry, jsonvalue.Member{Key: "attributes", Value: attrs})
		}
		members = append(members, jsonvalue.Member{Key: kind, Value: jsonvalue.Object(entry...)})
	}
	return Context{v: jsonvalue.Object(
		jsonvalue.Member{Key: "kind", Value: jsonvalue.String("multi")},
		jsonvalue.Member{Key: "contexts", Value: jsonvalue.Object(members...)},
	)}
}

// WithAttribute returns a copy of a single-kind context with an attribute set. On a context that is not a
// single-kind object it returns the context unchanged.
func (c Context) WithAttribute(name string, value Value) Context {
	if !c.v.IsObject() {
		return c
	}
	var attrs []jsonvalue.Member
	if existing := c.v.Field("attributes"); existing.IsObject() {
		for _, key := range existing.Keys() {
			attrs = append(attrs, jsonvalue.Member{Key: key, Value: existing.Field(key)})
		}
	}
	attrs = append(attrs, jsonvalue.Member{Key: name, Value: value})
	var members []jsonvalue.Member
	for _, key := range c.v.Keys() {
		if key != "attributes" {
			members = append(members, jsonvalue.Member{Key: key, Value: c.v.Field(key)})
		}
	}
	members = append(members, jsonvalue.Member{Key: "attributes", Value: jsonvalue.Object(attrs...)})
	return Context{v: jsonvalue.Object(members...)}
}

// ContextFromJSON reads a context from JSON text. It never fails: text that is not JSON gives a context the
// evaluator answers with INVALID_CONTEXT, like any other invalid one.
func ContextFromJSON(text []byte) Context {
	v, err := jsonvalue.Parse(text)
	if err != nil {
		return Context{}
	}
	return Context{v: v}
}

// ContextFromValue wraps a Value as a context; the evaluator checks its shape.
func ContextFromValue(v Value) Context { return Context{v: v} }

// Value is the context as JSON.
func (c Context) Value() Value { return c.v }

// MarshalJSON writes the context as JSON.
func (c Context) MarshalJSON() ([]byte, error) { return c.v.MarshalJSON() }
