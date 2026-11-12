package evaluator

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// BucketCount is the number of buckets: they run 0 to 99999, and a rollout weight of 1000 is one percent.
const BucketCount = 100_000

// Bucket is where a context lands for a flag or a segment: the first four bytes of the SHA-256 of the UTF-8
// bytes of "<key>.<salt>.<contextKey>", read as a big-endian uint32, modulo 100000. A lone surrogate is
// hashed as U+FFFD.
func Bucket(key, salt, contextKey string) uint32 {
	sum := sha256.Sum256([]byte(wtf8.ToUTF8(key + "." + salt + "." + contextKey)))
	return binary.BigEndian.Uint32(sum[:4]) % BucketCount
}

// hashPrefix is the first four bytes of the hash as a number, which the pins check.
func hashPrefix(key, salt, contextKey string) uint32 {
	sum := sha256.Sum256([]byte(wtf8.ToUTF8(key + "." + salt + "." + contextKey)))
	return binary.BigEndian.Uint32(sum[:4])
}

// kindBucket is the bucket for the key of kind (empty is "user"): the key as it is for "user", "<kind>:<key>"
// for any other kind, and 0 when the context does not have that kind.
func kindBucket(key, salt, kind string, ctx contexts) int {
	if kind == "" {
		kind = DefaultKind
	}
	entry, ok := ctx[kind]
	if !ok {
		return 0
	}
	if kind == DefaultKind {
		return int(Bucket(key, salt, entry.key))
	}
	return int(Bucket(key, salt, kind+":"+entry.key))
}
