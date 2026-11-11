package evaluator

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// bucketCount is the number of buckets; a rollout weight of 1000 is 1%.
const bucketCount = 100000

// Bucket is where a context lands for a flag or segment: the first four bytes of the SHA-256 of the
// UTF-8 bytes of "<key>.<salt>.<contextKey>", read as a big-endian uint32, modulo 100000. A lone
// surrogate in any of the three is hashed as U+FFFD.
func Bucket(key, salt, contextKey string) uint32 {
	return Hash32(key, salt, contextKey) % bucketCount
}

// Hash32 is the unreduced uint32 that Bucket reduces; the bucket pins check it too.
func Hash32(key, salt, contextKey string) uint32 {
	input := wtf8.ToUTF8(key) + "." + wtf8.ToUTF8(salt) + "." + wtf8.ToUTF8(contextKey)
	sum := sha256.Sum256([]byte(input))
	return binary.BigEndian.Uint32(sum[:4])
}
