package toggleslash

import (
	"encoding/json"
	"sort"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// Value is a JSON value: what a context is made of, what a JSON flag serves, and what an Event carries. Numbers
// are IEEE-754 binary64, as the evaluation spec requires; a string keeps a lone surrogate. Read one with Kind and
// the As… methods; build one with ValueOf.
type Value = jsonvalue.Value

// Kind is the JSON type of a Value.
type Kind = jsonvalue.Kind

// The kinds of a Value.
const (
	KindNull   = jsonvalue.KindNull
	KindBool   = jsonvalue.KindBool
	KindNumber = jsonvalue.KindNumber
	KindString = jsonvalue.KindString
	KindArray  = jsonvalue.KindArray
	KindObject = jsonvalue.KindObject
)

// Context is who a flag is evaluated for. It is a Value in the shape of docs/evaluation.md: build one with
// NewContext, NewContextKind, NewMultiContext or ParseContext. A context that is not valid is not an error here;
// evaluating with it answers the fallback and ErrInvalidContext.
type Context = Value

// ValueOf turns a Go value into a Value. It knows nil, bool, every integer width, float32, float64, string,
// json.Number, Value, []any, []string, []bool, []float64, []int, map[string]any and map[string]string. Every number
// becomes the binary64 nearest to it, as a number the server serves does. A value ValueOf does not know becomes
// null. A string that is not valid UTF-8 has each ill-formed byte replaced with U+FFFD.
func ValueOf(x any) Value {
	switch v := x.(type) {
	case nil:
		return jsonvalue.Null()
	case Value:
		return v
	case bool:
		return jsonvalue.Bool(v)
	case string:
		return jsonvalue.String(wtf8.Sanitize(v))
	case int:
		return jsonvalue.Number(float64(v))
	case int8:
		return jsonvalue.Number(float64(v))
	case int16:
		return jsonvalue.Number(float64(v))
	case int32:
		return jsonvalue.Number(float64(v))
	case int64:
		return jsonvalue.Number(float64(v))
	case uint:
		return jsonvalue.Number(float64(v))
	case uint8:
		return jsonvalue.Number(float64(v))
	case uint16:
		return jsonvalue.Number(float64(v))
	case uint32:
		return jsonvalue.Number(float64(v))
	case uint64:
		return jsonvalue.Number(float64(v))
	case float32:
		return jsonvalue.Number(float64(v))
	case float64:
		return jsonvalue.Number(v)
	case json.Number:
		if parsed, err := jsonvalue.Parse(v.String()); err == nil && parsed.Kind() == jsonvalue.KindNumber {
			return parsed
		}
		return jsonvalue.Null()
	case []any:
		items := make([]Value, len(v))
		for i, item := range v {
			items[i] = ValueOf(item)
		}
		return jsonvalue.Array(items)
	case []string:
		items := make([]Value, len(v))
		for i, item := range v {
			items[i] = ValueOf(item)
		}
		return jsonvalue.Array(items)
	case []bool:
		items := make([]Value, len(v))
		for i, item := range v {
			items[i] = jsonvalue.Bool(item)
		}
		return jsonvalue.Array(items)
	case []float64:
		items := make([]Value, len(v))
		for i, item := range v {
			items[i] = jsonvalue.Number(item)
		}
		return jsonvalue.Array(items)
	case []int:
		items := make([]Value, len(v))
		for i, item := range v {
			items[i] = jsonvalue.Number(float64(item))
		}
		return jsonvalue.Array(items)
	case map[string]any:
		members := make([]jsonvalue.Member, 0, len(v))
		for _, key := range sortedKeys(v) {
			members = append(members, jsonvalue.Member{Key: wtf8.Sanitize(key), Value: ValueOf(v[key])})
		}
		return jsonvalue.Object(members...)
	case map[string]string:
		members := make([]jsonvalue.Member, 0, len(v))
		for _, key := range sortedKeys(v) {
			members = append(members, jsonvalue.Member{Key: wtf8.Sanitize(key), Value: ValueOf(v[key])})
		}
		return jsonvalue.Object(members...)
	}
	return jsonvalue.Null()
}

// sortedKeys orders a map's keys so the Value built from it does not depend on Go's random map order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// NewContext builds a context of the default kind, "user", for key and the given attributes. attributes may be nil.
func NewContext(key string, attributes map[string]any) Context {
	return newSingle("", key, attributes)
}

// NewContextKind builds a single context of the given kind. A kind that is not valid makes an invalid context.
func NewContextKind(kind, key string, attributes map[string]any) Context {
	return newSingle(kind, key, attributes)
}

func newSingle(kind, key string, attributes map[string]any) Context {
	var members []jsonvalue.Member
	if kind != "" {
		members = append(members, jsonvalue.Member{Key: "kind", Value: ValueOf(kind)})
	}
	members = append(members, jsonvalue.Member{Key: "key", Value: ValueOf(key)})
	if attributes != nil {
		members = append(members, jsonvalue.Member{Key: "attributes", Value: ValueOf(attributes)})
	}
	return jsonvalue.Object(members...)
}

// NewMultiContext builds a multi-kind context from single contexts, keyed by kind. It takes each entry's key and
// attributes (any kind the entry names is ignored); an entry that is not an object is passed through, which makes
// the whole context invalid.
func NewMultiContext(contexts map[string]Context) Context {
	entries := make([]jsonvalue.Member, 0, len(contexts))
	for _, kind := range sortedKeys(contexts) {
		entry := contexts[kind]
		if !entry.IsObject() {
			entries = append(entries, jsonvalue.Member{Key: wtf8.Sanitize(kind), Value: entry})
			continue
		}
		var members []jsonvalue.Member
		for _, name := range []string{"key", "attributes"} {
			if v, ok := entry.Get(name); ok {
				members = append(members, jsonvalue.Member{Key: name, Value: v})
			}
		}
		entries = append(entries, jsonvalue.Member{Key: wtf8.Sanitize(kind), Value: jsonvalue.Object(members...)})
	}
	return jsonvalue.Object(
		jsonvalue.Member{Key: "kind", Value: jsonvalue.String("multi")},
		jsonvalue.Member{Key: "contexts", Value: jsonvalue.Object(entries...)},
	)
}

// ParseContext reads a context from JSON text. It is the form that keeps a number such as 1e999 and a lone
// surrogate escape as the evaluation spec reads them. Text that is not JSON gives a null Context, which
// evaluates as an invalid context.
func ParseContext(data []byte) Context {
	v, err := jsonvalue.ParseBytes(data)
	if err != nil {
		return jsonvalue.Null()
	}
	return v
}
