package toggleslash

import (
	"net/http"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
)

// Defaults of contract section 2.
const (
	defaultPollInterval   = 30 * time.Second
	defaultFlushInterval  = 30 * time.Second
	defaultRequestTimeout = 10 * time.Second
	streamIdleTimeout     = 60 * time.Second
	backoffBase           = time.Second
	maxRetryAfter         = 300 * time.Second
	maxEventsPerRequest   = 500
)

// options is what New builds; each Option overwrites one field of the defaults.
type options struct {
	stream         bool
	pollInterval   time.Duration
	flushInterval  time.Duration
	requestTimeout time.Duration
	afterEval      func(EvaluationEvent)
	onError        func(error)

	// Seams for the tests of this package.
	jitter    func() float64
	now       func() time.Time
	watchdog  time.Duration
	evaluate  func(rs *evaluator.Ruleset, flagKey string, ctx Value) evaluator.Result
	transport func(*http.Transport)
}

func defaultOptions() options {
	return options{
		stream:         true,
		pollInterval:   defaultPollInterval,
		flushInterval:  defaultFlushInterval,
		requestTimeout: defaultRequestTimeout,
		watchdog:       streamIdleTimeout,
	}
}

// An Option changes one setting of New.
type Option func(*options)

// WithStream turns the live stream on (the default) or off. With it off the SDK polls the ruleset
// every poll interval.
func WithStream(on bool) Option { return func(o *options) { o.stream = on } }

// WithPollInterval sets how often the ruleset is polled when the stream is off or down. The default is
// 30 seconds; a value of zero or less is ignored.
func WithPollInterval(d time.Duration) Option {
	return func(o *options) {
		if d > 0 {
			o.pollInterval = d
		}
	}
}

// WithFlushInterval sets how often usage counts are sent. The default is 30 seconds; a value of zero
// or less is ignored.
func WithFlushInterval(d time.Duration) Option {
	return func(o *options) {
		if d > 0 {
			o.flushInterval = d
		}
	}
}

// WithRequestTimeout bounds every request, from connecting to the last byte of the body (a stream is
// bounded until its headers arrive). The default is 10 seconds; a value of zero or less is ignored.
func WithRequestTimeout(d time.Duration) Option {
	return func(o *options) {
		if d > 0 {
			o.requestTimeout = d
		}
	}
}

// WithAfterEvaluation installs a hook that runs on the calling goroutine after every evaluation,
// fallbacks included, before the evaluation method returns. An error or panic in it goes to the
// onError function and changes nothing the caller sees.
func WithAfterEvaluation(fn func(EvaluationEvent)) Option {
	return func(o *options) {
		if fn != nil {
			o.afterEval = fn
		}
	}
}

// WithOnError installs the function every failure the SDK absorbs is passed to. It runs on the
// goroutine where the failure happened with no SDK lock held, and one that panics is ignored. The
// errors carry no URL, key, context or flag key.
func WithOnError(fn func(error)) Option {
	return func(o *options) {
		if fn != nil {
			o.onError = fn
		}
	}
}

func withJitter(fn func() float64) Option { return func(o *options) { o.jitter = fn } }
func withNow(fn func() time.Time) Option  { return func(o *options) { o.now = fn } }
func withWatchdog(d time.Duration) Option { return func(o *options) { o.watchdog = d } }

func withEvaluator(fn func(*evaluator.Ruleset, string, Value) evaluator.Result) Option {
	return func(o *options) { o.evaluate = fn }
}

func withTransport(fn func(*http.Transport)) Option { return func(o *options) { o.transport = fn } }
