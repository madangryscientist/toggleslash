// Package toggleslash is the ToggleSlash server SDK for Go. A Client holds the flag rules of one
// environment, keeps them current from the server, and evaluates flags locally: an evaluation does no I/O,
// takes no lock a network call holds, and never panics at the caller. It also counts how often each flag
// variation was served (counts only, never a context) and sends the totals in batches.
//
// The contract every ToggleSlash SDK keeps is docs/sdk-contract.md; DESIGN.md maps it to this package.
package toggleslash

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
)

// Why a request was cut short, told apart by context.Cause.
var (
	errClosed  = errors.New("toggleslash: client closed")
	errTimeout = errors.New("toggleslash: request timed out")
)

// state is everything the SDK holds that readers see. It is immutable: a change is a copy, an edit and one
// atomic store, so a reader sees a ruleset and the tag that names it together, and Status is one snapshot.
type state struct {
	rs         *evaluator.Ruleset
	etag       string
	lastSyncAt time.Time
	lastError  LastError
	stream     StreamState
}

// Client keeps one ruleset current and evaluates flags against it. It is safe for concurrent use. Make one
// with New and call Close when done.
type Client struct {
	cfg     config
	baseURL string
	sdkKey  string
	http    *http.Client

	state atomic.Pointer[state]
	mu    sync.Mutex // guards the copy-edit-store of state, never held across I/O or a callback

	root   context.Context
	cancel context.CancelCauseFunc

	readyOnce sync.Once
	readyCh   chan struct{}
	readyVal  bool

	usageMu sync.Mutex // guards counts and closed
	counts  map[usageKey]int64
	closed  bool

	flushMu   sync.Mutex // serialises flushes; held across the flush's requests, never taken by evaluation
	usageHold time.Time  // guarded by flushMu: the server's 429 asked for no usage until then

	syncHold time.Time // used by the sync goroutine only: the same, for the ruleset and the stream

	syncTask  *task
	flushTask *task
	closeTask *task
	closing   atomic.Bool
}

// New starts a client for the server at url with a server SDK key. It returns at once and never fails: a bad
// URL or an unreachable server shows as Status().LastError and as fallbacks, never as an error here. It starts
// two goroutines, which Close stops.
func New(url, sdkKey string, opts ...Option) *Client {
	cfg := defaultConfig()
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	c := &Client{
		cfg:       cfg,
		baseURL:   strings.TrimRight(url, "/"),
		sdkKey:    sdkKey,
		readyCh:   make(chan struct{}),
		counts:    map[usageKey]int64{},
		syncTask:  newTask(),
		flushTask: newTask(),
		closeTask: newTask(),
	}
	c.http = newHTTPClient(cfg.transport)
	c.root, c.cancel = context.WithCancelCause(context.Background())
	first := &state{stream: StreamOff}
	if cfg.stream {
		first.stream = StreamConnecting
	}
	c.state.Store(first)
	go c.syncLoop()
	go c.flushLoop()
	return c
}

func newHTTPClient(rt http.RoundTripper) *http.Client {
	if rt == nil {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.DisableCompression = true
		rt = t
	}
	return &http.Client{
		Transport: rt,
		// A redirect is a failure (http_3xx), so the key is never sent to another place.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// update copies the state, lets f edit the copy and stores it. Once the client is closed the stream stays
// "closed", whatever a goroutine that is winding down still tries to set.
func (c *Client) update(f func(*state)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	next := *c.state.Load()
	f(&next)
	if c.state.Load().stream == StreamClosed {
		next.stream = StreamClosed
	}
	c.state.Store(&next)
}

// Status is what this SDK has loaded and how its stream is doing. It reads memory, makes no network call and
// never blocks.
func (c *Client) Status() Status {
	s := c.state.Load()
	return Status{ETag: s.etag, LastSyncAt: s.lastSyncAt, Stream: s.stream, LastError: s.lastError}
}

// Ready blocks until the first ruleset load has finished and reports whether a ruleset is held. It is bounded
// by the request timeout. The answer is fixed when the first attempt settles: false means flags serve their
// fallbacks until the SDK reaches the server. After Close it answers false if it had not settled.
func (c *Client) Ready() bool {
	<-c.readyCh
	return c.readyVal
}

func (c *Client) settleReady(ok bool) {
	c.readyOnce.Do(func() {
		c.readyVal = ok
		close(c.readyCh)
	})
}

// Close stops following the server, sends the pending usage counts and returns, in about twice the request
// timeout at most. It is safe to call twice or from many goroutines, and from inside a callback the SDK made.
func (c *Client) Close() {
	if !c.closing.CompareAndSwap(false, true) {
		c.closeTask.wait()
		return
	}
	defer close(c.closeTask.done)
	c.settleReady(false)
	c.update(func(s *state) { s.stream = StreamClosed })
	c.usageMu.Lock()
	c.closed = true
	c.usageMu.Unlock()
	c.cancel(errClosed)
	c.syncTask.wait()
	c.flushTask.wait()
	errs := c.flush(context.Background(), true)
	if n := c.unsentAtClose(); n > 0 {
		errs = append(errs, fmt.Errorf("closed with %d usage counts unsent: the server asked to wait", n))
	}
	c.closeTask.callback(func() {
		for _, err := range errs {
			c.callOnError(err)
		}
	})
	c.http.CloseIdleConnections()
}
