package toggleslash

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

func errorReason(code ErrorCode) Reason {
	return Reason{Kind: ReasonError, Code: code}
}

func fallbackDetail[T any](fallback T, code ErrorCode) Detail[T] {
	return Detail[T]{Value: fallback, Reason: errorReason(code)}
}

// variation is the one evaluation path every accessor goes through. convert says whether a served value is of
// the kind the caller asked for, and what it is as a Go value. It never panics: a panic in the evaluation, in
// the context conversion or in the hook is recovered, and the caller gets the fallback or the answer.
func variation[T any](c *Client, flagKey string, context any, fallback T, convert func(jsonv.Value) (T, bool)) Detail[T] {
	snap := c.snap.Load()
	detail := evaluateOnce(c, snap, flagKey, context, fallback, convert)
	if c.cfg.afterEvaluation != nil {
		c.runHook(snap, flagKey, context, detail.Value, detail.VariationIndex, detail.Reason)
	}
	return detail
}

func evaluateOnce[T any](c *Client, snap *snapshot, flagKey string, context any, fallback T, convert func(jsonv.Value) (T, bool)) (detail Detail[T]) {
	defer func() {
		if r := recover(); r != nil {
			// The panic value is not kept: it can come from the context.
			c.reportError(nil, &PanicError{where: "evaluation"})
			detail = fallbackDetail(fallback, ErrEvaluationFailed)
		}
	}()
	if snap == nil {
		return fallbackDetail(fallback, ErrNotReady)
	}
	value, ok := toContext(context)
	if !ok {
		value = jsonv.Null()
	}
	result := c.cfg.evaluate(snap.rules, flagKey, value)
	if result.Index < 0 {
		return Detail[T]{Value: fallback, Reason: result.Reason}
	}
	served, ok := convert(result.Value)
	if !ok {
		return fallbackDetail(fallback, ErrTypeMismatch)
	}
	c.recordUsage(flagKey, result.Index)
	index := result.Index
	return Detail[T]{Value: served, VariationIndex: &index, Reason: result.Reason}
}

// runHook calls the afterEvaluation hook with the ETag and version of the ruleset the evaluation used. A panic
// in it goes to onError and changes nothing.
func (c *Client) runHook(snap *snapshot, flagKey string, context any, value any, index *int, reason Reason) {
	event := Event{FlagKey: flagKey, Context: context, Value: value, VariationIndex: index, Reason: reason}
	if snap != nil {
		event.ETag = snap.etag
		if version, ok := snap.rules.FlagVersion(flagKey); ok {
			event.FlagVersion = &version
		}
	}
	defer func() {
		if r := recover(); r != nil {
			c.reportError(nil, &PanicError{Value: r, where: "afterEvaluation hook"})
		}
	}()
	c.cfg.afterEvaluation(event)
}

func asBool(v jsonv.Value) (bool, bool)     { return v.AsBool() }
func asFloat(v jsonv.Value) (float64, bool) { return v.AsNumber() }
func asString(v jsonv.Value) (string, bool) { return v.AsString() }

// asInt accepts a number with no fractional part that fits a signed 64-bit integer (2.0 counts as 2).
func asInt(v jsonv.Value) (int64, bool) {
	f, ok := v.AsNumber()
	// The upper bound is exclusive: 2^63 is the first float64 above the int64 range.
	if !ok || math.IsNaN(f) || f != math.Trunc(f) || f < -(1<<63) || f >= 1<<63 {
		return 0, false
	}
	return int64(f), true
}

// jsonConverter accepts a served value of the same JSON kind as the fallback; a null fallback accepts any.
func jsonConverter(fallback Value) func(jsonv.Value) (Value, bool) {
	return func(v jsonv.Value) (Value, bool) {
		if fallback.IsNull() || v.Kind() == fallback.Kind() {
			return v, true
		}
		return Value{}, false
	}
}

// BoolVariation is the flag's boolean value for a context, or fallback.
func (c *Client) BoolVariation(flagKey string, context any, fallback bool) bool {
	return c.BoolVariationDetail(flagKey, context, fallback).Value
}

// BoolVariationDetail is BoolVariation with the variation index and the reason.
func (c *Client) BoolVariationDetail(flagKey string, context any, fallback bool) Detail[bool] {
	return variation(c, flagKey, context, fallback, asBool)
}

// StringVariation is the flag's string value for a context, or fallback. A string with a lone surrogate comes
// back in its generalised (WTF-8) form, which is not valid UTF-8.
func (c *Client) StringVariation(flagKey string, context any, fallback string) string {
	return c.StringVariationDetail(flagKey, context, fallback).Value
}

// StringVariationDetail is StringVariation with the variation index and the reason.
func (c *Client) StringVariationDetail(flagKey string, context any, fallback string) Detail[string] {
	return variation(c, flagKey, context, fallback, asString)
}

// IntVariation is the flag's integer value for a context, or fallback. A served number with a fractional part,
// or outside the signed 64-bit range, is TYPE_MISMATCH.
func (c *Client) IntVariation(flagKey string, context any, fallback int64) int64 {
	return c.IntVariationDetail(flagKey, context, fallback).Value
}

// IntVariationDetail is IntVariation with the variation index and the reason.
func (c *Client) IntVariationDetail(flagKey string, context any, fallback int64) Detail[int64] {
	return variation(c, flagKey, context, fallback, asInt)
}

// FloatVariation is the flag's number value for a context, or fallback.
func (c *Client) FloatVariation(flagKey string, context any, fallback float64) float64 {
	return c.FloatVariationDetail(flagKey, context, fallback).Value
}

// FloatVariationDetail is FloatVariation with the variation index and the reason.
func (c *Client) FloatVariationDetail(flagKey string, context any, fallback float64) Detail[float64] {
	return variation(c, flagKey, context, fallback, asFloat)
}

// JSONVariation is the flag's value of any JSON kind for a context, or fallback. The served value must be of
// the fallback's kind (array and object are different kinds); a null fallback accepts any kind.
func (c *Client) JSONVariation(flagKey string, context any, fallback Value) Value {
	return c.JSONVariationDetail(flagKey, context, fallback).Value
}

// JSONVariationDetail is JSONVariation with the variation index and the reason.
func (c *Client) JSONVariationDetail(flagKey string, context any, fallback Value) Detail[Value] {
	return variation(c, flagKey, context, fallback, jsonConverter(fallback))
}
