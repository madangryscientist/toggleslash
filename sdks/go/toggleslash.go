// Package toggleslash is the ToggleSlash server SDK for Go. It keeps one environment's ruleset in memory,
// current with the server, evaluates flags locally for a context the caller passes, and reports which
// variations it served. It is not a client-side SDK: it holds a server key (fs_server_...).
//
// Nothing here panics at a caller, blocks on the network in an evaluation, or sends anything about an end
// user. See README.md for the mapping from the contract's names to this package's.
package toggleslash

import (
	"context"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/eval"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

// The contract's defaults.
const (
	defaultPollInterval   = 30 * time.Second
	defaultFlushInterval  = 30 * time.Second
	defaultRequestTimeout = 10 * time.Second
	defaultWatchdog       = 60 * time.Second
)

// evaluator is the seam a test replaces to make an evaluation panic.
type evaluator func(rs *eval.Ruleset, flagKey string, context jsonvalue.Value) eval.Result

type config struct {
	stream          *bool
	pollInterval    time.Duration
	flushInterval   time.Duration
	requestTimeout  time.Duration
	afterEvaluation func(EvaluationEvent)
	onError         func(error)
	httpClient      *http.Client

	// Seams for the package's own tests.
	now      func() time.Time
	random   func() float64
	watchdog time.Duration
	evaluate evaluator
}

// Option configures NewClient.
type Option func(*config)

// WithStream turns the stream on (the default) or off. Off means poll only.
func WithStream(on bool) Option { return func(c *config) { c.stream = &on } }

// WithPollInterval sets how often to poll when the stream is down or off, and the longest reconnect wait.
// Zero or negative keeps the default of 30 s.
func WithPollInterval(d time.Duration) Option { return func(c *config) { c.pollInterval = d } }

// WithFlushInterval sets how often usage counts are sent. Zero or negative keeps the default of 30 s.
func WithFlushInterval(d time.Duration) Option { return func(c *config) { c.flushInterval = d } }

// WithRequestTimeout sets the longest any one request may take, body included. Zero or negative keeps the
// default of 10 s.
func WithRequestTimeout(d time.Duration) Option { return func(c *config) { c.requestTimeout = d } }

// WithAfterEvaluation installs a hook called after every evaluation, a fallback included, on the caller's
// goroutine, before the evaluation method returns. A panic in it goes to the onError callback and changes
// nothing else. A nil function is ignored.
func WithAfterEvaluation(fn func(EvaluationEvent)) Option {
	return func(c *config) { c.afterEvaluation = fn }
}

// WithOnError installs a callback for every failure the SDK absorbs. It is called on the goroutine that
// found the failure and never while the SDK holds a lock of its own. A panic in it is ignored.
func WithOnError(fn func(error)) Option { return func(c *config) { c.onError = fn } }

// WithHTTPClient replaces the SDK's own http.Client. The SDK does not change it: a client that follows
// redirects will resend the Authorization header to a same-host redirect target.
func WithHTTPClient(h *http.Client) Option { return func(c *config) { c.httpClient = h } }

func withClock(now func() time.Time) Option { return func(c *config) { c.now = now } }
func withRandom(r func() float64) Option    { return func(c *config) { c.random = r } }
func withWatchdog(d time.Duration) Option   { return func(c *config) { c.watchdog = d } }
func withEvaluator(fn evaluator) Option     { return func(c *config) { c.evaluate = fn } }

// snapshot is a ruleset and the ETag that names it. It is never changed after it is stored, so a reader
// that loads the pointer sees one pair.
type snapshot struct {
	rs   *eval.Ruleset
	etag string
}

// Client is a ToggleSlash server SDK client. Every method is safe to call from many goroutines at once.
type Client struct {
	cfg    config
	url    string
	sdkKey string
	http   *http.Client

	snap atomic.Pointer[snapshot]

	statusMu   sync.RWMutex
	lastSyncAt time.Time
	stream     StreamState
	lastError  string
	closed     bool

	readyOnce sync.Once
	readyCh   chan struct{}
	readyVal  bool

	syncHold   hold
	eventsHold hold

	usageMu     sync.Mutex
	usage       map[usageKey]int64
	usageClosed bool
	flushMu     sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc
	stop   chan struct{}

	syncDone  chan struct{}
	flushDone chan struct{}
	closeOnce sync.Once
	done      chan struct{}

	goroutines sync.Map // goroutine id -> struct{}, the goroutines the SDK owns
}

// NewClient builds a client and starts its sync and flush goroutines. It does not wait for the network and
// it never fails: a URL it cannot use makes each request a "network" error.
func NewClient(url, sdkKey string, opts ...Option) *Client {
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
		cfg.pollInterval = defaultPollInterval
	}
	if cfg.flushInterval <= 0 {
		cfg.flushInterval = defaultFlushInterval
	}
	if cfg.requestTimeout <= 0 {
		cfg.requestTimeout = defaultRequestTimeout
	}
	if cfg.watchdog <= 0 {
		cfg.watchdog = defaultWatchdog
	}
	if cfg.now == nil {
		cfg.now = time.Now
	}
	if cfg.random == nil {
		cfg.random = randomFloat
	}
	if cfg.evaluate == nil {
		cfg.evaluate = defaultEvaluate
	}
	c := &Client{
		cfg:       cfg,
		url:       strings.TrimRight(url, "/"),
		sdkKey:    sdkKey,
		readyCh:   make(chan struct{}),
		usage:     map[usageKey]int64{},
		stop:      make(chan struct{}),
		syncDone:  make(chan struct{}),
		flushDone: make(chan struct{}),
		done:      make(chan struct{}),
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.http = cfg.httpClient
	if c.http == nil {
		c.http = newHTTPClient()
	}
	if *cfg.stream {
		c.stream = StreamConnecting
	} else {
		c.stream = StreamOff
	}
	go c.syncLoop()
	go c.flushLoop()
	return c
}

func defaultEvaluate(rs *eval.Ruleset, flagKey string, context jsonvalue.Value) eval.Result {
	return rs.Evaluate(flagKey, context)
}

func newHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &http.Client{
		Transport: transport,
		// A redirect is an http_3xx error, and Authorization is never resent to its target.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// Ready waits for the first ruleset load to finish and answers whether a ruleset is held. It is bounded by
// the request timeout, and the answer is fixed when the first attempt settles: a later load does not turn
// false into true. After Close it answers false if the first attempt had not settled. Called from one of the
// SDK's own callbacks it does not wait: it answers with the state held.
func (c *Client) Ready() bool {
	select {
	case <-c.readyCh:
		return c.readyVal
	default:
	}
	if c.onOwnGoroutine() {
		return c.snap.Load() != nil
	}
	<-c.readyCh
	return c.readyVal
}

func (c *Client) settleReady(v bool) {
	c.readyOnce.Do(func() {
		c.readyVal = v
		close(c.readyCh)
	})
}

// Status is one consistent snapshot of what the SDK has loaded and how its stream is doing. It makes no
// request and never fails.
func (c *Client) Status() Status {
	c.statusMu.RLock()
	defer c.statusMu.RUnlock()
	st := Status{LastSyncAt: c.lastSyncAt, Stream: c.stream, LastError: c.lastError}
	if s := c.snap.Load(); s != nil {
		st.ETag = s.etag
	}
	return st
}

// Close stops following the server, stops the flush loop, waits for the loops and a flush already running,
// and sends the final usage flush. It never fails, is safe to call twice or concurrently (every call waits
// for the same shutdown) and returns within about twice the request timeout. Evaluation still answers from
// the ruleset held. Called from one of the SDK's own callbacks it starts the shutdown and returns at once.
func (c *Client) Close() {
	defer func() { _ = recover() }()
	c.closeOnce.Do(func() {
		c.statusMu.Lock()
		c.closed = true
		c.stream = StreamClosed
		c.statusMu.Unlock()
		c.usageMu.Lock()
		c.usageClosed = true
		c.usageMu.Unlock()
		c.cancel()
		close(c.stop)
		go c.shutdown()
	})
	if c.onOwnGoroutine() {
		return
	}
	<-c.done
}

func (c *Client) shutdown() {
	defer close(c.done)
	defer func() { _ = recover() }()
	c.markGoroutine()
	defer c.unmarkGoroutine()
	<-c.syncDone
	<-c.flushDone
	c.flush(true)
	c.settleReady(false)
	if t, ok := c.http.Transport.(interface{ CloseIdleConnections() }); ok {
		t.CloseIdleConnections()
	}
}

func goid() int64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	var id int64
	for _, b := range buf[len("goroutine "):n] {
		if b < '0' || b > '9' {
			break
		}
		id = id*10 + int64(b-'0')
	}
	return id
}

func (c *Client) markGoroutine()   { c.goroutines.Store(goid(), struct{}{}) }
func (c *Client) unmarkGoroutine() { c.goroutines.Delete(goid()) }

func (c *Client) onOwnGoroutine() bool {
	_, ok := c.goroutines.Load(goid())
	return ok
}
