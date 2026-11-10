package toggleslash

import (
	"context"
	"net/http"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
)

// The defaults of contract 2.
const (
	defaultPollInterval   = 30 * time.Second
	defaultFlushInterval  = 30 * time.Second
	defaultRequestTimeout = 10 * time.Second
	defaultWatchdog       = 60 * time.Second
	defaultRetryAfterCap  = 300 * time.Second
)

// Option configures Create.
type Option func(*config)

type config struct {
	url             string
	sdkKey          string
	stream          bool
	pollInterval    time.Duration
	flushInterval   time.Duration
	requestTimeout  time.Duration
	afterEvaluation func(EvaluationEvent)
	onError         func(error)

	// Test seams. They are unexported options, never a global and never read from the environment.
	httpClient    *http.Client
	now           func() time.Time
	sleep         func(ctx context.Context, d time.Duration) bool
	jitter        func() float64
	watchdog      time.Duration
	retryAfterCap time.Duration
	evaluate      func(rs *evaluator.Ruleset, flagKey string, ctx Context) evaluator.Result
}

// WithStream follows the server's stream (the default is on). Off means poll only.
func WithStream(on bool) Option { return func(c *config) { c.stream = on } }

// WithPollInterval sets how often to poll when the stream is down or off, and the longest reconnect
// wait (default 30 s). A value of zero or less is the default.
func WithPollInterval(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.pollInterval = d
		}
	}
}

// WithFlushInterval sets how often usage counts are sent (default 30 s). A value of zero or less is
// the default.
func WithFlushInterval(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.flushInterval = d
		}
	}
}

// WithRequestTimeout sets the longest any one request may take, body included (default 10 s). A value
// of zero or less is the default.
func WithRequestTimeout(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.requestTimeout = d
		}
	}
}

// WithAfterEvaluation sets the hook called after every evaluation, on the caller's goroutine, before
// the evaluation returns. An error it raises (a panic) goes to the onError function.
func WithAfterEvaluation(hook func(EvaluationEvent)) Option {
	return func(c *config) { c.afterEvaluation = hook }
}

// WithOnError sets the function told about every failure the SDK absorbs. It is for logging and
// metrics. A panic in it is ignored. See the README for which goroutine calls it.
func WithOnError(fn func(error)) Option { return func(c *config) { c.onError = fn } }

func withHTTPClient(h *http.Client) Option  { return func(c *config) { c.httpClient = h } }
func withClock(now func() time.Time) Option { return func(c *config) { c.now = now } }
func withSleep(sleep func(ctx context.Context, d time.Duration) bool) Option {
	return func(c *config) { c.sleep = sleep }
}
func withJitter(j func() float64) Option  { return func(c *config) { c.jitter = j } }
func withWatchdog(d time.Duration) Option { return func(c *config) { c.watchdog = d } }
func withEvaluator(f func(rs *evaluator.Ruleset, flagKey string, ctx Context) evaluator.Result) Option {
	return func(c *config) { c.evaluate = f }
}
