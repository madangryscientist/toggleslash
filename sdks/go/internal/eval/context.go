// Package eval is the Go implementation of docs/evaluation.md: it loads a ruleset, validates it once
// and evaluates a flag for a context. It has no network code, no clock and no platform regular
// expression engine.
package eval

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

// DefaultKind is what a kind that is read and absent means, everywhere.
const DefaultKind = "user"

// BucketCount is the number of buckets: 0 to 99999, so a rollout weight of 1000 is 1%.
const BucketCount = 100000

type kindContext struct {
	key   string
	attrs jsonvalue.Value // an object, or null
}

// contexts is a valid context as a map from kind to that kind's key and attributes.
type contexts map[string]kindContext

// field reads an object member; absent is null, as the spec treats it.
func field(obj jsonvalue.Value, name string) jsonvalue.Value {
	v, _ := obj.Get(name)
	return v
}

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

func readEntry(v jsonvalue.Value) (kindContext, bool) {
	key, ok := field(v, "key").AsString()
	if !ok || key == "" {
		return kindContext{}, false
	}
	attrs := field(v, "attributes")
	if attrs.IsNull() {
		return kindContext{key: key}, true
	}
	if attrs.Kind() != jsonvalue.Object {
		return kindContext{}, false
	}
	return kindContext{key: key, attrs: attrs}, true
}

func readMulti(v jsonvalue.Value) (contexts, bool) {
	if !field(v, "key").IsNull() || !field(v, "attributes").IsNull() {
		return nil, false
	}
	list := field(v, "contexts")
	if list.Kind() != jsonvalue.Object || len(list.Members()) == 0 {
		return nil, false
	}
	out := make(contexts, len(list.Members()))
	for _, m := range list.Members() {
		if !isKind(m.Key) || m.Value.Kind() != jsonvalue.Object || !field(m.Value, "kind").IsNull() {
			return nil, false
		}
		entry, ok := readEntry(m.Value)
		if !ok {
			return nil, false
		}
		out[m.Key] = entry
	}
	return out, true
}

// normaliseContext reads a context as a map from kind to key and attributes, or reports it invalid.
func normaliseContext(v jsonvalue.Value) (contexts, bool) {
	if v.Kind() != jsonvalue.Object {
		return nil, false
	}
	kind := DefaultKind
	if k := field(v, "kind"); !k.IsNull() {
		s, ok := k.AsString()
		if !ok {
			return nil, false
		}
		kind = s
	}
	if kind == "multi" {
		return readMulti(v)
	}
	if !isKind(kind) {
		return nil, false
	}
	entry, ok := readEntry(v)
	if !ok {
		return nil, false
	}
	return contexts{kind: entry}, true
}

// Bucket is the SHA-256 of the UTF-8 bytes of "<key>.<salt>.<contextKey>", as a big-endian uint32 of
// its first four bytes. A lone surrogate in any part is hashed as U+FFFD.
func Bucket(key, salt, contextKey string) uint32 {
	sum := sha256.Sum256([]byte(jsonvalue.ReplaceLoneSurrogates(key + "." + salt + "." + contextKey)))
	return binary.BigEndian.Uint32(sum[:4])
}

// kindBucket is the bucket for the key of kind (empty is user): the key as it is for user,
// "<kind>:<key>" for any other kind, and 0 when the context does not have that kind.
func kindBucket(key, salt, kind string, ctxs contexts) int {
	if kind == "" {
		kind = DefaultKind
	}
	kc, ok := ctxs[kind]
	if !ok {
		return 0
	}
	contextKey := kc.key
	if kind != DefaultKind {
		contextKey = kind + ":" + kc.key
	}
	return int(Bucket(key, salt, contextKey) % BucketCount)
}

// parseReference splits an attribute reference (a name that starts with "/") into its components, decoding
// ~1 as / and ~0 as ~ in one left-to-right pass. It reports false when a component is empty or a ~ is
// followed by anything but 0 or 1, or ends the component.
func parseReference(text string) ([]string, bool) {
	var out []string
	rest := text[1:]
	for {
		end := 0
		for end < len(rest) && rest[end] != '/' {
			end++
		}
		part := rest[:end]
		if part == "" {
			return nil, false
		}
		decoded := make([]byte, 0, len(part))
		for i := 0; i < len(part); i++ {
			if part[i] != '~' {
				decoded = append(decoded, part[i])
				continue
			}
			if i+1 >= len(part) {
				return nil, false
			}
			switch part[i+1] {
			case '0':
				decoded = append(decoded, '~')
			case '1':
				decoded = append(decoded, '/')
			default:
				return nil, false
			}
			i++
		}
		out = append(out, string(decoded))
		if end >= len(rest) {
			return out, true
		}
		rest = rest[end+1:]
	}
}
