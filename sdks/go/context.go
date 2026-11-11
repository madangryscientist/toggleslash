package toggleslash

import (
	"encoding/json"
	"math"
	"unicode/utf8"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// maxContextDepth bounds the conversion of a Go value, so a cyclic map is an invalid context rather than a
// stack overflow.
const maxContextDepth = 10000

// toContext turns what the caller passed as a context into a Value. It reports false for a context that
// cannot be one (the caller then gets INVALID_CONTEXT). It may panic when a type the caller owns does (a
// MarshalJSON method), which variation recovers.
//
// Accepted: a Value; nil (JSON null); []byte or json.RawMessage holding JSON text; a map[string]any, []any,
// string, bool, number or json.Number tree; and any other value json.Marshal can write.
func toContext(ctx any) (jsonv.Value, bool) {
	switch v := ctx.(type) {
	case jsonv.Value:
		return v, true
	case []byte:
		return parseText(v)
	case json.RawMessage:
		return parseText(v)
	}
	return convert(ctx, 0)
}

func parseText(text []byte) (jsonv.Value, bool) {
	parsed, err := jsonv.Parse(jsonv.StripBOM(jsonv.DecodeUTF8(text)))
	if err != nil {
		return jsonv.Null(), false
	}
	return parsed, true
}

func goString(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return string(jsonv.DecodeUTF8([]byte(s)))
}

func convert(x any, depth int) (jsonv.Value, bool) {
	if depth > maxContextDepth {
		return jsonv.Null(), false
	}
	switch v := x.(type) {
	case nil:
		return jsonv.Null(), true
	case jsonv.Value:
		return v, true
	case bool:
		return jsonv.Bool(v), true
	case string:
		return jsonv.String(goString(v)), true
	case float64:
		return numberUnlessNaN(v)
	case float32:
		return numberUnlessNaN(float64(v))
	case int:
		return jsonv.Number(float64(v)), true
	case int8:
		return jsonv.Number(float64(v)), true
	case int16:
		return jsonv.Number(float64(v)), true
	case int32:
		return jsonv.Number(float64(v)), true
	case int64:
		return jsonv.Number(float64(v)), true
	case uint:
		return jsonv.Number(float64(v)), true
	case uint8:
		return jsonv.Number(float64(v)), true
	case uint16:
		return jsonv.Number(float64(v)), true
	case uint32:
		return jsonv.Number(float64(v)), true
	case uint64:
		return jsonv.Number(float64(v)), true
	case json.Number:
		return parseText([]byte(v))
	case []byte:
		return parseText(v)
	case json.RawMessage:
		return parseText(v)
	case map[string]any:
		object := jsonv.NewObject()
		for name, item := range v {
			value, ok := convert(item, depth+1)
			if !ok {
				return jsonv.Null(), false
			}
			object.Set(goString(name), value)
		}
		return jsonv.ObjectValue(object), true
	case []any:
		items := make([]jsonv.Value, len(v))
		for i, item := range v {
			value, ok := convert(item, depth+1)
			if !ok {
				return jsonv.Null(), false
			}
			items[i] = value
		}
		return jsonv.Array(items), true
	}
	text, err := json.Marshal(x)
	if err != nil {
		return jsonv.Null(), false
	}
	return parseText(text)
}

func numberUnlessNaN(f float64) (jsonv.Value, bool) {
	if math.IsNaN(f) {
		return jsonv.Null(), false
	}
	return jsonv.Number(f), true
}
