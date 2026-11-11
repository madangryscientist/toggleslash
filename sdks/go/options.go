package toggleslash

import (
	"log/slog"
	"math/rand/v2"
	"net/http"
	"time"
)

const (
	defaultInterval = 30 * time.Second
	defaultTimeout  = 10 * time.Second
	// streamIdle is how long the stream may go without a byte before it is dropped.
	streamIdle = 60 * time.Second
)

// Option changes how New builds a Client.
type Option func(*config)

type config struct {
	stream         bool
	pollInterval   time.Duration
	flushInterval  time.Duration
	requestTimeout time.Duration
	afterEvaluate  func(EvaluationEvent)
	onError        func(error)
	httpClient     *http.Client
	logger         *slog.Logger

	// Seams for the tests.
	clock    clock
	jitter   func() float64
	watchdog time.Duration
}

func defaultConfig() config {
	return config{
		stream:         true,
		pollInterval:   defaultInterval,
		flushInterval:  defaultInterval,
		requestTimeout: defaultTimeout,
		clock:          realClock{},
		jitter:         rand.Float64,
		watchdog:       streamIdle,
	}
}

// WithStream turns the stream on (the default) or off. Off polls only.
func WithStream(on bool) Option { return func(c *config) { c.stream = on } }

// WithPollInterval sets how often to poll when the stream is down or off, and
// the longest reconnect wait. Default 30 s; zero or less is the default.
func WithPollInterval(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.pollInterval = d
		}
	}
}

// WithFlushInterval sets how often usage counts are sent. Default 30 s; zero
// or less is the default.
func WithFlushInterval(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.flushInterval = d
		}
	}
}

// WithRequestTimeout sets the longest any one request may take, body
// included. Default 10 s; zero or less is the default.
func WithRequestTimeout(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.requestTimeout = d
		}
	}
}

// WithAfterEvaluation sets the hook called after every evaluation, once per
// call, on the caller's goroutine, before the call returns. A panic in it goes
// to the OnError callback and never reaches the caller. A nil function is ignored.
func WithAfterEvaluation(fn func(EvaluationEvent)) Option {
	return func(c *config) {
		if fn != nil {
			c.afterEvaluate = fn
		}
	}
}

// WithOnError sets the callback told about every failure the SDK absorbs: a
// failed ruleset, stream or usage request, a hook that panicked, an
// evaluation that panicked and usage counts lost at Close. It runs on the
// goroutine that met the failure, with no SDK lock held. A panic in it is
// ignored. A nil function is ignored.
func WithOnError(fn func(error)) Option {
	return func(c *config) {
		if fn != nil {
			c.onError = fn
		}
	}
}

// WithLogger sends each failure to the logger as a warning when no
// WithOnError is given.
func WithLogger(l *slog.Logger) Option { return func(c *config) { c.logger = l } }

// WithHTTPClient makes the SDK send its requests with a client of the
// caller's. The SDK copies it, turns off its timeout (the SDK bounds each
// request itself) and makes it not follow redirects.
func WithHTTPClient(h *http.Client) Option { return func(c *config) { c.httpClient = h } }

func withClock(k clock) Option { return func(c *config) { c.clock = k } }

func withJitter(fn func() float64) Option { return func(c *config) { c.jitter = fn } }

func withWatchdog(d time.Duration) Option { return func(c *config) { c.watchdog = d } }
