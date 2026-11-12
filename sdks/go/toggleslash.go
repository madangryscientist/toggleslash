// Package toggleslash is the Go server SDK for ToggleSlash, a feature flag service. A Client keeps one
// environment's ruleset current (a conditional fetch, a stream, a poll as a fallback) and evaluates flags against
// it in this process: Bool, String, Float64, Int and JSON variations, each also in a …Detail form that says which
// variation was served and why. Nothing about the end user leaves the process; usage counts, which hold no
// context, are batched and sent.
//
// The SDK follows docs/sdk-contract.md. A Client never panics at its caller and never blocks an evaluation on the
// network. Close it when done.
//
//	c := toggleslash.New("https://flags.example.com", sdkKey)
//	defer c.Close()
//	c.Ready()
//	on := c.BoolVariation("new-checkout", toggleslash.NewContext("user-42", nil), false)
package toggleslash

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

const (
	defaultPollInterval   = 30 * time.Second
	defaultFlushInterval  = 30 * time.Second
	defaultRequestTimeout = 10 * time.Second
	// streamIdle is how long a stream may send no byte at all before it is dropped. The server sends a keep-alive
	// every 25 s.
	streamIdle = 60 * time.Second
	// retryAfterCap is the longest a 429 can make the SDK wait.
	retryAfterCap = 300 * time.Second
	backoffBase   = time.Second
	// maxBatch is the most entries in one usage request. The server takes 1000 and 128 KiB.
	maxBatch = 500
)

// Option configures New.
type Option func(*config)

type config struct {
	stream         bool
	pollInterval   time.Duration
	flushInterval  time.Duration
	requestTimeout time.Duration
	afterEval      func(Event)
	onError        func(error)
	httpClient     *http.Client

	// Seams for the tests of this package.
	clock    clock
	random   func() float64
	watchdog time.Duration
	evaluate func(*evaluator.Ruleset, string, jsonvalue.Value) evaluator.Result
}

// WithStream turns the stream on or off. It is on by default; off, the SDK only polls.
func WithStream(on bool) Option { return func(c *config) { c.stream = on } }

// WithPollInterval sets how often the SDK polls when the stream is off or down, and the longest wait between
// stream attempts. The default is 30 s. A value that is not positive is the default.
func WithPollInterval(d time.Duration) Option { return func(c *config) { c.pollInterval = d } }

// WithFlushInterval sets how often usage counts are sent. The default is 30 s. A value that is not positive is
// the default.
func WithFlushInterval(d time.Duration) Option { return func(c *config) { c.flushInterval = d } }

// WithRequestTimeout sets the longest any one request may take, the body read included, and so the longest Ready
// waits and about half the longest Close waits. The default is 10 s. A value that is not positive is the default.
func WithRequestTimeout(d time.Duration) Option { return func(c *config) { c.requestTimeout = d } }

// WithAfterEvaluation sets a function called synchronously, on the caller's goroutine, after every evaluation, a
// fallback included, with what the caller is about to get. A panic in it goes to the OnError function and never
// reaches the caller, who gets the same answer either way.
func WithAfterEvaluation(fn func(Event)) Option { return func(c *config) { c.afterEval = fn } }

// WithOnError sets a function told about every failure the SDK absorbs: a failed ruleset, stream or usage
// request, a panic in the evaluation or the hook, and usage counts lost at Close. It is never needed for
// correctness and runs on the goroutine that hit the failure, with no SDK lock held; a panic in it is ignored.
func WithOnError(fn func(error)) Option { return func(c *config) { c.onError = fn } }

// WithHTTPClient replaces the HTTP client. The default clones the default transport, turns off compression (the
// SDK never asks for it, so a stream is never decoded) and does not follow redirects, so the key is never sent
// anywhere but the URL given.
func WithHTTPClient(hc *http.Client) Option { return func(c *config) { c.httpClient = hc } }

func withClock(clk clock) Option { return func(c *config) { c.clock = clk } }

func withRandom(fn func() float64) Option { return func(c *config) { c.random = fn } }

func withWatchdog(d time.Duration) Option { return func(c *config) { c.watchdog = d } }

func withEvaluator(fn func(*evaluator.Ruleset, string, jsonvalue.Value) evaluator.Result) Option {
	return func(c *config) { c.evaluate = fn }
}

// snapshot is a ruleset and the ETag that names it. It is immutable, and a sync swaps the pointer to a new one,
// so an evaluation that loads it once sees a ruleset and an ETag that belong together.
type snapshot struct {
	rules *evaluator.Ruleset
	etag  string
}

// Client is a connection to one ToggleSlash environment. It is safe for concurrent use. Make one with New.
type Client struct {
	url    string
	key    string
	cfg    config
	hc     *http.Client
	clk    clock
	random func() float64

	ctx    context.Context
	cancel context.CancelCauseFunc

	snap atomic.Pointer[snapshot]

	// smu guards the status fields and the ready answer, and makes a sync's swap of the snapshot and its
	// lastSyncAt one step.
	smu        sync.Mutex
	lastSyncAt time.Time
	lastError  LastError
	stream     StreamState
	closed     bool
	settled    bool
	readyVal   bool
	readyCh    chan struct{}

	holds holds
	use   usage
	// flushMu serialises flushes. It is held across usage requests and never across a callback.
	flushMu sync.Mutex

	loops     tracker
	closeOnce sync.Once
	done      chan struct{}
}

var (
	errTimeout = errors.New("timeout")
	errClosed  = errors.New("closed")
)

// New makes a Client for the server at url, authenticating with sdkKey, a server SDK key. It never fails and
// never waits for the network: the first ruleset request starts at once on the client's own goroutine, and Ready
// reports how it went. A trailing "/" on url is removed.
func New(url, sdkKey string, opts ...Option) *Client {
	cfg := config{
		stream:         true,
		pollInterval:   defaultPollInterval,
		flushInterval:  defaultFlushInterval,
		requestTimeout: defaultRequestTimeout,
		watchdog:       streamIdle,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.pollInterval <= 0 {
		cfg.pollInterval = defaultPollInterval
	}
	if cfg.flushInterval <= 0 {
		cfg.flushInterval = defaultFlushInterval
	}
	if cfg.requestTimeout <= 0 {
		cfg.requestTimeout = defaultRequestTimeout
	}
	if cfg.watchdog <= 0 {
		cfg.watchdog = streamIdle
	}
	if cfg.clock == nil {
		cfg.clock = realClock{}
	}
	if cfg.random == nil {
		cfg.random = rand.Float64
	}
	if cfg.evaluate == nil {
		cfg.evaluate = func(rs *evaluator.Ruleset, key string, ctx jsonvalue.Value) evaluator.Result {
			return rs.Evaluate(key, ctx)
		}
	}
	hc := cfg.httpClient
	if hc == nil {
		hc = defaultHTTPClient()
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	c := &Client{
		url:     strings.TrimRight(url, "/"),
		key:     sdkKey,
		cfg:     cfg,
		hc:      hc,
		clk:     cfg.clock,
		random:  cfg.random,
		ctx:     ctx,
		cancel:  cancel,
		stream:  StreamConnecting,
		readyCh: make(chan struct{}),
		done:    make(chan struct{}),
	}
	c.loops.init()
	if !cfg.stream {
		c.stream = StreamOff
	}
	c.loops.add(2)
	go c.syncLoop()
	go c.flushLoop()
	return c
}

func defaultHTTPClient() *http.Client {
	var transport http.RoundTripper
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		clone := base.Clone()
		clone.DisableCompression = true
		transport = clone
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// Ready blocks until the first ruleset load has finished, bounded by the request timeout, and reports whether a
// ruleset is now held. A false means flags serve their fallbacks until the SDK reaches the server. The answer is
// fixed once given. After Close it is false if the first load had not finished.
func (c *Client) Ready() bool {
	<-c.readyCh
	return c.readyVal
}

// settleLocked fixes the ready answer. The caller holds smu.
func (c *Client) settleLocked(v bool) {
	if c.settled {
		return
	}
	c.settled = true
	c.readyVal = v
	close(c.readyCh)
}

// Status is a snapshot of what the SDK has loaded and how its stream is doing. It reads memory and makes no
// request.
func (c *Client) Status() Status {
	c.smu.Lock()
	defer c.smu.Unlock()
	st := Status{LastSyncAt: c.lastSyncAt, Stream: c.stream, LastError: c.lastError}
	if snap := c.snap.Load(); snap != nil {
		st.ETag = snap.etag
	}
	if c.closed {
		st.Stream = StreamClosed
	}
	return st
}

// Close stops following the server, sends the pending usage counts and returns. It is safe to call twice and
// from several goroutines at once: every call returns when the one shutdown is done. It returns within about
// twice the request timeout. After Close, evaluation still answers from the ruleset held but counts nothing, and
// no ruleset or stream request is made. A Close called from the SDK's own callback does not wait for the goroutine
// that is running the callback.
func (c *Client) Close() {
	first := false
	c.closeOnce.Do(func() { first = true })
	if !first {
		<-c.done
		return
	}
	c.smu.Lock()
	c.closed = true
	c.settleLocked(false)
	c.smu.Unlock()
	c.use.close()
	c.cancel(errClosed)
	c.loops.wait()
	errs := c.flush(true)
	close(c.done)
	// After done is closed, so that a Close called from the OnError function returns at once.
	for _, err := range errs {
		c.report(err)
	}
}

// report passes an error to the OnError function. A panic in it is ignored. It is called with no SDK lock held.
func (c *Client) report(err error) {
	fn := c.cfg.onError
	if fn == nil || err == nil {
		return
	}
	defer func() { _ = recover() }()
	fn(err)
}

// reportFromLoop is report for a goroutine the SDK started: while the callback runs, Close does not wait for that
// goroutine, so a Close or Ready called from the callback cannot wait on itself.
func (c *Client) reportFromLoop(err error) {
	if c.cfg.onError == nil || err == nil {
		return
	}
	c.loops.mark()
	defer c.loops.unmark()
	c.report(err)
}

// tracker counts the goroutines the SDK started and which of them are inside an application callback.
type tracker struct {
	mu      sync.Mutex
	running int
	marked  int
	changed chan struct{}
}

func (t *tracker) init() { t.changed = make(chan struct{}) }

// bump records a change and wakes every waiter. The caller holds mu.
func (t *tracker) bump() {
	close(t.changed)
	t.changed = make(chan struct{})
}

func (t *tracker) add(n int) {
	t.mu.Lock()
	t.running += n
	t.bump()
	t.mu.Unlock()
}

func (t *tracker) done() {
	t.mu.Lock()
	t.running--
	t.bump()
	t.mu.Unlock()
}

func (t *tracker) mark() {
	t.mu.Lock()
	t.marked++
	t.bump()
	t.mu.Unlock()
}

func (t *tracker) unmark() {
	t.mu.Lock()
	t.marked--
	t.bump()
	t.mu.Unlock()
}

// wait returns when every goroutine the SDK started has finished or is inside a callback.
func (t *tracker) wait() {
	for {
		t.mu.Lock()
		if t.running-t.marked <= 0 {
			t.mu.Unlock()
			return
		}
		ch := t.changed
		t.mu.Unlock()
		<-ch
	}
}

// holds are the two Retry-After deadlines: one shared by the ruleset and stream routes, one for usage reports.
type holds struct {
	mu    sync.Mutex
	sync  time.Time
	usage time.Time
}

// raise lengthens a hold to until; a shorter one never shortens it.
func raise(deadline *time.Time, until time.Time) {
	if until.After(*deadline) {
		*deadline = until
	}
}
