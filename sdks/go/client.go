// Package toggleslash is the ToggleSlash server SDK for Go: it keeps a ruleset from a ToggleSlash
// server fresh, evaluates flags locally against it, and reports usage counts back.
//
// Build a client with New. A client never panics at a caller and never does network work inside
// Variation, VariationDetail or Status: those read memory only. Call Close when finished.
package toggleslash

import (
	"context"
	"math"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
)

// snapshot is a ruleset and the ETag it came with, swapped in as one.
type snapshot struct {
	rules *evaluator.Ruleset
	etag  string
}

// Client is a ToggleSlash server SDK client. It is safe for concurrent use.
type Client struct {
	cfg    config
	base   string
	key    string
	hc     *http.Client
	own    *http.Transport
	ctx    context.Context
	cancel context.CancelFunc

	snap atomic.Pointer[snapshot]

	// stateMu guards what Status reports, so the snapshot, the sync time and the last error that
	// belong together are read together. It is never held across a network call or a callback.
	stateMu  sync.Mutex
	lastSync time.Time
	synced   bool
	lastErr  string
	stream   StreamState

	// holdMu guards the two Retry-After holds, each a time.
	holdMu   sync.Mutex
	holdSync time.Time
	holdUse  time.Time

	usageMu sync.Mutex
	usage   map[usageKey]int64
	flushMu sync.Mutex

	readyCh   chan struct{}
	readyOnce sync.Once
	readyOK   atomic.Bool

	// loops counts the SDK goroutines that are running and not inside an application callback.
	// Close waits for it to reach zero: a goroutine that is inside a callback is not waited for,
	// since it does nothing more until the callback returns, and the callback may be the caller.
	loopMu   sync.Mutex
	loopCond *sync.Cond
	loops    int

	closeMu       sync.Mutex
	closeStarted  bool
	closing       atomic.Bool
	closeDone     chan struct{}
	inFinalReport atomic.Bool
}

// New starts a client for the server at url, authenticating with the server SDK key sdkKey. It
// returns at once; the first ruleset loads in the background, and Ready waits for it. It never
// panics and never returns nil.
func New(url, sdkKey string, opts ...Option) *Client {
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		cfg:       cfg,
		base:      strings.TrimRight(url, "/"),
		key:       sdkKey,
		ctx:       ctx,
		cancel:    cancel,
		usage:     map[usageKey]int64{},
		readyCh:   make(chan struct{}),
		closeDone: make(chan struct{}),
		stream:    StreamOff,
	}
	c.loopCond = sync.NewCond(&c.loopMu)
	c.hc = c.buildHTTPClient()
	if cfg.stream {
		c.stream = StreamConnecting
	}
	c.startLoop(c.syncLoop)
	c.startLoop(c.flushLoop)
	return c
}

func (c *Client) buildHTTPClient() *http.Client {
	var hc http.Client
	if c.cfg.httpClient != nil {
		hc = *c.cfg.httpClient
	} else {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.DisableCompression = true
		hc.Transport = t
		c.own = t
	}
	hc.Timeout = 0
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &hc
}

// startLoop runs fn on a new goroutine that Close waits for.
func (c *Client) startLoop(fn func()) {
	c.loopMu.Lock()
	c.loops++
	c.loopMu.Unlock()
	go func() {
		defer c.loopDone()
		defer func() { _ = recover() }()
		fn()
	}()
}

func (c *Client) loopDone() {
	c.loopMu.Lock()
	c.loops--
	if c.loops == 0 {
		c.loopCond.Broadcast()
	}
	c.loopMu.Unlock()
}

func (c *Client) loopResume() {
	c.loopMu.Lock()
	c.loops++
	c.loopMu.Unlock()
}

// reportFromLoop delivers an error to onError from an SDK goroutine. While the callback runs the
// goroutine is not counted, so a Close called from the callback does not wait for it.
func (c *Client) reportFromLoop(e *Error) {
	if c.cfg.onError == nil {
		return
	}
	c.loopDone()
	defer c.loopResume()
	c.deliver(e)
}

// reportFromCaller delivers an error to onError on the current goroutine.
func (c *Client) reportFromCaller(e *Error) { c.deliver(e) }

func (c *Client) deliver(e *Error) {
	fn := c.cfg.onError
	if fn == nil {
		return
	}
	defer func() { _ = recover() }()
	fn(e)
}

// Ready blocks until the first ruleset attempt has settled, and reports whether a ruleset loaded.
// The answer never changes once given. After Close, a Ready that had not settled answers false.
func (c *Client) Ready() bool {
	<-c.readyCh
	return c.readyOK.Load()
}

func (c *Client) settleReady(ok bool) {
	c.readyOnce.Do(func() {
		c.readyOK.Store(ok)
		close(c.readyCh)
	})
}

// Status returns one snapshot of the sync state.
func (c *Client) Status() Status {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	st := Status{Stream: c.stream, LastError: c.lastErr}
	if s := c.snap.Load(); s != nil {
		st.ETag = s.etag
	}
	if c.synced {
		t := c.lastSync
		st.LastSyncAt = &t
	}
	return st
}

func (c *Client) setStream(s StreamState) {
	c.stateMu.Lock()
	if c.stream != StreamClosed {
		c.stream = s
	}
	c.stateMu.Unlock()
}

// Variation returns the value of a flag for a context, or fallback when it cannot be served.
func (c *Client) Variation(flagKey string, ctx Context, fallback Value) Value {
	return c.VariationDetail(flagKey, ctx, fallback).Value
}

// VariationDetail is Variation with the variation index and the reason.
func (c *Client) VariationDetail(flagKey string, ctx Context, fallback Value) Detail {
	return c.detail(flagKey, ctx, fallback, nil)
}

// BoolVariation returns a boolean flag's value, or fallback.
func (c *Client) BoolVariation(flagKey string, ctx Context, fallback bool) bool {
	return c.BoolVariationDetail(flagKey, ctx, fallback).Value
}

// BoolVariationDetail is BoolVariation with the variation index and the reason.
func (c *Client) BoolVariationDetail(flagKey string, ctx Context, fallback bool) TypedDetail[bool] {
	d := c.detail(flagKey, ctx, Bool(fallback), nil)
	return typed(d, fallback, func(v Value) bool { return v.Bool() })
}

// StringVariation returns a string flag's value, or fallback.
func (c *Client) StringVariation(flagKey string, ctx Context, fallback string) string {
	return c.StringVariationDetail(flagKey, ctx, fallback).Value
}

// StringVariationDetail is StringVariation with the variation index and the reason.
func (c *Client) StringVariationDetail(flagKey string, ctx Context, fallback string) TypedDetail[string] {
	d := c.detail(flagKey, ctx, String(fallback), nil)
	return typed(d, fallback, func(v Value) string { return v.String() })
}

// NumberVariation returns a number flag's value as a float64, or fallback.
func (c *Client) NumberVariation(flagKey string, ctx Context, fallback float64) float64 {
	return c.NumberVariationDetail(flagKey, ctx, fallback).Value
}

// NumberVariationDetail is NumberVariation with the variation index and the reason.
func (c *Client) NumberVariationDetail(flagKey string, ctx Context, fallback float64) TypedDetail[float64] {
	d := c.detail(flagKey, ctx, Number(fallback), nil)
	return typed(d, fallback, func(v Value) float64 { return v.Number() })
}

// IntVariation returns a number flag's value as an int64, or fallback. A served number with a
// fractional part, or outside the int64 range, answers fallback with TypeMismatch; 2.0 is the
// integer 2.
func (c *Client) IntVariation(flagKey string, ctx Context, fallback int64) int64 {
	return c.IntVariationDetail(flagKey, ctx, fallback).Value
}

// IntVariationDetail is IntVariation with the variation index and the reason.
func (c *Client) IntVariationDetail(flagKey string, ctx Context, fallback int64) TypedDetail[int64] {
	d := c.detail(flagKey, ctx, Number(float64(fallback)), isInt64)
	return typed(d, fallback, func(v Value) int64 { return int64(v.Number()) })
}

// JSONVariation is Variation for an array or object flag: it answers fallback unless the flag
// serves the same kind as the fallback (a null fallback accepts any).
func (c *Client) JSONVariation(flagKey string, ctx Context, fallback Value) Value {
	return c.Variation(flagKey, ctx, fallback)
}

func typed[T any](d Detail, fallback T, read func(Value) T) TypedDetail[T] {
	if d.VariationIndex == NoVariation {
		return TypedDetail[T]{Value: fallback, VariationIndex: d.VariationIndex, Reason: d.Reason}
	}
	return TypedDetail[T]{Value: read(d.Value), VariationIndex: d.VariationIndex, Reason: d.Reason}
}

// isInt64 accepts a number with no fractional part in [-2^63, 2^63). A served number is a binary64,
// so 2^63 - 1 reads as 2^63 and is out of range.
func isInt64(v Value) bool {
	n := v.Number()
	return n == math.Trunc(n) && n >= -(1<<63) && n < (1<<63)
}

func (c *Client) fallbackDetail(fallback Value, reason Reason) Detail {
	return Detail{Value: fallback, VariationIndex: NoVariation, Reason: reason}
}

// detail builds the answer in the order of the contract's section 4, counts it when it is a served
// value, and runs the hook. accept is an extra check on a served value of the right kind (the
// integer accessor's range).
func (c *Client) detail(flagKey string, ctx Context, fallback Value, accept func(Value) bool) Detail {
	snap := c.snap.Load()
	if snap == nil {
		d := c.fallbackDetail(fallback, errorReason(NotReady))
		c.after(nil, flagKey, ctx, d, false)
		return d
	}
	res, ok := c.safeEvaluate(snap.rules, flagKey, ctx)
	if !ok {
		d := c.fallbackDetail(fallback, errorReason(EvaluationFailed))
		c.after(snap, flagKey, ctx, d, false)
		return d
	}
	if res.VariationIndex == NoVariation {
		d := c.fallbackDetail(fallback, res.Reason)
		c.after(snap, flagKey, ctx, d, false)
		return d
	}
	if !kindAccepted(fallback, res.Value) || (accept != nil && !accept(res.Value)) {
		d := c.fallbackDetail(fallback, errorReason(TypeMismatch))
		c.after(snap, flagKey, ctx, d, false)
		return d
	}
	d := Detail{Value: res.Value, VariationIndex: res.VariationIndex, Reason: res.Reason}
	c.after(snap, flagKey, ctx, d, true)
	return d
}

func kindAccepted(fallback, served Value) bool {
	if fallback.IsNull() {
		return true
	}
	return fallback.Kind() == served.Kind()
}

// safeEvaluate runs the evaluator, turning a panic into ok == false and an error for onError.
func (c *Client) safeEvaluate(rs *evaluator.Ruleset, flagKey string, ctx Context) (res evaluator.Result, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
			c.reportFromCaller(&Error{
				Source:  SourceEvaluator,
				Message: "the evaluator raised",
				Err:     &PanicError{Value: r},
			})
		}
	}()
	return c.cfg.evaluate(rs, flagKey, ctx), true
}

// after counts a served answer and runs the hook, in that order, outside every lock. A panic in
// the hook goes to onError and changes nothing else.
func (c *Client) after(snap *snapshot, flagKey string, ctx Context, d Detail, counted bool) {
	if counted && !c.closing.Load() {
		c.count(flagKey, d.VariationIndex)
	}
	hook := c.cfg.hook
	if hook == nil {
		return
	}
	ev := EvaluationEvent{
		FlagKey:        flagKey,
		Context:        ctx,
		Value:          d.Value,
		VariationIndex: d.VariationIndex,
		Reason:         d.Reason,
	}
	if snap != nil {
		ev.ETag = snap.etag
		if v, ok := snap.rules.FlagVersion(flagKey); ok {
			ev.FlagVersion = &v
		}
	}
	defer func() {
		if r := recover(); r != nil {
			c.reportFromCaller(&Error{
				Source:  SourceHook,
				Message: "the afterEvaluation hook raised",
				Err:     &PanicError{Value: r},
			})
		}
	}()
	hook(ev)
}

// Close stops the client: it abandons the ruleset and stream requests, stops the flush loop, waits
// for a flush already running, and sends a final usage flush. It is safe to call twice and from
// several goroutines; every call returns when the shutdown is done, within about twice the request
// timeout. A Ready that had not settled answers false. Evaluation keeps answering from the ruleset
// held, and counts nothing.
func (c *Client) Close() {
	c.closeMu.Lock()
	if c.closeStarted {
		c.closeMu.Unlock()
		if c.inFinalReport.Load() {
			// Called from the onError callback of the final flush: it is the shutdown itself.
			return
		}
		<-c.closeDone
		return
	}
	c.closeStarted = true
	c.closing.Store(true)
	c.closeMu.Unlock()

	c.stateMu.Lock()
	c.stream = StreamClosed
	c.stateMu.Unlock()
	c.cancel()
	c.settleReady(false)

	c.loopMu.Lock()
	for c.loops > 0 {
		c.loopCond.Wait()
	}
	c.loopMu.Unlock()

	for _, e := range c.flush(true) {
		c.inFinalReport.Store(true)
		c.reportFromCaller(e)
		c.inFinalReport.Store(false)
	}
	if c.own != nil {
		c.own.CloseIdleConnections()
	}
	close(c.closeDone)
}
