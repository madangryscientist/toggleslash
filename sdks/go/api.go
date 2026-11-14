package toggleslash

import (
	"context"
	"errors"
	"math"
	"strconv"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/eval"
	"github.com/madangryscientist/toggleslash/sdks/go/jsonval"
)

// ReasonKind is a reason's kind, spelled as docs/evaluation.md spells it.
type ReasonKind string

// The reason kinds of docs/evaluation.md "Result".
const (
	ReasonOff                ReasonKind = "OFF"
	ReasonPrerequisiteFailed ReasonKind = "PREREQUISITE_FAILED"
	ReasonTargetMatch        ReasonKind = "TARGET_MATCH"
	ReasonRuleMatch          ReasonKind = "RULE_MATCH"
	ReasonFallthrough        ReasonKind = "FALLTHROUGH"
	ReasonError              ReasonKind = "ERROR"
)

// ErrorCode is the code an ERROR reason carries.
type ErrorCode string

// The evaluator's codes and the SDK's own (docs/sdk-contract.md section 4).
const (
	CodeInvalidContext            ErrorCode = "INVALID_CONTEXT"
	CodeFlagNotFound              ErrorCode = "FLAG_NOT_FOUND"
	CodeSegmentNotFound           ErrorCode = "SEGMENT_NOT_FOUND"
	CodeMalformedFlag             ErrorCode = "MALFORMED_FLAG"
	CodeMalformedSegment          ErrorCode = "MALFORMED_SEGMENT"
	CodePrerequisiteCycle         ErrorCode = "PREREQUISITE_CYCLE"
	CodePrerequisiteDepthExceeded ErrorCode = "PREREQUISITE_DEPTH_EXCEEDED"

	CodeNotReady         ErrorCode = "NOT_READY"
	CodeTypeMismatch     ErrorCode = "TYPE_MISMATCH"
	CodeEvaluationFailed ErrorCode = "EVALUATION_FAILED"
)

// Reason says why a value was served, or which error stopped the evaluation.
// Code is empty unless Kind is ReasonError, RuleIndex is nil unless Kind is
// ReasonRuleMatch, and FlagKey names the flag that failed, which is a
// prerequisite's key when Kind is ReasonPrerequisiteFailed.
type Reason struct {
	Kind      ReasonKind
	RuleIndex *int
	FlagKey   string
	Code      ErrorCode
}

// Detail is one variationDetail answer: the value, the variation it came from
// (nil for none) and the reason.
type Detail[T any] struct {
	Value          T
	VariationIndex *int
	Reason         Reason
}

// Event is what AfterEvaluation receives (docs/sdk-contract.md section 10).
type Event struct {
	// FlagKey is the key the caller asked for.
	FlagKey string
	// Context is the context the caller passed. The SDK sends none of it
	// anywhere.
	Context Context
	// Value is what the caller is about to get, as the JSON value type.
	Value jsonval.Value
	// VariationIndex is nil for a fallback.
	VariationIndex *int
	// Reason is the reason the caller gets.
	Reason Reason
	// ETag is the ETag of the ruleset this evaluation used, and empty before
	// the first load.
	ETag string
	// FlagVersion is the flag's version in that ruleset, or nil when the
	// ruleset has no such flag or the flag is malformed.
	FlagVersion *int64
}

// StreamState is what the stream is doing (docs/sdk-contract.md section 8).
type StreamState string

// The stream states of docs/sdk-contract.md section 8.
const (
	StreamOff          StreamState = "off"
	StreamConnecting   StreamState = "connecting"
	StreamConnected    StreamState = "connected"
	StreamDisconnected StreamState = "disconnected"
	StreamClosed       StreamState = "closed"
)

// LastError is why the most recent ruleset or stream attempt failed.
type LastError string

// The fixed last error codes of docs/sdk-contract.md section 8.
const (
	LastErrorTimeout    LastError = "timeout"
	LastErrorBadRuleset LastError = "bad_ruleset"
	LastErrorNetwork    LastError = "network"
)

// HTTPLastError builds the open ended `http_<status>` code.
func HTTPLastError(status int) LastError {
	return LastError("http_" + strconv.Itoa(status))
}

// Status is what the SDK has loaded and how its stream is doing. It reads
// memory only.
type Status struct {
	// ETag is the ETag of the ruleset held, as received; empty means none.
	ETag string
	// LastSyncAt is when the SDK last confirmed its ruleset matches the
	// server; nil means never.
	LastSyncAt *time.Time
	// Stream is the stream state.
	Stream StreamState
	// LastError is why the most recent ruleset or stream attempt failed; nil
	// means none.
	LastError *LastError
}

// New builds a client and starts its loops. It does not wait for the network;
// its only errors are a missing URL or SDK key.
func New(opts Options) (*Client, error) {
	if opts.URL == "" {
		return nil, errors.New("toggleslash: Options.URL is required")
	}
	if opts.SDKKey == "" {
		return nil, errors.New("toggleslash: Options.SDKKey is required")
	}
	for len(opts.URL) > 0 && opts.URL[len(opts.URL)-1] == '/' {
		opts.URL = opts.URL[:len(opts.URL)-1]
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = DefaultPollInterval
	}
	if opts.FlushInterval <= 0 {
		opts.FlushInterval = DefaultFlushInterval
	}
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = DefaultRequestTimeout
	}
	if opts.clock == nil {
		opts.clock = systemClock{}
	}
	if opts.jitter == nil {
		opts.jitter = defaultJitter
	}
	if opts.watchdog <= 0 {
		opts.watchdog = defaultWatchdog
	}
	if opts.evaluate == nil {
		opts.evaluate = func(ruleset *eval.Ruleset, flagKey string, context jsonval.Value) eval.Result {
			return ruleset.Evaluate(flagKey, context)
		}
	}
	stream := true
	if opts.Stream != nil {
		stream = *opts.Stream
	}

	c := &Client{
		opts:    opts,
		stream:  stream,
		http:    opts.httpClient,
		readyCh: make(chan struct{}),
		usage:   map[usageKey]int64{},
	}
	if c.http == nil {
		c.http = newHTTPClient()
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.queueCond = newCond(&c.queueMu)
	c.status.Store(&statusSnapshot{stream: c.initialStreamState()})

	c.wg.Add(2)
	go c.syncLoop()
	go c.flushLoop()
	go c.notifyLoop()
	return c, nil
}

func (c *Client) initialStreamState() StreamState {
	if c.stream {
		return StreamConnecting
	}
	return StreamOff
}

// Ready waits for the first ruleset load to end and answers whether it worked.
// The answer is fixed when that load ends; ctx bounds the wait only.
func (c *Client) Ready(ctx context.Context) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	// The answer is fixed once the first load ends (contract 5.3), so a
	// settled ready wins over a context that is already done: the select below
	// would otherwise pick between two ready cases at random.
	select {
	case <-c.readyCh:
		return c.readyOK.Load()
	default:
	}
	select {
	case <-c.readyCh:
		return c.readyOK.Load()
	case <-ctx.Done():
		return false
	case <-c.ctx.Done():
		// Close settles an unsettled ready false; wait for that settlement so
		// every caller sees the same answer.
		select {
		case <-c.readyCh:
			return c.readyOK.Load()
		default:
			return false
		}
	}
}

// Status reports what the client holds, reading memory only.
func (c *Client) Status() Status {
	snapshot := c.status.Load()
	status := Status{ETag: snapshot.etag, Stream: snapshot.stream}
	if snapshot.hasLastSync {
		at := snapshot.lastSyncAt
		status.LastSyncAt = &at
	}
	if snapshot.lastError != "" {
		code := snapshot.lastError
		status.LastError = &code
	}
	return status
}

// Close stops every loop, waits for them and sends the pending usage counts.
// It never fails and is safe to call twice or from several goroutines. Called
// from inside OnError, it stops following the server and returns at once; the
// rest of shutdown, the final flush included, finishes on a goroutine of its
// own (contract 6.4).
func (c *Client) Close() {
	if c.inOnError.Load() > 0 {
		c.stopFollowing()
		go c.shutDown()
		return
	}
	c.shutDown()
}

func (c *Client) stopFollowing() {
	c.closed.Store(true)
	c.updateStatus(func(*statusSnapshot) {})
	c.settleReady(false)
	c.cancel()
}

func (c *Client) shutDown() {
	c.closeOnce.Do(func() {
		c.stopFollowing()
		c.wg.Wait()
		c.queueMu.Lock()
		c.queueClosed = true
		c.queueCond.Broadcast()
		c.queueMu.Unlock()
	})
}

// BoolVariation is boolVariationDetail's value, with the fallback on failure.
func (c *Client) BoolVariation(flagKey string, context Context, fallback bool) bool {
	return c.boolDetail(flagKey, context, fallback).Value
}

// BoolVariationDetail is variationDetail for a boolean.
func (c *Client) BoolVariationDetail(flagKey string, context Context, fallback bool) Detail[bool] {
	return c.boolDetail(flagKey, context, fallback)
}

func (c *Client) boolDetail(flagKey string, context Context, fallback bool) Detail[bool] {
	e := c.evaluateFlag(flagKey, context, func(value jsonval.Value) bool {
		_, ok := value.(jsonval.Bool)
		return ok
	})
	c.after(e, flagKey, context, jsonval.Bool(fallback))
	if e.served {
		value, _ := e.value.(jsonval.Bool)
		return Detail[bool]{Value: bool(value), VariationIndex: e.index, Reason: e.reason}
	}
	return Detail[bool]{Value: fallback, VariationIndex: e.index, Reason: e.reason}
}

// StringVariation is stringVariationDetail's value, with the fallback on
// failure.
func (c *Client) StringVariation(flagKey string, context Context, fallback string) string {
	return c.stringDetail(flagKey, context, fallback).Value
}

// StringVariationDetail is variationDetail for a string.
func (c *Client) StringVariationDetail(flagKey string, context Context, fallback string) Detail[string] {
	return c.stringDetail(flagKey, context, fallback)
}

func (c *Client) stringDetail(flagKey string, context Context, fallback string) Detail[string] {
	e := c.evaluateFlag(flagKey, context, func(value jsonval.Value) bool {
		_, ok := value.(jsonval.String)
		return ok
	})
	c.after(e, flagKey, context, jsonval.String(fallback))
	if e.served {
		value, _ := e.value.(jsonval.String)
		return Detail[string]{Value: string(value), VariationIndex: e.index, Reason: e.reason}
	}
	return Detail[string]{Value: fallback, VariationIndex: e.index, Reason: e.reason}
}

// IntVariation is intVariationDetail's value, with the fallback on failure.
func (c *Client) IntVariation(flagKey string, context Context, fallback int64) int64 {
	return c.intDetail(flagKey, context, fallback).Value
}

// IntVariationDetail is variationDetail for a signed 64-bit integer. A served
// number with a fraction or outside the type's range is a TYPE_MISMATCH
// (docs/sdk-contract.md section 4, "Typed languages").
func (c *Client) IntVariationDetail(flagKey string, context Context, fallback int64) Detail[int64] {
	return c.intDetail(flagKey, context, fallback)
}

func (c *Client) intDetail(flagKey string, context Context, fallback int64) Detail[int64] {
	e := c.evaluateFlag(flagKey, context, integral)
	c.after(e, flagKey, context, jsonval.Number(float64(fallback)))
	if e.served {
		value, _ := e.value.(jsonval.Number)
		return Detail[int64]{Value: int64(value), VariationIndex: e.index, Reason: e.reason}
	}
	return Detail[int64]{Value: fallback, VariationIndex: e.index, Reason: e.reason}
}

// FloatVariation is floatVariationDetail's value, with the fallback on
// failure.
func (c *Client) FloatVariation(flagKey string, context Context, fallback float64) float64 {
	return c.floatDetail(flagKey, context, fallback).Value
}

// FloatVariationDetail is variationDetail for a number.
func (c *Client) FloatVariationDetail(flagKey string, context Context, fallback float64) Detail[float64] {
	return c.floatDetail(flagKey, context, fallback)
}

func (c *Client) floatDetail(flagKey string, context Context, fallback float64) Detail[float64] {
	e := c.evaluateFlag(flagKey, context, func(value jsonval.Value) bool {
		_, ok := value.(jsonval.Number)
		return ok
	})
	c.after(e, flagKey, context, jsonval.Number(fallback))
	if e.served {
		value, _ := e.value.(jsonval.Number)
		return Detail[float64]{Value: float64(value), VariationIndex: e.index, Reason: e.reason}
	}
	return Detail[float64]{Value: fallback, VariationIndex: e.index, Reason: e.reason}
}

// JSONVariation is jsonVariationDetail's value, with the fallback on failure.
func (c *Client) JSONVariation(flagKey string, context Context, fallback jsonval.Value) jsonval.Value {
	return c.jsonDetail(flagKey, context, fallback).Value
}

// JSONVariationDetail is variationDetail for any JSON value. A null fallback
// accepts a served value of any kind; any other fallback compares kinds.
func (c *Client) JSONVariationDetail(flagKey string, context Context, fallback jsonval.Value) Detail[jsonval.Value] {
	return c.jsonDetail(flagKey, context, fallback)
}

func (c *Client) jsonDetail(flagKey string, context Context, fallback jsonval.Value) Detail[jsonval.Value] {
	accept := func(value jsonval.Value) bool { return true }
	if !isNull(fallback) {
		want := kindOf(fallback)
		accept = func(value jsonval.Value) bool { return kindOf(value) == want }
	}
	e := c.evaluateFlag(flagKey, context, accept)
	c.after(e, flagKey, context, fallback)
	if e.served {
		// An Array or Object is a pointer to shared storage: the caller gets a
		// copy, so mutating it cannot change the ruleset held.
		return Detail[jsonval.Value]{Value: jsonval.Clone(e.value), VariationIndex: e.index, Reason: e.reason}
	}
	return Detail[jsonval.Value]{Value: fallback, VariationIndex: e.index, Reason: e.reason}
}

// integral is the integer accessor's kind test: a whole served number within
// signed 64 bits. 2.0 is the integer 2, 2.5 and 2^63 are not.
func integral(value jsonval.Value) bool {
	number, ok := value.(jsonval.Number)
	if !ok {
		return false
	}
	f := float64(number)
	return f >= minInt64 && f < maxInt64 && f == math.Trunc(f)
}

const (
	minInt64 = -9223372036854775808.0
	maxInt64 = 9223372036854775808.0
)
