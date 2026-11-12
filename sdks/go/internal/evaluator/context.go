package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"

// DefaultKind is what a kind that is read and absent means, everywhere.
const DefaultKind = "user"

// kindContext is the key and attributes of one kind. attrs is null when there are none.
type kindContext struct {
	key   string
	attrs jsonvalue.Value
}

// contexts is a valid context as a map from kind to that kind's key and attributes.
type contexts map[string]kindContext

// IsKind reports whether s is a valid kind: ASCII letters, digits, '.', '_' and '-', at least one, and
// neither "kind" nor "multi".
func IsKind(s string) bool {
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

func isAbsent(v jsonvalue.Value) bool { return v.IsNull() }

// field returns the named property, which is null when it is missing.
func field(obj jsonvalue.Value, name string) jsonvalue.Value {
	v, _ := obj.Get(name)
	return v
}

func readEntry(obj jsonvalue.Value) (kindContext, bool) {
	key, ok := field(obj, "key").AsString()
	if !ok || key == "" {
		return kindContext{}, false
	}
	attrs := field(obj, "attributes")
	if isAbsent(attrs) {
		return kindContext{key: key}, true
	}
	if !attrs.IsObject() {
		return kindContext{}, false
	}
	return kindContext{key: key, attrs: attrs}, true
}

func readMulti(obj jsonvalue.Value) (contexts, bool) {
	list := field(obj, "contexts")
	if !isAbsent(field(obj, "key")) || !isAbsent(field(obj, "attributes")) || !list.IsObject() {
		return nil, false
	}
	out := contexts{}
	for _, m := range list.Members() {
		if !IsKind(m.Key) || !m.Value.IsObject() || !isAbsent(field(m.Value, "kind")) {
			return nil, false
		}
		entry, ok := readEntry(m.Value)
		if !ok {
			return nil, false
		}
		out[m.Key] = entry
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// normalise reads a context as a map from kind to key and attributes, or reports that it is not valid.
func normalise(v jsonvalue.Value) (contexts, bool) {
	if !v.IsObject() {
		return nil, false
	}
	kind := DefaultKind
	if raw := field(v, "kind"); !isAbsent(raw) {
		s, ok := raw.AsString()
		if !ok {
			return nil, false
		}
		kind = s
	}
	if kind == "multi" {
		return readMulti(v)
	}
	if !IsKind(kind) {
		return nil, false
	}
	entry, ok := readEntry(v)
	if !ok {
		return nil, false
	}
	return contexts{kind: entry}, true
}
