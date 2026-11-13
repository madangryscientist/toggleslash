package eval

import "github.com/madangryscientist/toggleslash/sdks/go/jsonval"

// kindContext is one kind's key and attributes, after normalising.
type kindContext struct {
	key        string
	attributes jsonval.Object
}

// contextMap is a valid context as a map from kind to that kind's entry.
type contextMap map[string]kindContext

// normaliseContext reads a context into a map from kind to its entry, and
// reports whether it is valid at all.
func normaliseContext(value jsonval.Value) (contextMap, bool) {
	obj, ok := asObject(value)
	if !ok {
		return nil, false
	}
	kind := DefaultKind
	if v := obj["kind"]; !absent(v) {
		s, ok := asString(v)
		if !ok {
			return nil, false
		}
		kind = s
	}
	if kind == "multi" {
		return readMulti(obj)
	}
	if !isKind(kind) {
		return nil, false
	}
	entry, ok := readEntry(obj)
	if !ok {
		return nil, false
	}
	return contextMap{kind: entry}, true
}

func readMulti(obj jsonval.Object) (contextMap, bool) {
	if !absent(obj["key"]) || !absent(obj["attributes"]) {
		return nil, false
	}
	contexts, ok := asObject(obj["contexts"])
	if !ok {
		return nil, false
	}
	out := contextMap{}
	for kind, raw := range contexts {
		if !isKind(kind) {
			return nil, false
		}
		entry, ok := asObject(raw)
		if !ok {
			return nil, false
		}
		if !absent(entry["kind"]) {
			return nil, false
		}
		context, ok := readEntry(entry)
		if !ok {
			return nil, false
		}
		out[kind] = context
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func readEntry(obj jsonval.Object) (kindContext, bool) {
	key, ok := keyOf(obj["key"])
	if !ok {
		return kindContext{}, false
	}
	if v := obj["attributes"]; !absent(v) {
		attributes, ok := asObject(v)
		if !ok {
			return kindContext{}, false
		}
		return kindContext{key: key, attributes: attributes}, true
	}
	return kindContext{key: key}, true
}
