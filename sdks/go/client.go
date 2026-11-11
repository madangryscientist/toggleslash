package toggleslash

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/eval"
)

// Client follows one environment's ruleset and evaluates flags from it. Every method is safe to call from
// many goroutines at once. Make one with NewClient and share it.
type Client struct {
	cfg       config
	baseURL   string
	sdkKey    string
	httpc     *http.Client
	transport *http.Transport

	// snap is the ruleset and its ETag, replaced whole. install stores it under stateMu so Status reads the
	// same pair as the fields beside it.
	snap atomic.Pointer[snapshot]

	stateMu    sync.Mutex
	lastSyncAt time.Time
	stream     StreamState
	lastError  SyncError

	usageMu sync.Mutex
	pending map[usageKey]int64
	flushMu sync.Mutex

	// syncHold and usageHold are Retry-After deadlines in Unix nanoseconds on the client's clock.
	syncHold  atomic.Int64
	usageHold atomic.Int64

	ctx       context.Context
	cancel    context.CancelFunc
	syncDone  chan struct{}
	flushDone chan struct{}

	readyCh   chan struct{}
	readyOnce sync.Once
	readyVal  atomic.Bool

	closing atomic.Bool
	done    chan struct{}
	// inCallback counts the application callbacks (OnError) now running on a goroutine Close waits for: the
	// two loops and Close's own final flush. Close stops waiting while it is above zero, so a Close called
	// from such a callback returns instead of waiting for itself.
	inCallback atomic.Int32
}

// NewClient starts a client for the ToggleSlash server at url, which authenticates with sdkKey, a server
// SDK key. It returns at once and never fails: the first ruleset request runs on a goroutine, and until it
// ends every evaluation returns its fallback with the NOT_READY reason. Call Ready to wait for it, and
// Close when done.
func NewClient(url, sdkKey string, opts ...Option) *Client {
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	defaults := defaultConfig()
	if cfg.pollInterval <= 0 {
		cfg.pollInterval = defaults.pollInterval
	}
	if cfg.flushInterval <= 0 {
		cfg.flushInterval = defaults.flushInterval
	}
	if cfg.requestTimeout <= 0 {
		cfg.requestTimeout = defaults.requestTimeout
	}
	if cfg.watchdog <= 0 {
		cfg.watchdog = defaults.watchdog
	}
	if cfg.clock == nil {
		cfg.clock = defaults.clock
	}
	if cfg.jitter == nil {
		cfg.jitter = defaults.jitter
	}
	if cfg.evaluator == nil {
		cfg.evaluator = defaults.evaluator
	}

	c := &Client{
		cfg:       cfg,
		baseURL:   strings.TrimRight(url, "/"),
		sdkKey:    sdkKey,
		pending:   make(map[usageKey]int64),
		syncDone:  make(chan struct{}),
		flushDone: make(chan struct{}),
		readyCh:   make(chan struct{}),
		done:      make(chan struct{}),
		stream:    StreamOff,
	}
	if cfg.stream {
		c.stream = StreamConnecting
	}
	c.httpc, c.transport = newHTTPClient(cfg.httpClient)
	c.ctx, c.cancel = context.WithCancel(context.Background())
	go c.runSync(c.ctx)
	go c.runFlush(c.ctx)
	return c
}

// Ready waits for the first ruleset request to end, which takes at most the request timeout, and reports
// whether a ruleset is held. The answer is fixed then: a later load does not turn false into true. After
// Close it reports false if the first request had not ended. It may be called any number of times.
func (c *Client) Ready() bool {
	<-c.readyCh
	return c.readyVal.Load()
}

func (c *Client) settleReady(held bool) {
	c.readyOnce.Do(func() {
		c.readyVal.Store(held)
		close(c.readyCh)
	})
}

// Status is one consistent snapshot of what the client has loaded. It makes no request and never waits.
func (c *Client) Status() Status {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	st := Status{LastSyncAt: c.lastSyncAt, Stream: c.stream, LastError: c.lastError}
	if held := c.snap.Load(); held != nil {
		st.ETag = held.etag
	}
	if c.closing.Load() {
		st.Stream = StreamClosed
	}
	return st
}

// report hands an error to the OnError function. A panic in that function is ignored.
func (c *Client) report(err error) {
	f := c.cfg.onError
	if f == nil || err == nil {
		return
	}
	defer func() { _ = recover() }()
	f(err)
}

// reportFromLoop is report, from a goroutine that Close waits for.
func (c *Client) reportFromLoop(err error) {
	c.inCallback.Add(1)
	defer c.inCallback.Add(-1)
	c.report(err)
}

// BoolVariation is the value of a boolean flag for ctx, or fallback.
func (c *Client) BoolVariation(flagKey string, ctx Context, fallback bool) bool {
	return c.BoolVariationDetail(flagKey, ctx, fallback).Value
}

// StringVariation is the value of a string flag for ctx, or fallback.
func (c *Client) StringVariation(flagKey string, ctx Context, fallback string) string {
	return c.StringVariationDetail(flagKey, ctx, fallback).Value
}

// IntVariation is the value of an integer flag for ctx, or fallback. A served number with a fraction, or
// outside the range of int64, is a TYPE_MISMATCH and gives fallback.
func (c *Client) IntVariation(flagKey string, ctx Context, fallback int64) int64 {
	return c.IntVariationDetail(flagKey, ctx, fallback).Value
}

// FloatVariation is the value of a number flag for ctx, or fallback.
func (c *Client) FloatVariation(flagKey string, ctx Context, fallback float64) float64 {
	return c.FloatVariationDetail(flagKey, ctx, fallback).Value
}

// JSONVariation is the value of a flag of any kind for ctx, or fallback. A null fallback accepts any
// served value; otherwise the served value must be of the fallback's JSON kind.
func (c *Client) JSONVariation(flagKey string, ctx Context, fallback Value) Value {
	return c.JSONVariationDetail(flagKey, ctx, fallback).Value
}

// BoolVariationDetail is BoolVariation with the variation index and the reason.
func (c *Client) BoolVariationDetail(flagKey string, ctx Context, fallback bool) Detail[bool] {
	return variation(c, flagKey, ctx, fallback, Bool,
		func(v Value) (bool, bool) { return v.AsBool() })
}

// StringVariationDetail is StringVariation with the variation index and the reason.
func (c *Client) StringVariationDetail(flagKey string, ctx Context, fallback string) Detail[string] {
	return variation(c, flagKey, ctx, fallback, String,
		func(v Value) (string, bool) { return v.AsString() })
}

// IntVariationDetail is IntVariation with the variation index and the reason.
func (c *Client) IntVariationDetail(flagKey string, ctx Context, fallback int64) Detail[int64] {
	return variation(c, flagKey, ctx, fallback,
		func(n int64) Value { return Number(float64(n)) }, intFromValue)
}

// FloatVariationDetail is FloatVariation with the variation index and the reason.
func (c *Client) FloatVariationDetail(flagKey string, ctx Context, fallback float64) Detail[float64] {
	return variation(c, flagKey, ctx, fallback, Number,
		func(v Value) (float64, bool) { return v.AsNumber() })
}

// JSONVariationDetail is JSONVariation with the variation index and the reason.
func (c *Client) JSONVariationDetail(flagKey string, ctx Context, fallback Value) Detail[Value] {
	return variation(c, flagKey, ctx, fallback, func(v Value) Value { return v },
		func(v Value) (Value, bool) {
			if fallback.IsNull() || v.Kind() == fallback.Kind() {
				return v, true
			}
			return Value{}, false
		})
}

// outcome is the result of one evaluation before it is typed: a served value (index >= 0) or a failure.
type outcome struct {
	value  Value
	index  int
	reason Reason
}

// variation is every accessor: evaluate, apply the mismatch rule through convert, count a served value,
// run the hook, and answer. wrap turns the answer back into a Value for the hook.
func variation[T any](
	c *Client, flagKey string, ctx Context, fallback T,
	wrap func(T) Value, convert func(Value) (T, bool),
) Detail[T] {
	held := c.snap.Load()
	detail := Detail[T]{Value: fallback}
	counted := false
	switch held {
	case nil:
		detail.Reason = Reason{Kind: ReasonError, Code: ErrNotReady}
	default:
		out, ok := c.runEvaluator(held.ruleset, flagKey, ctx)
		switch {
		case !ok:
			detail.Reason = Reason{Kind: ReasonError, Code: ErrEvaluationFailed}
		case out.index < 0:
			detail.Reason = out.reason
		default:
			if typed, matches := convert(out.value); matches {
				index := out.index
				detail = Detail[T]{Value: typed, VariationIndex: &index, Reason: out.reason}
				counted = true
			} else {
				detail.Reason = Reason{Kind: ReasonError, Code: ErrTypeMismatch}
			}
		}
	}
	if counted {
		c.record(flagKey, *detail.VariationIndex)
	}
	if c.cfg.afterEvaluation != nil {
		c.runHook(held, flagKey, ctx, wrap(detail.Value), detail.VariationIndex, detail.Reason)
	}
	return detail
}

// runEvaluator runs the evaluator, turning a panic into ok == false and a report to OnError.
func (c *Client) runEvaluator(rs *eval.Ruleset, flagKey string, ctx Context) (out outcome, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			c.report(&PanicError{Where: "the evaluator", Value: "evaluation raised"})
			out, ok = outcome{}, false
		}
	}()
	res := c.cfg.evaluator(rs, flagKey, ctx)
	return outcome{
		value: res.Value,
		index: res.VariationIndex,
		reason: Reason{
			Kind:      ReasonKind(res.Reason.Kind),
			RuleIndex: res.Reason.RuleIndex,
			FlagKey:   res.Reason.FlagKey,
			Code:      ErrorCode(res.Reason.Code),
		},
	}, true
}

// runHook calls the AfterEvaluation function with the ETag and flag version of the snapshot the evaluation
// used. A panic in it goes to OnError.
func (c *Client) runHook(held *snapshot, flagKey string, ctx Context, value Value, index *int, reason Reason) {
	event := EvaluationEvent{
		FlagKey: flagKey, Context: ctx, Value: value, VariationIndex: index, Reason: reason,
	}
	if held != nil {
		event.ETag = held.etag
		if version, ok := held.ruleset.FlagVersion(flagKey); ok {
			event.FlagVersion = &version
		}
	}
	defer func() {
		if r := recover(); r != nil {
			c.report(&PanicError{Where: "the AfterEvaluation function", Value: r})
		}
	}()
	c.cfg.afterEvaluation(event)
}

// Close stops following the server, sends the final usage counts and releases the client's goroutines and
// connections. It never fails, may be called more than once and from many goroutines, and every call
// returns when the same shutdown is done: within about twice the request timeout. A Close called from the
// OnError function, on a goroutine the client owns, stops the rest of the shutdown and returns at once.
func (c *Client) Close() {
	if c.closing.CompareAndSwap(false, true) {
		c.shutdown()
		close(c.done)
	}
	c.waitFor(c.done)
}

func (c *Client) shutdown() {
	c.cancel()
	c.waitFor(c.syncDone)
	c.waitFor(c.flushDone)
	c.settleReady(false)
	c.finalFlush()
	if c.transport != nil {
		c.transport.CloseIdleConnections()
	}
}

// waitFor waits for ch, or stops waiting while an application callback runs on a goroutine of the client.
func (c *Client) waitFor(ch <-chan struct{}) {
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ch:
			return
		case <-tick.C:
			if c.inCallback.Load() > 0 {
				return
			}
		}
	}
}
