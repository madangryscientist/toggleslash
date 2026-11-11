package toggleslash

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

// outcome is one evaluation before it is shaped for a typed accessor. matched is true when the flag served a
// value of the kind the caller asked for, so detail.Value is the served value and not the fallback.
type outcome struct {
	detail  Detail[jsonvalue.Value]
	matched bool
}

// sameKind is the rule for the generic accessors: the served value must be of the fallback's JSON kind (array and
// object are distinct). A null fallback accepts any kind; a null served value against another fallback is a mismatch.
func sameKind(fallback jsonvalue.Value) func(jsonvalue.Value) bool {
	if fallback.IsNull() {
		return func(jsonvalue.Value) bool { return true }
	}
	kind := fallback.Kind()
	return func(v jsonvalue.Value) bool { return v.Kind() == kind }
}

func isKind(kind jsonvalue.Kind) func(jsonvalue.Value) bool {
	return func(v jsonvalue.Value) bool { return v.Kind() == kind }
}

// asInt64 reads a number as an int64: a fraction, or a value outside the signed 64-bit range, is not one, and 2.0 is 2.
func asInt64(v jsonvalue.Value) (int64, bool) {
	f, ok := v.AsNumber()
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) || f < -(1<<63) || f >= 1<<63 {
		return 0, false
	}
	return int64(f), true
}

func isInt64(v jsonvalue.Value) bool {
	_, ok := asInt64(v)
	return ok
}

func notReady(fallback jsonvalue.Value, code string) Detail[jsonvalue.Value] {
	return Detail[jsonvalue.Value]{
		Value:          fallback,
		VariationIndex: -1,
		Reason:         Reason{Kind: ReasonError, Code: code, RuleIndex: -1},
	}
}

// evaluate does one evaluation: the ruleset is loaded once, the answer is shaped by accept, the usage is counted,
// and the hook runs last. It never panics.
func (c *Client) evaluate(flagKey string, ctx, fallback jsonvalue.Value, accept func(jsonvalue.Value) bool) outcome {
	held := c.snap.Load()
	out := c.answer(held, flagKey, ctx, fallback, accept)
	if c.cfg.afterEvaluation != nil {
		event := EvaluationEvent{
			FlagKey:        flagKey,
			Context:        ctx,
			Value:          out.detail.Value,
			VariationIndex: out.detail.VariationIndex,
			Reason:         out.detail.Reason,
			FlagVersion:    -1,
		}
		if held != nil {
			event.ETag = held.etag
			if version, ok := held.ruleset.FlagVersion(flagKey); ok {
				event.FlagVersion = version
			}
		}
		c.runHook(event)
	}
	return out
}

func (c *Client) answer(held *snapshot, flagKey string, ctx, fallback jsonvalue.Value, accept func(jsonvalue.Value) bool) (out outcome) {
	out.detail = notReady(fallback, ErrorNotReady)
	if held == nil {
		return out
	}
	defer func() {
		if recover() != nil {
			c.report(&Error{Op: "evaluate", Code: "failed"})
			out = outcome{detail: notReady(fallback, ErrorEvaluationFailed)}
		}
	}()
	result := c.cfg.evaluator(held.ruleset, flagKey, ctx)
	if result.VariationIndex < 0 {
		return outcome{detail: Detail[jsonvalue.Value]{Value: fallback, VariationIndex: -1, Reason: result.Reason}}
	}
	if !accept(result.Value) {
		return outcome{detail: notReady(fallback, ErrorTypeMismatch)}
	}
	c.record(flagKey, result.VariationIndex)
	return outcome{
		detail:  Detail[jsonvalue.Value]{Value: result.Value, VariationIndex: result.VariationIndex, Reason: result.Reason},
		matched: true,
	}
}

func (c *Client) runHook(event EvaluationEvent) {
	defer func() {
		if recover() != nil {
			c.report(&Error{Op: "hook", Code: "panic"})
		}
	}()
	c.cfg.afterEvaluation(event)
}

// Variation is the flag's value for a context, or fallback. The served value must be of the fallback's JSON kind
// (a null fallback accepts any kind); otherwise the answer is the fallback. It is local, answers from memory,
// and never panics.
func (c *Client) Variation(flagKey string, ctx, fallback jsonvalue.Value) jsonvalue.Value {
	return c.VariationDetail(flagKey, ctx, fallback).Value
}

// VariationDetail is Variation with the variation index and the reason.
func (c *Client) VariationDetail(flagKey string, ctx, fallback jsonvalue.Value) Detail[jsonvalue.Value] {
	return c.evaluate(flagKey, ctx, fallback, sameKind(fallback)).detail
}

// JSONVariation is Variation for an array or object flag. It compares kinds only (array and object are distinct) and
// never the fallback's contents.
func (c *Client) JSONVariation(flagKey string, ctx, fallback jsonvalue.Value) jsonvalue.Value {
	return c.Variation(flagKey, ctx, fallback)
}

// JSONVariationDetail is JSONVariation with the variation index and the reason.
func (c *Client) JSONVariationDetail(flagKey string, ctx, fallback jsonvalue.Value) Detail[jsonvalue.Value] {
	return c.VariationDetail(flagKey, ctx, fallback)
}

// BoolVariation is Variation for a boolean flag.
func (c *Client) BoolVariation(flagKey string, ctx jsonvalue.Value, fallback bool) bool {
	return c.BoolVariationDetail(flagKey, ctx, fallback).Value
}

// BoolVariationDetail is BoolVariation with the variation index and the reason.
func (c *Client) BoolVariationDetail(flagKey string, ctx jsonvalue.Value, fallback bool) Detail[bool] {
	out := c.evaluate(flagKey, ctx, jsonvalue.FromBool(fallback), isKind(jsonvalue.Bool))
	value := fallback
	if out.matched {
		value, _ = out.detail.Value.AsBool()
	}
	return Detail[bool]{Value: value, VariationIndex: out.detail.VariationIndex, Reason: out.detail.Reason}
}

// StringVariation is Variation for a string flag. A string may hold a lone surrogate, which Go's string keeps in
// its WTF-8 form.
func (c *Client) StringVariation(flagKey string, ctx jsonvalue.Value, fallback string) string {
	return c.StringVariationDetail(flagKey, ctx, fallback).Value
}

// StringVariationDetail is StringVariation with the variation index and the reason.
func (c *Client) StringVariationDetail(flagKey string, ctx jsonvalue.Value, fallback string) Detail[string] {
	out := c.evaluate(flagKey, ctx, jsonvalue.FromString(fallback), isKind(jsonvalue.String))
	value := fallback
	if out.matched {
		value, _ = out.detail.Value.AsString()
	}
	return Detail[string]{Value: value, VariationIndex: out.detail.VariationIndex, Reason: out.detail.Reason}
}

// FloatVariation is Variation for a number flag. A value too large for a float64 (1e999) is +Inf.
func (c *Client) FloatVariation(flagKey string, ctx jsonvalue.Value, fallback float64) float64 {
	return c.FloatVariationDetail(flagKey, ctx, fallback).Value
}

// FloatVariationDetail is FloatVariation with the variation index and the reason.
func (c *Client) FloatVariationDetail(flagKey string, ctx jsonvalue.Value, fallback float64) Detail[float64] {
	out := c.evaluate(flagKey, ctx, jsonvalue.FromFloat(fallback), isKind(jsonvalue.Number))
	value := fallback
	if out.matched {
		value, _ = out.detail.Value.AsNumber()
	}
	return Detail[float64]{Value: value, VariationIndex: out.detail.VariationIndex, Reason: out.detail.Reason}
}

// IntVariation is Variation for an integer flag. A served number with a fraction, or outside the signed 64-bit range,
// is a type mismatch and the answer is the fallback; 2.0 counts as 2. The largest servable value is the largest
// float64 below 2^63 (2^63 - 1024).
func (c *Client) IntVariation(flagKey string, ctx jsonvalue.Value, fallback int64) int64 {
	return c.IntVariationDetail(flagKey, ctx, fallback).Value
}

// IntVariationDetail is IntVariation with the variation index and the reason.
func (c *Client) IntVariationDetail(flagKey string, ctx jsonvalue.Value, fallback int64) Detail[int64] {
	out := c.evaluate(flagKey, ctx, jsonvalue.FromInt(fallback), isInt64)
	value := fallback
	if out.matched {
		value, _ = asInt64(out.detail.Value)
	}
	return Detail[int64]{Value: value, VariationIndex: out.detail.VariationIndex, Reason: out.detail.Reason}
}
