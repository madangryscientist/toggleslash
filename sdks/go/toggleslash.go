// Package toggleslash is the ToggleSlash server SDK for Go. It keeps a copy of an environment's ruleset
// fresh (a stream with a polling fallback), evaluates flags locally with no I/O, and sends usage counts
// (never a context or an end-user value) back to the server.
//
// A Client never panics at its caller, never blocks an evaluation on the network, and does not keep the
// process alive: its goroutines are plain goroutines. Call Close to send the last usage counts.
package toggleslash

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
)

// snapshot is everything Status and an evaluation read, replaced whole so a reader sees one consistent
// state: the ruleset and its ETag always travel together.
type snapshot struct {
	rs         *evaluator.Ruleset
	etag       string
	lastSyncAt time.Time
	stream     StreamState
	lastError  string
}

// Client is a ToggleSlash server SDK client. It is safe for concurrent use.
type Client struct {
	cfg  config
	base string
	key  string
	hc   *http.Client

	snap    atomic.Pointer[snapshot]
	writeMu sync.Mutex

	// The ruleset and stream requests run under ctx, which Close cancels. Usage requests run under
	// usageCtx, which nothing cancels, so a count is never sent twice.
	ctx      context.Context
	cancel   context.CancelFunc
	usageCtx context.Context
	syncDone chan struct{}

	readyOnce sync.Once
	readyCh   chan struct{}
	readyVal  atomic.Bool

	usageMu sync.Mutex
	counts  map[usageKey]int64
	flushMu sync.Mutex

	flushStop chan struct{}
	flushDone chan struct{}

	// holdSync (ruleset and stream) and holdUsage are Retry-After deadlines in unix nanoseconds.
	holdSync  atomic.Int64
	holdUsage atomic.Int64

	closing      atomic.Bool
	closeMu      sync.Mutex
	closeStarted bool
	closeDone    chan struct{}

	// Each counts the application callbacks one of the SDK's own goroutines is running right now. Close does
	// not wait for a goroutine so marked: it is doing nothing more until the callback returns, and waiting
	// for it would deadlock a callback that calls Close.
	syncCB, flushCB, closerCB atomic.Int32
	pokeMu                    sync.Mutex
	pokeCh                    chan struct{}
}

// New creates a client and starts it. It returns at once: the first ruleset request runs in the background
// (see Ready). url is the server's base URL (trailing slashes are removed) and sdkKey a server key. It never
// returns nil and never panics.
func New(url, sdkKey string, opts ...Option) *Client {
	cfg := newConfig(opts)
	c := &Client{
		cfg:       cfg,
		base:      strings.TrimRight(url, "/"),
		key:       sdkKey,
		counts:    map[usageKey]int64{},
		readyCh:   make(chan struct{}),
		syncDone:  make(chan struct{}),
		flushStop: make(chan struct{}),
		flushDone: make(chan struct{}),
		closeDone: make(chan struct{}),
		pokeCh:    make(chan struct{}),
	}
	c.hc = cfg.httpClient
	if c.hc == nil {
		c.hc = newHTTPClient(cfg.tweakTransport)
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.usageCtx = context.Background()
	initial := &snapshot{stream: StreamOff}
	if cfg.stream {
		initial.stream = StreamConnecting
	}
	c.snap.Store(initial)
	go c.syncLoop()
	go c.flushLoop()
	return c
}

// newHTTPClient builds the client the SDK uses: the default transport's settings, no compression (so no
// Accept-Encoding header is added), and no redirect followed, so a 3xx is an http_3xx error and the key is
// never sent anywhere else.
func newHTTPClient(tweak func(*http.Transport)) *http.Client {
	var tr *http.Transport
	if d, ok := http.DefaultTransport.(*http.Transport); ok {
		tr = d.Clone()
	} else {
		tr = &http.Transport{Proxy: http.ProxyFromEnvironment}
	}
	tr.DisableCompression = true
	if tweak != nil {
		tweak(tr)
	}
	return &http.Client{
		Transport:     tr,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (c *Client) clock() time.Time {
	if c.cfg.now != nil {
		return c.cfg.now()
	}
	return time.Now()
}

// update replaces the snapshot with a changed copy. Once Close has begun the stream state stays closed.
func (c *Client) update(change func(*snapshot)) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	n := *c.snap.Load()
	change(&n)
	if c.closing.Load() {
		n.stream = StreamClosed
	}
	c.snap.Store(&n)
}

// settle fixes the answer of Ready at the first call.
func (c *Client) settle() {
	c.readyOnce.Do(func() {
		c.readyVal.Store(c.snap.Load().rs != nil)
		close(c.readyCh)
	})
}

// Ready blocks until the first ruleset request has ended, then reports whether a ruleset is held. The answer
// is fixed at that moment. It is bounded by the request timeout. After Close it is false if the first load
// had not ended.
func (c *Client) Ready() bool {
	<-c.readyCh
	return c.readyVal.Load()
}

// Status returns one consistent snapshot of the client. It makes no request.
func (c *Client) Status() Status {
	s := c.snap.Load()
	return Status{ETag: s.etag, LastSyncAt: s.lastSyncAt, Stream: s.stream, LastError: s.lastError}
}

// notify gives an error to onError, on the calling goroutine, with no SDK lock held. A panic in onError is
// ignored. mark is the counter of the SDK goroutine making the call (nil for an application goroutine).
func (c *Client) notify(err error, mark *atomic.Int32) {
	f := c.cfg.onError
	if f == nil || err == nil {
		return
	}
	if mark != nil {
		mark.Add(1)
		if c.closing.Load() {
			c.poke()
		}
		defer mark.Add(-1)
	}
	defer func() { _ = recover() }()
	f(err)
}

func (c *Client) poke() {
	c.pokeMu.Lock()
	close(c.pokeCh)
	c.pokeCh = make(chan struct{})
	c.pokeMu.Unlock()
}

func (c *Client) pokeChan() chan struct{} {
	c.pokeMu.Lock()
	defer c.pokeMu.Unlock()
	return c.pokeCh
}

// await waits for done, or returns early when one of the given SDK goroutines is running an application
// callback (see the marks above).
func (c *Client) await(done <-chan struct{}, marks ...*atomic.Int32) {
	for {
		ch := c.pokeChan()
		for _, m := range marks {
			if m.Load() > 0 {
				return
			}
		}
		select {
		case <-done:
			return
		case <-ch:
		}
	}
}

// Close stops the client: no more ruleset requests, stream attempts or polls, then the last usage flush. It
// is safe to call twice and from several goroutines; every call returns when the shutdown is done, which is
// within about twice the request timeout. Evaluation still answers from the ruleset held afterwards. It does
// not wait for a goroutine of the SDK's own that is inside one of your callbacks, so calling it from onError
// is safe.
func (c *Client) Close() {
	defer func() { _ = recover() }()
	if c.closerCB.Load() > 0 {
		return
	}
	c.closeMu.Lock()
	first := !c.closeStarted
	c.closeStarted = true
	c.closeMu.Unlock()
	if !first {
		c.await(c.closeDone, &c.syncCB, &c.flushCB)
		return
	}
	defer close(c.closeDone)
	c.closing.Store(true)
	c.update(func(*snapshot) {})
	c.cancel()
	c.await(c.syncDone, &c.syncCB)
	c.settle()
	close(c.flushStop)
	c.await(c.flushDone, &c.flushCB)
	c.flushFinal()
	if t, ok := c.hc.Transport.(interface{ CloseIdleConnections() }); ok {
		t.CloseIdleConnections()
	}
}
