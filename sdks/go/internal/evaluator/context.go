package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"

const defaultKind = "user"

// kindContext is the key and attributes of one kind, after normalising. attrs is null or an object.
type kindContext struct {
	kind  string
	key   string
	attrs jsonv.Value
}

// contexts is a valid context as a list of kinds; a single context has one entry.
type contexts []kindContext

func (c contexts) find(kind string) (*kindContext, bool) {
	for i := range c {
		if c[i].kind == kind {
			return &c[i], true
		}
	}
	return nil, false
}

// isKind is ^[A-Za-z0-9._-]+$, and neither "kind" nor "multi". It walks bytes; no regular expression.
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

// absent reports whether the member is missing or null (the same thing for an optional field).
func absent(obj jsonv.Value, name string) bool {
	v, ok := obj.Get(name)
	return !ok || v.IsNull()
}

func readEntry(kind string, obj jsonv.Value) (kindContext, bool) {
	kv, _ := obj.Get("key")
	key, ok := kv.AsString()
	if !ok || key == "" {
		return kindContext{}, false
	}
	attrs, has := obj.Get("attributes")
	if !has || attrs.IsNull() {
		return kindContext{kind: kind, key: key}, true
	}
	if !attrs.IsObject() {
		return kindContext{}, false
	}
	return kindContext{kind: kind, key: key, attrs: attrs}, true
}

func readMulti(obj jsonv.Value) (contexts, bool) {
	list, _ := obj.Get("contexts")
	if !absent(obj, "key") || !absent(obj, "attributes") || !list.IsObject() {
		return nil, false
	}
	out := make(contexts, 0, list.Len())
	for _, kind := range list.Keys() {
		raw, _ := list.Get(kind)
		if !isKind(kind) || !raw.IsObject() || !absent(raw, "kind") {
			return nil, false
		}
		entry, ok := readEntry(kind, raw)
		if !ok {
			return nil, false
		}
		out = append(out, entry)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// normaliseContext returns the context by kind, or false when it is not valid.
func normaliseContext(v jsonv.Value) (contexts, bool) {
	if !v.IsObject() {
		return nil, false
	}
	kind := defaultKind
	if kv, ok := v.Get("kind"); ok && !kv.IsNull() {
		s, isStr := kv.AsString()
		if !isStr {
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

// kindBucket is the bucket for the key of kind: the key as it is for "user", "<kind>:<key>" for any
// other kind, and 0 when the context does not have that kind.
func kindBucket(key, salt, kind string, c contexts) int {
	ctx, ok := c.find(kind)
	if !ok {
		return 0
	}
	contextKey := ctx.key
	if kind != defaultKind {
		contextKey = kind + ":" + ctx.key
	}
	return int(Bucket(key, salt, contextKey))
}
