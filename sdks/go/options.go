package toggleslash

import (
	"net/http"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/eval"
	"github.com/madangryscientist/toggleslash/sdks/go/jsonval"
)

// The defaults of docs/sdk-contract.md section 2.
const (
	DefaultPollInterval   = 30 * time.Second
	DefaultFlushInterval  = 30 * time.Second
	DefaultRequestTimeout = 10 * time.Second

	// defaultWatchdog is contract 7.2's 60 s on an open stream.
	defaultWatchdog = 60 * time.Second

	// holdCap is contract 7.4's cap on a Retry-After wait.
	holdCap = 300 * time.Second

	// maxUsageEntries is contract 9's limit per events request.
	maxUsageEntries = 500

	// maxRulesetDepth is the client's JSON nesting limit, above 7.1's floor
	// of 512.
	maxRulesetDepth = 1024
)

// Hooks are the application callbacks of docs/sdk-contract.md section 10.
type Hooks struct {
	// AfterEvaluation runs after every variation and variationDetail call, a
	// fallback included, on the caller's goroutine and before the call
	// returns.
	AfterEvaluation func(Event)
}

// Options configures a client. Every field but URL and SDKKey has a default,
// and a nil or zero field means not given (docs/DESIGN.md section 1).
type Options struct {
	// URL is the server's base URL, with trailing `/` characters removed by
	// New.
	URL string
	// SDKKey is a server key. It is never logged, returned or put in an
	// error.
	SDKKey string
	// Stream follows GET /sdk/v1/stream. Nil means on; false polls only.
	Stream *bool
	// PollInterval is how often a poll happens when the stream is down or
	// off, and the longest reconnect wait. Zero means 30 s.
	PollInterval time.Duration
	// FlushInterval is how often usage counts are sent. Zero means 30 s.
	FlushInterval time.Duration
	// RequestTimeout bounds one request, body included. Zero means 10 s.
	RequestTimeout time.Duration
	// Hooks holds the application callbacks.
	Hooks Hooks
	// OnError is told about every failure the SDK absorbs. Nil ignores them.
	OnError func(error)

	// The rest are test seams (PROMPT.md, "Phase 3: client": an HTTP client,
	// a clock, a jitter source, a shorter watchdog, an evaluator that
	// raises). They are unexported because only this package's tests set
	// them, and nothing in the SDK reads the environment or a global.

	httpClient *http.Client
	clock      clock
	jitter     func() float64
	watchdog   time.Duration
	evaluate   func(*eval.Ruleset, string, jsonval.Value) eval.Result
}
