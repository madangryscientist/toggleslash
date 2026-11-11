package toggleslash

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

type accessor uint8

const (
	accBool accessor = iota
	accString
	accInt
	accFloat
	accJSON
)

// outcome is one evaluation, built by the rules of contract section 4.
type outcome struct {
	value  jsonvalue.Value // the served value when served, else the fallback
	served bool
	index  *int
	reason Reason
}

func fallbackOutcome(fallback jsonvalue.Value, kind ReasonKind, code ErrorCode) outcome {
	return outcome{value: fallback, reason: Reason{Kind: kind, ErrorCode: code}}
}

// acceptable is the mismatch rule: JSON kinds only, a null fallback accepts anything, and the integer
// accessor wants a whole number inside the int64 range (2.0 is the integer 2).
func acceptable(served, fallback jsonvalue.Value, acc accessor) bool {
	switch acc {
	case accBool:
		return served.Kind() == jsonvalue.Bool
	case accString:
		return served.Kind() == jsonvalue.String
	case accFloat:
		return served.Kind() == jsonvalue.Number
	case accInt:
		f, ok := served.Number()
		return ok && f == math.Trunc(f) && f >= -(1<<63) && f < 1<<63
	default:
		return fallback.IsNull() || served.Kind() == fallback.Kind()
	}
}

func reasonOf(r evaluator.Reason) Reason {
	return Reason{Kind: ReasonKind(r.Kind), RuleIndex: r.RuleIndex, FlagKey: r.FlagKey, ErrorCode: ErrorCode(r.Code)}
}

// run evaluates against one snapshot and builds the result. Anything it raises is EVALUATION_FAILED.
func (c *Client) run(snap *snapshot, flagKey string, ctx Context, fallback jsonvalue.Value, acc accessor) (out outcome) {
	var raised any
	failed := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				raised, failed = r, true
			}
		}()
		res := c.cfg.evaluate(snap.ruleset, flagKey, ctx)
		switch {
		case !res.HasVariation:
			out = outcome{value: fallback, reason: reasonOf(res.Reason)}
		case !acceptable(res.Value, fallback, acc):
			out = fallbackOutcome(fallback, ReasonKindError, ErrorCodeTypeMismatch)
		default:
			index := res.VariationIndex
			out = outcome{value: res.Value, served: true, index: &index, reason: reasonOf(res.Reason)}
		}
	}()
	if failed {
		c.report(&Error{Op: "evaluate", Code: string(ErrorCodeEvaluationFailed), Err: &PanicError{Value: raised}})
		return fallbackOutcome(fallback, ReasonKindError, ErrorCodeEvaluationFailed)
	}
	if out.served {
		c.count(flagKey, *out.index)
	}
	return out
}

func (c *Client) evaluate(flagKey string, ctx Context, fallback jsonvalue.Value, acc accessor) outcome {
	snap := c.snap.Load()
	var out outcome
	if snap == nil {
		out = fallbackOutcome(fallback, ReasonKindError, ErrorCodeNotReady)
	} else {
		out = c.run(snap, flagKey, ctx, fallback, acc)
	}
	if c.cfg.afterEvaluation != nil {
		c.callHook(snap, flagKey, ctx, out)
	}
	return out
}

// callHook calls the afterEvaluation hook on the caller's goroutine. A panic in it, or in building its
// event, goes to onError.
func (c *Client) callHook(snap *snapshot, flagKey string, ctx Context, out outcome) {
	defer func() {
		if r := recover(); r != nil {
			c.report(&Error{Op: "hook", Err: &PanicError{Value: r}})
		}
	}()
	event := EvaluationEvent{
		FlagKey:        flagKey,
		Context:        ctx,
		Value:          out.value,
		VariationIndex: out.index,
		Reason:         out.reason,
	}
	if snap != nil {
		event.ETag = snap.etag
		if v, ok := snap.ruleset.FlagVersion(flagKey); ok {
			version := int64(v)
			event.FlagVersion = &version
		}
	}
	c.cfg.afterEvaluation(event)
}

func detail[T any](out outcome, fallback T, convert func(jsonvalue.Value) T) Detail[T] {
	d := Detail[T]{Value: fallback, VariationIndex: out.index, Reason: out.reason}
	if out.served {
		d.Value = convert(out.value)
	}
	return d
}

// BoolVariationDetail evaluates a boolean flag for a context, with the variation index and the reason. It
// never blocks on the network and never panics: on any problem it returns fallback with an ERROR reason.
func (c *Client) BoolVariationDetail(flagKey string, ctx Context, fallback bool) Detail[bool] {
	out := c.evaluate(flagKey, ctx, jsonvalue.NewBool(fallback), accBool)
	return detail(out, fallback, func(v jsonvalue.Value) bool { b, _ := v.Bool(); return b })
}

// BoolVariation is BoolVariationDetail without the detail.
func (c *Client) BoolVariation(flagKey string, ctx Context, fallback bool) bool {
	return c.BoolVariationDetail(flagKey, ctx, fallback).Value
}

// StringVariationDetail evaluates a string flag for a context.
func (c *Client) StringVariationDetail(flagKey string, ctx Context, fallback string) Detail[string] {
	out := c.evaluate(flagKey, ctx, jsonvalue.NewString(fallback), accString)
	return detail(out, fallback, func(v jsonvalue.Value) string { s, _ := v.String(); return s })
}

// StringVariation is StringVariationDetail without the detail. The string may hold a lone surrogate as
// WTF-8 bytes, which is not valid UTF-8, if the flag serves one.
func (c *Client) StringVariation(flagKey string, ctx Context, fallback string) string {
	return c.StringVariationDetail(flagKey, ctx, fallback).Value
}

// IntVariationDetail evaluates a flag that serves a whole number. A served number with a fractional part,
// or outside the int64 range, is the fallback with ERROR TYPE_MISMATCH; 2.0 is the integer 2.
func (c *Client) IntVariationDetail(flagKey string, ctx Context, fallback int64) Detail[int64] {
	out := c.evaluate(flagKey, ctx, jsonvalue.NewNumber(float64(fallback)), accInt)
	return detail(out, fallback, func(v jsonvalue.Value) int64 { f, _ := v.Number(); return int64(f) })
}

// IntVariation is IntVariationDetail without the detail.
func (c *Client) IntVariation(flagKey string, ctx Context, fallback int64) int64 {
	return c.IntVariationDetail(flagKey, ctx, fallback).Value
}

// FloatVariationDetail evaluates a flag that serves a number. A served number that is not finite (1e999)
// comes back as +Inf or -Inf.
func (c *Client) FloatVariationDetail(flagKey string, ctx Context, fallback float64) Detail[float64] {
	out := c.evaluate(flagKey, ctx, jsonvalue.NewNumber(fallback), accFloat)
	return detail(out, fallback, func(v jsonvalue.Value) float64 { f, _ := v.Number(); return f })
}

// FloatVariation is FloatVariationDetail without the detail.
func (c *Client) FloatVariation(flagKey string, ctx Context, fallback float64) float64 {
	return c.FloatVariationDetail(flagKey, ctx, fallback).Value
}

// JSONVariationDetail evaluates a flag of any kind. The served value must be of the fallback's JSON kind
// (array and object are distinct kinds); a null fallback accepts any served value, and a null served value
// against a non-null fallback is a TYPE_MISMATCH.
func (c *Client) JSONVariationDetail(flagKey string, ctx Context, fallback jsonvalue.Value) Detail[jsonvalue.Value] {
	out := c.evaluate(flagKey, ctx, fallback, accJSON)
	return detail(out, fallback, func(v jsonvalue.Value) jsonvalue.Value { return v })
}

// JSONVariation is JSONVariationDetail without the detail.
func (c *Client) JSONVariation(flagKey string, ctx Context, fallback jsonvalue.Value) jsonvalue.Value {
	return c.JSONVariationDetail(flagKey, ctx, fallback).Value
}
