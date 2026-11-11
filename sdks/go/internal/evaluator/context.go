package evaluator

import (
	"crypto/sha256"
	"encoding/binary"
	"strings"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

// kindCtx is the key and attributes of one kind, after normalising.
type kindCtx struct {
	kind  string
	key   string
	attrs *jsonvalue.Object
}

// contexts is a valid context as a map from kind to key and attributes.
type contexts struct {
	list   []kindCtx
	byKind map[string]int
}

const linearKinds = 4

func (c *contexts) get(kind string) *kindCtx {
	if c.byKind != nil {
		if i, ok := c.byKind[kind]; ok {
			return &c.list[i]
		}
		return nil
	}
	for i := range c.list {
		if c.list[i].kind == kind {
			return &c.list[i]
		}
	}
	return nil
}

// isKind is ^[A-Za-z0-9._-]+$ and neither "kind" nor "multi".
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

// field reads a property; null and absent are the same, so a missing one is the zero (null) Value.
func field(v jsonvalue.Value, name string) jsonvalue.Value {
	f, _ := v.Get(name)
	return f
}

func readEntry(kind string, v jsonvalue.Value) (kindCtx, bool) {
	key := field(v, "key")
	if !key.IsString() || key.StringValue() == "" {
		return kindCtx{}, false
	}
	attrs := field(v, "attributes")
	switch {
	case attrs.IsNull():
		return kindCtx{kind: kind, key: key.StringValue()}, true
	case attrs.IsObject():
		return kindCtx{kind: kind, key: key.StringValue(), attrs: attrs.Object()}, true
	}
	return kindCtx{}, false
}

func readMulti(v jsonvalue.Value) (contexts, bool) {
	if !field(v, "key").IsNull() || !field(v, "attributes").IsNull() {
		return contexts{}, false
	}
	entries := field(v, "contexts")
	if !entries.IsObject() || entries.Object().Len() == 0 {
		return contexts{}, false
	}
	o := entries.Object()
	out := contexts{list: make([]kindCtx, 0, o.Len())}
	for i := 0; i < o.Len(); i++ {
		kind, raw := o.At(i)
		if !isKind(kind) || !raw.IsObject() || !field(raw, "kind").IsNull() {
			return contexts{}, false
		}
		entry, ok := readEntry(kind, raw)
		if !ok {
			return contexts{}, false
		}
		out.list = append(out.list, entry)
	}
	if len(out.list) > linearKinds {
		out.byKind = make(map[string]int, len(out.list))
		for i, e := range out.list {
			out.byKind[e.kind] = i
		}
	}
	return out, true
}

// normalise reads a context as a map from kind to key and attributes, or reports that it is not valid.
func normalise(v jsonvalue.Value) (contexts, bool) {
	if !v.IsObject() {
		return contexts{}, false
	}
	kind := field(v, "kind")
	if kind.IsNull() {
		kind = jsonvalue.String(defaultKind)
	}
	if !kind.IsString() {
		return contexts{}, false
	}
	if kind.StringValue() == "multi" {
		return readMulti(v)
	}
	if !isKind(kind.StringValue()) {
		return contexts{}, false
	}
	entry, ok := readEntry(kind.StringValue(), v)
	if !ok {
		return contexts{}, false
	}
	return contexts{list: []kindCtx{entry}}, true
}

// hash32 is the first four bytes of SHA-256 of "<key>.<salt>.<contextKey>" as a big-endian uint32. A lone
// surrogate hashes as U+FFFD, which is what encoding the string to UTF-8 does.
func hash32(key, salt, contextKey string) uint32 {
	input := jsonvalue.ReplaceLoneSurrogates(key + "." + salt + "." + contextKey)
	sum := sha256.Sum256([]byte(input))
	return binary.BigEndian.Uint32(sum[:4])
}

// bucketOf is hash32 modulo 100000.
func bucketOf(key, salt, contextKey string) int {
	return int(hash32(key, salt, contextKey) % bucketCount)
}

// kindBucket is the bucket for the key of kind (empty is user): the key as it is for user, "<kind>:<key>"
// for any other kind, and 0 when the context does not have that kind.
func (c *contexts) kindBucket(key, salt, kind string) int {
	if kind == "" {
		kind = defaultKind
	}
	k := c.get(kind)
	if k == nil {
		return 0
	}
	if kind == defaultKind {
		return bucketOf(key, salt, k.key)
	}
	var b strings.Builder
	b.WriteString(kind)
	b.WriteByte(':')
	b.WriteString(k.key)
	return bucketOf(key, salt, b.String())
}
