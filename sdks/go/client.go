package toggleslash

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
)

// outcome is one evaluation as the typed accessors see it.
type outcome struct {
	value  Value
	idx    int
	reason Reason
}

func reasonOf(r evaluator.Reason) Reason {
	return Reason{Kind: ReasonKind(r.Kind), RuleIndex: r.RuleIndex, FlagKey: r.FlagKey, Code: ErrorCode(r.Code)}
}

func errorReason(code ErrorCode) Reason { return Reason{Kind: ReasonError, Code: code} }

// evaluate is the one evaluation path of contract section 4. accept says whether a served value has the kind the
// caller asked for. The ruleset is loaded once, so the value, the ETag and the flag version all come from one
// snapshot.
func (c *Client) evaluate(flagKey string, ctx Context, fallback Value, accept func(Value) bool) outcome {
	snap := c.snap.Load()
	out := outcome{value: fallback, idx: NoVariation}
	if snap == nil {
		out.reason = errorReason(ErrNotReady)
	} else {
		res, failed := c.safeEvaluate(snap, flagKey, ctx)
		switch {
		case failed:
			out.reason = errorReason(ErrEvaluationFailed)
		case res.VariationIndex == evaluator.NoVariation:
			out.reason = reasonOf(res.Reason)
		case !accept(res.Value):
			out.reason = errorReason(ErrTypeMismatch)
		default:
			out = outcome{value: res.Value, idx: res.VariationIndex, reason: reasonOf(res.Reason)}
			c.use.record(flagKey, out.idx, utcDay(c.clk.Now()))
		}
	}
	c.runHook(snap, flagKey, ctx, out)
	return out
}

// safeEvaluate calls the evaluator and turns a panic into a failure plus one report.
func (c *Client) safeEvaluate(snap *snapshot, flagKey string, ctx Context) (res evaluator.Result, failed bool) {
	defer func() {
		if r := recover(); r != nil {
			failed = true
			c.report(&PanicError{Where: "evaluation", Value: r})
		}
	}()
	return c.cfg.evaluate(snap.rules, flagKey, ctx), false
}

// runHook calls the AfterEvaluation function, if there is one, on the caller's goroutine. A panic in it goes to
// the OnError function and never reaches the caller.
func (c *Client) runHook(snap *snapshot, flagKey string, ctx Context, out outcome) {
	fn := c.cfg.afterEval
	if fn == nil {
		return
	}
	ev := Event{
		FlagKey:        flagKey,
		Context:        ctx,
		Value:          out.value,
		VariationIndex: out.idx,
		Reason:         out.reason,
		FlagVersion:    NoVersion,
	}
	if snap != nil {
		ev.ETag = snap.etag
		if v, ok := snap.rules.Version(flagKey); ok && v >= 0 && v < math.Exp2(63) {
			ev.FlagVersion = int64(v)
		}
	}
	defer func() {
		if r := recover(); r != nil {
			c.report(&PanicError{Where: "afterEvaluation hook", Value: r})
		}
	}()
	fn(ev)
}

func kindIs(want Kind) func(Value) bool { return func(v Value) bool { return v.Kind() == want } }

// isInt64 reports whether f is a whole number an int64 holds. 2.0 counts as 2; 2.5 and 1e30 do not.
func isInt64(f float64) bool {
	return f == math.Trunc(f) && f >= -math.Exp2(63) && f < math.Exp2(63)
}

func detail[T any](out outcome, value T) Detail[T] {
	return Detail[T]{Value: value, VariationIndex: out.idx, Reason: out.reason}
}

// BoolVariation returns the flag's value for ctx as a bool, or fallback when the flag serves no bool.
func (c *Client) BoolVariation(flagKey string, ctx Context, fallback bool) bool {
	return c.BoolVariationDetail(flagKey, ctx, fallback).Value
}

// BoolVariationDetail is BoolVariation with the variation index and the reason.
func (c *Client) BoolVariationDetail(flagKey string, ctx Context, fallback bool) Detail[bool] {
	out := c.evaluate(flagKey, ctx, ValueOf(fallback), kindIs(KindBool))
	b, _ := out.value.AsBool()
	return detail(out, b)
}

// StringVariation returns the flag's value for ctx as a string, or fallback when the flag serves no string.
func (c *Client) StringVariation(flagKey string, ctx Context, fallback string) string {
	return c.StringVariationDetail(flagKey, ctx, fallback).Value
}

// StringVariationDetail is StringVariation with the variation index and the reason.
func (c *Client) StringVariationDetail(flagKey string, ctx Context, fallback string) Detail[string] {
	out := c.evaluate(flagKey, ctx, ValueOf(fallback), kindIs(KindString))
	s, _ := out.value.AsString()
	return detail(out, s)
}

// Float64Variation returns the flag's value for ctx as a float64, or fallback when the flag serves no number.
func (c *Client) Float64Variation(flagKey string, ctx Context, fallback float64) float64 {
	return c.Float64VariationDetail(flagKey, ctx, fallback).Value
}

// Float64VariationDetail is Float64Variation with the variation index and the reason.
func (c *Client) Float64VariationDetail(flagKey string, ctx Context, fallback float64) Detail[float64] {
	out := c.evaluate(flagKey, ctx, ValueOf(fallback), kindIs(KindNumber))
	f, _ := out.value.AsNumber()
	return detail(out, f)
}

// IntVariation returns the flag's value for ctx as an int64, or fallback when the flag serves no number, or a
// number with a fraction, or one outside the int64 range. 2.0 counts as 2.
func (c *Client) IntVariation(flagKey string, ctx Context, fallback int64) int64 {
	return c.IntVariationDetail(flagKey, ctx, fallback).Value
}

// IntVariationDetail is IntVariation with the variation index and the reason.
func (c *Client) IntVariationDetail(flagKey string, ctx Context, fallback int64) Detail[int64] {
	accept := func(v Value) bool {
		f, ok := v.AsNumber()
		return ok && isInt64(f)
	}
	out := c.evaluate(flagKey, ctx, ValueOf(fallback), accept)
	if out.idx == NoVariation {
		return detail(out, fallback)
	}
	f, _ := out.value.AsNumber()
	return detail(out, int64(f))
}

// JSONVariation returns the flag's value for ctx as a Value of any kind. A fallback of any other kind than null
// accepts only a served value of its own kind; a null fallback accepts every kind.
func (c *Client) JSONVariation(flagKey string, ctx Context, fallback Value) Value {
	return c.JSONVariationDetail(flagKey, ctx, fallback).Value
}

// JSONVariationDetail is JSONVariation with the variation index and the reason.
func (c *Client) JSONVariationDetail(flagKey string, ctx Context, fallback Value) Detail[Value] {
	accept := func(Value) bool { return true }
	if !fallback.IsNull() {
		accept = kindIs(fallback.Kind())
	}
	out := c.evaluate(flagKey, ctx, fallback, accept)
	return detail(out, out.value)
}
