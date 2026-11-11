package evaluator

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// BucketCount is the number of buckets; a rollout weight of 1000 is 1%.
const BucketCount = 100000

// Hash32 is the first four bytes of SHA-256 of "<key>.<salt>.<contextKey>" as a big-endian uint32.
// A lone surrogate hashes as U+FFFD, the spec's UTF-8 encoding of it.
func Hash32(key, salt, contextKey string) uint32 {
	sum := sha256.Sum256([]byte(wtf8.ReplaceLone(key + "." + salt + "." + contextKey)))
	return binary.BigEndian.Uint32(sum[:4])
}

// Bucket is Hash32 modulo BucketCount.
func Bucket(key, salt, contextKey string) int {
	return int(Hash32(key, salt, contextKey) % BucketCount)
}
