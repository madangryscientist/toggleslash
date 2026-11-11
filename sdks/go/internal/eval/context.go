package eval

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// kindContext is the key and attributes of one kind, after normalising.
type kindContext struct {
	key        string
	attributes *jsonv.Members // nil when absent
}

// contexts is a valid context as a map from kind to that kind's key and attributes.
type contexts map[string]kindContext

func readEntry(o *jsonv.Members) (kindContext, bool) {
	key, ok := isKey(field(o, "key"))
	if !ok {
		return kindContext{}, false
	}
	attrs := field(o, "attributes")
	if attrs.IsNull() {
		return kindContext{key: key}, true
	}
	a, ok := attrs.AsObject()
	if !ok {
		return kindContext{}, false
	}
	return kindContext{key: key, attributes: a}, true
}

func readMulti(o *jsonv.Members) (contexts, bool) {
	if !field(o, "key").IsNull() || !field(o, "attributes").IsNull() {
		return nil, false
	}
	entries, ok := field(o, "contexts").AsObject()
	if !ok || entries.Len() == 0 {
		return nil, false
	}
	out := make(contexts, entries.Len())
	for _, kind := range entries.Keys() {
		raw, _ := entries.Get(kind)
		ro, ok := raw.AsObject()
		if !isKindString(kind) || !ok || !field(ro, "kind").IsNull() {
			return nil, false
		}
		entry, ok := readEntry(ro)
		if !ok {
			return nil, false
		}
		out[kind] = entry
	}
	return out, true
}

// normaliseContext returns the context as a map from kind to {key, attributes}, or nil when it is not valid.
func normaliseContext(v jsonv.Value) contexts {
	o, ok := v.AsObject()
	if !ok {
		return nil
	}
	kind := defaultKind
	if kv := field(o, "kind"); !kv.IsNull() {
		s, ok := kv.AsString()
		if !ok {
			return nil
		}
		kind = s
	}
	if kind == "multi" {
		out, _ := readMulti(o)
		return out
	}
	if !isKindString(kind) {
		return nil
	}
	entry, ok := readEntry(o)
	if !ok {
		return nil
	}
	return contexts{kind: entry}
}

// bucket is where a context lands for a flag or segment: SHA-256 of the UTF-8 bytes of
// "<key>.<salt>.<contextKey>", the first 4 bytes as a big-endian uint32, modulo 100000. A lone surrogate
// becomes U+FFFD here and nowhere else.
func bucket(key, salt, contextKey string) int {
	sum := sha256.Sum256([]byte(jsonv.ToUTF8(key + "." + salt + "." + contextKey)))
	return int(binary.BigEndian.Uint32(sum[:4]) % bucketCount)
}

// kindBucket is the bucket for the key of kind: the key as it is for "user", "<kind>:<key>" for any other
// kind, and 0 when the context does not have that kind.
func kindBucket(key, salt, kind string, c contexts) int {
	entry, ok := c[kind]
	if !ok {
		return 0
	}
	if kind == defaultKind {
		return bucket(key, salt, entry.key)
	}
	return bucket(key, salt, kind+":"+entry.key)
}

// Bucket exposes bucket for the pin tests.
func Bucket(key, salt, contextKey string) (uint32, int) {
	sum := sha256.Sum256([]byte(jsonv.ToUTF8(key + "." + salt + "." + contextKey)))
	u := binary.BigEndian.Uint32(sum[:4])
	return u, int(u % bucketCount)
}
