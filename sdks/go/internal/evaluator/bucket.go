package evaluator

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// Bucket is where a context lands for a flag or a segment: the first four bytes of SHA-256 over the UTF-8 bytes
// of "<key>.<salt>.<contextKey>", read big-endian and taken modulo 100000. A lone surrogate in any of the three
// strings becomes U+FFFD in those bytes, and only here.
func Bucket(key, salt, contextKey string) (uint32, int) {
	sum := sha256.Sum256([]byte(jsonv.ToValidUTF8(key + "." + salt + "." + contextKey)))
	u := binary.BigEndian.Uint32(sum[:4])
	return u, int(u % bucketCount)
}

// kindBucket is the bucket for the key of a kind (empty is user): the key itself for user, "<kind>:<key>" for
// any other kind, and 0 with no hash when the context lacks that kind.
func kindBucket(key, salt, kind string, ctxs contexts) int {
	if kind == "" {
		kind = defaultKind
	}
	c, ok := ctxs.get(kind)
	if !ok {
		return 0
	}
	contextKey := c.key
	if kind != defaultKind {
		contextKey = kind + ":" + c.key
	}
	_, b := Bucket(key, salt, contextKey)
	return b
}
