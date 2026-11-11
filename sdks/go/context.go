package toggleslash

// Context is the end user, or other entity, a flag is evaluated for: a single context
// ({"kind", "key", "attributes"}) or a multi context ({"kind": "multi", "contexts": {...}}), exactly
// as docs/evaluation.md defines them. No kind means "user". It is a Value, so a context read from
// JSON works as it is, and an invalid one is not an error: the answer is the fallback with
// ERROR INVALID_CONTEXT.
type Context = Value

// UserContext builds a single context of kind "user" with the given key and attributes.
func UserContext(key string, attributes map[string]any) Context {
	return NewContext("user", key, attributes)
}

// NewContext builds a single context of the given kind. A nil attributes map leaves "attributes"
// out.
func NewContext(kind, key string, attributes map[string]any) Context {
	keys := []string{"kind", "key"}
	values := []Value{String(kind), String(key)}
	if attributes != nil {
		keys = append(keys, "attributes")
		values = append(values, FromAny(attributes))
	}
	return Object(keys, values)
}

// NewMultiContext builds a multi context from single contexts. Each one's kind names its entry, and
// a context that is not a single context (it has no string kind, or is itself multi) makes the
// whole context invalid, which the evaluator answers as INVALID_CONTEXT.
func NewMultiContext(contexts ...Context) Context {
	kinds := make([]string, 0, len(contexts))
	entries := make([]Value, 0, len(contexts))
	for _, c := range contexts {
		kind := c.Field("kind")
		if kind.Kind() != KindString {
			return Value{}
		}
		kinds = append(kinds, kind.String())
		entry := []string{"key"}
		entryValues := []Value{c.Field("key")}
		if c.Has("attributes") {
			entry = append(entry, "attributes")
			entryValues = append(entryValues, c.Field("attributes"))
		}
		entries = append(entries, Object(entry, entryValues))
	}
	return Object([]string{"kind", "contexts"}, []Value{String("multi"), Object(kinds, entries)})
}
