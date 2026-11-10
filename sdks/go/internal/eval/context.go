// Package eval evaluates a flag for a context to docs/evaluation.md. It is the Go implementation of that
// spec and has no network or SDK code in it. Compile validates and indexes a ruleset once; Evaluate never
// panics on any input and answers every failure as an ERROR result.
package eval

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// defaultKind is what a kind that is read and absent means, everywhere.
const defaultKind = "user"

// bucketCount: buckets run 0..99999 and a rollout weight of 1000 is 1%.
const bucketCount = 100_000

// kindContext is the key and attributes of one kind, after normalising.
type kindContext struct {
	kind     string
	key      string
	keyValue jsonvalue.Value
	attrs    jsonvalue.Value // null or an object
}

// contexts is a valid context as a list of its kinds.
type contexts []kindContext

func (c contexts) get(kind string) *kindContext {
	for i := range c {
		if c[i].kind == kind {
			return &c[i]
		}
	}
	return nil
}

// isKind is ^[A-Za-z0-9._-]+$, and neither "kind" nor "multi".
func isKind(s string) bool {
	if s == "" || s == "kind" || s == "multi" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '.' || c == '_' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}

func readEntry(kind string, v jsonvalue.Value) (kindContext, bool) {
	keyValue := v.Field("key")
	key, ok := keyValue.Text()
	if !ok || key == "" {
		return kindContext{}, false
	}
	attrs := v.Field("attributes")
	if !attrs.IsNull() && !attrs.IsObject() {
		return kindContext{}, false
	}
	return kindContext{kind: kind, key: key, keyValue: keyValue, attrs: attrs}, true
}

func readMulti(v jsonvalue.Value) (contexts, bool) {
	inner := v.Field("contexts")
	if !v.Field("key").IsNull() || !v.Field("attributes").IsNull() || !inner.IsObject() {
		return nil, false
	}
	var out contexts
	for _, kind := range inner.Keys() {
		raw := inner.Field(kind)
		if !isKind(kind) || !raw.IsObject() || !raw.Field("kind").IsNull() {
			return nil, false
		}
		entry, ok := readEntry(kind, raw)
		if !ok {
			return nil, false
		}
		out = append(out, entry)
	}
	return out, len(out) > 0
}

// normaliseContext reads a context as a list of kinds, or reports that it is not valid.
func normaliseContext(v jsonvalue.Value) (contexts, bool) {
	if !v.IsObject() {
		return nil, false
	}
	kind := defaultKind
	if kv := v.Field("kind"); !kv.IsNull() {
		s, ok := kv.Text()
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

// bucket is where a context lands for a flag or segment: SHA-256 of the UTF-8 bytes of
// "<key>.<salt>.<contextKey>", the first 4 bytes as a big-endian uint32, modulo 100000. A lone surrogate
// is encoded as U+FFFD, as encoding to UTF-8 does.
func bucket(key, salt, contextKey string) int {
	return int(hash32(key, salt, contextKey) % bucketCount)
}

// hash32 is the first four bytes of the SHA-256 of "<key>.<salt>.<contextKey>", big-endian.
func hash32(key, salt, contextKey string) uint32 {
	sum := sha256.Sum256([]byte(wtf8.ToUTF8(key + "." + salt + "." + contextKey)))
	return binary.BigEndian.Uint32(sum[:4])
}

// kindBucket is the bucket for the key of kind: the key as it is for "user", "<kind>:<key>" for any other
// kind, and 0 when the context does not have that kind.
func kindBucket(key, salt, kind string, c contexts) int {
	context := c.get(kind)
	if context == nil {
		return 0
	}
	if kind == defaultKind {
		return bucket(key, salt, context.key)
	}
	return bucket(key, salt, kind+":"+context.key)
}
