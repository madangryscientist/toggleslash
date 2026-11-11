// Package toggleslash is the Go server SDK for ToggleSlash, a feature flag service.
//
// A Client keeps one environment's flag rules in memory, current with the server, and evaluates flags
// locally for a context you pass. It also counts which variations it served and reports the counts. The
// rules it follows are docs/sdk-contract.md; evaluation itself is docs/evaluation.md.
//
//	client := toggleslash.NewClient("https://flags.example.com", sdkKey)
//	defer client.Close()
//	client.Ready()
//	ctx := toggleslash.Object(toggleslash.Member{Key: "key", Value: toggleslash.String("user-42")})
//	if client.BoolVariation("new-checkout", ctx, false) { ... }
//
// No method panics or returns an error: a failure becomes the caller's fallback with an ERROR reason, or
// goes to the function given to WithOnError, or both. Evaluation never does I/O and never waits. Call
// Close when the client is no longer needed; a client that is not closed keeps two goroutines and its
// connections until the process exits.
package toggleslash

import (
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/eval"
)

// Defaults of the canonical options (docs/sdk-contract.md section 2).
const (
	defaultPollInterval   = 30 * time.Second
	defaultFlushInterval  = 30 * time.Second
	defaultRequestTimeout = 10 * time.Second
	// defaultWatchdog: the server sends a keep-alive every 25 s; this long without a byte and the stream is
	// dead (contract 7.2).
	defaultWatchdog = 60 * time.Second
)

// config is the options after they are applied. The fields from clock down are test seams: they are
// unexported options, set only by this package's tests.
type config struct {
	stream          bool
	pollInterval    time.Duration
	flushInterval   time.Duration
	requestTimeout  time.Duration
	afterEvaluation func(EvaluationEvent)
	onError         func(error)
	httpClient      *http.Client

	clock     clock
	jitter    func() float64
	watchdog  time.Duration
	evaluator func(*eval.Ruleset, string, Context) eval.Result
}

func defaultConfig() config {
	return config{
		stream:         true,
		pollInterval:   defaultPollInterval,
		flushInterval:  defaultFlushInterval,
		requestTimeout: defaultRequestTimeout,
		clock:          realClock{},
		jitter:         rand.Float64,
		watchdog:       defaultWatchdog,
		evaluator: func(rs *eval.Ruleset, key string, ctx Context) eval.Result {
			return rs.Evaluate(key, ctx)
		},
	}
}

// An Option changes one setting of NewClient.
type Option func(*config)

// WithStream turns the live-update stream on or off. The default is on; off polls only.
func WithStream(on bool) Option { return func(c *config) { c.stream = on } }

// WithPollInterval sets how often the client polls when the stream is down or off, and the longest wait
// before it reconnects the stream. The default is 30 s; a value of zero or less is the default.
func WithPollInterval(d time.Duration) Option { return func(c *config) { c.pollInterval = d } }

// WithFlushInterval sets how often usage counts are sent. The default is 30 s; a value of zero or less is
// the default.
func WithFlushInterval(d time.Duration) Option { return func(c *config) { c.flushInterval = d } }

// WithRequestTimeout sets the longest any one request may take, its body included. The default is 10 s; a
// value of zero or less is the default.
func WithRequestTimeout(d time.Duration) Option { return func(c *config) { c.requestTimeout = d } }

// WithAfterEvaluation sets a function called after every evaluation, a fallback included, once per call, on
// the caller's goroutine, before the accessor returns. A panic in it goes to the OnError function and never
// reaches the caller. The event carries the context the caller passed; the client sends none of it anywhere.
func WithAfterEvaluation(f func(EvaluationEvent)) Option {
	return func(c *config) { c.afterEvaluation = f }
}

// WithOnError sets a function told about every failure the client absorbs: a failed ruleset or stream
// attempt (not one that Close abandoned), a failed usage request, a panicking evaluation, a panicking hook
// and the counts lost at Close. It runs on the goroutine that met the failure, with no client lock held,
// and a panic in it is recovered and ignored.
func WithOnError(f func(error)) Option { return func(c *config) { c.onError = f } }

// WithHTTPClient sets the HTTP client for every request, for a proxy or a private certificate authority.
// The client is copied and its redirect policy replaced: a redirect is never followed, so the key is
// never sent to another address and a 3xx is an "http_3xx" failure. Its Timeout field is ignored in
// favour of WithRequestTimeout, which is also what bounds a stream's first answer.
func WithHTTPClient(h *http.Client) Option { return func(c *config) { c.httpClient = h } }

// The unexported options below are the test seams (docs/sdk-contract.md section 13).

func withClock(k clock) Option            { return func(c *config) { c.clock = k } }
func withJitter(f func() float64) Option  { return func(c *config) { c.jitter = f } }
func withWatchdog(d time.Duration) Option { return func(c *config) { c.watchdog = d } }
func withEvaluator(f func(*eval.Ruleset, string, Context) eval.Result) Option {
	return func(c *config) { c.evaluator = f }
}

// clock is the time the client waits on and reads. Request deadlines and the stream's watchdog use real
// time: they are about a real socket.
type clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) stopper
}

type stopper interface{ Stop() bool }

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) AfterFunc(d time.Duration, f func()) stopper { return time.AfterFunc(d, f) }
