// Package toggleslash is the ToggleSlash server SDK for Go.
//
// A Client keeps one ruleset current by following the server's stream (polling when the stream is down) and
// evaluates flags locally, so the typed methods (BoolVariation, StringVariation, IntVariation, Float64Variation
// and JSONVariation, each with a ...Detail twin) do no I/O and never block. Usage counts, which hold no context
// and nothing about an end user, are sent in batches. Nothing here panics at a caller and no method returns an
// error: a failure becomes the fallback you passed, with a reason, and is told to the WithOnError callback.
//
//	client := toggleslash.New("https://flags.example.com", os.Getenv("TOGGLESLASH_SDK_KEY"))
//	defer client.Close()
//	client.Ready()
//	on := client.BoolVariation("new-checkout", toggleslash.UserContext("user-1"), false)
//
// The contract is docs/sdk-contract.md in the ToggleSlash repository, and sdks/go/DESIGN.md maps each of its
// names to Go.
package toggleslash

import (
	"net/http"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/eval"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

const (
	defaultInterval = 30 * time.Second
	defaultTimeout  = 10 * time.Second
	// streamIdle is how long the stream may go without a byte (the server sends a keep-alive every 25 s).
	streamIdle = 60 * time.Second
	// maxRetryAfter is the longest wait a 429 can ask for.
	maxRetryAfter = 300 * time.Second
	backoffBase   = time.Second
)

// config holds the options after the defaults. The seams (the fields after onError) are for tests in this
// package: they are not options a caller can pass.
type config struct {
	url             string
	sdkKey          string
	stream          bool
	pollInterval    time.Duration
	flushInterval   time.Duration
	requestTimeout  time.Duration
	afterEvaluation func(EvaluationEvent)
	onError         func(error)

	httpClient *http.Client
	now        func() time.Time
	jitter     func() float64
	idle       time.Duration
	evaluate   func(*eval.Ruleset, string, jsonvalue.Value) eval.Result
}

// Option configures a Client.
type Option func(*config)

// WithStream turns the stream on or off. It is on by default; off polls only.
func WithStream(on bool) Option { return func(c *config) { c.stream = on } }

// WithPollInterval sets how often the ruleset is polled when the stream is down or off, and the longest
// reconnect wait. The default is 30 seconds; zero or negative is the default.
func WithPollInterval(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.pollInterval = d
		}
	}
}

// WithFlushInterval sets how often usage counts are sent. The default is 30 seconds; zero or negative is
// the default.
func WithFlushInterval(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.flushInterval = d
		}
	}
}

// WithRequestTimeout sets the longest any one request may take, and so the longest Ready waits. The
// default is 10 seconds; zero or negative is the default.
func WithRequestTimeout(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.requestTimeout = d
		}
	}
}

// WithAfterEvaluation sets a function called synchronously, on the caller's goroutine, after every
// variation method, a fallback included, with what the caller is about to get. A panic in it goes to the
// OnError function and never reaches the caller, who gets the same answer and the same usage count with or
// without it. A nil function is ignored.
func WithAfterEvaluation(f func(EvaluationEvent)) Option {
	return func(c *config) {
		if f != nil {
			c.afterEvaluation = f
		}
	}
}

// WithOnError sets a function told about every failure the SDK absorbs (a failed sync, a failed usage
// request, a hook that panicked). The value is an *Error. It is never needed for correctness; a panic in it is
// ignored. A nil function is ignored.
func WithOnError(f func(error)) Option {
	return func(c *config) {
		if f != nil {
			c.onError = f
		}
	}
}
