package toggleslash

import (
	"fmt"
	"math"
	"sort"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// ParseContext reads a context from JSON text. Unlike encoding/json it keeps a number such as 1e999
// (as an infinity) and a lone surrogate escape in a string, so a context reaches the evaluator exactly
// as docs/evaluation.md describes it. It fails only when the text is not JSON.
func ParseContext(raw []byte) (Context, error) {
	v, err := jsonv.Parse(raw)
	if err != nil {
		return Value{}, fmt.Errorf("toggleslash: the context is not valid JSON")
	}
	return v, nil
}

// NewUserContext returns the context {"key": key}: a user context, since an absent kind means "user".
func NewUserContext(key string) Context {
	return NewObject(map[string]Value{"key": NewString(key)})
}

// ContextFrom converts ordinary Go values to a Context: a map[string]any becomes an object; the leaves
// may be nil, bool, string, any integer or float type, []any, []string, map[string]any,
// map[string]string or a Value. A value of any other type becomes null, which the evaluator reads as
// an attribute that is not there (and an invalid context where a key or kind is expected).
func ContextFrom(fields map[string]any) Context { return fromAny(fields) }

func fromAny(x any) Value {
	switch v := x.(type) {
	case nil:
		return Value{}
	case Value:
		return v
	case bool:
		return NewBool(v)
	case string:
		return NewString(v)
	case float64:
		return NewNumber(v)
	case float32:
		return NewNumber(float64(v))
	case int:
		return NewNumber(float64(v))
	case int8:
		return NewNumber(float64(v))
	case int16:
		return NewNumber(float64(v))
	case int32:
		return NewNumber(float64(v))
	case int64:
		return NewNumber(float64(v))
	case uint:
		return NewNumber(float64(v))
	case uint8:
		return NewNumber(float64(v))
	case uint16:
		return NewNumber(float64(v))
	case uint32:
		return NewNumber(float64(v))
	case uint64:
		return NewNumber(float64(v))
	case []any:
		items := make([]Value, len(v))
		for i, e := range v {
			items[i] = fromAny(e)
		}
		return NewArray(items)
	case []string:
		items := make([]Value, len(v))
		for i, e := range v {
			items[i] = NewString(e)
		}
		return NewArray(items)
	case map[string]any:
		obj := make(map[string]Value, len(v))
		for _, k := range sortedKeys(v) {
			obj[k] = fromAny(v[k])
		}
		return NewObject(obj)
	case map[string]string:
		obj := make(map[string]Value, len(v))
		for k, e := range v {
			obj[k] = NewString(e)
		}
		return NewObject(obj)
	}
	return Value{}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// asInt64 reads a served number as an integer: it needs no fractional part and must lie in the int64
// range, and 2.0 is the integer 2.
func asInt64(f float64) (int64, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) || math.Trunc(f) != f {
		return 0, false
	}
	if f < -9223372036854775808.0 || f >= 9223372036854775808.0 {
		return 0, false
	}
	return int64(f), true
}
