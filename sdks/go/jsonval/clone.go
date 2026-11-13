package jsonval

// Clone answers a copy of v that shares no array or object storage with it, so
// a caller that mutates a served value cannot change the ruleset it came from.
// Scalars are immutable and come back as they are.
func Clone(v Value) Value {
	switch node := v.(type) {
	case Array:
		out := make(Array, len(node))
		for i, item := range node {
			out[i] = Clone(item)
		}
		return out
	case Object:
		out := make(Object, len(node))
		for key, item := range node {
			out[key] = Clone(item)
		}
		return out
	default:
		return v
	}
}
