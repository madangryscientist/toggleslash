// Package toggleslash is the ToggleSlash server SDK for Go.
//
// A Client keeps a copy of one environment's ruleset, follows the server for changes and evaluates flags
// locally, so an evaluation is a memory read: it never waits for the network. It keeps docs/sdk-contract.md
// and evaluates to docs/evaluation.md; DESIGN.md has the mapping of the contract's names to this package.
//
//	client := toggleslash.New("https://flags.example.com", sdkKey)
//	defer client.Close()
//	client.Ready()
//	on := client.BoolVariation("new-checkout", toggleslash.UserContext("u1", nil), false)
//
// Nothing in this package panics at a caller or returns an error from an evaluation: a failure becomes the
// caller's fallback with a Reason of kind ReasonError, or goes to the WithOnError callback, or both.
package toggleslash

import (
	"crypto/tls"
	"math/rand/v2"
	"strings"
	"time"
)

const (
	defaultPollInterval   = 30 * time.Second
	defaultFlushInterval  = 30 * time.Second
	defaultRequestTimeout = 10 * time.Second
	defaultIdle           = 60 * time.Second
)

// Option changes how New builds a Client.
type Option func(*config)

// config is read-only once New has returned: the goroutines read it without a lock.
type config struct {
	stream          bool
	pollInterval    time.Duration
	flushInterval   time.Duration
	requestTimeout  time.Duration
	afterEvaluation func(Event)
	onError         func(error)

	// Seams for tests in this package; there is no exported option for any of them.
	clock     clock
	jitter    func() float64 // in [0.5, 1)
	idle      time.Duration  // the stream watchdog
	tlsConfig *tls.Config
	evaluate  evaluateFunc
}

func defaultConfig() config {
	return config{
		stream:         true,
		pollInterval:   defaultPollInterval,
		flushInterval:  defaultFlushInterval,
		requestTimeout: defaultRequestTimeout,
		clock:          realClock{},
		jitter:         func() float64 { return 0.5 + rand.Float64()/2 },
		idle:           defaultIdle,
		evaluate:       defaultEvaluate,
	}
}

// WithStream turns the stream (GET /sdk/v1/stream) on or off. The default is on. Off means poll only.
func WithStream(on bool) Option { return func(c *config) { c.stream = on } }

// WithPollInterval sets how often the client polls when the stream is down or off, and the longest wait
// before it reconnects. The default is 30 s. A duration of zero or less is ignored.
func WithPollInterval(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.pollInterval = d
		}
	}
}

// WithFlushInterval sets how often usage counts are sent. The default is 30 s. A duration of zero or less
// is ignored.
func WithFlushInterval(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.flushInterval = d
		}
	}
}

// WithRequestTimeout sets the longest any one request may take, body included. The default is 10 s. A
// duration of zero or less is ignored.
func WithRequestTimeout(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.requestTimeout = d
		}
	}
}

// WithAfterEvaluation sets a callback that runs after every evaluation, a fallback included, once per
// call, on the goroutine that called the accessor, before the accessor returns. A panic in it goes to the
// WithOnError callback and the caller gets the same answer.
func WithAfterEvaluation(f func(Event)) Option { return func(c *config) { c.afterEvaluation = f } }

// WithOnError sets a callback told about every failure the client absorbs: a failed ruleset, stream or
// usage request, an evaluation that panicked, a callback that panicked and usage counts lost at Close. It
// runs on the client's own goroutine (the sync goroutine for a ruleset or stream failure, the flush
// goroutine for a usage failure, the goroutine that called Close for the final flush) or, for a panic in an
// evaluation or in WithAfterEvaluation, on the goroutine that called the accessor. No lock of the client is
// held when it runs. A panic in it is ignored.
func WithOnError(f func(error)) Option { return func(c *config) { c.onError = f } }

func withClock(k clock) Option { return func(c *config) { c.clock = k } }

func withJitter(f func() float64) Option { return func(c *config) { c.jitter = f } }

func withIdle(d time.Duration) Option { return func(c *config) { c.idle = d } }

func withTLSConfig(t *tls.Config) Option { return func(c *config) { c.tlsConfig = t } }

func withEvaluate(f evaluateFunc) Option { return func(c *config) { c.evaluate = f } }

func trimURL(u string) string { return strings.TrimRight(u, "/") }
