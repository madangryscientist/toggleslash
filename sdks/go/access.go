package toggleslash

import (
	"fmt"
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
)

// The typed accessors. Each returns the served value when it is of the kind the fallback has, and the
// fallback otherwise; the Detail twin says why. None of them panics, does I/O or waits.

// BoolVariation evaluates a flag for a context and returns a bool.
func (c *Client) BoolVariation(flagKey string, ctx Context, fallback bool) bool {
	return c.BoolVariationDetail(flagKey, ctx, fallback).Value
}

// BoolVariationDetail is BoolVariation with the variation index and reason.
func (c *Client) BoolVariationDetail(flagKey string, ctx Context, fallback bool) Detail[bool] {
	return variation(c, flagKey, ctx, fallback, Bool(fallback), func(v Value) (bool, bool) { return v.AsBool() })
}

// StringVariation evaluates a flag for a context and returns a string.
func (c *Client) StringVariation(flagKey string, ctx Context, fallback string) string {
	return c.StringVariationDetail(flagKey, ctx, fallback).Value
}

// StringVariationDetail is StringVariation with the variation index and reason.
func (c *Client) StringVariationDetail(flagKey string, ctx Context, fallback string) Detail[string] {
	return variation(c, flagKey, ctx, fallback, String(fallback), func(v Value) (string, bool) { return v.AsString() })
}

// NumberVariation evaluates a flag for a context and returns a float64.
func (c *Client) NumberVariation(flagKey string, ctx Context, fallback float64) float64 {
	return c.NumberVariationDetail(flagKey, ctx, fallback).Value
}

// NumberVariationDetail is NumberVariation with the variation index and reason.
func (c *Client) NumberVariationDetail(flagKey string, ctx Context, fallback float64) Detail[float64] {
	return variation(c, flagKey, ctx, fallback, Number(fallback), func(v Value) (float64, bool) { return v.AsNumber() })
}

// IntVariation evaluates a flag for a context and returns an int64. A served number with a fractional
// part, or outside the int64 range, is a TYPE_MISMATCH and gives the fallback; 2.0 is 2.
func (c *Client) IntVariation(flagKey string, ctx Context, fallback int64) int64 {
	return c.IntVariationDetail(flagKey, ctx, fallback).Value
}

// IntVariationDetail is IntVariation with the variation index and reason.
func (c *Client) IntVariationDetail(flagKey string, ctx Context, fallback int64) Detail[int64] {
	return variation(c, flagKey, ctx, fallback, Number(float64(fallback)), asInt64)
}

// JSONVariation evaluates a flag for a context and returns a Value of any kind. The kind of the fallback
// is the kind expected: a null fallback accepts every kind, any other accepts only its own.
func (c *Client) JSONVariation(flagKey string, ctx Context, fallback Value) Value {
	return c.JSONVariationDetail(flagKey, ctx, fallback).Value
}

// JSONVariationDetail is JSONVariation with the variation index and reason.
func (c *Client) JSONVariationDetail(flagKey string, ctx Context, fallback Value) Detail[Value] {
	accept := func(v Value) (Value, bool) {
		return v, fallback.IsNull() || v.Kind() == fallback.Kind()
	}
	return variation(c, flagKey, ctx, fallback, fallback, accept)
}

// asInt64 accepts a finite number with no fractional part inside the int64 range.
func asInt64(v Value) (int64, bool) {
	f, ok := v.AsNumber()
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) {
		return 0, false
	}
	if f < -9223372036854775808.0 || f >= 9223372036854775808.0 {
		return 0, false
	}
	return int64(f), true
}

func errorReason(code ErrorCode) Reason {
	return Reason{Kind: ReasonError, RuleIndex: -1, ErrorCode: code}
}

// variation is the one path every accessor takes: contract 4's order, the usage count, then the hook.
func variation[T any](c *Client, flagKey string, ctx Context, fallback T, fallbackValue Value, accept func(Value) (T, bool)) (out Detail[T]) {
	out = Detail[T]{Value: fallback, VariationIndex: NoVariation, Reason: errorReason(ErrEvaluationFailed)}
	eventValue := fallbackValue
	v := c.view.Load()
	defer func() {
		// Nothing below may reach the caller. A panic that got this far is an evaluation failure.
		if r := recover(); r != nil {
			out = Detail[T]{Value: fallback, VariationIndex: NoVariation, Reason: errorReason(ErrEvaluationFailed)}
			c.report(panicError("evaluation", r))
		}
	}()

	if v.ruleset == nil {
		out.Reason = errorReason(ErrNotReady)
	} else {
		res, failed := c.evaluateSafely(v, flagKey, ctx)
		switch {
		case failed:
			out.Reason = errorReason(ErrEvaluationFailed)
		case res.VariationIndex < 0:
			out.Reason = res.Reason
		default:
			if served, ok := accept(res.Value); ok {
				out = Detail[T]{Value: served, VariationIndex: res.VariationIndex, Reason: res.Reason}
				eventValue = res.Value
				c.count(flagKey, res.VariationIndex)
			} else {
				out.Reason = errorReason(ErrTypeMismatch)
			}
		}
	}
	c.runHook(v, flagKey, ctx, eventValue, out.VariationIndex, out.Reason)
	return out
}

// evaluateSafely runs the evaluator and turns a panic into a failure with one onError call.
func (c *Client) evaluateSafely(v *view, flagKey string, ctx Context) (res evaluator.Result, failed bool) {
	defer func() {
		if r := recover(); r != nil {
			failed = true
			c.report(panicError("evaluation", r))
		}
	}()
	return c.cfg.evaluate(v.ruleset, flagKey, ctx), false
}

// runHook calls the afterEvaluation hook, if there is one. A panic in it goes to onError.
func (c *Client) runHook(v *view, flagKey string, ctx Context, value Value, index int, reason Reason) {
	if c.cfg.afterEvaluation == nil {
		return
	}
	event := EvaluationEvent{
		FlagKey:        flagKey,
		Context:        ctx,
		Value:          value,
		VariationIndex: index,
		Reason:         reason,
		ETag:           v.etag,
	}
	if v.ruleset != nil {
		if version, ok := v.ruleset.FlagVersion(flagKey); ok {
			event.FlagVersion = &version
		}
	}
	defer func() {
		if r := recover(); r != nil {
			c.report(panicError("hook", r))
		}
	}()
	c.cfg.afterEvaluation(event)
}

// panicError makes the error for onError from a recovered panic. A hook is the application's own code, so
// its panic value is passed as raised (an error as it is, anything else wrapped with its text). A panic
// inside the SDK's own work is reported in fixed words: its value could carry a context or a key.
func panicError(what string, r any) error {
	if what != "hook" {
		return fmt.Errorf("toggleslash: %s failed unexpectedly", what)
	}
	if err, ok := r.(error); ok {
		return err
	}
	return fmt.Errorf("toggleslash: %s panicked: %v", what, r)
}
