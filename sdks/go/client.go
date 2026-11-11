package toggleslash

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
)

// snapshot is a ruleset and the ETag that names it. It is immutable, and a new one replaces the old in a single
// atomic store, so a reader sees a ruleset with its own ETag, never one with another's.
type snapshot struct {
	rules *evaluator.Ruleset
	etag  string
}

// loop is one of the SDK's goroutines. inCallback counts application callbacks it is running: Close does not
// wait for a goroutine that is inside one, since Close may be what that callback called.
type loop struct {
	done       chan struct{}
	inCallback atomic.Int32
}

func newLoop() *loop { return &loop{done: make(chan struct{})} }

// Client is a ToggleSlash server SDK client. It is safe for use from any number of goroutines. Make one with
// New; it keeps itself current in the background until Close.
type Client struct {
	cfg  config
	http *http.Client

	snap atomic.Pointer[snapshot]

	// mu guards the status fields and the snapshot swap, so Status is one consistent view.
	mu         sync.Mutex
	lastSyncAt time.Time
	stream     StreamState
	lastError  string
	holdUntil  time.Time // a 429 on the ruleset or stream asked for nothing before this time

	readyOnce sync.Once
	readyCh   chan struct{}
	readyVal  bool

	ctx    context.Context
	cancel context.CancelCauseFunc

	syncLoop  *loop
	flushLoop *loop
	// closerLoop stands for the goroutine running the first Close, whose final flush may call onError. A second
	// Close made from inside that callback must not wait for the first.
	closerLoop loop

	closeOnce sync.Once
	closing   atomic.Bool
	closed    chan struct{}

	// usageMu guards pending and usageHold. flushMu serialises flushes. Neither is held across a request.
	usageMu   sync.Mutex
	pending   map[usageKey]int64
	usageHold time.Time
	flushMu   sync.Mutex
}

// New makes a client and starts it. It returns at once without waiting for the network, never returns nil and
// never panics. url is the server's base URL (trailing slashes are removed) and sdkKey the server SDK key.
func New(url, sdkKey string, opts ...Option) *Client {
	cfg := defaultConfig(url, sdkKey)
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	c := &Client{
		cfg:       cfg,
		http:      cfg.httpClient,
		readyCh:   make(chan struct{}),
		closed:    make(chan struct{}),
		syncLoop:  newLoop(),
		flushLoop: newLoop(),
		pending:   map[usageKey]int64{},
	}
	if c.http == nil {
		c.http = newHTTPClient()
	}
	c.ctx, c.cancel = context.WithCancelCause(context.Background())
	if cfg.stream {
		c.stream = StreamConnecting
	} else {
		c.stream = StreamOff
	}
	go c.runSync()
	go c.runFlush()
	return c
}

// Ready blocks until the first ruleset request has ended, then reports whether it left a ruleset in place.
// It is bounded by the request timeout, and its answer never changes. After Close it is false if it had not
// settled.
func (c *Client) Ready() bool {
	<-c.readyCh
	return c.readyVal
}

func (c *Client) settleReady(loaded bool) {
	c.readyOnce.Do(func() {
		c.readyVal = loaded
		close(c.readyCh)
	})
}

// Status is a consistent snapshot of local state: it reads memory and makes no request.
func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := Status{LastSyncAt: c.lastSyncAt, Stream: c.stream, LastError: c.lastError}
	if snap := c.snap.Load(); snap != nil {
		s.ETag = snap.etag
	}
	if c.ctx.Err() != nil {
		s.Stream = StreamClosed
	}
	return s
}

// Close stops following the server and sends the pending usage counts. It waits for the loops to stop and for
// the final flush (about twice the request timeout at the most), but not out a Retry-After hold. It is safe to
// call any number of times from any goroutine; every call returns once the same shutdown has finished. Called
// from inside one of the SDK's own callbacks it stops the rest and returns without waiting for that callback.
// Evaluation still works after it.
func (c *Client) Close() {
	first := false
	c.closeOnce.Do(func() { first = true })
	if !first {
		if c.inOwnCallback() {
			return
		}
		<-c.closed
		return
	}
	c.closing.Store(true)
	c.cancel(errClosed)
	c.settleReady(false)
	for _, l := range []*loop{c.syncLoop, c.flushLoop} {
		if l.inCallback.Load() == 0 {
			<-l.done
		}
	}
	c.finishUsage()
	if t, ok := c.http.Transport.(interface{ CloseIdleConnections() }); ok {
		t.CloseIdleConnections()
	}
	close(c.closed)
}

func (c *Client) inOwnCallback() bool {
	return c.closerLoop.inCallback.Load() > 0 || c.syncLoop.inCallback.Load() > 0 || c.flushLoop.inCallback.Load() > 0
}

// reportError tells the onError callback, marking the goroutine as inside a callback while it runs. A panic in
// the callback is ignored. who is the loop the goroutine belongs to (c.closerLoop for Close), or nil for a caller's goroutine.
func (c *Client) reportError(who *loop, err error) {
	if err == nil || c.cfg.onError == nil {
		return
	}
	if who != nil {
		who.inCallback.Add(1)
		defer who.inCallback.Add(-1)
	}
	defer func() { _ = recover() }()
	c.cfg.onError(err)
}

// holdWait is how much longer a 429 on the ruleset or stream asks the SDK to stay away.
func (c *Client) holdWait() time.Duration {
	c.mu.Lock()
	until := c.holdUntil
	c.mu.Unlock()
	return until.Sub(c.cfg.clock.Now())
}

// holdFor records a 429's Retry-After; a shorter one never shortens a longer hold.
func (c *Client) holdFor(d time.Duration) {
	until := c.cfg.clock.Now().Add(d)
	c.mu.Lock()
	if until.After(c.holdUntil) {
		c.holdUntil = until
	}
	c.mu.Unlock()
}
