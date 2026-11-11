package toggleslash

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

// outcome is one evaluation's answer before it is typed.
type outcome struct {
	value  jsonvalue.Value
	index  int // -1 when the value is the fallback
	reason Reason
	snap   *snapshot
}

func convertReason(r evaluator.Reason) Reason {
	out := Reason{Kind: ReasonKind(r.Kind), Code: ErrorCode(r.Code), FlagKey: r.FlagKey}
	if r.Kind == evaluator.ReasonRuleMatch {
		out.RuleIndex = r.RuleIndex
	}
	return out
}

func errReason(code ErrorCode) Reason { return Reason{Kind: ReasonError, Code: code} }

// kindMatches is the mismatch rule of contract 4: JSON kinds only, a null fallback accepts anything and a
// null served value against another kind is a mismatch.
func kindMatches(fallback, served jsonvalue.Value) bool {
	if fallback.IsNull() {
		return true
	}
	return fallback.Kind() == served.Kind()
}

// fitsInt64 reports whether a served number is a whole number a signed 64-bit integer holds. 2.0 is 2.
func fitsInt64(v jsonvalue.Value) bool {
	n, ok := v.AsNumber()
	return ok && n == math.Trunc(n) && n >= -(1<<63) && n < 1<<63
}

// evaluate runs the result rules of contract 4 and 9, then the hook. integer selects the integer
// accessor's extra rule.
func (c *Client) evaluate(flagKey string, ctx Context, fallback jsonvalue.Value, integer bool) (out outcome) {
	out = outcome{value: fallback, index: -1, reason: errReason(ErrEvaluationFailed), snap: c.snap.Load()}
	defer func() {
		if v := recover(); v != nil {
			out.value, out.index, out.reason = fallback, -1, errReason(ErrEvaluationFailed)
			c.notify(&PanicError{Where: "evaluation", Value: v}, nil)
		}
		c.callHook(flagKey, ctx, out)
	}()
	snap := out.snap
	if snap.rs == nil {
		out.reason = errReason(ErrNotReady)
		return out
	}
	if c.cfg.beforeEvaluate != nil {
		c.cfg.beforeEvaluate(flagKey)
	}
	r := snap.rs.Evaluate(flagKey, ctx.parsed)
	if r.Index < 0 {
		out.reason = convertReason(r.Reason)
		return out
	}
	if !kindMatches(fallback, r.Value) || (integer && !fitsInt64(r.Value)) {
		out.reason = errReason(ErrTypeMismatch)
		return out
	}
	out.value, out.index, out.reason = r.Value, r.Index, convertReason(r.Reason)
	c.countUsage(flagKey, r.Index)
	return out
}

// callHook calls afterEvaluation on the caller's goroutine. A panic in it goes to onError.
func (c *Client) callHook(flagKey string, ctx Context, out outcome) {
	f := c.cfg.afterEval
	if f == nil {
		return
	}
	ev := EvaluationEvent{FlagKey: flagKey, Context: ctx, Value: out.value, Reason: out.reason, ETag: out.snap.etag}
	if out.index >= 0 {
		i := out.index
		ev.VariationIndex = &i
	}
	if out.snap.rs != nil {
		if v, ok := out.snap.rs.FlagVersion(flagKey); ok && v < 1<<63 {
			n := int64(v)
			ev.FlagVersion = &n
		}
	}
	defer func() {
		if v := recover(); v != nil {
			c.notify(&PanicError{Where: "afterEvaluation hook", Value: v}, nil)
		}
	}()
	f(ev)
}

func (o outcome) indexPtr() *int {
	if o.index < 0 {
		return nil
	}
	i := o.index
	return &i
}

func detail[T any](o outcome, value T) Detail[T] {
	return Detail[T]{Value: value, VariationIndex: o.indexPtr(), Reason: o.reason}
}

// Variation evaluates a flag for a context. The kind of fallback is the kind the caller expects: a flag that
// serves another kind answers the fallback (use VariationDetail to see TYPE_MISMATCH); a JSON null fallback
// accepts any kind. It never panics and does no I/O.
func (c *Client) Variation(flagKey string, ctx Context, fallback jsonvalue.Value) jsonvalue.Value {
	return c.VariationDetail(flagKey, ctx, fallback).Value
}

// VariationDetail is Variation with the variation index and the reason.
func (c *Client) VariationDetail(flagKey string, ctx Context, fallback jsonvalue.Value) Detail[jsonvalue.Value] {
	o := c.evaluate(flagKey, ctx, fallback, false)
	return detail(o, o.value)
}

// BoolVariation evaluates a flag that serves true or false.
func (c *Client) BoolVariation(flagKey string, ctx Context, fallback bool) bool {
	return c.BoolVariationDetail(flagKey, ctx, fallback).Value
}

// BoolVariationDetail is BoolVariation with the variation index and the reason.
func (c *Client) BoolVariationDetail(flagKey string, ctx Context, fallback bool) Detail[bool] {
	o := c.evaluate(flagKey, ctx, jsonvalue.FromBool(fallback), false)
	if b, ok := o.value.AsBool(); ok && o.index >= 0 {
		return detail(o, b)
	}
	return detail(o, fallback)
}

// StringVariation evaluates a flag that serves a string.
func (c *Client) StringVariation(flagKey string, ctx Context, fallback string) string {
	return c.StringVariationDetail(flagKey, ctx, fallback).Value
}

// StringVariationDetail is StringVariation with the variation index and the reason.
func (c *Client) StringVariationDetail(flagKey string, ctx Context, fallback string) Detail[string] {
	o := c.evaluate(flagKey, ctx, jsonvalue.FromString(fallback), false)
	if s, ok := o.value.AsString(); ok && o.index >= 0 {
		return detail(o, s)
	}
	return detail(o, fallback)
}

// Float64Variation evaluates a flag that serves a number.
func (c *Client) Float64Variation(flagKey string, ctx Context, fallback float64) float64 {
	return c.Float64VariationDetail(flagKey, ctx, fallback).Value
}

// Float64VariationDetail is Float64Variation with the variation index and the reason.
func (c *Client) Float64VariationDetail(flagKey string, ctx Context, fallback float64) Detail[float64] {
	o := c.evaluate(flagKey, ctx, jsonvalue.FromNumber(fallback), false)
	if n, ok := o.value.AsNumber(); ok && o.index >= 0 {
		return detail(o, n)
	}
	return detail(o, fallback)
}

// Int64Variation is the integer accessor: a flag that serves a number with a fractional part, or one outside
// the signed 64-bit range, answers the fallback with TYPE_MISMATCH. 2.0 is 2.
func (c *Client) Int64Variation(flagKey string, ctx Context, fallback int64) int64 {
	return c.Int64VariationDetail(flagKey, ctx, fallback).Value
}

// Int64VariationDetail is Int64Variation with the variation index and the reason.
func (c *Client) Int64VariationDetail(flagKey string, ctx Context, fallback int64) Detail[int64] {
	o := c.evaluate(flagKey, ctx, jsonvalue.FromNumber(float64(fallback)), true)
	if n, ok := o.value.AsNumber(); ok && o.index >= 0 {
		return detail(o, int64(n))
	}
	return detail(o, fallback)
}
