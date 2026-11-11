package toggleslash

import (
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
)

const (
	defaultInterval       = 30 * time.Second
	defaultRequestTimeout = 10 * time.Second
	defaultWatchdog       = 60 * time.Second
)

// config is everything New reads. The fields from clock down are test seams: nothing exported sets them.
type config struct {
	stream          bool
	pollInterval    time.Duration
	flushInterval   time.Duration
	requestTimeout  time.Duration
	afterEvaluation func(EvaluationEvent)
	onError         func(error)
	transport       http.RoundTripper

	clock    clock
	jitter   func() float64
	watchdog time.Duration
	evaluate func(*evaluator.Ruleset, string, Value) evaluator.Result
}

func defaultConfig() config {
	return config{
		stream:         true,
		pollInterval:   defaultInterval,
		flushInterval:  defaultInterval,
		requestTimeout: defaultRequestTimeout,
		clock:          realClock{},
		jitter:         rand.Float64,
		watchdog:       defaultWatchdog,
		evaluate:       (*evaluator.Ruleset).Evaluate,
	}
}

// Option changes how New builds a client.
type Option func(*config)

// WithStream follows the server's stream for live updates (the default is true). False polls only.
func WithStream(on bool) Option { return func(c *config) { c.stream = on } }

// WithPollInterval sets how often the ruleset is polled while the stream is down or off, and the longest wait
// before a stream reconnect (default 30 seconds). A value that is zero or negative is ignored.
func WithPollInterval(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.pollInterval = d
		}
	}
}

// WithFlushInterval sets how often usage counts are sent (default 30 seconds). A value that is zero or
// negative is ignored.
func WithFlushInterval(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.flushInterval = d
		}
	}
}

// WithRequestTimeout sets the longest any one request may take, body included, and so the longest Ready and
// Close wait (default 10 seconds). A value that is zero or negative is ignored.
func WithRequestTimeout(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.requestTimeout = d
		}
	}
}

// WithAfterEvaluation calls f on the calling goroutine after every evaluation, a fallback included, before
// the accessor returns. A panic in f goes to the error handler and never reaches the caller. The SDK has no
// OpenTelemetry dependency; f is where a caller adds one.
func WithAfterEvaluation(f func(EvaluationEvent)) Option {
	return func(c *config) { c.afterEvaluation = f }
}

// WithOnError sets the function told about every failure the SDK absorbs. It is never needed for
// correctness. A panic in it is ignored, and no SDK lock is held while it runs, so it may call the client.
func WithOnError(f func(error)) Option { return func(c *config) { c.onError = f } }

// WithTransport replaces the HTTP transport (the default is a clone of http.DefaultTransport with
// compression off). Redirects are never followed whatever the transport.
func WithTransport(t http.RoundTripper) Option { return func(c *config) { c.transport = t } }
