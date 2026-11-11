package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"

const defaultKind = "user"

// kindContext is the key and attributes of one kind.
type kindContext struct {
	kind  string
	key   string
	attrs *jsonv.Obj
}

// contexts is a valid context: one entry per kind.
type contexts []kindContext

func (cs contexts) get(kind string) (kindContext, bool) {
	for _, c := range cs {
		if c.kind == kind {
			return c, true
		}
	}
	return kindContext{}, false
}

// isKind is ^[A-Za-z0-9._-]+$ and neither "kind" nor "multi". ASCII only, byte by byte.
func isKind(s string) bool {
	if s == "" || s == "kind" || s == "multi" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}

func absent(v jsonv.Value) bool { return v.Kind() == jsonv.Null }

func readEntry(o jsonv.Value, kind string) (kindContext, bool) {
	key := o.Field("key")
	if key.Kind() != jsonv.String || key.String() == "" {
		return kindContext{}, false
	}
	attrs := o.Field("attributes")
	if absent(attrs) {
		return kindContext{kind: kind, key: key.String()}, true
	}
	if attrs.Kind() != jsonv.Object {
		return kindContext{}, false
	}
	return kindContext{kind: kind, key: key.String(), attrs: attrs.Object()}, true
}

// normalise reads a context (single or multi) or reports that it is not valid.
func normalise(v jsonv.Value) (contexts, bool) {
	if v.Kind() != jsonv.Object {
		return nil, false
	}
	kindValue := v.Field("kind")
	if absent(kindValue) {
		kindValue = jsonv.NewString(defaultKind)
	}
	if kindValue.Kind() == jsonv.String && kindValue.String() == "multi" {
		return readMulti(v)
	}
	if kindValue.Kind() != jsonv.String || !isKind(kindValue.String()) {
		return nil, false
	}
	entry, ok := readEntry(v, kindValue.String())
	if !ok {
		return nil, false
	}
	return contexts{entry}, true
}

func readMulti(v jsonv.Value) (contexts, bool) {
	inner := v.Field("contexts")
	if !absent(v.Field("key")) || !absent(v.Field("attributes")) || inner.Kind() != jsonv.Object {
		return nil, false
	}
	var out contexts
	for _, kind := range inner.Object().Keys() {
		raw, _ := inner.Object().Get(kind)
		if !isKind(kind) || raw.Kind() != jsonv.Object || !absent(raw.Field("kind")) {
			return nil, false
		}
		entry, ok := readEntry(raw, kind)
		if !ok {
			return nil, false
		}
		out = append(out, entry)
	}
	return out, len(out) > 0
}

// kindBucket is the bucket for the key of kind (empty is "user"): the key as it is for "user",
// "<kind>:<key>" for any other kind, and 0 when the context does not have that kind.
func kindBucket(key, salt, kind string, cs contexts) int {
	if kind == "" {
		kind = defaultKind
	}
	c, ok := cs.get(kind)
	if !ok {
		return 0
	}
	if kind == defaultKind {
		return Bucket(key, salt, c.key)
	}
	return Bucket(key, salt, kind+":"+c.key)
}
