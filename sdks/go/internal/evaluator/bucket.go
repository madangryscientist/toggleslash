// Package evaluator is the Go implementation of docs/evaluation.md: it reads a ruleset once, then
// answers "what does this flag serve for this context". It does no I/O, starts no goroutine and imports
// no regular-expression package: `matches` has its own parser and virtual machine.
package evaluator

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// BucketCount: buckets run 0..99999, and a rollout weight of 1000 is 1%.
const BucketCount = 100000

// Bucket is where a context lands for a flag or segment: SHA-256 of the UTF-8 bytes of
// "<key>.<salt>.<contextKey>", the first 4 bytes as a big-endian uint32, modulo 100000. A lone
// surrogate in any part hashes as U+FFFD.
func Bucket(key, salt, contextKey string) uint32 {
	return hash32(key, salt, contextKey) % BucketCount
}

// hash32 is the first four bytes of that SHA-256 as a big-endian uint32.
func hash32(key, salt, contextKey string) uint32 {
	sum := sha256.Sum256([]byte(wtf8.ToUTF8(key + "." + salt + "." + contextKey)))
	return binary.BigEndian.Uint32(sum[:4])
}
