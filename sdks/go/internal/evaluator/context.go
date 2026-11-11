package evaluator

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

const defaultKind = "user"

func codePoints(s string) []rune { return wtf8.CodePoints(s) }

// optional returns the property of an object, treating null as absent.
func optional(o jsonvalue.Value, name string) (jsonvalue.Value, bool) {
	v, ok := o.Get(name)
	if !ok || v.IsNull() {
		return jsonvalue.Null, false
	}
	return v, true
}

func isKey(v jsonvalue.Value) bool {
	s, ok := v.AsString()
	return ok && s != ""
}

// validKind reports whether s matches ^[A-Za-z0-9._-]+$ and is neither "kind" nor "multi".
func validKind(s string) bool {
	if s == "" || s == "kind" || s == "multi" {
		return false
	}
	for i := range len(s) {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || isDigitByte(c) || c == '.' || c == '_' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}

// optionalKind reads an optional kind field: absent (or null) is fine, a present value must be a valid kind.
func optionalKind(o jsonvalue.Value, name string) (kind string, ok bool) {
	v, present := optional(o, name)
	if !present {
		return defaultKind, true
	}
	s, isStr := v.AsString()
	if !isStr || !validKind(s) {
		return "", false
	}
	return s, true
}

type contextEntry struct {
	key        string
	keyValue   jsonvalue.Value
	attributes jsonvalue.Value
}

// Context is a valid evaluation context, read as a map from kind to its key and attributes.
type Context struct {
	kinds map[string]contextEntry
}

func newEntry(o jsonvalue.Value) (contextEntry, bool) {
	k, present := optional(o, "key")
	if !present || !isKey(k) {
		return contextEntry{}, false
	}
	s, _ := k.AsString()
	attrs, hasAttrs := optional(o, "attributes")
	if hasAttrs && attrs.Kind() != jsonvalue.KindObject {
		return contextEntry{}, false
	}
	return contextEntry{key: s, keyValue: k, attributes: attrs}, true
}

// NewContext reads a context as docs/evaluation.md ("Context") defines it. It returns nil when the value is
// not a valid context.
func NewContext(v jsonvalue.Value) *Context {
	if v.Kind() != jsonvalue.KindObject {
		return nil
	}
	kind := defaultKind
	if kv, present := optional(v, "kind"); present {
		s, ok := kv.AsString()
		if !ok {
			return nil
		}
		kind = s
	}
	if kind == "multi" {
		if _, has := optional(v, "key"); has {
			return nil
		}
		if _, has := optional(v, "attributes"); has {
			return nil
		}
		contexts, ok := v.Get("contexts")
		if !ok || contexts.Kind() != jsonvalue.KindObject || len(contexts.Members()) == 0 {
			return nil
		}
		c := &Context{kinds: make(map[string]contextEntry, len(contexts.Members()))}
		for _, m := range contexts.Members() {
			if !validKind(m.Key) || m.Value.Kind() != jsonvalue.KindObject {
				return nil
			}
			if _, has := optional(m.Value, "kind"); has {
				return nil
			}
			e, ok := newEntry(m.Value)
			if !ok {
				return nil
			}
			c.kinds[m.Key] = e
		}
		return c
	}
	if !validKind(kind) {
		return nil
	}
	e, ok := newEntry(v)
	if !ok {
		return nil
	}
	return &Context{kinds: map[string]contextEntry{kind: e}}
}

// Bucket is the bucketing function of docs/evaluation.md: the first four bytes of SHA-256 of
// "<key>.<salt>.<contextKey>", big-endian, modulo 100000. A lone surrogate hashes as U+FFFD.
func Bucket(key, salt, contextKey string) int {
	sum := sha256.Sum256([]byte(wtf8.ToValidUTF8(key + "." + salt + "." + contextKey)))
	return int(binary.BigEndian.Uint32(sum[:4]) % 100000)
}

// kindBucket is the bucket of the context's key of the given kind, or 0 when it has no such kind.
func (c *Context) kindBucket(key, salt, kind string) int {
	e, ok := c.kinds[kind]
	if !ok {
		return 0
	}
	if kind == defaultKind {
		return Bucket(key, salt, e.key)
	}
	return Bucket(key, salt, kind+":"+e.key)
}

// read returns the value an attribute reference reaches for one kind, and whether there is one.
func (c *Context) read(kind string, ref attributeRef) (jsonvalue.Value, bool) {
	e, ok := c.kinds[kind]
	if !ok {
		return jsonvalue.Null, false
	}
	var cur jsonvalue.Value
	if ref.path[0] == "key" {
		cur = e.keyValue
	} else {
		if e.attributes.Kind() != jsonvalue.KindObject {
			return jsonvalue.Null, false
		}
		cur, ok = e.attributes.Get(ref.path[0])
		if !ok {
			return jsonvalue.Null, false
		}
	}
	for _, name := range ref.path[1:] {
		if cur.Kind() != jsonvalue.KindObject {
			return jsonvalue.Null, false
		}
		cur, ok = cur.Get(name)
		if !ok {
			return jsonvalue.Null, false
		}
	}
	return cur, true
}

// attributeRef is the parsed `attribute` of a clause: a literal name is a path of one component.
type attributeRef struct {
	path []string
}

// parseAttribute reads an attribute name. A name starting with "/" is a reference; an invalid one is not ok.
func parseAttribute(name string) (attributeRef, bool) {
	if name == "" {
		return attributeRef{}, false
	}
	if name[0] != '/' {
		return attributeRef{path: []string{name}}, true
	}
	var path []string
	rest := name[1:]
	start := 0
	for i := 0; i <= len(rest); i++ {
		if i < len(rest) && rest[i] != '/' {
			continue
		}
		part := rest[start:i]
		start = i + 1
		if part == "" {
			return attributeRef{}, false
		}
		decoded := make([]byte, 0, len(part))
		for j := 0; j < len(part); j++ {
			if part[j] != '~' {
				decoded = append(decoded, part[j])
				continue
			}
			if j+1 >= len(part) {
				return attributeRef{}, false
			}
			switch part[j+1] {
			case '0':
				decoded = append(decoded, '~')
			case '1':
				decoded = append(decoded, '/')
			default:
				return attributeRef{}, false
			}
			j++
		}
		path = append(path, string(decoded))
	}
	return attributeRef{path: path}, true
}
