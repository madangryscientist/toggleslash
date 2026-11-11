package eval

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// bucketCount: buckets run 0 to 99999; a rollout weight of 1000 is 1%.
const bucketCount = 100_000

// Uint32 is the first four bytes of the SHA-256 of the UTF-8 bytes of `<key>.<salt>.<contextKey>` as a
// big-endian unsigned integer. A lone surrogate is hashed as U+FFFD.
func Uint32(key, salt, contextKey string) uint32 {
	sum := sha256.Sum256([]byte(wtf8.ToUTF8(key + "." + salt + "." + contextKey)))
	return binary.BigEndian.Uint32(sum[:4])
}

// Bucket is where a context lands for a flag or segment: Uint32 modulo 100000.
func Bucket(key, salt, contextKey string) int {
	return int(Uint32(key, salt, contextKey) % bucketCount)
}
