package toggleslash

import (
	"errors"
	"fmt"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// BoolVariation returns the boolean the flag serves for the context, or fallback when it cannot be
// answered (no ruleset yet, an unknown flag, or a flag that serves another kind of value).
func (c *Client) BoolVariation(flagKey string, ctx Context, fallback bool) bool {
	return c.BoolVariationDetail(flagKey, ctx, fallback).Value
}

// BoolVariationDetail is BoolVariation with the variation index and the reason.
func (c *Client) BoolVariationDetail(flagKey string, ctx Context, fallback bool) Detail[bool] {
	return evaluate(c, flagKey, ctx, fallback, NewBool(fallback), func(v Value) (bool, bool) { return v.Bool() })
}

// StringVariation returns the string the flag serves for the context, or fallback.
func (c *Client) StringVariation(flagKey string, ctx Context, fallback string) string {
	return c.StringVariationDetail(flagKey, ctx, fallback).Value
}

// StringVariationDetail is StringVariation with the variation index and the reason.
func (c *Client) StringVariationDetail(flagKey string, ctx Context, fallback string) Detail[string] {
	return evaluate(c, flagKey, ctx, fallback, NewString(fallback), func(v Value) (string, bool) { return v.Str() })
}

// IntVariation returns the integer the flag serves for the context, or fallback. A served number with
// a fractional part, or outside the int64 range, is a type mismatch and gives the fallback; 2.0 is the
// integer 2.
func (c *Client) IntVariation(flagKey string, ctx Context, fallback int64) int64 {
	return c.IntVariationDetail(flagKey, ctx, fallback).Value
}

// IntVariationDetail is IntVariation with the variation index and the reason.
func (c *Client) IntVariationDetail(flagKey string, ctx Context, fallback int64) Detail[int64] {
	return evaluate(c, flagKey, ctx, fallback, NewNumber(float64(fallback)), func(v Value) (int64, bool) {
		f, ok := v.Number()
		if !ok {
			return 0, false
		}
		return asInt64(f)
	})
}

// FloatVariation returns the number the flag serves for the context, or fallback.
func (c *Client) FloatVariation(flagKey string, ctx Context, fallback float64) float64 {
	return c.FloatVariationDetail(flagKey, ctx, fallback).Value
}

// FloatVariationDetail is FloatVariation with the variation index and the reason.
func (c *Client) FloatVariationDetail(flagKey string, ctx Context, fallback float64) Detail[float64] {
	return evaluate(c, flagKey, ctx, fallback, NewNumber(fallback), func(v Value) (float64, bool) { return v.Number() })
}

// JSONVariation returns the JSON value the flag serves for the context, or fallback. A null fallback
// accepts a value of any kind; otherwise the served value must be of the fallback's kind (an array and
// an object are different kinds).
func (c *Client) JSONVariation(flagKey string, ctx Context, fallback Value) Value {
	return c.JSONVariationDetail(flagKey, ctx, fallback).Value
}

// JSONVariationDetail is JSONVariation with the variation index and the reason.
func (c *Client) JSONVariationDetail(flagKey string, ctx Context, fallback Value) Detail[Value] {
	return evaluate(c, flagKey, ctx, fallback, fallback, func(v Value) (Value, bool) {
		if fallback.IsNull() || v.Kind() == fallback.Kind() {
			return v, true
		}
		return Value{}, false
	})
}

func reasonOf(r evaluator.Reason) Reason {
	return Reason{Kind: ReasonKind(r.Kind), Code: ErrorCode(r.Code), RuleIndex: r.RuleIndex, FlagKey: r.FlagKey}
}

func errorReason(code ErrorCode) Reason { return Reason{Kind: ReasonError, Code: code} }

// evaluate is contract section 4's result building, for every accessor. as converts the served value to
// the accessor's type and reports whether it is of the kind the caller asked for.
func evaluate[T any](c *Client, flagKey string, ctx Context, fallback T, fallbackValue Value, as func(Value) (T, bool)) Detail[T] {
	snap := c.snap.Load()
	out := Detail[T]{Value: fallback, Reason: errorReason(ErrNotReady)}
	served := fallbackValue
	counted := false
	switch snap {
	case nil:
	default:
		res, err := c.safeEvaluate(snap.rs, flagKey, ctx)
		switch {
		case err != nil:
			out.Reason = errorReason(ErrEvaluationFailed)
			c.reportError(err)
		case res.Index < 0:
			out.Reason = reasonOf(res.Reason)
		default:
			if v, ok := as(res.Value); ok {
				idx := res.Index
				out = Detail[T]{Value: v, VariationIndex: &idx, Reason: reasonOf(res.Reason)}
				served = res.Value
				counted = true
			} else {
				out.Reason = errorReason(ErrTypeMismatch)
			}
		}
	}
	if counted && !c.closing.Load() {
		c.record(flagKey, *out.VariationIndex)
	}
	c.runHook(snap, flagKey, ctx, served, out.VariationIndex, out.Reason)
	return out
}

// safeEvaluate runs the evaluator, turning a panic into an error so no failure reaches the caller.
func (c *Client) safeEvaluate(rs *evaluator.Ruleset, flagKey string, ctx jsonv.Value) (res evaluator.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New(fmt.Sprint("evaluation failed: ", describePanic(r)))
		}
	}()
	if c.opt.evaluate != nil {
		return c.opt.evaluate(rs, flagKey, ctx), nil
	}
	return rs.Evaluate(flagKey, ctx), nil
}

// describePanic names what was raised without printing a value that could hold a context.
func describePanic(r any) string {
	switch r.(type) {
	case error:
		return "an error was raised"
	case string:
		return "a message was raised"
	}
	return "something was raised"
}

// runHook calls the afterEvaluation hook on the calling goroutine. What it raises goes to onError and
// changes nothing the caller sees.
func (c *Client) runHook(snap *snapshot, flagKey string, ctx Context, served Value, index *int, reason Reason) {
	hook := c.opt.afterEval
	if hook == nil {
		return
	}
	ev := EvaluationEvent{FlagKey: flagKey, Context: ctx, Value: served, VariationIndex: index, Reason: reason}
	if snap != nil {
		ev.ETag, ev.HasETag = snap.etag, snap.hasETag
		if v, ok := snap.rs.FlagVersion(flagKey); ok {
			n := int64(v)
			ev.FlagVersion = &n
		}
	}
	defer func() {
		if r := recover(); r != nil {
			c.reportError(errors.New("the afterEvaluation hook raised: " + describePanic(r)))
		}
	}()
	hook(ev)
}
