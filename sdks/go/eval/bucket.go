package eval

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonval"
)

// bucketOf is the bucket the context lands in for a flag or a segment rule. A
// context without the kind buckets to 0 without hashing.
func bucketOf(key, salt, kind string, contexts contextMap) int {
	context, ok := contexts[kind]
	if !ok {
		return 0
	}
	contextKey := context.key
	if kind != DefaultKind {
		contextKey = kind + ":" + context.key
	}
	_, bucket := bucketHash(key, salt, contextKey)
	return bucket
}

// bucketHash is SHA-256 of `key.salt.contextKey`: its first four bytes as a
// big-endian uint32, and that value modulo 100000. A lone surrogate hashes as
// U+FFFD.
func bucketHash(key, salt, contextKey string) (uint32, int) {
	text := jsonval.ReplaceLoneSurrogates(key + "." + salt + "." + contextKey)
	digest := sha256.Sum256([]byte(text))
	raw := binary.BigEndian.Uint32(digest[:4])
	return raw, int(raw % BucketCount)
}
