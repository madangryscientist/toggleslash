package toggleslash

import (
	"fmt"
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

// Flaggable is the Go types a flag value can be read as: the five accessors below, as one constraint.
type Flaggable interface {
	bool | string | int64 | float64 | Value
}

// PanicError is what the error handler gets when the evaluator or the AfterEvaluation function panicked.
type PanicError struct {
	// Value is what was passed to panic.
	Value any
	where string
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("toggleslash: %s panicked: %v", e.where, e.Value)
}

// outcome is one evaluation before it is typed. served is false when the caller gets its fallback.
type outcome struct {
	state  *state
	value  Value
	index  int
	reason Reason
	served bool
}

// evaluate is the result order of contract 4.
func (c *Client) evaluate(flagKey string, ctx Context, fallback Value, accept func(served, fallback Value) bool) outcome {
	st := c.state.Load()
	o := outcome{state: st, value: fallback, index: -1}
	if st.rs == nil {
		o.reason = Reason{Kind: ReasonError, Code: ErrNotReady}
		return o
	}
	res, perr := c.safeEvaluate(st.rs, flagKey, ctx.v)
	if perr != nil {
		c.callOnError(perr)
		o.reason = Reason{Kind: ReasonError, Code: ErrEvaluationFailed}
		return o
	}
	if res.VariationIndex < 0 {
		o.reason = res.Reason
		return o
	}
	if !accept(res.Value, fallback) {
		o.reason = Reason{Kind: ReasonError, Code: ErrTypeMismatch}
		return o
	}
	c.count(flagKey, res.VariationIndex)
	return outcome{state: st, value: res.Value, index: res.VariationIndex, reason: res.Reason, served: true}
}

func (c *Client) safeEvaluate(rs *evaluator.Ruleset, flagKey string, ctx Value) (res evaluator.Result, perr *PanicError) {
	defer func() {
		if r := recover(); r != nil {
			perr = &PanicError{Value: r, where: "evaluation"}
		}
	}()
	return c.cfg.evaluate(rs, flagKey, ctx), nil
}

// callHook gives the AfterEvaluation function what this evaluation did. A panic in it goes to the error
// handler and changes nothing else.
func (c *Client) callHook(flagKey string, ctx Context, o outcome) {
	if c.cfg.afterEvaluation == nil {
		return
	}
	ev := EvaluationEvent{
		FlagKey: flagKey, Context: ctx, Value: o.value, VariationIndex: o.index, Reason: o.reason,
		ETag: o.state.etag,
	}
	if o.state.rs != nil {
		if v, ok := o.state.rs.FlagVersion(flagKey); ok {
			ev.FlagVersion = &v
		}
	}
	defer func() {
		if r := recover(); r != nil {
			c.callOnError(&PanicError{Value: r, where: "the AfterEvaluation function"})
		}
	}()
	c.cfg.afterEvaluation(ev)
}

// accessor says how one Go type maps to and from a JSON value.
type accessor[T any] struct {
	toValue   func(T) Value
	fromValue func(Value) T
	accept    func(served, fallback Value) bool
}

func detail[T any](c *Client, flagKey string, ctx Context, fallback T, a accessor[T]) Detail[T] {
	o := c.evaluate(flagKey, ctx, a.toValue(fallback), a.accept)
	d := Detail[T]{Value: fallback, VariationIndex: o.index, Reason: o.reason}
	if o.served {
		d.Value = a.fromValue(o.value)
	}
	c.callHook(flagKey, ctx, o)
	return d
}

var (
	boolAccessor = accessor[bool]{
		toValue: jsonvalue.Bool, fromValue: Value.BoolValue,
		accept: func(v, _ Value) bool { return v.IsBool() },
	}
	stringAccessor = accessor[string]{
		toValue: jsonvalue.String, fromValue: Value.StringValue,
		accept: func(v, _ Value) bool { return v.IsString() },
	}
	floatAccessor = accessor[float64]{
		toValue: jsonvalue.Number, fromValue: Value.NumberValue,
		accept: func(v, _ Value) bool { return v.IsNumber() },
	}
	intAccessor = accessor[int64]{
		toValue:   func(i int64) Value { return jsonvalue.Number(float64(i)) },
		fromValue: func(v Value) int64 { return int64(v.NumberValue()) },
		accept:    func(v, _ Value) bool { return isInt64(v) },
	}
	jsonAccessor = accessor[Value]{
		toValue:   func(v Value) Value { return v },
		fromValue: func(v Value) Value { return v },
		// A null fallback says nothing about the type the caller wants.
		accept: func(v, fallback Value) bool { return fallback.IsNull() || v.Kind() == fallback.Kind() },
	}
)

// isInt64 reports whether v is a number with no fractional part that an int64 can hold. 2.0 is the integer 2.
func isInt64(v Value) bool {
	if !v.IsFiniteNumber() {
		return false
	}
	n := v.NumberValue()
	return n == math.Trunc(n) && n >= -(1<<63) && n < 1<<63
}

// BoolVariation is the flag's value for a context, or fallback. It does no I/O and never panics.
func (c *Client) BoolVariation(flagKey string, ctx Context, fallback bool) bool {
	return c.BoolVariationDetail(flagKey, ctx, fallback).Value
}

// BoolVariationDetail is BoolVariation with the variation index and the reason.
func (c *Client) BoolVariationDetail(flagKey string, ctx Context, fallback bool) Detail[bool] {
	return detail(c, flagKey, ctx, fallback, boolAccessor)
}

// StringVariation is the flag's value for a context, or fallback. A served string that holds a lone
// surrogate is returned as its three-byte WTF-8 form, which is not valid UTF-8.
func (c *Client) StringVariation(flagKey string, ctx Context, fallback string) string {
	return c.StringVariationDetail(flagKey, ctx, fallback).Value
}

// StringVariationDetail is StringVariation with the variation index and the reason.
func (c *Client) StringVariationDetail(flagKey string, ctx Context, fallback string) Detail[string] {
	return detail(c, flagKey, ctx, fallback, stringAccessor)
}

// IntVariation is the flag's value as an int64, or fallback. A served number with a fractional part, or one an
// int64 cannot hold, is a TYPE_MISMATCH. Numbers are read as binary64, so 2^63-1024 is the largest it serves.
func (c *Client) IntVariation(flagKey string, ctx Context, fallback int64) int64 {
	return c.IntVariationDetail(flagKey, ctx, fallback).Value
}

// IntVariationDetail is IntVariation with the variation index and the reason.
func (c *Client) IntVariationDetail(flagKey string, ctx Context, fallback int64) Detail[int64] {
	return detail(c, flagKey, ctx, fallback, intAccessor)
}

// FloatVariation is the flag's value as a float64, or fallback.
func (c *Client) FloatVariation(flagKey string, ctx Context, fallback float64) float64 {
	return c.FloatVariationDetail(flagKey, ctx, fallback).Value
}

// FloatVariationDetail is FloatVariation with the variation index and the reason.
func (c *Client) FloatVariationDetail(flagKey string, ctx Context, fallback float64) Detail[float64] {
	return detail(c, flagKey, ctx, fallback, floatAccessor)
}

// JSONVariation is the flag's value as a JSON Value, or fallback. The served value must be the same JSON type
// as the fallback; a null fallback accepts any type.
func (c *Client) JSONVariation(flagKey string, ctx Context, fallback Value) Value {
	return c.JSONVariationDetail(flagKey, ctx, fallback).Value
}

// JSONVariationDetail is JSONVariation with the variation index and the reason.
func (c *Client) JSONVariationDetail(flagKey string, ctx Context, fallback Value) Detail[Value] {
	return detail(c, flagKey, ctx, fallback, jsonAccessor)
}

// Variation is the typed accessor chosen by T: the flag's value for a context, or fallback.
func Variation[T Flaggable](c *Client, flagKey string, ctx Context, fallback T) T {
	return VariationDetail(c, flagKey, ctx, fallback).Value
}

// VariationDetail is Variation with the variation index and the reason.
func VariationDetail[T Flaggable](c *Client, flagKey string, ctx Context, fallback T) Detail[T] {
	switch f := any(fallback).(type) {
	case bool:
		return retype[T](c.BoolVariationDetail(flagKey, ctx, f))
	case string:
		return retype[T](c.StringVariationDetail(flagKey, ctx, f))
	case int64:
		return retype[T](c.IntVariationDetail(flagKey, ctx, f))
	case float64:
		return retype[T](c.FloatVariationDetail(flagKey, ctx, f))
	default:
		return retype[T](c.JSONVariationDetail(flagKey, ctx, any(fallback).(Value)))
	}
}

// retype moves a Detail between two type parameters that are the same type at run time.
func retype[T, U any](d Detail[U]) Detail[T] {
	return Detail[T]{Value: any(d.Value).(T), VariationIndex: d.VariationIndex, Reason: d.Reason}
}
