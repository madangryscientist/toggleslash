package toggleslash

import (
	"net/http"
	"time"
)

const (
	defaultPollInterval   = 30 * time.Second
	defaultFlushInterval  = 30 * time.Second
	defaultRequestTimeout = 10 * time.Second
	defaultWatchdog       = 60 * time.Second
	defaultRetryAfterCap  = 300 * time.Second
	maxBackoffExponent    = 16
)

type config struct {
	stream         bool
	pollInterval   time.Duration
	flushInterval  time.Duration
	requestTimeout time.Duration
	afterEval      func(EvaluationEvent)
	onError        func(error)

	// Seams for tests; the exported options never set them.
	httpClient    *http.Client
	watchdog      time.Duration
	retryAfterCap time.Duration
	random        func() float64
	now           func() time.Time
	// beforeEvaluate runs inside an evaluation, before the evaluator; a test makes it panic.
	beforeEvaluate func(flagKey string)
	// tweakTransport adjusts the transport New builds (a test adds a root certificate).
	tweakTransport func(*http.Transport)
}

// Option configures New.
type Option func(*config)

// WithStream turns the live stream on (the default) or off. With it off the client only polls.
func WithStream(on bool) Option { return func(c *config) { c.stream = on } }

// WithPollInterval sets how often the ruleset is polled (30 s by default). A value that is not positive is
// ignored.
func WithPollInterval(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.pollInterval = d
		}
	}
}

// WithFlushInterval sets how often usage counts are sent (30 s by default). A value that is not positive is
// ignored.
func WithFlushInterval(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.flushInterval = d
		}
	}
}

// WithRequestTimeout bounds a ruleset or events request, body included (10 s by default). A value that is
// not positive is ignored.
func WithRequestTimeout(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.requestTimeout = d
		}
	}
}

// WithAfterEvaluation installs a hook called on the caller's goroutine after every evaluation. A nil
// function is ignored.
func WithAfterEvaluation(f func(EvaluationEvent)) Option {
	return func(c *config) {
		if f != nil {
			c.afterEval = f
		}
	}
}

// WithOnError installs a function that receives every failure the SDK absorbs. A nil function is ignored.
func WithOnError(f func(error)) Option {
	return func(c *config) {
		if f != nil {
			c.onError = f
		}
	}
}

func newConfig(opts []Option) config {
	c := config{
		stream:         true,
		pollInterval:   defaultPollInterval,
		flushInterval:  defaultFlushInterval,
		requestTimeout: defaultRequestTimeout,
		watchdog:       defaultWatchdog,
		retryAfterCap:  defaultRetryAfterCap,
	}
	for _, o := range opts {
		if o != nil {
			o(&c)
		}
	}
	return c
}
