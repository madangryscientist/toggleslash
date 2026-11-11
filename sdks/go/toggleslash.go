// Package toggleslash is the Go server SDK for ToggleSlash. It loads a ruleset from the server, keeps it
// current through the stream (or by polling), evaluates flags locally and sends usage counts. It follows
// docs/sdk-contract.md; README.md has the table that maps the contract's names onto this package.
//
// A Client never panics at its caller, and Variation, Status and Ready's settled answers never do I/O or
// wait on the network.
package toggleslash

import (
	"context"
	"crypto/tls"
	"errors"
	"math/rand/v2"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

const (
	defaultPollInterval   = 30 * time.Second
	defaultFlushInterval  = 30 * time.Second
	defaultRequestTimeout = 10 * time.Second
	streamIdleTimeout     = 60 * time.Second
	maxRetryAfter         = 300 * time.Second
)

// Option configures New.
type Option func(*config)

type config struct {
	stream          bool
	pollInterval    time.Duration
	flushInterval   time.Duration
	requestTimeout  time.Duration
	afterEvaluation func(EvaluationEvent)
	onError         func(error)

	// Test seams. They are options inside the package, never a global or an environment variable.
	clock    clock
	jitter   func() float64
	idle     time.Duration
	evaluate func(*evaluator.Ruleset, string, jsonvalue.Value) evaluator.Result
	tls      *tls.Config
}

// WithStream turns the stream (GET /sdk/v1/stream) on or off. The default is on; off means poll only.
func WithStream(on bool) Option { return func(c *config) { c.stream = on } }

// WithPollInterval sets how often to poll when the stream is down or off, and the longest reconnect wait.
// The default is 30 s. A zero or negative duration keeps the default.
func WithPollInterval(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.pollInterval = d
		}
	}
}

// WithFlushInterval sets how often usage counts are sent. The default is 30 s. A zero or negative
// duration keeps the default.
func WithFlushInterval(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.flushInterval = d
		}
	}
}

// WithRequestTimeout sets the longest any one request may take, body included. The default is 10 s. A zero
// or negative duration keeps the default.
func WithRequestTimeout(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.requestTimeout = d
		}
	}
}

// WithAfterEvaluation sets the hook called after every evaluation, a fallback included, on the caller's
// goroutine before the evaluation returns. A panic in it goes to the onError function and changes nothing
// for the caller.
func WithAfterEvaluation(hook func(EvaluationEvent)) Option {
	return func(c *config) { c.afterEvaluation = hook }
}

// WithOnError sets the function told about every failure the SDK absorbs, as a *Error. It runs inline on
// the goroutine that met the failure and never under an SDK lock, so it may call back into the Client. A
// panic in it is ignored.
func WithOnError(f func(error)) Option { return func(c *config) { c.onError = f } }

func withClock(cl clock) Option             { return func(c *config) { c.clock = cl } }
func withJitter(f func() float64) Option    { return func(c *config) { c.jitter = f } }
func withTLS(t *tls.Config) Option          { return func(c *config) { c.tls = t } }
func withStreamIdle(d time.Duration) Option { return func(c *config) { c.idle = d } }
func withEvaluator(f func(*evaluator.Ruleset, string, jsonvalue.Value) evaluator.Result) Option {
	return func(c *config) { c.evaluate = f }
}

// snapshot is the ruleset and the ETag that names it. It is immutable and swapped as a whole, so a reader
// never sees a ruleset with another ruleset's ETag.
type snapshot struct {
	ruleset *evaluator.Ruleset
	etag    string
}

type usageKey struct {
	flag  string
	index int
	day   string
}

// Client is a ToggleSlash server SDK client. It is safe for use by many goroutines. Make one with New and
// call Close when done with it; a client that is never closed does not keep the process alive.
type Client struct {
	cfg     config
	baseURL string
	sdkKey  string
	http    *http.Client
	tr      *http.Transport

	root       context.Context
	cancelRoot context.CancelCauseFunc

	snap atomic.Pointer[snapshot]

	// mu guards the status fields, the closing flag and the two Retry-After holds. It is held for memory
	// operations only: never across a request, a hook or onError.
	mu         sync.Mutex
	lastSyncAt time.Time
	lastError  string
	stream     StreamState
	closing    bool
	holdSync   time.Time // ruleset and stream
	holdUsage  time.Time // events

	firstDone   chan struct{}
	firstOnce   sync.Once
	firstResult bool

	// usageMu guards the usage map and noCount (nothing is counted once Close has begun).
	usageMu sync.Mutex
	usage   map[usageKey]int64
	noCount bool

	// flushMu serialises flushes.
	flushMu sync.Mutex

	loopDone  chan struct{}
	flushDone chan struct{}
	loopGID   atomic.Int64
	flushGID  atomic.Int64

	closeOnce sync.Once
}

var errRequestTimeout = errors.New("request timeout")

// New makes a client and starts loading the ruleset. It returns at once and never fails: a bad URL or key
// makes each request an error ("network", "http_401") that Status and the onError function report.
func New(url, sdkKey string, opts ...Option) *Client {
	cfg := config{
		stream:         true,
		pollInterval:   defaultPollInterval,
		flushInterval:  defaultFlushInterval,
		requestTimeout: defaultRequestTimeout,
		idle:           streamIdleTimeout,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.clock == nil {
		cfg.clock = realClock{}
	}
	if cfg.jitter == nil {
		cfg.jitter = rand.Float64
	}
	if cfg.evaluate == nil {
		cfg.evaluate = func(rs *evaluator.Ruleset, key string, ctx jsonvalue.Value) evaluator.Result {
			return rs.Evaluate(key, ctx)
		}
	}
	tr := &http.Transport{}
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		tr = base.Clone()
	}
	tr.DisableCompression = true
	tr.TLSClientConfig = cfg.tls
	root, cancel := context.WithCancelCause(context.Background())
	c := &Client{
		cfg:        cfg,
		baseURL:    strings.TrimRight(url, "/"),
		sdkKey:     sdkKey,
		tr:         tr,
		root:       root,
		cancelRoot: cancel,
		firstDone:  make(chan struct{}),
		usage:      map[usageKey]int64{},
		loopDone:   make(chan struct{}),
		flushDone:  make(chan struct{}),
	}
	c.http = &http.Client{
		Transport:     tr,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	if cfg.stream {
		c.stream = StreamConnecting
	} else {
		c.stream = StreamOff
	}
	go c.loop()
	go c.flushLoop()
	return c
}

// Ready waits for the first ruleset load to finish and answers whether a ruleset is now held. The load is
// bounded by the request timeout, and the answer is fixed once it settles: every later call gives the same
// one. After Close it answers false if the first load had not settled. Called from the SDK's own goroutine
// (an onError function, say) before the first load settled, it does not wait: it answers whether a
// ruleset is held.
func (c *Client) Ready() bool {
	select {
	case <-c.firstDone:
		return c.firstResult
	default:
	}
	if c.onOwnGoroutine() {
		return c.snap.Load() != nil
	}
	select {
	case <-c.firstDone:
	case <-c.root.Done():
		c.settleFirst(false)
	}
	return c.firstResult
}

// settleFirst fixes the answer of Ready. Only the first call counts.
func (c *Client) settleFirst(ok bool) {
	c.firstOnce.Do(func() {
		c.firstResult = ok
		close(c.firstDone)
	})
}

// Status returns what the SDK has loaded and how its stream is doing, as one snapshot. It makes no request.
func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := Status{LastSyncAt: c.lastSyncAt, Stream: c.stream, LastError: c.lastError}
	if s := c.snap.Load(); s != nil {
		st.ETag = s.etag
	}
	return st
}

// Close stops the loops, waits for them and for a flush already running, and sends the final usage flush.
// It is safe to call twice or from many goroutines: each call returns when the shutdown is done. Evaluation
// still answers from the ruleset held after Close, and nothing is counted. Called from the SDK's own
// goroutine (an onError function), Close stops everything and returns without waiting for the shutdown.
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closing = true
		c.stream = StreamClosed
		c.mu.Unlock()
		c.usageMu.Lock()
		c.noCount = true
		c.usageMu.Unlock()
		c.cancelRoot(errClosed)
		c.settleFirst(false)
	})
	if c.onOwnGoroutine() {
		return
	}
	<-c.loopDone
	<-c.flushDone
	c.tr.CloseIdleConnections()
}

var errClosed = errors.New("closed")

func (c *Client) isClosing() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closing
}

// onOwnGoroutine reports whether the caller is the loop or the flush goroutine, found by goroutine id.
func (c *Client) onOwnGoroutine() bool {
	id := goroutineID()
	return id != 0 && (id == c.loopGID.Load() || id == c.flushGID.Load())
}

func goroutineID() int64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	text := strings.TrimPrefix(string(buf[:n]), "goroutine ")
	end := strings.IndexByte(text, ' ')
	if end < 0 {
		return 0
	}
	id, err := strconv.ParseInt(text[:end], 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// report tells onError about a failure. A panic in onError is ignored. No SDK lock may be held.
func (c *Client) report(err *Error) {
	if err == nil || c.cfg.onError == nil {
		return
	}
	defer func() { _ = recover() }()
	c.cfg.onError(err)
}

// setStream moves the stream state, unless Close has begun: closed is final.
func (c *Client) setStream(s StreamState) {
	c.mu.Lock()
	if !c.closing {
		c.stream = s
	}
	c.mu.Unlock()
}

// clock is the SDK's time source for the loop's waits, the flush timer, Retry-After holds and the usage
// day. Request deadlines and the stream watchdog use real time.
type clock interface {
	Now() time.Time
	NewTimer(d time.Duration) timer
}

type timer interface {
	C() <-chan time.Time
	Stop() bool
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
func (realClock) NewTimer(d time.Duration) timer {
	return realTimer{time.NewTimer(d)}
}

type realTimer struct{ t *time.Timer }

func (r realTimer) C() <-chan time.Time { return r.t.C }
func (r realTimer) Stop() bool          { return r.t.Stop() }
