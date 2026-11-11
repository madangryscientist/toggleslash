package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"

// kindContext is the key and attributes of one kind.
type kindContext struct {
	kind  string
	key   string
	attrs *jsonv.Object // nil when there are none
}

// contexts is a valid context read as a map from kind to key and attributes. There are few kinds, so it is a
// slice.
type contexts []kindContext

func (c contexts) get(kind string) (kindContext, bool) {
	for _, k := range c {
		if k.kind == kind {
			return k, true
		}
	}
	return kindContext{}, false
}

// isKind is true for a string that matches ^[A-Za-z0-9._-]+$ and is neither "kind" nor "multi".
func isKind(s string) bool {
	if s == "" || s == "kind" || s == "multi" {
		return false
	}
	for i := range len(s) {
		c := s[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}

func readEntry(kind string, o jsonv.Value) (kindContext, bool) {
	key, ok := o.Get("key").AsString()
	if !ok || key == "" {
		return kindContext{}, false
	}
	attrs := o.Get("attributes")
	if attrs.IsNull() {
		return kindContext{kind: kind, key: key}, true
	}
	obj, ok := attrs.AsObject()
	if !ok {
		return kindContext{}, false
	}
	return kindContext{kind: kind, key: key, attrs: obj}, true
}

func readMulti(v jsonv.Value) (contexts, bool) {
	if !v.Get("key").IsNull() || !v.Get("attributes").IsNull() {
		return nil, false
	}
	list, ok := v.Get("contexts").AsObject()
	if !ok || list.Len() == 0 {
		return nil, false
	}
	out := make(contexts, 0, list.Len())
	for _, kind := range list.Keys() {
		raw, _ := list.Get(kind)
		if !isKind(kind) {
			return nil, false
		}
		if _, isObject := raw.AsObject(); !isObject || !raw.Get("kind").IsNull() {
			return nil, false
		}
		entry, ok := readEntry(kind, raw)
		if !ok {
			return nil, false
		}
		out = append(out, entry)
	}
	return out, true
}

// normaliseContext reads a context as a map from kind to key and attributes; ok is false for an invalid one.
func normaliseContext(v jsonv.Value) (contexts, bool) {
	if _, ok := v.AsObject(); !ok {
		return nil, false
	}
	kindValue := v.Get("kind")
	kind := defaultKind
	if !kindValue.IsNull() {
		s, ok := kindValue.AsString()
		if !ok {
			return nil, false
		}
		kind = s
	}
	if kind == "multi" {
		return readMulti(v)
	}
	if !isKind(kind) {
		return nil, false
	}
	entry, ok := readEntry(kind, v)
	if !ok {
		return nil, false
	}
	return contexts{entry}, true
}
