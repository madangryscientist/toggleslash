package toggleslash

// The evaluation methods. Each accessor has a value form and a Detail form;
// they share one path (contract section 4):
//
//  1. no ruleset held: the fallback, NOT_READY;
//  2. evaluate; no variation index: the fallback and the evaluator's reason;
//  3. the served value's kind is not the fallback's: the fallback, TYPE_MISMATCH;
//  4. otherwise the value, its index and the reason, and the call is counted;
//  5. a panic anywhere above: the fallback, EVALUATION_FAILED and one OnError.
//
// None of them does I/O, takes a lock a network call holds, or panics.

// BoolVariation is the flag's boolean for the context, or fallback.
func (c *Client) BoolVariation(flagKey string, ctx Context, fallback bool) bool {
	return c.BoolVariationDetail(flagKey, ctx, fallback).Value
}

// BoolVariationDetail is BoolVariation with the variation index and the reason.
func (c *Client) BoolVariationDetail(flagKey string, ctx Context, fallback bool) Detail[bool] {
	return evaluate(c, flagKey, ctx, fallback, BoolValue,
		func(v Value) (bool, bool) { return v.Bool(), v.Kind() == KindBool })
}

// StringVariation is the flag's string for the context, or fallback.
func (c *Client) StringVariation(flagKey string, ctx Context, fallback string) string {
	return c.StringVariationDetail(flagKey, ctx, fallback).Value
}

// StringVariationDetail is StringVariation with the variation index and the reason.
func (c *Client) StringVariationDetail(flagKey string, ctx Context, fallback string) Detail[string] {
	return evaluate(c, flagKey, ctx, fallback, StringValue,
		func(v Value) (string, bool) { return v.String(), v.Kind() == KindString })
}

// IntVariation is the flag's integer for the context, or fallback. A served
// number with a fractional part, or outside the int64 range, is a
// TYPE_MISMATCH and gives the fallback; 2.0 is 2.
func (c *Client) IntVariation(flagKey string, ctx Context, fallback int64) int64 {
	return c.IntVariationDetail(flagKey, ctx, fallback).Value
}

// IntVariationDetail is IntVariation with the variation index and the reason.
func (c *Client) IntVariationDetail(flagKey string, ctx Context, fallback int64) Detail[int64] {
	return evaluate(c, flagKey, ctx, fallback,
		func(n int64) Value { return NumberValue(float64(n)) },
		func(v Value) (int64, bool) {
			if v.Kind() != KindNumber {
				return 0, false
			}
			return integerOf(v.Number())
		})
}

// FloatVariation is the flag's number for the context, or fallback.
func (c *Client) FloatVariation(flagKey string, ctx Context, fallback float64) float64 {
	return c.FloatVariationDetail(flagKey, ctx, fallback).Value
}

// FloatVariationDetail is FloatVariation with the variation index and the reason.
func (c *Client) FloatVariationDetail(flagKey string, ctx Context, fallback float64) Detail[float64] {
	return evaluate(c, flagKey, ctx, fallback, NumberValue,
		func(v Value) (float64, bool) { return v.Number(), v.Kind() == KindNumber })
}

// JSONVariation is the flag's JSON value for the context, or fallback. A null
// fallback accepts a value of any kind; otherwise the served kind must be the
// fallback's (array and object are different kinds), and a served null is a
// TYPE_MISMATCH.
func (c *Client) JSONVariation(flagKey string, ctx Context, fallback Value) Value {
	return c.JSONVariationDetail(flagKey, ctx, fallback).Value
}

// JSONVariationDetail is JSONVariation with the variation index and the reason.
func (c *Client) JSONVariationDetail(flagKey string, ctx Context, fallback Value) Detail[Value] {
	return evaluate(c, flagKey, ctx, fallback, func(v Value) Value { return v },
		func(v Value) (Value, bool) {
			if fallback.IsNull() {
				return v, true
			}
			return v, !v.IsNull() && v.Kind() == fallback.Kind()
		})
}

// evaluate is the one path every accessor takes. wrap turns a T into the
// Value the hook reports; convert reads a served Value as a T and says whether
// its kind is the fallback's.
func evaluate[T any](c *Client, flagKey string, ctx Context, fallback T, wrap func(T) Value, convert func(Value) (T, bool)) (detail Detail[T]) {
	snap := c.snap.Load()
	served := Value{}
	servedValue := false

	func() {
		defer func() {
			if r := recover(); r != nil {
				detail = Detail[T]{Value: fallback, Reason: Reason{Kind: ReasonError, Code: ErrEvaluationFailed}}
				servedValue = false
				c.reportError(panicError("evaluation", r))
			}
		}()
		if snap == nil {
			detail = Detail[T]{Value: fallback, Reason: Reason{Kind: ReasonError, Code: ErrNotReady}}
			return
		}
		res := snap.ruleset.Evaluate(flagKey, ctx.v)
		if res.VariationIndex == nil {
			detail = Detail[T]{Value: fallback, Reason: res.Reason}
			return
		}
		value, ok := convert(res.Value)
		if !ok {
			detail = Detail[T]{Value: fallback, Reason: Reason{Kind: ReasonError, Code: ErrTypeMismatch}}
			return
		}
		index := *res.VariationIndex
		c.record(flagKey, index)
		detail = Detail[T]{Value: value, VariationIndex: &index, Reason: res.Reason}
		served, servedValue = res.Value, true
	}()

	if c.hook != nil {
		event := EvaluationEvent{
			FlagKey:        flagKey,
			Context:        ctx,
			VariationIndex: detail.VariationIndex,
			Reason:         detail.Reason,
		}
		if servedValue {
			event.Value = served
		} else {
			event.Value = wrap(fallback)
		}
		if snap != nil {
			event.ETag = snap.etag
			if version, ok := snap.ruleset.FlagVersion(flagKey); ok {
				event.FlagVersion = &version
			}
		}
		c.runHook(event)
	}
	return detail
}

// runHook calls the application's hook on the caller's goroutine. A panic goes
// to OnError and the caller still gets its answer.
func (c *Client) runHook(event EvaluationEvent) {
	defer func() {
		if r := recover(); r != nil {
			c.reportError(panicError("afterEvaluation", r))
		}
	}()
	c.hook(event)
}
