// Package toggleslash is the ToggleSlash server SDK for Go.
//
// It keeps a copy of an environment's ruleset, follows the server for changes, evaluates flags locally
// and sends usage counts. Nothing about an end user leaves the process.
//
//	client := toggleslash.Create("https://flags.example.com", sdkKey)
//	defer client.Close()
//	client.Ready()
//	ctx, _ := toggleslash.ParseJSON([]byte(`{"key": "u1", "attributes": {"plan": "pro"}}`))
//	on := client.BoolVariation("new-checkout", ctx, false)
//
// The contract this implements is docs/sdk-contract.md in the ToggleSlash repository. In short: no
// method panics or returns an error to its caller, Variation and Status never do I/O or wait, the last
// good ruleset is kept while the server is away, and every method is safe to call from many goroutines.
package toggleslash

import (
	"context"
	"errors"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
)

var (
	errTimeout = errors.New("timeout")
	errClosed  = errors.New("closed")
)

// view is everything Status shows and the ruleset the evaluator reads, as one immutable value. A writer
// copies it, changes the copy and stores the pointer, so a reader sees one moment, never a mix.
type view struct {
	ruleset    *evaluator.Ruleset
	etag       string
	lastSyncAt time.Time
	stream     StreamState
	lastError  string
}

// Client is a ToggleSlash server SDK client. Make one with Create. It is safe for concurrent use.
type Client struct {
	cfg  config
	base string
	http *http.Client
	tr   *http.Transport // the transport the client made, or nil when one was injected

	view    atomic.Pointer[view]
	writeMu sync.Mutex // a copy of the view and its store; never held across I/O or a callback

	root   context.Context
	cancel context.CancelCauseFunc

	readyCh   chan struct{}
	readyOnce sync.Once
	readyVal  atomic.Bool
	firstHeld atomic.Bool // the first load's result, known before the ready channel closes

	syncDone  chan struct{}
	flushDone chan struct{}
	syncGid   atomic.Uint64
	flushGid  atomic.Uint64

	syncHold  hold // ruleset and stream share it
	usageHold hold // the events route has its own

	usageMu     sync.Mutex
	counts      map[usageKey]int64
	countClosed bool
	flushMu     sync.Mutex

	closeStarted atomic.Bool
	closerGid    atomic.Uint64
	closeDone    chan struct{}
}

// Create makes a client and starts it. It returns at once and never fails: a URL it cannot use makes
// each request a "network" error that Status and the onError function report.
func Create(url, sdkKey string, opts ...Option) *Client {
	cfg := config{
		url:            strings.TrimRight(url, "/"),
		sdkKey:         sdkKey,
		stream:         true,
		pollInterval:   defaultPollInterval,
		flushInterval:  defaultFlushInterval,
		requestTimeout: defaultRequestTimeout,
		watchdog:       defaultWatchdog,
		retryAfterCap:  defaultRetryAfterCap,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	c := &Client{cfg: cfg, base: cfg.url, counts: map[usageKey]int64{}}
	c.applyDefaults()
	c.root, c.cancel = context.WithCancelCause(context.Background())
	c.readyCh = make(chan struct{})
	c.syncDone = make(chan struct{})
	c.flushDone = make(chan struct{})
	c.closeDone = make(chan struct{})
	initial := &view{stream: StreamOff}
	if cfg.stream {
		initial.stream = StreamConnecting
	}
	c.view.Store(initial)
	go c.runSync()
	go c.runFlush()
	return c
}

func (c *Client) applyDefaults() {
	if c.cfg.now == nil {
		c.cfg.now = time.Now
	}
	if c.cfg.sleep == nil {
		c.cfg.sleep = realSleep
	}
	if c.cfg.jitter == nil {
		c.cfg.jitter = realJitter
	}
	if c.cfg.evaluate == nil {
		c.cfg.evaluate = func(rs *evaluator.Ruleset, flagKey string, ctx Context) evaluator.Result {
			return rs.Evaluate(flagKey, ctx)
		}
	}
	if c.cfg.httpClient != nil {
		c.http = c.cfg.httpClient
		return
	}
	// A clone of the default transport with no proxy from the environment, so the SDK talks to the one
	// URL it was given. Redirects are not followed: a 3xx is an http_3xx error and the key is never
	// replayed to another place.
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		c.tr = base.Clone()
	} else {
		c.tr = &http.Transport{}
	}
	c.tr.Proxy = nil
	c.http = &http.Client{
		Transport: c.tr,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// update copies the view, lets fn change the copy and stores it. It does nothing once the client is
// closed, so a late result never moves Status. It reports whether it applied.
func (c *Client) update(fn func(v *view)) bool {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	old := c.view.Load()
	if old.stream == StreamClosed {
		return false
	}
	nv := *old
	fn(&nv)
	c.view.Store(&nv)
	return true
}

func (c *Client) markClosed() {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	nv := *c.view.Load()
	nv.stream = StreamClosed
	c.view.Store(&nv)
}

// Ready blocks until the first ruleset attempt has settled and reports whether a ruleset is held. The
// wait is bounded by the request timeout. The answer is fixed when the first attempt settles, so asking
// again, or after a later load, gives the same answer. After Close it is false if it had not settled.
// Called from the SDK's own goroutine (an onError function, for the first load's failure) it returns at
// once with what is held.
func (c *Client) Ready() bool {
	select {
	case <-c.readyCh:
		return c.readyVal.Load()
	default:
	}
	if c.onLoopGoroutine() {
		return c.firstHeld.Load()
	}
	<-c.readyCh
	return c.readyVal.Load()
}

func (c *Client) settle(held bool) {
	c.readyOnce.Do(func() {
		c.readyVal.Store(held)
		close(c.readyCh)
	})
}

// Status returns one consistent snapshot. It reads memory and never waits.
func (c *Client) Status() Status {
	v := c.view.Load()
	return Status{ETag: v.etag, LastSyncAt: v.lastSyncAt, Stream: v.stream, LastError: v.lastError}
}

// Close stops following the server, abandons the requests in flight, waits for the SDK's goroutines,
// then sends the usage counts still pending. It is idempotent and safe from many goroutines: each call
// returns when the one shutdown is done. It returns within about twice the request timeout. The error is
// always nil; it is there so a Client is an io.Closer.
func (c *Client) Close() error {
	if !c.closeStarted.CompareAndSwap(false, true) {
		if c.closerGid.Load() == goroutineID() {
			return nil // called again from a callback the shutdown itself is running
		}
		<-c.closeDone
		return nil
	}
	defer close(c.closeDone)
	defer func() { _ = recover() }()
	c.closerGid.Store(goroutineID())
	c.shutdown()
	return nil
}

func (c *Client) shutdown() {
	c.markClosed()
	c.cancel(errClosed)
	c.usageMu.Lock()
	c.countClosed = true
	c.usageMu.Unlock()
	me := goroutineID()
	if c.syncGid.Load() != me {
		<-c.syncDone
	}
	if c.flushGid.Load() != me {
		<-c.flushDone
	}
	c.settle(false)
	for _, err := range c.flush(true) {
		c.report(err)
	}
	if c.tr != nil {
		c.tr.CloseIdleConnections()
	}
}

// report hands an error to the onError function. A panic in that function is ignored. No SDK lock is held
// by any caller.
func (c *Client) report(err error) {
	if err == nil || c.cfg.onError == nil {
		return
	}
	defer func() { _ = recover() }()
	c.cfg.onError(err)
}

// onLoopGoroutine reports whether the caller is one of the SDK's own goroutines (it is, inside an
// onError function the SDK called there).
func (c *Client) onLoopGoroutine() bool {
	me := goroutineID()
	return me != 0 && (me == c.syncGid.Load() || me == c.flushGid.Load())
}

func goroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	s := strings.TrimPrefix(string(buf[:n]), "goroutine ")
	end := strings.IndexByte(s, ' ')
	if end < 0 {
		return 0
	}
	id, err := strconv.ParseUint(s[:end], 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// hold is a Retry-After hold: a point in time before which nothing is sent.
type hold struct {
	mu    sync.Mutex
	until time.Time
}

// extend moves the hold later; it never shortens it.
func (h *hold) extend(t time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if t.After(h.until) {
		h.until = t
	}
}

func (h *hold) remaining(now time.Time) time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	if d := h.until.Sub(now); d > 0 {
		return d
	}
	return 0
}
