// Package toggleslash is the ToggleSlash server SDK for Go: it keeps a ruleset current from the server (the stream,
// with a poll when the stream is down), evaluates flags locally against it, and reports how often each variation
// was served. It implements docs/sdk-contract.md.
//
// Nothing here panics or returns an error to a caller. A flag that cannot be answered gives the fallback, with a
// reason saying why, and every failure the SDK absorbs goes to the function WithOnError sets.
package toggleslash

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/eval"
	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

const (
	defaultInterval = 30 * time.Second
	defaultTimeout  = 10 * time.Second
)

// Option configures New.
type Option func(*config)

type config struct {
	stream          *bool
	pollInterval    time.Duration
	flushInterval   time.Duration
	requestTimeout  time.Duration
	afterEvaluation func(EvaluationEvent)
	onError         func(error)

	// Seams for the tests of this package.
	clock     clock
	jitter    func() float64
	rootCAs   *x509.CertPool
	watchdog  time.Duration
	evaluator func(*eval.Ruleset, string, jsonvalue.Value) eval.Result
}

// WithStream sets whether to follow the server's stream (default true). False polls only.
func WithStream(on bool) Option { return func(c *config) { c.stream = &on } }

// WithPollInterval sets how often to poll when the stream is down or off, and the longest reconnect wait
// (default 30 s). A value of zero or less is the default.
func WithPollInterval(d time.Duration) Option { return func(c *config) { c.pollInterval = d } }

// WithFlushInterval sets how often usage counts are sent (default 30 s). A value of zero or less is the default.
func WithFlushInterval(d time.Duration) Option { return func(c *config) { c.flushInterval = d } }

// WithRequestTimeout sets the longest any one request may take, and so the longest Ready and Close wait
// (default 10 s). A value of zero or less is the default.
func WithRequestTimeout(d time.Duration) Option { return func(c *config) { c.requestTimeout = d } }

// WithAfterEvaluation sets a function called synchronously, on the caller's goroutine, after every evaluation
// (a fallback included) with what the caller is about to get. A panic in it goes to onError and never reaches the
// caller, and the caller gets the same answer with or without it.
func WithAfterEvaluation(f func(EvaluationEvent)) Option {
	return func(c *config) { c.afterEvaluation = f }
}

// WithOnError sets a function told about every failure the SDK absorbs. It is never needed for correctness; a panic
// in it is ignored. It may be called from any of the SDK's goroutines, and it may call Close, Status, Ready and
// the Variation methods: no lock of the SDK's is held while it runs.
func WithOnError(f func(error)) Option { return func(c *config) { c.onError = f } }

func withClock(k clock) Option            { return func(c *config) { c.clock = k } }
func withJitter(f func() float64) Option  { return func(c *config) { c.jitter = f } }
func withRootCAs(p *x509.CertPool) Option { return func(c *config) { c.rootCAs = p } }
func withWatchdog(d time.Duration) Option { return func(c *config) { c.watchdog = d } }
func withEvaluator(f func(*eval.Ruleset, string, jsonvalue.Value) eval.Result) Option {
	return func(c *config) { c.evaluator = f }
}

// Client keeps one ruleset current and evaluates flags against it. It is safe for use by any number of goroutines.
// Create one with New and call Close when done.
type Client struct {
	cfg    config
	base   string
	sdkKey string
	clk    clock
	http   *http.Client
	// streaming is whether to follow the stream (WithStream, default true).
	streaming bool

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// snap is the ruleset and its ETag, swapped together. Evaluation loads it once and uses that for the whole call.
	snap atomic.Pointer[snapshot]

	// mu guards the fields below and the swap of snap, so Status is one snapshot. It is held only for field reads and
	// writes, never across I/O or a callback.
	mu         sync.Mutex
	lastSyncAt int64
	lastError  string
	stream     string

	syncHold  hold
	usageHold hold

	usageMu sync.Mutex
	pending map[usageKey]int64
	flushMu sync.Mutex

	sdkGoroutines goroutineSet

	readyOnce sync.Once
	readyCh   chan struct{}
	readyVal  atomic.Bool

	closing  atomic.Bool
	closedCh chan struct{}
}

// New creates a client and starts following the server. It returns at once and never fails: a URL or key that does
// not work gives a client whose first load fails, so Ready answers false and flags serve their fallbacks. url is the
// server's base URL, for example "https://flags.example.com" (trailing slashes are dropped); sdkKey is a server SDK
// key. A client key cannot read the ruleset.
func New(url, sdkKey string, opts ...Option) *Client {
	cfg := config{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.stream == nil {
		on := true
		cfg.stream = &on
	}
	if cfg.pollInterval <= 0 {
		cfg.pollInterval = defaultInterval
	}
	if cfg.flushInterval <= 0 {
		cfg.flushInterval = defaultInterval
	}
	if cfg.requestTimeout <= 0 {
		cfg.requestTimeout = defaultTimeout
	}
	if cfg.watchdog <= 0 {
		cfg.watchdog = defaultWatchdog
	}
	if cfg.clock == nil {
		cfg.clock = realClock{}
	}
	if cfg.jitter == nil {
		cfg.jitter = rand.Float64
	}
	if cfg.evaluator == nil {
		cfg.evaluator = func(r *eval.Ruleset, key string, ctx jsonvalue.Value) eval.Result { return r.Evaluate(key, ctx) }
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		cfg:        cfg,
		base:       strings.TrimRight(url, "/"),
		sdkKey:     sdkKey,
		clk:        cfg.clock,
		streaming:  *cfg.stream,
		http:       newHTTPClient(cfg),
		ctx:        ctx,
		cancel:     cancel,
		lastSyncAt: -1,
		pending:    make(map[usageKey]int64),
		readyCh:    make(chan struct{}),
		closedCh:   make(chan struct{}),
	}
	if c.streaming {
		c.stream = StreamConnecting
	} else {
		c.stream = StreamOff
	}
	c.wg.Add(2)
	go c.run()
	go c.flushLoop()
	return c
}

// newHTTPClient builds the client the SDK's requests use. Redirects are not followed: a 3xx is an error, and the
// Authorization header is never sent to a redirect target.
func newHTTPClient(cfg config) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.rootCAs != nil {
		transport.TLSClientConfig = &tls.Config{RootCAs: cfg.rootCAs, MinVersion: tls.VersionTLS12}
	}
	return &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// report hands an error to onError. No lock is held by the caller, and a panic in the function is ignored.
func (c *Client) report(err error) {
	f := c.cfg.onError
	if f == nil {
		return
	}
	defer func() { _ = recover() }()
	f(err)
}

// settleReady fixes the answer of Ready. Only the first call counts.
func (c *Client) settleReady(ok bool) {
	c.readyOnce.Do(func() {
		c.readyVal.Store(ok)
		close(c.readyCh)
	})
}

// Ready waits until the first ruleset load has finished and reports whether it worked. It always answers the same
// afterwards. False means flags serve their fallbacks until the SDK reaches the server. It is bounded by the
// request timeout, and Close settles it false if it is still waiting. Called from a callback on one of the SDK's own
// goroutines (the function WithOnError sets) it does not wait: it answers false if the load has not settled.
func (c *Client) Ready() bool {
	select {
	case <-c.readyCh:
		return c.readyVal.Load()
	default:
	}
	if c.sdkGoroutines.onSDKGoroutine() {
		return false
	}
	<-c.readyCh
	return c.readyVal.Load()
}

// Status reports what the client has loaded and how its stream is doing. It reads memory, makes no network call
// and never blocks on one.
func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := Status{LastSyncAt: c.lastSyncAt, Stream: c.stream, LastError: c.lastError}
	if held := c.snap.Load(); held != nil {
		s.ETag = held.etag
	}
	if c.closing.Load() {
		s.Stream = StreamClosed
	}
	return s
}

// Close stops following the server and sends the pending usage counts. It never fails, is safe to call twice or
// from many goroutines (every call returns after the same shutdown), and returns within about twice the request
// timeout. Afterwards evaluation still answers, nothing is counted, and Status reports the stream closed. Called
// from a callback on one of the SDK's own goroutines it starts the shutdown and returns without waiting for it.
func (c *Client) Close() {
	if c.closing.CompareAndSwap(false, true) {
		c.settleReady(false)
		c.cancel()
		go c.shutdown()
	}
	if c.sdkGoroutines.onSDKGoroutine() {
		return
	}
	<-c.closedCh
}

// shutdown waits for the loops and any flush running, then sends the last counts.
func (c *Client) shutdown() {
	defer close(c.closedCh)
	defer c.sdkGoroutines.add()()
	c.wg.Wait()
	c.finish()
	if closer, ok := c.http.Transport.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}
