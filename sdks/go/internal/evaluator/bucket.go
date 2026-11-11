package evaluator

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// bucketCount is the number of buckets; a rollout weight of 1000 is 1 %.
const bucketCount = 100000

// Hash returns the first four bytes of SHA-256 over the UTF-8 bytes of "<key>.<salt>.<contextKey>" as a
// big-endian uint32, and that number modulo 100000. A lone surrogate in any part hashes as U+FFFD.
func Hash(key, salt, contextKey string) (uint32, int) {
	sum := sha256.Sum256([]byte(jsonv.ToUTF8(key) + "." + jsonv.ToUTF8(salt) + "." + jsonv.ToUTF8(contextKey)))
	n := binary.BigEndian.Uint32(sum[:4])
	return n, int(n % bucketCount)
}

// kindBucket is the bucket for the key of kind (empty is "user"): the key as it is for "user",
// "<kind>:<key>" for any other kind, and 0 when the context does not have that kind.
func kindBucket(key, salt, kind string, ctx contexts) int {
	if kind == "" {
		kind = defaultKind
	}
	c, ok := ctx.get(kind)
	if !ok {
		return 0
	}
	contextKey := c.key
	if kind != defaultKind {
		contextKey = kind + ":" + c.key
	}
	_, b := Hash(key, salt, contextKey)
	return b
}
