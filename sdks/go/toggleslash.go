// Package toggleslash is the Go server SDK for ToggleSlash. It keeps a copy of an environment's
// ruleset current (over a live stream, with a polling fallback), evaluates flags against it locally
// with no network round trip, and sends usage counts back in batches. It implements
// docs/sdk-contract.md; docs/evaluation.md is the specification of the evaluator.
//
//	client := toggleslash.New("https://toggleslash.example", sdkKey)
//	defer client.Close()
//	client.Ready(ctx) // optional: wait for the first load
//	on := client.BoolVariation("new-checkout", toggleslash.NewUserContext("user-1"), false)
//
// The SDK never panics at its caller and never returns an error from an evaluation: an evaluation
// that cannot be answered gives the fallback you passed, and the ...Detail form says why. Everything
// it absorbs goes to the function given to WithOnError. A Client is safe for use from many goroutines.
// It sends no context, no flag value and nothing about an end user over the network.
package toggleslash

import (
	"context"
	"math/rand/v2"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Client is a connection to a ToggleSlash server. Make one with New and Close it when done.
type Client struct {
	base string
	key  string
	opt  options

	transport *http.Transport
	http      *http.Client

	// snap is the ruleset and its tag, swapped as one pointer.
	snap atomic.Pointer[snapshot]

	// statusMu guards the fields below it, which Status reads as one snapshot, and the two holds.
	statusMu    sync.Mutex
	lastSync    time.Time
	lastErr     LastError
	streamState StreamState
	holdRuleset time.Time // a 429 on the ruleset or the stream asked for nothing before this
	holdEvents  time.Time // a 429 on the events asked for nothing before this

	readyCh   chan struct{}
	readyOnce sync.Once
	readyVal  bool

	loopCtx   context.Context
	stopLoops context.CancelFunc
	wg        sync.WaitGroup
	closing   atomic.Bool
	closeOnce sync.Once

	usageMu sync.Mutex
	counts  map[usageKey]int64
	flushMu sync.Mutex

	// owned holds the ids of the goroutines the SDK runs its own code on.
	owned sync.Map
}

// New starts a client for the server at url, authenticating with the SDK key. It returns at once and
// never fails: the first load happens in the background (Ready waits for it), and until a ruleset
// is held every evaluation answers its fallback with the reason ErrNotReady.
func New(url, sdkKey string, opts ...Option) *Client {
	opt := defaultOptions()
	for _, o := range opts {
		o(&opt)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if opt.transport != nil {
		opt.transport(transport)
	}
	c := &Client{
		base:      strings.TrimRight(url, "/"),
		key:       sdkKey,
		opt:       opt,
		transport: transport,
		http: &http.Client{
			Transport: transport,
			// A redirect is an answer like any other (http_3xx), never followed with the key.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		readyCh: make(chan struct{}),
		counts:  make(map[usageKey]int64),
	}
	if opt.stream {
		c.streamState = StreamConnecting
	} else {
		c.streamState = StreamOff
	}
	c.loopCtx, c.stopLoops = context.WithCancel(context.Background())
	c.wg.Add(2)
	go c.syncLoop()
	go c.flushLoop()
	return c
}

func (c *Client) now() time.Time {
	if c.opt.now != nil {
		return c.opt.now()
	}
	return time.Now()
}

func (c *Client) jitter() float64 {
	if c.opt.jitter != nil {
		return c.opt.jitter()
	}
	return rand.Float64()
}

// settleReady fixes the answer of Ready; the first call wins.
func (c *Client) settleReady(held bool) {
	c.readyOnce.Do(func() {
		c.readyVal = held
		close(c.readyCh)
	})
}

// Ready waits until the first load has finished and reports whether a ruleset is held. The first
// load is bounded by the request timeout, so Ready needs no timeout of its own; if ctx is done first
// it reports false and the answer stays open. Once settled the answer never changes, and after Close
// an unsettled Ready is false. Called from inside an SDK callback (which runs on an SDK goroutine)
// it does not wait and reports whether a ruleset is held.
func (c *Client) Ready(ctx context.Context) bool {
	select {
	case <-c.readyCh:
		return c.readyVal
	default:
	}
	if c.onOwnGoroutine() {
		return c.snap.Load() != nil
	}
	select {
	case <-c.readyCh:
		return c.readyVal
	case <-ctx.Done():
		return false
	}
}

// Status returns what the client knows about its own sync, as one consistent snapshot. It does no I/O
// and never waits on the network.
func (c *Client) Status() Status {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	st := Status{LastSyncAt: c.lastSync, Stream: c.streamState, LastError: c.lastErr}
	if s := c.snap.Load(); s != nil {
		st.ETag, st.HasETag = s.etag, s.hasETag
	}
	if c.closing.Load() {
		st.Stream = StreamClosed
	}
	return st
}

// Close stops the client: it abandons the requests in flight, stops the loops, and sends the usage
// counts it holds. It returns within about twice the request timeout, is safe to call twice or from
// many goroutines at once (each returns when the shutdown is done), and evaluation keeps answering
// from the ruleset held afterwards, without counting. A call from inside an SDK callback starts the
// shutdown on another goroutine and returns at once.
func (c *Client) Close() {
	c.closing.Store(true)
	if c.onOwnGoroutine() {
		go c.shutdown()
		return
	}
	c.shutdown()
}

func (c *Client) shutdown() {
	c.closeOnce.Do(func() {
		c.stopLoops()
		c.wg.Wait()
		c.settleReady(false)
		unregister := c.own()
		c.finish()
		unregister()
		c.transport.CloseIdleConnections()
	})
}

// reportError passes an absorbed failure to onError, with no SDK lock held; a panic in it is ignored.
func (c *Client) reportError(err error) {
	if c.opt.onError == nil {
		return
	}
	defer func() { _ = recover() }()
	c.opt.onError(err)
}

// own marks the calling goroutine as one the SDK runs its own code on, and returns the function that
// unmarks it.
func (c *Client) own() func() {
	id := goroutineID()
	if id == 0 {
		return func() {}
	}
	c.owned.Store(id, struct{}{})
	return func() { c.owned.Delete(id) }
}

func (c *Client) onOwnGoroutine() bool {
	id := goroutineID()
	if id == 0 {
		return false
	}
	_, ok := c.owned.Load(id)
	return ok
}

// goroutineID reads the id from the first line of the stack trace ("goroutine 12 [running]:"). It
// answers 0 if the text is not that, which makes the caller "not an SDK goroutine".
func goroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	s := strings.TrimPrefix(string(buf[:n]), "goroutine ")
	end := strings.IndexByte(s, ' ')
	if end <= 0 {
		return 0
	}
	id, err := strconv.ParseUint(s[:end], 10, 64)
	if err != nil {
		return 0
	}
	return id
}
