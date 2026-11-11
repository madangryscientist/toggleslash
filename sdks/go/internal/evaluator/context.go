package evaluator

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wjson"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// BucketCount is how many buckets a key lands in: 0 to 99999.
const BucketCount = 100000

// defaultKind is what an absent kind means, everywhere.
const defaultKind = "user"

// kindContext is the key and attributes of one kind, after normalising.
type kindContext struct {
	kind       string
	key        string
	attributes *wjson.Object // nil when there are none
}

// contexts is a valid context as its kinds, in the order they were given.
type contexts []kindContext

func (c contexts) get(kind string) (*kindContext, bool) {
	for i := range c {
		if c[i].kind == kind {
			return &c[i], true
		}
	}
	return nil, false
}

func readEntry(kind string, o *wjson.Object) (kindContext, bool) {
	key, _ := o.Get("key")
	if key.Kind() != wjson.KindString || key.String() == "" {
		return kindContext{}, false
	}
	attributes, _ := o.Get("attributes")
	switch attributes.Kind() {
	case wjson.KindNull:
		return kindContext{kind: kind, key: key.String()}, true
	case wjson.KindObject:
		return kindContext{kind: kind, key: key.String(), attributes: attributes.Object()}, true
	}
	return kindContext{}, false
}

func readMulti(o *wjson.Object) (contexts, bool) {
	if !absent(o, "key") || !absent(o, "attributes") {
		return nil, false
	}
	raw, _ := o.Get("contexts")
	entries := raw.Object()
	if entries == nil {
		return nil, false
	}
	var out contexts
	for _, kind := range entries.Keys() {
		value, _ := entries.Get(kind)
		inner := value.Object()
		if !isKind(kind) || inner == nil || !absent(inner, "kind") {
			return nil, false
		}
		entry, ok := readEntry(kind, inner)
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

// normaliseContext turns a context value into its kinds, or reports false when
// it is not a valid context.
func normaliseContext(value wjson.Value) (contexts, bool) {
	o := value.Object()
	if o == nil {
		return nil, false
	}
	kindValue, present := o.Get("kind")
	kind := defaultKind
	if present && !kindValue.IsNull() {
		if kindValue.Kind() != wjson.KindString {
			return nil, false
		}
		kind = kindValue.String()
	}
	if kind == "multi" {
		return readMulti(o)
	}
	if !isKind(kind) {
		return nil, false
	}
	entry, ok := readEntry(kind, o)
	if !ok {
		return nil, false
	}
	return contexts{entry}, true
}

// Bucket is where a context lands for a flag or segment: SHA-256 of the UTF-8
// bytes of "<key>.<salt>.<contextKey>", the first four bytes as a big-endian
// uint32, modulo 100000. It returns the bucket and the uint32.
func Bucket(key, salt, contextKey string) (int, uint32) {
	sum := sha256.Sum256([]byte(wtf8.ToUTF8(key + "." + salt + "." + contextKey)))
	n := binary.BigEndian.Uint32(sum[:4])
	return int(n % BucketCount), n
}

// kindBucket is the bucket for the key of kind (empty is "user"): the key as it
// is for "user", "<kind>:<key>" for any other kind, and 0 when the context does
// not have that kind.
func kindBucket(key, salt, kind string, c contexts) int {
	if kind == "" {
		kind = defaultKind
	}
	ctx, ok := c.get(kind)
	if !ok {
		return 0
	}
	contextKey := ctx.key
	if kind != defaultKind {
		contextKey = kind + ":" + ctx.key
	}
	b, _ := Bucket(key, salt, contextKey)
	return b
}
