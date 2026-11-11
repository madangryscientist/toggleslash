package toggleslash

import (
	"github.com/madangryscientist/toggleslash/sdks/go/internal/eval"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

// evaluation is the answer of contract section 4, before a typed accessor reads it.
type evaluation struct {
	value  jsonvalue.Value // the served value or the fallback
	index  int             // -1: none
	reason Reason
}

func publicReason(r eval.Reason) Reason {
	out := Reason{Kind: ReasonKind(r.Kind)}
	switch r.Kind {
	case eval.ReasonError:
		out.Code = ErrorCode(r.Code)
	case eval.ReasonRuleMatch:
		i := r.RuleIndex
		out.RuleIndex = &i
	case eval.ReasonPrerequisiteFailed:
		out.PrerequisiteKey = r.FlagKey
	}
	return out
}

func errorReason(code ErrorCode) Reason { return Reason{Kind: ReasonError, Code: code} }

// accepts says whether a served value has the kind the accessor wants.
type accepts func(served jsonvalue.Value) bool

func kindIs(k jsonvalue.Kind) accepts {
	return func(v jsonvalue.Value) bool { return v.Kind == k }
}

func integral(v jsonvalue.Value) bool {
	if v.Kind != jsonvalue.Number {
		return false
	}
	_, ok := wholeInt64(v.N)
	return ok
}

// sameKindAs is the generic accessor's rule: a null fallback accepts any served value, and otherwise the
// JSON kinds must match (so a null served value against a non-null fallback is a mismatch).
func sameKindAs(fallback jsonvalue.Value) accepts {
	return func(v jsonvalue.Value) bool { return fallback.Kind == jsonvalue.Null || v.Kind == fallback.Kind }
}

// resolve builds the result in the order of contract section 4, calls the hook and counts the evaluation.
func (c *Client) resolve(flagKey string, ctx Context, fallback jsonvalue.Value, accept accepts) (out evaluation) {
	// Nothing in here is meant to panic. If something does, the caller still gets its fallback.
	defer func() {
		if r := recover(); r != nil {
			out = evaluation{value: fallback, index: -1, reason: errorReason(ErrEvaluationFailed)}
			c.report(&Error{Op: "evaluate", Code: "evaluation failed"})
		}
	}()
	snap := c.snap.Load()
	out = evaluation{value: fallback, index: -1, reason: errorReason(ErrNotReady)}
	counted := false
	if snap != nil {
		out, counted = c.evaluateSafely(snap, flagKey, ctx, fallback, accept)
	}
	if counted {
		c.record(flagKey, out.index)
	}
	c.callHook(snap, flagKey, ctx, out)
	return out
}

// evaluateSafely is steps 2 to 4, and step 5 when they panic.
func (c *Client) evaluateSafely(snap *snapshot, flagKey string, ctx Context, fallback jsonvalue.Value, accept accepts) (out evaluation, counted bool) {
	defer func() {
		if r := recover(); r != nil {
			out, counted = evaluation{value: fallback, index: -1, reason: errorReason(ErrEvaluationFailed)}, false
			c.report(&Error{Op: "evaluate", Code: "evaluation failed"})
		}
	}()
	res := c.cfg.evaluate(snap.rs, flagKey, ctx.v)
	if res.VariationIndex < 0 {
		return evaluation{value: fallback, index: -1, reason: publicReason(res.Reason)}, false
	}
	if !accept(res.Value) {
		return evaluation{value: fallback, index: -1, reason: errorReason(ErrTypeMismatch)}, false
	}
	return evaluation{value: res.Value, index: res.VariationIndex, reason: publicReason(res.Reason)}, true
}

func (c *Client) callHook(snap *snapshot, flagKey string, ctx Context, out evaluation) {
	hook := c.cfg.afterEvaluation
	if hook == nil {
		return
	}
	ev := EvaluationEvent{FlagKey: flagKey, Context: ctx, Value: Value{out.value}, Reason: out.reason}
	if out.index >= 0 {
		i := out.index
		ev.VariationIndex = &i
	}
	if snap != nil {
		ev.ETag = snap.etag
		if v, ok := snap.rs.Version(flagKey); ok {
			n := int64(v)
			ev.FlagVersion = &n
		}
	}
	defer func() {
		if r := recover(); r != nil {
			c.report(&Error{Op: "hook", Code: "hook panicked"})
		}
	}()
	hook(ev)
}

func (e evaluation) indexPtr() *int {
	if e.index < 0 {
		return nil
	}
	i := e.index
	return &i
}

// BoolVariation is the flag's boolean value for ctx, or fallback.
func (c *Client) BoolVariation(flagKey string, ctx Context, fallback bool) bool {
	return c.BoolVariationDetail(flagKey, ctx, fallback).Value
}

// BoolVariationDetail is BoolVariation with the variation index and the reason.
func (c *Client) BoolVariationDetail(flagKey string, ctx Context, fallback bool) Detail[bool] {
	e := c.resolve(flagKey, ctx, jsonvalue.Boolean(fallback), kindIs(jsonvalue.Bool))
	v, ok := fallback, e.index >= 0
	if ok {
		v = e.value.B
	}
	return Detail[bool]{Value: v, VariationIndex: e.indexPtr(), Reason: e.reason}
}

// StringVariation is the flag's string value for ctx, or fallback.
func (c *Client) StringVariation(flagKey string, ctx Context, fallback string) string {
	return c.StringVariationDetail(flagKey, ctx, fallback).Value
}

// StringVariationDetail is StringVariation with the variation index and the reason.
func (c *Client) StringVariationDetail(flagKey string, ctx Context, fallback string) Detail[string] {
	e := c.resolve(flagKey, ctx, jsonvalue.Str(fallback), kindIs(jsonvalue.String))
	v := fallback
	if e.index >= 0 {
		v = e.value.S
	}
	return Detail[string]{Value: v, VariationIndex: e.indexPtr(), Reason: e.reason}
}

// FloatVariation is the flag's number value for ctx, or fallback.
func (c *Client) FloatVariation(flagKey string, ctx Context, fallback float64) float64 {
	return c.FloatVariationDetail(flagKey, ctx, fallback).Value
}

// FloatVariationDetail is FloatVariation with the variation index and the reason.
func (c *Client) FloatVariationDetail(flagKey string, ctx Context, fallback float64) Detail[float64] {
	e := c.resolve(flagKey, ctx, jsonvalue.Num(fallback), kindIs(jsonvalue.Number))
	v := fallback
	if e.index >= 0 {
		v = e.value.N
	}
	return Detail[float64]{Value: v, VariationIndex: e.indexPtr(), Reason: e.reason}
}

// IntVariation is the flag's integer value for ctx, or fallback. A served number with a fractional part, or
// outside the int64 range, is a TYPE_MISMATCH and gives fallback; 2.0 counts as 2.
func (c *Client) IntVariation(flagKey string, ctx Context, fallback int64) int64 {
	return c.IntVariationDetail(flagKey, ctx, fallback).Value
}

// IntVariationDetail is IntVariation with the variation index and the reason.
func (c *Client) IntVariationDetail(flagKey string, ctx Context, fallback int64) Detail[int64] {
	e := c.resolve(flagKey, ctx, jsonvalue.Num(float64(fallback)), integral)
	v := fallback
	if e.index >= 0 {
		v, _ = wholeInt64(e.value.N)
	}
	return Detail[int64]{Value: v, VariationIndex: e.indexPtr(), Reason: e.reason}
}

// AnyVariation is the generic accessor: the flag's value of any JSON kind. The fallback's kind decides what
// matches; a null fallback accepts any served value.
func (c *Client) AnyVariation(flagKey string, ctx Context, fallback Value) Value {
	return c.AnyVariationDetail(flagKey, ctx, fallback).Value
}

// AnyVariationDetail is AnyVariation with the variation index and the reason.
func (c *Client) AnyVariationDetail(flagKey string, ctx Context, fallback Value) Detail[Value] {
	e := c.resolve(flagKey, ctx, fallback.v, sameKindAs(fallback.v))
	return Detail[Value]{Value: Value{e.value}, VariationIndex: e.indexPtr(), Reason: e.reason}
}

// JSONVariation is AnyVariation for an array or an object: the fallback's kind decides, and an array and an
// object are different kinds.
func (c *Client) JSONVariation(flagKey string, ctx Context, fallback Value) Value {
	return c.AnyVariation(flagKey, ctx, fallback)
}

// JSONVariationDetail is JSONVariation with the variation index and the reason.
func (c *Client) JSONVariationDetail(flagKey string, ctx Context, fallback Value) Detail[Value] {
	return c.AnyVariationDetail(flagKey, ctx, fallback)
}
