package eval

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"

// defaultKind is what a kind that is read and absent means, everywhere.
const defaultKind = "user"

// kindContext is the key and attributes of one kind, after normalising. Attributes is the zero Value
// (null) when there are none.
type kindContext struct {
	key        string
	attributes jsonvalue.Value
}

// Contexts is a valid context as a map from kind to that kind's key and attributes.
type Contexts map[string]kindContext

// isKind is `^[A-Za-z0-9._-]+$`, and neither `kind` nor `multi`.
func isKind(s string) bool {
	if s == "" || s == "kind" || s == "multi" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}

func readEntry(v jsonvalue.Value) (kindContext, bool) {
	key, _ := v.Get("key")
	if key.Kind != jsonvalue.String || key.S == "" {
		return kindContext{}, false
	}
	attrs, _ := v.Get("attributes")
	switch attrs.Kind {
	case jsonvalue.Null:
		return kindContext{key: key.S}, true
	case jsonvalue.Object:
		return kindContext{key: key.S, attributes: attrs}, true
	}
	return kindContext{}, false
}

func readMulti(v jsonvalue.Value) (Contexts, bool) {
	key, _ := v.Get("key")
	attrs, _ := v.Get("attributes")
	contexts, _ := v.Get("contexts")
	if !key.IsAbsent() || !attrs.IsAbsent() || contexts.Kind != jsonvalue.Object {
		return nil, false
	}
	out := Contexts{}
	for _, kind := range contexts.O.Keys {
		raw := contexts.O.M[kind]
		if !isKind(kind) || raw.Kind != jsonvalue.Object {
			return nil, false
		}
		if inner, _ := raw.Get("kind"); !inner.IsAbsent() {
			return nil, false
		}
		entry, ok := readEntry(raw)
		if !ok {
			return nil, false
		}
		out[kind] = entry
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// NormaliseContext reads a context as a map from kind to `{ key, attributes }`, or reports false when it
// is not valid.
func NormaliseContext(v jsonvalue.Value) (Contexts, bool) {
	if v.Kind != jsonvalue.Object {
		return nil, false
	}
	kindValue, _ := v.Get("kind")
	kind := defaultKind
	if !kindValue.IsAbsent() {
		if kindValue.Kind != jsonvalue.String {
			return nil, false
		}
		kind = kindValue.S
	}
	if kind == "multi" {
		return readMulti(v)
	}
	if !isKind(kind) {
		return nil, false
	}
	entry, ok := readEntry(v)
	if !ok {
		return nil, false
	}
	return Contexts{kind: entry}, true
}

// kindBucket is the bucket for the key of kind (empty is `user`): the key as it is for `user`,
// `<kind>:<key>` for any other kind, and 0 when the context does not have that kind.
func kindBucket(key, salt, kind string, contexts Contexts) int {
	if kind == "" {
		kind = defaultKind
	}
	c, ok := contexts[kind]
	if !ok {
		return 0
	}
	if kind == defaultKind {
		return Bucket(key, salt, c.key)
	}
	return Bucket(key, salt, kind+":"+c.key)
}
