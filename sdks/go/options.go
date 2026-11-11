package toggleslash

import (
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

const (
	defaultPollInterval   = 30 * time.Second
	defaultFlushInterval  = 30 * time.Second
	defaultRequestTimeout = 10 * time.Second
	// streamIdle is how long the stream may stay silent before it is dropped. The server sends a keep-alive every
	// 25 seconds.
	streamIdle = 60 * time.Second
	// maxRetryAfter caps a Retry-After hold.
	maxRetryAfter = 300 * time.Second
	// backoffBase is the first reconnect ceiling.
	backoffBase = time.Second
	// maxBatch is the most usage entries one request carries.
	maxBatch = 500
)

// config is the client's settings. Only New writes it; nothing changes it afterwards.
type config struct {
	url             string
	sdkKey          string
	stream          bool
	pollInterval    time.Duration
	flushInterval   time.Duration
	requestTimeout  time.Duration
	afterEvaluation func(Event)
	onError         func(error)

	// Seams for tests; the public options never set them.
	httpClient *http.Client
	clock      clock
	random     func() float64
	idle       time.Duration
	evaluate   func(*evaluator.Ruleset, string, jsonv.Value) evaluator.Result
}

func defaultConfig(url, sdkKey string) config {
	return config{
		url:            strings.TrimRight(url, "/"),
		sdkKey:         sdkKey,
		stream:         true,
		pollInterval:   defaultPollInterval,
		flushInterval:  defaultFlushInterval,
		requestTimeout: defaultRequestTimeout,
		clock:          realClock{},
		random:         rand.Float64,
		idle:           streamIdle,
		evaluate: func(rs *evaluator.Ruleset, key string, ctx jsonv.Value) evaluator.Result {
			return rs.Evaluate(key, ctx)
		},
	}
}

// Option changes one setting of New.
type Option func(*config)

// WithStream turns the live stream on (the default) or off. Off, the SDK polls every poll interval.
func WithStream(on bool) Option { return func(c *config) { c.stream = on } }

// WithPollInterval sets how often the ruleset is polled when the stream is off or down, and the ceiling of the
// reconnect backoff. A value of zero or less is the default, 30 seconds.
func WithPollInterval(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.pollInterval = d
		}
	}
}

// WithFlushInterval sets how often usage counts are sent. A value of zero or less is the default, 30 seconds.
func WithFlushInterval(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.flushInterval = d
		}
	}
}

// WithRequestTimeout sets the longest any one request may take, body included, and so the longest Ready waits.
// A value of zero or less is the default, 10 seconds.
func WithRequestTimeout(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.requestTimeout = d
		}
	}
}

// WithAfterEvaluation sets a hook that runs once per evaluation, on the caller's goroutine, after the answer is
// known. A panic in it is recovered and goes to the onError callback; the evaluation is unchanged.
func WithAfterEvaluation(hook func(Event)) Option {
	return func(c *config) { c.afterEvaluation = hook }
}

// WithOnError sets a callback told about every failure the SDK absorbs. It is never needed for correctness; a
// panic in it is ignored. It runs on the goroutine that hit the failure.
func WithOnError(f func(error)) Option { return func(c *config) { c.onError = f } }

func withClock(k clock) Option             { return func(c *config) { c.clock = k } }
func withRandom(f func() float64) Option   { return func(c *config) { c.random = f } }
func withIdle(d time.Duration) Option      { return func(c *config) { c.idle = d } }
func withHTTPClient(h *http.Client) Option { return func(c *config) { c.httpClient = h } }
func withEvaluate(f func(*evaluator.Ruleset, string, jsonv.Value) evaluator.Result) Option {
	return func(c *config) { c.evaluate = f }
}
