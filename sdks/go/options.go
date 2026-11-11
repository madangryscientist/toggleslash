package toggleslash

import (
	"net/http"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
)

// Defaults of the options, in the contract's section 2.
const (
	DefaultPollInterval   = 30 * time.Second
	DefaultFlushInterval  = 30 * time.Second
	DefaultRequestTimeout = 10 * time.Second
)

type config struct {
	stream         bool
	pollInterval   time.Duration
	flushInterval  time.Duration
	requestTimeout time.Duration
	hook           func(EvaluationEvent)
	onError        func(error)
	httpClient     *http.Client

	// Seams for tests.
	now      func() time.Time
	jitter   func() float64
	watchdog time.Duration
	evaluate func(*evaluator.Ruleset, string, Context) evaluator.Result
}

// Option configures New.
type Option func(*config)

// WithStream turns the live-update stream on or off. It is on by default; off makes the SDK poll.
func WithStream(on bool) Option { return func(c *config) { c.stream = on } }

// WithPollInterval sets how often the ruleset is polled while the stream is off or down. A
// non-positive duration is ignored. The default is 30 seconds.
func WithPollInterval(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.pollInterval = d
		}
	}
}

// WithFlushInterval sets how often usage counts are sent. A non-positive duration is ignored. The
// default is 30 seconds.
func WithFlushInterval(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.flushInterval = d
		}
	}
}

// WithRequestTimeout bounds every request to the server, body included. A non-positive duration is
// ignored. The default is 10 seconds.
func WithRequestTimeout(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.requestTimeout = d
		}
	}
}

// WithAfterEvaluation sets the hook that runs, on the caller's goroutine, after every
// Variation and VariationDetail, a fallback included. A panic in it goes to the onError callback
// and changes nothing else.
func WithAfterEvaluation(fn func(EvaluationEvent)) Option {
	return func(c *config) { c.hook = fn }
}

// WithOnError sets the callback that receives every error the SDK handles: a failed sync, a failed
// flush, a panic in the hook or the evaluator. The value is an *Error. A panic in the callback is
// ignored. It runs on an SDK goroutine or the caller's, and must not block for long.
func WithOnError(fn func(error)) Option { return func(c *config) { c.onError = fn } }

// WithHTTPClient sets the client the SDK sends requests with (a custom transport, a root CA for
// TLS). The SDK copies it and sets its own redirect policy (a redirect is an error) and no overall
// timeout (the SDK bounds each request itself).
func WithHTTPClient(hc *http.Client) Option { return func(c *config) { c.httpClient = hc } }

func defaultConfig() config {
	return config{
		stream:         true,
		pollInterval:   DefaultPollInterval,
		flushInterval:  DefaultFlushInterval,
		requestTimeout: DefaultRequestTimeout,
		now:            time.Now,
		watchdog:       streamWatchdog,
		evaluate:       evaluator.Evaluate,
	}
}

// streamWatchdog is how long a stream may be silent before it is dropped (the server sends a
// keep-alive well inside it).
const streamWatchdog = 60 * time.Second

// The options below are seams for the package's own tests.

func withClock(now func() time.Time) Option { return func(c *config) { c.now = now } }

func withJitter(fn func() float64) Option { return func(c *config) { c.jitter = fn } }

func withWatchdog(d time.Duration) Option { return func(c *config) { c.watchdog = d } }

func withEvaluate(fn func(*evaluator.Ruleset, string, Context) evaluator.Result) Option {
	return func(c *config) { c.evaluate = fn }
}
