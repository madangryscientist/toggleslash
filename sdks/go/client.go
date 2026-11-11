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

// snapshot is a ruleset and the ETag that names it. It is never changed after
// it is built: a sync swaps the whole pointer, so a reader sees the old pair
// or the new one, never a mix.
type snapshot struct {
	ruleset *evaluator.Ruleset
	etag    string
}

// Client keeps one environment's ruleset current and evaluates flags against
// it. Every method is safe to call from many goroutines at once.
type Client struct {
	base    string
	key     string
	stream  bool
	poll    time.Duration
	flushIv time.Duration
	timeout time.Duration
	hook    func(EvaluationEvent)
	onError func(error)

	hc        *http.Client
	transport *http.Transport // the one the SDK made, to close its idle connections; nil for a caller's client
	clock     clock
	jitter    func() float64
	watchdog  time.Duration

	// ctx ends when Close starts: it stops the loops and abandons their requests.
	ctx    context.Context
	cancel context.CancelFunc
	// loops counts the sync and flush goroutines.
	loops     sync.WaitGroup
	closing   atomic.Bool
	closeOnce sync.Once
	done      chan struct{}
	own       goroutineSet

	snap atomic.Pointer[snapshot]

	// stMu guards the status fields and the writes of snap. It is held only for
	// assignments, never across I/O or a callback.
	stMu        sync.Mutex
	lastSyncAt  time.Time
	lastError   string
	streamState StreamState

	// hold is the time before which nothing goes to the ruleset or stream route.
	holdMu sync.Mutex
	hold   time.Time

	firstOnce  sync.Once
	firstDone  chan struct{}
	firstReady bool

	// usageMu guards counts and usageHold. flushMu serialises flushes and is
	// held across I/O, but no evaluation ever takes it.
	usageMu   sync.Mutex
	counts    map[usageKey]int64
	usageHold time.Time
	flushMu   sync.Mutex
}

// New builds a client and starts it. It does not wait for the network: the
// first ruleset request runs on a goroutine, and Ready waits for it. New never
// returns an error and never panics; a URL it cannot use makes each request a
// "network" failure. url is the server's base URL (trailing slashes are
// removed) and sdkKey a server key.
func New(url, sdkKey string, opts ...Option) *Client {
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			safely(func() { opt(&cfg) })
		}
	}
	c := &Client{
		base:     strings.TrimRight(url, "/"),
		key:      sdkKey,
		stream:   cfg.stream,
		poll:     cfg.pollInterval,
		flushIv:  cfg.flushInterval,
		timeout:  cfg.requestTimeout,
		hook:     cfg.afterEvaluate,
		onError:  cfg.onError,
		clock:    cfg.clock,
		jitter:   cfg.jitter,
		watchdog: cfg.watchdog,

		done:      make(chan struct{}),
		firstDone: make(chan struct{}),
		counts:    map[usageKey]int64{},
	}
	if c.onError == nil && cfg.logger != nil {
		logger := cfg.logger
		c.onError = func(err error) { logger.Warn(err.Error()) }
	}
	c.hc, c.transport = newHTTPClient(cfg.httpClient)
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.streamState = StreamOff
	if c.stream {
		c.streamState = StreamConnecting
	}
	c.loops.Add(2)
	go c.syncLoop()
	go c.flushLoop()
	return c
}

// Ready waits for the first ruleset load to finish and reports whether a
// ruleset is held. It is bounded by the request timeout and gives the same
// answer every time: a later load does not turn false into true. After Close
// it reports false if the first load had not finished. Called from one of the
// SDK's own goroutines (an OnError callback), it does not wait: it reports
// whether a ruleset is held.
func (c *Client) Ready() bool { return c.ReadyContext(context.Background()) }

// ReadyContext is Ready that also gives up, reporting false, when ctx ends first.
func (c *Client) ReadyContext(ctx context.Context) (ready bool) {
	defer func() {
		if r := recover(); r != nil {
			ready = false
		}
	}()
	select {
	case <-c.firstDone:
		return c.firstReady
	default:
	}
	if c.own.contains() {
		return c.snap.Load() != nil
	}
	select {
	case <-c.firstDone:
		return c.firstReady
	case <-ctx.Done():
		return false
	}
}

func (c *Client) settleFirst(ok bool) {
	c.firstOnce.Do(func() {
		c.firstReady = ok
		close(c.firstDone)
	})
}

// Status is one consistent snapshot of what the SDK has loaded and how its
// stream is doing. It reads memory and makes no request.
func (c *Client) Status() Status {
	c.stMu.Lock()
	defer c.stMu.Unlock()
	s := Status{LastSyncAt: c.lastSyncAt, Stream: c.streamState, LastError: c.lastError}
	if sn := c.snap.Load(); sn != nil {
		s.ETag = sn.etag
	}
	if c.closing.Load() {
		s.Stream = StreamClosed
	}
	return s
}

// Close stops following the server, stops the flush loop, waits for both and
// for a flush already running, then sends the final usage counts. It is safe to
// call twice or from many goroutines: every call returns when the same
// shutdown is done, within about twice the request timeout. Evaluation still
// works afterwards but counts nothing. Called from one of the SDK's own
// goroutines it starts the shutdown and returns without waiting for it.
func (c *Client) Close() {
	safely(func() {
		c.closeOnce.Do(func() {
			c.closing.Store(true)
			go c.shutdown()
		})
		if c.own.contains() {
			return
		}
		<-c.done
	})
}

func (c *Client) shutdown() {
	defer close(c.done)
	defer c.own.enter()()
	c.cancel()
	c.loops.Wait()
	c.settleFirst(false)
	safely(c.finalFlush)
	if c.transport != nil {
		c.transport.CloseIdleConnections()
	}
}

// reportError hands an absorbed failure to OnError. It runs on the calling
// goroutine with no SDK lock held, and a panic in the callback is ignored.
func (c *Client) reportError(err error) {
	if c.onError == nil || err == nil {
		return
	}
	safely(func() { c.onError(err) })
}
