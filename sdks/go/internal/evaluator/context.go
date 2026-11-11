package evaluator

import (
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

// defaultKind is what a kind that is read and absent means, everywhere.
const defaultKind = "user"

// kindContext is the key and attributes of one kind, after normalising.
type kindContext struct {
	key        string
	attributes *jsonvalue.Object // nil when there are none
}

// contexts is a valid context as a map from kind to that kind's key and attributes.
type contexts map[string]kindContext

// isKind reports whether s matches ^[A-Za-z0-9._-]+$ and is neither "kind" nor "multi".
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

func isAbsent(v jsonvalue.Value) bool { return v.IsNull() }

// readEntry reads {key, attributes}: a non-empty string key and an attributes that is absent or an object.
func readEntry(obj *jsonvalue.Object) (kindContext, bool) {
	keyValue, _ := obj.Get("key")
	key, ok := keyValue.AsString()
	if !ok || key == "" {
		return kindContext{}, false
	}
	attributes, _ := obj.Get("attributes")
	if isAbsent(attributes) {
		return kindContext{key: key}, true
	}
	attrs, ok := attributes.AsObject()
	if !ok {
		return kindContext{}, false
	}
	return kindContext{key: key, attributes: attrs}, true
}

func readMulti(obj *jsonvalue.Object) (contexts, bool) {
	if key, _ := obj.Get("key"); !isAbsent(key) {
		return nil, false
	}
	if attributes, _ := obj.Get("attributes"); !isAbsent(attributes) {
		return nil, false
	}
	raw, _ := obj.Get("contexts")
	entries, ok := raw.AsObject()
	if !ok || entries.Len() == 0 {
		return nil, false
	}
	out := make(contexts, entries.Len())
	for _, kind := range entries.Keys() {
		value, _ := entries.Get(kind)
		entry, isObject := value.AsObject()
		if !isKind(kind) || !isObject {
			return nil, false
		}
		if inner, _ := entry.Get("kind"); !isAbsent(inner) {
			return nil, false
		}
		kc, ok := readEntry(entry)
		if !ok {
			return nil, false
		}
		out[kind] = kc
	}
	return out, true
}

// normalise reads a context as a map from kind to {key, attributes}, or reports that it is not valid.
func normalise(value jsonvalue.Value) (contexts, bool) {
	obj, ok := value.AsObject()
	if !ok {
		return nil, false
	}
	kindValue, _ := obj.Get("kind")
	kind := defaultKind
	if !isAbsent(kindValue) {
		s, isString := kindValue.AsString()
		if !isString {
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
	return contexts{kind: entry}, true
}

// kindBucket is the bucket for the key of kind (empty is "user"): the key as it is for "user",
// "<kind>:<key>" for any other kind, and 0 when the context does not have that kind.
func kindBucket(key, salt, kind string, ctx contexts) uint32 {
	if kind == "" {
		kind = defaultKind
	}
	kc, ok := ctx[kind]
	if !ok {
		return 0
	}
	if kind == defaultKind {
		return Bucket(key, salt, kc.key)
	}
	return Bucket(key, salt, kind+":"+kc.key)
}
