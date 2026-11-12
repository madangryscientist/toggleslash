package toggleslash

import (
	"math"
	"reflect"
)

// Contexts, to the "Context" and "Context kinds" sections of docs/evaluation.md.

const defaultKind = "user"

// kindContext is one context after normalising: its key and attributes (nil when absent).
type kindContext struct {
	key        string
	attributes map[string]any
}

func isKind(value any) bool {
	text, ok := value.(string)
	if !ok || text == "" || text == "kind" || text == "multi" {
		return false
	}
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

func isOptionalKind(value any) bool {
	return value == nil || isKind(value)
}

func readEntry(value map[string]any) (kindContext, bool) {
	key, ok := value["key"].(string)
	if !ok || key == "" {
		return kindContext{}, false
	}
	raw, present := value["attributes"]
	if !present || raw == nil {
		return kindContext{key: key}, true
	}
	attributes, ok := raw.(map[string]any)
	if !ok {
		return kindContext{}, false
	}
	return kindContext{key: key, attributes: attributes}, true
}

func readMulti(value map[string]any) (map[string]kindContext, bool) {
	if value["key"] != nil || value["attributes"] != nil {
		return nil, false
	}
	contexts, ok := value["contexts"].(map[string]any)
	if !ok || len(contexts) == 0 {
		return nil, false
	}
	out := make(map[string]kindContext, len(contexts))
	for kind, raw := range contexts {
		if !isKind(kind) {
			return nil, false
		}
		entry, ok := raw.(map[string]any)
		if !ok || entry["kind"] != nil {
			return nil, false
		}
		normal, ok := readEntry(entry)
		if !ok {
			return nil, false
		}
		out[kind] = normal
	}
	return out, true
}

// normaliseContext reads a context as a map from kind to its key and attributes, or reports it invalid.
func normaliseContext(value any) (map[string]kindContext, bool) {
	record, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	raw, present := record["kind"]
	kind := any(defaultKind)
	if present && raw != nil {
		kind = raw
	}
	if text, ok := kind.(string); ok && text == "multi" {
		return readMulti(record)
	}
	if !isKind(kind) {
		return nil, false
	}
	entry, ok := readEntry(record)
	if !ok {
		return nil, false
	}
	return map[string]kindContext{kind.(string): entry}, true
}

// kindBucket is the bucket for the key of a kind (an empty kind is "user"); it is 0 when the context does
// not have that kind, and no hash is computed then.
func kindBucket(key, salt, kind string, contexts map[string]kindContext) int {
	if kind == "" {
		kind = defaultKind
	}
	context, ok := contexts[kind]
	if !ok {
		return 0
	}
	contextKey := context.key
	if kind != defaultKind {
		contextKey = kind + ":" + context.key
	}
	return bucketOf(key, salt, contextKey)
}

// numericValue reads a number of any of Go's numeric types as binary64. A bool is not a number, and neither
// is anything else.
func numericValue(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case float32:
		return float64(number), true
	case int:
		return float64(number), true
	case int8:
		return float64(number), true
	case int16:
		return float64(number), true
	case int32:
		return float64(number), true
	case int64:
		return float64(number), true
	case uint:
		return float64(number), true
	case uint8:
		return float64(number), true
	case uint16:
		return float64(number), true
	case uint32:
		return float64(number), true
	case uint64:
		return float64(number), true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(reflected.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(reflected.Uint()), true
	case reflect.Float32, reflect.Float64:
		return reflected.Float(), true
	}
	return 0, false
}

// jsonKind is the JSON kind of a value: "null", "boolean", "number", "string", "array" or "object", and ""
// for a value of no JSON kind.
func jsonKind(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	if _, ok := numericValue(value); ok {
		return "number"
	}
	return ""
}

func isFiniteNumber(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
