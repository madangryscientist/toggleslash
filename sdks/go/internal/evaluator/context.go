package evaluator

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// kindContext is the key and attributes of one kind, after normalising.
type kindContext struct {
	key   string
	attrs map[string]value // nil when none
}

// contexts is a valid context as a map from kind to that kind's key and attributes.
type contexts map[string]kindContext

func readEntry(members map[string]value) (kindContext, bool) {
	key, ok := members["key"].String()
	if !ok || key == "" {
		return kindContext{}, false
	}
	attrs := members["attributes"]
	if attrs.IsNull() {
		return kindContext{key: key}, true
	}
	m, ok := attrs.Object()
	return kindContext{key: key, attrs: m}, ok
}

func readMulti(members map[string]value) (contexts, bool) {
	if !members["key"].IsNull() || !members["attributes"].IsNull() {
		return nil, false
	}
	inner, ok := members["contexts"].Object()
	if !ok {
		return nil, false
	}
	out := contexts{}
	for kind, raw := range inner {
		entry, isObject := raw.Object()
		if !isKindName(kind) || !isObject || !entry["kind"].IsNull() {
			return nil, false
		}
		kc, ok := readEntry(entry)
		if !ok {
			return nil, false
		}
		out[kind] = kc
	}
	return out, len(out) > 0
}

// normaliseContext is the context as a map from kind to {key, attrs}, or false when it is not valid.
func normaliseContext(v value) (contexts, bool) {
	members, ok := v.Object()
	if !ok {
		return nil, false
	}
	kindValue := members["kind"]
	kind := defaultKind
	if !kindValue.IsNull() {
		s, isString := kindValue.String()
		if !isString {
			return nil, false
		}
		kind = s
	}
	if kind == "multi" {
		return readMulti(members)
	}
	if !isKindName(kind) {
		return nil, false
	}
	entry, ok := readEntry(members)
	if !ok {
		return nil, false
	}
	return contexts{kind: entry}, true
}

// bucket is where a context lands for a flag or segment: SHA-256 of the UTF-8 bytes of
// <key>.<salt>.<contextKey>, the first 4 bytes as a big-endian uint32, modulo 100000. A lone
// surrogate hashes as U+FFFD.
func bucket(key, salt, contextKey string) int {
	text := wtf8.ToUTF8(key + "." + salt + "." + contextKey)
	sum := sha256.Sum256([]byte(text))
	return int(binary.BigEndian.Uint32(sum[:4]) % bucketCount)
}

// kindBucket is the bucket for the key of kind ("" is user): the key as it is for user,
// <kind>:<key> for any other kind, and 0 when the context does not have that kind.
func kindBucket(key, salt, kind string, cs contexts) int {
	if kind == "" {
		kind = defaultKind
	}
	kc, ok := cs[kind]
	if !ok {
		return 0
	}
	if kind == defaultKind {
		return bucket(key, salt, kc.key)
	}
	return bucket(key, salt, kind+":"+kc.key)
}
