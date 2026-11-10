package toggleslash

import (
	"context"
	"math"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/eval"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

// snapshot is a ruleset and the ETag it came with. It is immutable and swapped as one pointer, so a reader
// sees the ruleset and the tag from the same moment.
type snapshot struct {
	ruleset *eval.Ruleset
	etag    *string
}

// worker is one of the client's two goroutines. Close waits for it, except while it is inside a callback:
// then it is the callback that may be calling Close, and waiting would wait on itself.
type worker struct {
	done    chan struct{}
	inCall  atomic.Int32
	entered chan struct{}
}

func newWorker() *worker { return &worker{done: make(chan struct{}), entered: make(chan struct{}, 1)} }

func (w *worker) enter() {
	w.inCall.Add(1)
	select {
	case w.entered <- struct{}{}:
	default:
	}
}

func (w *worker) leave() { w.inCall.Add(-1) }

// wait returns when the worker has stopped, or is inside a callback (and so is not doing anything more).
func (w *worker) wait() {
	for {
		if w.inCall.Load() > 0 {
			return
		}
		select {
		case <-w.done:
			return
		case <-w.entered:
		}
	}
}

// Client is a ToggleSlash server SDK client. Create it with New. All methods are safe to call from any
// number of goroutines.
type Client struct {
	cfg config

	snap atomic.Pointer[snapshot]

	// statusMu guards the fields below it, and the store of a new snapshot, so Status sees one moment.
	statusMu   sync.Mutex
	lastSyncAt time.Time
	lastError  SyncErrorCode
	stream     StreamState
	holdUntil  time.Time // a 429 on the ruleset or stream asked for nothing to reach them before this

	// usageMu guards the pending counts and whether they are still being recorded.
	usageMu   sync.Mutex
	pending   map[usageKey]int64
	recording bool

	// flushMu makes flushes serial and guards the events route's own Retry-After hold.
	flushMu         sync.Mutex
	eventsHoldUntil time.Time

	closing atomic.Bool
	done    chan struct{}

	syncCtx     context.Context
	cancelSync  context.CancelFunc
	flushCtx    context.Context
	cancelFlush context.CancelFunc
	syncWorker  *worker
	flushWorker *worker

	settled     chan struct{}
	settleOnce  sync.Once
	readyResult atomic.Bool
}

// New creates a client and starts it. It returns at once without waiting for the network, never panics and
// never returns an error: a URL that cannot be used shows up as a "network" failure in Status and the
// OnError callback, like any other unreachable server. Call Ready to wait for the first load.
func New(url, sdkKey string, opts ...Option) *Client {
	return start(newConfig(url, sdkKey, opts...))
}

// newConfig is the defaults with the options applied.
func newConfig(url, sdkKey string, opts ...Option) config {
	cfg := config{
		url:            strings.TrimRight(url, "/"),
		sdkKey:         sdkKey,
		stream:         true,
		pollInterval:   defaultInterval,
		flushInterval:  defaultInterval,
		requestTimeout: defaultTimeout,
		idle:           streamIdle,
		now:            time.Now,
		jitter:         rand.Float64,
		evaluate:       (*eval.Ruleset).Evaluate,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return cfg
}

// start builds a client from a finished config. Tests use it to set the seams.
func start(cfg config) *Client {
	if cfg.httpClient == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		cfg.httpClient = &http.Client{Transport: transport}
	}
	// A redirect would carry the Authorization header to wherever it points; a 3xx is just a failed attempt.
	hc := *cfg.httpClient
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	cfg.httpClient = &hc

	c := &Client{
		cfg:         cfg,
		pending:     map[usageKey]int64{},
		recording:   true,
		done:        make(chan struct{}),
		settled:     make(chan struct{}),
		syncWorker:  newWorker(),
		flushWorker: newWorker(),
		stream:      StreamOff,
	}
	if cfg.stream {
		c.stream = StreamConnecting
	}
	c.syncCtx, c.cancelSync = context.WithCancel(context.Background())
	c.flushCtx, c.cancelFlush = context.WithCancel(context.Background())
	go c.runSync()
	go c.runFlush()
	return c
}

// settle fixes the answer of Ready; only the first call counts.
func (c *Client) settle(ok bool) {
	c.settleOnce.Do(func() {
		c.readyResult.Store(ok)
		close(c.settled)
	})
}

// Ready blocks until the first ruleset load has finished, bounded by the request timeout, and reports
// whether a ruleset is held. False means flags serve their fallbacks until the client reaches the server.
// The answer is fixed once the first attempt has settled; after Close it is false if nothing had loaded.
func (c *Client) Ready() bool {
	<-c.settled
	return c.readyResult.Load()
}

// Status returns what the client has loaded and how its stream is doing, from memory, in one snapshot.
func (c *Client) Status() Status {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	s := Status{LastSyncAt: c.lastSyncAt, Stream: c.stream, LastError: c.lastError}
	if snap := c.snap.Load(); snap != nil && snap.etag != nil {
		etag := *snap.etag
		s.ETag = &etag
	}
	return s
}

// report tells the OnError callback, on the calling goroutine, with no lock held. w is the worker doing so,
// or nil for any other goroutine. A panic in the callback is ignored.
func (c *Client) report(err *Error, w *worker) {
	if c.cfg.onError == nil {
		return
	}
	if w != nil {
		w.enter()
		defer w.leave()
	}
	defer func() { _ = recover() }()
	c.cfg.onError(err)
}

// resolution is what an evaluation produced before a typed method converts the value.
type resolution struct {
	result   eval.Result
	usable   bool // the evaluator served a variation
	snap     *snapshot
	code     ErrorCode // set when the result is an SDK error (NOT_READY, EVALUATION_FAILED)
	panicked any
}

func (c *Client) resolve(flagKey string, ctx Context) (r resolution) {
	r.snap = c.snap.Load()
	if r.snap == nil {
		r.code = ErrorNotReady
		return r
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			r = resolution{snap: r.snap, code: ErrorEvaluationFailed, panicked: recovered}
		}
	}()
	r.result = c.cfg.evaluate(r.snap.ruleset, flagKey, ctx.v)
	r.usable = r.result.VariationIndex != NoVariation
	return r
}

// accessor says how a typed method reads and writes a served value.
type accessor[T any] struct {
	// read converts a served value; it is false when the value is not of the accessor's kind.
	read func(fallback T, served Value) (T, bool)
	// write is the value as JSON, for the hook event.
	write func(T) Value
}

// variation is the one place the result order of the contract (section 4) is written.
func variation[T any](c *Client, a accessor[T], flagKey string, ctx Context, fallback T) Detail[T] {
	res := c.resolve(flagKey, ctx)
	detail := Detail[T]{Value: fallback, VariationIndex: NoVariation}
	switch {
	case res.code != "":
		detail.Reason = errorReason(res.code)
		if res.panicked != nil {
			c.report(&Error{Op: "evaluate", Code: string(ErrorEvaluationFailed), Message: "evaluation failed", Err: panicError(res.panicked)}, nil)
		}
	case !res.usable:
		detail.Reason = reasonOf(res.result.Reason)
	default:
		if value, ok := a.read(fallback, res.result.Value); ok {
			detail = Detail[T]{Value: value, VariationIndex: res.result.VariationIndex, Reason: reasonOf(res.result.Reason)}
			c.record(flagKey, detail.VariationIndex)
		} else {
			detail.Reason = errorReason(ErrorTypeMismatch)
		}
	}
	if c.cfg.afterEvaluation != nil {
		c.runHook(res, flagKey, ctx, a.write(detail.Value), detail.VariationIndex, detail.Reason)
	}
	return detail
}

func (c *Client) runHook(res resolution, flagKey string, ctx Context, value Value, index int, reason Reason) {
	event := EvaluationEvent{FlagKey: flagKey, Context: ctx, Value: value, VariationIndex: index, Reason: reason}
	if res.snap != nil {
		if res.snap.etag != nil {
			etag := *res.snap.etag
			event.ETag = &etag
		}
		if version, ok := res.snap.ruleset.FlagVersion(flagKey); ok {
			event.FlagVersion = &version
		}
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			c.report(&Error{Op: "hook", Code: "hook_panic", Message: "afterEvaluation panicked", Err: panicError(recovered)}, nil)
		}
	}()
	c.cfg.afterEvaluation(event)
}

type recoveredPanic struct{ value any }

func (p recoveredPanic) Error() string { return "panic" }

func panicError(v any) error {
	if err, ok := v.(error); ok {
		return err
	}
	return recoveredPanic{v}
}

var boolAccessor = accessor[bool]{
	read:  func(_ bool, v Value) (bool, bool) { return v.Bool() },
	write: func(b bool) Value { return jsonvalue.Bool(b) },
}

var stringAccessor = accessor[string]{
	read:  func(_ string, v Value) (string, bool) { return v.Text() },
	write: func(s string) Value { return jsonvalue.String(s) },
}

var float64Accessor = accessor[float64]{
	read:  func(_ float64, v Value) (float64, bool) { return v.Number() },
	write: func(n float64) Value { return jsonvalue.Number(n) },
}

var int64Accessor = accessor[int64]{
	read: func(_ int64, v Value) (int64, bool) {
		n, ok := v.Number()
		// 2^63 is the first float64 above the int64 range; NaN and the infinities fail these tests too.
		if !ok || n != math.Trunc(n) || n < -(1<<63) || n >= 1<<63 {
			return 0, false
		}
		return int64(n), true
	},
	write: func(n int64) Value { return jsonvalue.Number(float64(n)) },
}

// jsonAccessor accepts a value of the fallback's kind; a null fallback accepts any served value.
var jsonAccessor = accessor[Value]{
	read: func(fallback Value, served Value) (Value, bool) {
		if fallback.IsNull() {
			return served, true
		}
		return served, !served.IsNull() && served.Kind() == fallback.Kind()
	},
	write: func(v Value) Value { return v },
}

// BoolVariation is the flag's boolean for a context, or fallback.
func (c *Client) BoolVariation(flagKey string, ctx Context, fallback bool) bool {
	return c.BoolVariationDetail(flagKey, ctx, fallback).Value
}

// BoolVariationDetail is BoolVariation with the variation index and the reason.
func (c *Client) BoolVariationDetail(flagKey string, ctx Context, fallback bool) Detail[bool] {
	return variation(c, boolAccessor, flagKey, ctx, fallback)
}

// StringVariation is the flag's string for a context, or fallback.
func (c *Client) StringVariation(flagKey string, ctx Context, fallback string) string {
	return c.StringVariationDetail(flagKey, ctx, fallback).Value
}

// StringVariationDetail is StringVariation with the variation index and the reason.
func (c *Client) StringVariationDetail(flagKey string, ctx Context, fallback string) Detail[string] {
	return variation(c, stringAccessor, flagKey, ctx, fallback)
}

// Float64Variation is the flag's number for a context, or fallback.
func (c *Client) Float64Variation(flagKey string, ctx Context, fallback float64) float64 {
	return c.Float64VariationDetail(flagKey, ctx, fallback).Value
}

// Float64VariationDetail is Float64Variation with the variation index and the reason.
func (c *Client) Float64VariationDetail(flagKey string, ctx Context, fallback float64) Detail[float64] {
	return variation(c, float64Accessor, flagKey, ctx, fallback)
}

// IntVariation is the flag's integer for a context, or fallback. A served number with a fractional part or
// outside the signed 64-bit range is ERROR TYPE_MISMATCH; 2.0 is the integer 2.
func (c *Client) IntVariation(flagKey string, ctx Context, fallback int64) int64 {
	return c.IntVariationDetail(flagKey, ctx, fallback).Value
}

// IntVariationDetail is IntVariation with the variation index and the reason.
func (c *Client) IntVariationDetail(flagKey string, ctx Context, fallback int64) Detail[int64] {
	return variation(c, int64Accessor, flagKey, ctx, fallback)
}

// JSONVariation is the flag's JSON value for a context, or fallback. The served value must be of the
// fallback's JSON kind; a Null fallback accepts any served value.
func (c *Client) JSONVariation(flagKey string, ctx Context, fallback Value) Value {
	return c.JSONVariationDetail(flagKey, ctx, fallback).Value
}

// JSONVariationDetail is JSONVariation with the variation index and the reason.
func (c *Client) JSONVariationDetail(flagKey string, ctx Context, fallback Value) Detail[Value] {
	return variation(c, jsonAccessor, flagKey, ctx, fallback)
}

// Close stops following the server, sends the pending usage counts once and releases everything. It is safe
// to call twice, from any goroutine and from inside a callback; every call returns when the same shutdown is
// done. Evaluation still works afterwards, on the last ruleset, and counts nothing.
func (c *Client) Close() {
	if !c.closing.CompareAndSwap(false, true) {
		<-c.done
		return
	}
	c.statusMu.Lock()
	c.stream = StreamClosed
	c.statusMu.Unlock()
	c.usageMu.Lock()
	c.recording = false
	c.usageMu.Unlock()
	c.cancelSync()
	c.cancelFlush()
	c.syncWorker.wait()
	c.flushWorker.wait()
	c.settle(false)
	errs := c.finalFlush()
	close(c.done)
	for _, err := range errs {
		c.report(err, nil)
	}
}
