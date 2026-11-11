package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"

const defaultKind = "user"

// kindContext is the key and attributes of one kind.
type kindContext struct {
	kind  string
	key   string
	attrs map[string]jsonv.Value
}

// contexts is a valid context as a list of kinds, at most a handful long.
type contexts []kindContext

func (c contexts) get(kind string) (kindContext, bool) {
	for i := range c {
		if c[i].kind == kind {
			return c[i], true
		}
	}
	return kindContext{}, false
}

// isKind is ^[A-Za-z0-9._-]+$ and neither "kind" nor "multi".
func isKind(s string) bool {
	if s == "" || s == "kind" || s == "multi" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

func readEntry(kind string, obj map[string]jsonv.Value) (kindContext, bool) {
	key, ok := obj["key"].Str()
	if !ok || key == "" {
		return kindContext{}, false
	}
	a := obj["attributes"]
	if a.IsNull() {
		return kindContext{kind: kind, key: key}, true
	}
	attrs, ok := a.Object()
	if !ok {
		return kindContext{}, false
	}
	return kindContext{kind: kind, key: key, attrs: attrs}, true
}

// normalise reads a context as a list of kinds, or reports that it is not valid.
func normalise(v jsonv.Value) (contexts, bool) {
	obj, ok := v.Object()
	if !ok {
		return nil, false
	}
	kind := defaultKind
	if k := obj["kind"]; !k.IsNull() {
		s, isStr := k.Str()
		if !isStr {
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
	e, ok := readEntry(kind, obj)
	if !ok {
		return nil, false
	}
	return contexts{e}, true
}

func readMulti(obj map[string]jsonv.Value) (contexts, bool) {
	if !obj["key"].IsNull() || !obj["attributes"].IsNull() {
		return nil, false
	}
	members, ok := obj["contexts"].Object()
	if !ok {
		return nil, false
	}
	out := make(contexts, 0, len(members))
	for kind, raw := range members {
		entry, ok := raw.Object()
		if !ok || !isKind(kind) || !entry["kind"].IsNull() {
			return nil, false
		}
		e, ok := readEntry(kind, entry)
		if !ok {
			return nil, false
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}
