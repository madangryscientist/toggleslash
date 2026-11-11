package toggleslash

// Context is who a flag is evaluated for: a single context (a kind, a key and
// attributes) or a multi context (one of those per kind). The zero Context is
// not valid; evaluating it gives ERROR INVALID_CONTEXT and the fallback.
//
// A Context is immutable: With and the other builders return a new one.
type Context struct {
	v Value
}

// NewContext is a "user" context with the key.
func NewContext(key string) Context {
	o := NewObject()
	o.Set("key", StringValue(key))
	return Context{ObjectValue(o)}
}

// NewKindContext is a context of the given kind with the key.
func NewKindContext(kind, key string) Context {
	o := NewObject()
	o.Set("kind", StringValue(kind))
	o.Set("key", StringValue(key))
	return Context{ObjectValue(o)}
}

// With returns a copy with the attribute set. The value may be nil, a bool, any
// integer or float type, a string, a Value, []string, []any or map[string]any;
// anything else makes the context invalid. On a multi context it does nothing.
func (c Context) With(name string, value any) Context {
	root := c.v.Object()
	if root == nil {
		return c
	}
	converted, ok := valueOf(value)
	if !ok {
		return Context{}
	}
	if kind, _ := root.Get("kind"); kind.Kind() == KindString && kind.String() == "multi" {
		return c
	}
	next := root.Clone()
	var attributes *Object
	if existing, ok := next.Get("attributes"); ok && existing.Object() != nil {
		attributes = existing.Object().Clone()
	} else {
		attributes = NewObject()
	}
	attributes.Set(cleanString(name), converted)
	next.Set("attributes", ObjectValue(attributes))
	return Context{ObjectValue(next)}
}

// MultiContext combines single contexts of different kinds into one. A context
// of a kind that appears twice, or one that is itself multi, makes the result
// invalid.
func MultiContext(parts ...Context) Context {
	contexts := NewObject()
	for _, part := range parts {
		root := part.v.Object()
		if root == nil {
			return Context{}
		}
		kind := "user"
		if raw, ok := root.Get("kind"); ok && !raw.IsNull() {
			if raw.Kind() != KindString || raw.String() == "multi" {
				return Context{}
			}
			kind = raw.String()
		}
		if contexts.Has(kind) {
			return Context{}
		}
		inner := NewObject()
		for _, name := range root.Keys() {
			if name == "kind" {
				continue
			}
			item, _ := root.Get(name)
			inner.Set(name, item)
		}
		contexts.Set(kind, ObjectValue(inner))
	}
	out := NewObject()
	out.Set("kind", StringValue("multi"))
	out.Set("contexts", ObjectValue(contexts))
	return Context{ObjectValue(out)}
}

// ContextFromJSON reads a context from JSON text, exactly as written: a
// number such as 1e999 or a lone surrogate escape reaches the evaluator
// unchanged. Text that is not JSON gives an invalid context, never an error.
func ContextFromJSON(data []byte) Context {
	v, err := ParseValue(data)
	if err != nil {
		return Context{}
	}
	return Context{v}
}

// ContextFromValue wraps a Value as a context. Whether it is a valid context is
// the evaluator's answer.
func ContextFromValue(v Value) Context { return Context{v} }

// ContextFromMap converts a map such as {"key": "u1", "kind": "user",
// "attributes": {...}} to a context. A value of a type valueOf cannot convert
// gives an invalid context.
func ContextFromMap(m map[string]any) Context {
	v, ok := valueOf(m)
	if !ok {
		return Context{}
	}
	return Context{v}
}

// Value is the context as JSON.
func (c Context) Value() Value { return c.v }
