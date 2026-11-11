package toggleslash

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// errTimeout is the cause a request's context carries when requestTimeout (or the stream's watchdog) ran
// out, which is how a timeout is told from any other failure.
var errTimeout = errors.New("toggleslash: timeout")

// evaluateFunc is the evaluator's call; a test swaps it to make an evaluation panic.
type evaluateFunc func(rs *evaluator.Ruleset, flagKey string, context jsonvalue.Value) evaluator.Result

func defaultEvaluate(rs *evaluator.Ruleset, flagKey string, context jsonvalue.Value) evaluator.Result {
	return rs.Evaluate(flagKey, context)
}

// snapshot is everything Status reports and everything an evaluation reads about the ruleset. It is never
// changed once published: a writer copies it, changes the copy and stores it.
type snapshot struct {
	rs         *evaluator.Ruleset // nil before the first load
	etag       string
	lastSyncAt time.Time
	stream     StreamState
	lastError  SyncErrorCode
}

// Client is a ToggleSlash server SDK client. Make one with New. Every method is safe to call from many
// goroutines at once.
type Client struct {
	cfg    config
	url    string
	sdkKey string

	transport *http.Transport
	http      *http.Client

	state   atomic.Pointer[snapshot]
	writeMu sync.Mutex // held only for the copy of a snapshot, never across I/O or a callback

	// lifetime is cancelled by Close; the two loops and every ruleset and stream request hang off it.
	lifetime context.Context
	cancel   context.CancelFunc

	readyOnce sync.Once
	readyCh   chan struct{}
	readyOK   atomic.Bool

	syncDone  chan struct{}
	flushDone chan struct{}
	// syncCB and flushCB count the callbacks the two goroutines are inside, so a Close called from one
	// (or racing one) does not wait for a goroutine that is waiting for the application.
	syncCB  atomic.Int32
	flushCB atomic.Int32

	closeMu      sync.Mutex
	closeStarted bool
	closeDone    chan struct{}

	// syncHold is the time before which no ruleset or stream request goes out (a 429's Retry-After). Only
	// the sync goroutine reads or writes it.
	syncHold time.Time

	usageMu sync.Mutex
	pending map[usageKey]int64
	closed  bool // under usageMu: nothing is counted once Close has begun

	flushMu sync.Mutex
	// usageHold is the same for the events route. Under flushMu.
	usageHold time.Time
}

// New makes a client and starts its two goroutines (the sync loop, which does the first load at once, and
// the flush loop). It returns at once and never waits for the network; call Ready to wait for the first
// load. It never returns nil and never panics. url is the server's base URL (trailing "/" characters are
// removed) and sdkKey a server key. Call Close when done: an unclosed client holds two goroutines and
// does not keep a program from exiting.
func New(url, sdkKey string, opts ...Option) *Client {
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	c := &Client{
		cfg:       cfg,
		url:       trimURL(url),
		sdkKey:    sdkKey,
		readyCh:   make(chan struct{}),
		syncDone:  make(chan struct{}),
		flushDone: make(chan struct{}),
		closeDone: make(chan struct{}),
		pending:   map[usageKey]int64{},
	}
	c.lifetime, c.cancel = context.WithCancel(context.Background())
	c.transport = newTransport(cfg.tlsConfig)
	c.http = &http.Client{
		Transport:     c.transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	initial := &snapshot{stream: StreamOff}
	if cfg.stream {
		initial.stream = StreamConnecting
	}
	c.state.Store(initial)
	c.startLoops()
	return c
}

// startLoops starts the client's two goroutines. They are the only ones it ever starts.
func (c *Client) startLoops() {
	go c.syncLoop(c.lifetime)
	go c.flushLoop(c.lifetime)
}

// update changes the snapshot. fn gets a copy and changes it; a stream state of closed is final.
func (c *Client) update(fn func(*snapshot)) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	next := *c.state.Load()
	fn(&next)
	if c.state.Load().stream == StreamClosed {
		next.stream = StreamClosed
	}
	c.state.Store(&next)
}

// settleReady fixes the answer of Ready; the first call wins.
func (c *Client) settleReady(ok bool) {
	c.readyOnce.Do(func() {
		c.readyOK.Store(ok)
		close(c.readyCh)
	})
}

// Ready waits for the first load to finish and reports whether it left the client holding a ruleset. It
// is bounded by the request timeout (the first load is one request), does not wait for the server to come
// back, and gives the same answer on every call. After Close it is false if it had not settled. It is safe
// to call from inside the WithOnError callback: the first load has settled before that callback runs.
func (c *Client) Ready() bool { return c.ReadyContext(context.Background()) }

// ReadyContext is Ready, and also stops waiting when ctx is done, answering whether a ruleset is held then.
func (c *Client) ReadyContext(ctx context.Context) bool {
	select {
	case <-c.readyCh:
		return c.readyOK.Load()
	case <-ctx.Done():
		return c.state.Load().rs != nil
	}
}

// Status reads what the client has loaded and how its stream is doing, as one consistent snapshot. It
// makes no request and never fails. It holds no URL, key, error text, context or flag key.
func (c *Client) Status() Status {
	s := c.state.Load()
	return Status{ETag: s.etag, LastSyncAt: s.lastSyncAt, Stream: s.stream, LastError: s.lastError}
}

// Close stops following the server, stops the flush loop, waits for both, sends the final usage counts
// and releases the connections. It always returns nil, is safe to call twice or from many goroutines (each
// call returns when the shutdown is done) and returns within about twice the request timeout even when the
// server hangs. Evaluation still answers from the ruleset held, and counts nothing. Called from inside a
// callback on one of the client's own goroutines, it does not wait for that goroutine.
func (c *Client) Close() error {
	c.closeMu.Lock()
	if c.closeStarted {
		c.closeMu.Unlock()
		<-c.closeDone
		return nil
	}
	c.closeStarted = true
	c.closeMu.Unlock()

	c.usageMu.Lock()
	c.closed = true
	c.usageMu.Unlock()
	c.update(func(s *snapshot) { s.stream = StreamClosed })
	c.cancel()
	c.settleReady(false)
	c.waitFor(c.syncDone, &c.syncCB)
	c.waitFor(c.flushDone, &c.flushCB)
	for _, err := range c.safeFlush(true) {
		c.callOnError(err)
	}
	c.transport.CloseIdleConnections()
	close(c.closeDone)
	return nil
}

// waitFor waits for a goroutine to finish, or to be inside an application callback, which it may be
// waiting on this very call for.
func (c *Client) waitFor(done <-chan struct{}, inCallback *atomic.Int32) {
	if inCallback.Load() > 0 {
		return
	}
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return
		case <-tick.C:
			if inCallback.Load() > 0 {
				return
			}
		}
	}
}

// panicError is a panic the SDK recovered, as an error. Its text is the panic's own value, which is the
// application's (a hook) or the evaluator's, never the SDK's reading of a context.
type panicError struct {
	what  string
	value any
}

func (e *panicError) Error() string {
	return fmt.Sprintf("toggleslash: %s panicked: %v", e.what, e.value)
}

func (e *panicError) Unwrap() error {
	err, _ := e.value.(error)
	return err
}

// callOnError tells the application, if it asked. A panic in the callback is ignored.
func (c *Client) callOnError(err error) {
	f := c.cfg.onError
	if f == nil {
		return
	}
	defer func() { _ = recover() }()
	f(err)
}

// reportFrom calls onError on a client goroutine, marked while it is inside the callback.
func (c *Client) reportFrom(marker *atomic.Int32, errs ...error) {
	marker.Add(1)
	defer marker.Add(-1)
	for _, err := range errs {
		c.callOnError(err)
	}
}

// accept turns the served Value into the accessor's type, or says it is the wrong kind.
type accept[T any] func(Value) (T, bool)

// variation is the one place a result is built, in the order of contract section 4.
func variation[T any](c *Client, flagKey string, ctx Context, fallback T, fallbackValue Value, take accept[T]) Detail[T] {
	snap := c.state.Load()
	detail := Detail[T]{Value: fallback, VariationIndex: NoVariation}
	served := fallbackValue
	counted := false

	if snap.rs == nil {
		detail.Reason = errorReason(ErrNotReady)
	} else {
		res, failure := c.safeEvaluate(snap.rs, flagKey, ctx.v)
		switch {
		case failure != nil:
			detail.Reason = errorReason(ErrEvaluationFailed)
			c.callOnError(failure)
		case res.VariationIndex < 0:
			detail.Reason = fromEvaluator(res.Reason)
		default:
			value, ok := take(res.Value)
			if !ok {
				detail.Reason = errorReason(ErrTypeMismatch)
				break
			}
			detail = Detail[T]{Value: value, VariationIndex: res.VariationIndex, Reason: fromEvaluator(res.Reason)}
			served = res.Value
			counted = true
		}
	}
	if counted {
		c.count(flagKey, detail.VariationIndex)
	}
	if hook := c.cfg.afterEvaluation; hook != nil {
		c.callHook(hook, Event{
			FlagKey:        flagKey,
			Context:        ctx,
			Value:          served,
			VariationIndex: detail.VariationIndex,
			Reason:         detail.Reason,
			ETag:           snap.etag,
			FlagVersion:    flagVersion(snap, flagKey),
		})
	}
	return detail
}

func (c *Client) safeEvaluate(rs *evaluator.Ruleset, flagKey string, ctx Value) (res evaluator.Result, failure error) {
	defer func() {
		if r := recover(); r != nil {
			failure = &panicError{what: "evaluation", value: r}
		}
	}()
	return c.cfg.evaluate(rs, flagKey, ctx), nil
}

func (c *Client) callHook(hook func(Event), ev Event) {
	defer func() {
		if r := recover(); r != nil {
			c.callOnError(&panicError{what: "the afterEvaluation hook", value: r})
		}
	}()
	hook(ev)
}

// flagVersion is the flag's version in the snapshot's ruleset, or nil for none (no such flag, a malformed
// one, or a version that does not fit an int64).
func flagVersion(snap *snapshot, flagKey string) *int64 {
	if snap.rs == nil {
		return nil
	}
	v, ok := snap.rs.FlagVersion(flagKey)
	if !ok || v >= 1<<63 {
		return nil
	}
	n := int64(v)
	return &n
}

// BoolVariation is the flag's boolean value for the context, or fallback.
func (c *Client) BoolVariation(flagKey string, ctx Context, fallback bool) bool {
	return c.BoolVariationDetail(flagKey, ctx, fallback).Value
}

// BoolVariationDetail is BoolVariation with the variation index and the reason.
func (c *Client) BoolVariationDetail(flagKey string, ctx Context, fallback bool) Detail[bool] {
	return variation(c, flagKey, ctx, fallback, Bool(fallback), func(v Value) (bool, bool) { return v.AsBool() })
}

// StringVariation is the flag's string value for the context, or fallback. A lone surrogate in the served
// string is returned as U+FFFD; JSONVariation keeps it.
func (c *Client) StringVariation(flagKey string, ctx Context, fallback string) string {
	return c.StringVariationDetail(flagKey, ctx, fallback).Value
}

// StringVariationDetail is StringVariation with the variation index and the reason.
func (c *Client) StringVariationDetail(flagKey string, ctx Context, fallback string) Detail[string] {
	return variation(c, flagKey, ctx, fallback, String(fallback), func(v Value) (string, bool) {
		s, ok := v.AsString()
		return wtf8.ToUTF8(s), ok
	})
}

// IntVariation is the flag's integer value for the context, or fallback. A served number with a fractional
// part, or outside the int64 range, is a type mismatch (ErrTypeMismatch) and gives fallback; 2.0 is 2.
func (c *Client) IntVariation(flagKey string, ctx Context, fallback int64) int64 {
	return c.IntVariationDetail(flagKey, ctx, fallback).Value
}

// IntVariationDetail is IntVariation with the variation index and the reason.
func (c *Client) IntVariationDetail(flagKey string, ctx Context, fallback int64) Detail[int64] {
	return variation(c, flagKey, ctx, fallback, Number(float64(fallback)), func(v Value) (int64, bool) {
		n, ok := v.AsNumber()
		if !ok || n != math.Trunc(n) || n < -(1<<63) || n >= 1<<63 {
			return 0, false
		}
		return int64(n), true
	})
}

// FloatVariation is the flag's number value for the context, or fallback.
func (c *Client) FloatVariation(flagKey string, ctx Context, fallback float64) float64 {
	return c.FloatVariationDetail(flagKey, ctx, fallback).Value
}

// FloatVariationDetail is FloatVariation with the variation index and the reason.
func (c *Client) FloatVariationDetail(flagKey string, ctx Context, fallback float64) Detail[float64] {
	return variation(c, flagKey, ctx, fallback, Number(fallback), func(v Value) (float64, bool) { return v.AsNumber() })
}

// JSONVariation is the flag's value of any kind for the context, or fallback. The kind the flag serves
// must be the kind of fallback (boolean, number, string, array or object), except that a null fallback
// accepts any value; a null served against a fallback that is not null is a type mismatch. JSONVariation
// with a null fallback is the accessor for "whatever it is".
func (c *Client) JSONVariation(flagKey string, ctx Context, fallback Value) Value {
	return c.JSONVariationDetail(flagKey, ctx, fallback).Value
}

// JSONVariationDetail is JSONVariation with the variation index and the reason.
func (c *Client) JSONVariationDetail(flagKey string, ctx Context, fallback Value) Detail[Value] {
	return variation(c, flagKey, ctx, fallback, fallback, func(v Value) (Value, bool) {
		return v, fallback.IsNull() || v.Kind() == fallback.Kind()
	})
}
