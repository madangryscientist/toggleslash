package toggleslash

// Context is who a flag is evaluated for: a JSON object with a "key" (and an optional "kind"), or a "multi"
// context holding one such object per kind. The zero Context is not valid, so an evaluation with it answers
// the fallback with reason ERROR INVALID_CONTEXT.
type Context struct{ v Value }

// NewContext builds a user context from a key and attributes. An attribute named "key" or "kind" is
// overwritten by the key and the kind. A value that ValueFrom cannot convert gives an invalid Context.
func NewContext(key string, attributes map[string]any) Context {
	return build("", key, attributes)
}

// NewKindContext is NewContext for a context of the given kind.
func NewKindContext(kind, key string, attributes map[string]any) Context {
	return build(kind, key, attributes)
}

func build(kind, key string, attributes map[string]any) Context {
	m := make(map[string]any, len(attributes)+2)
	for name, value := range attributes {
		m[name] = value
	}
	m["key"] = key
	if kind != "" {
		m["kind"] = kind
	}
	v, err := ValueFrom(m)
	if err != nil {
		return Context{}
	}
	return Context{v: v}
}

// ContextFromValue wraps a JSON value as a Context without checking it; the evaluation does.
func ContextFromValue(v Value) Context { return Context{v: v} }

// ParseContext reads a Context from JSON text. Text that is not JSON gives an invalid Context.
func ParseContext(data []byte) Context {
	v, err := ParseValue(data)
	if err != nil {
		return Context{}
	}
	return Context{v: v}
}

// Value is the context as JSON.
func (c Context) Value() Value { return c.v }
