package toggleslash

import "time"

// ReasonKind is why a flag evaluated the way it did. The spelling is the contract's.
type ReasonKind string

// The reason kinds of docs/evaluation.md, spelled as the contract spells them.
const (
	ReasonOff                ReasonKind = "OFF"
	ReasonPrerequisiteFailed ReasonKind = "PREREQUISITE_FAILED"
	ReasonTargetMatch        ReasonKind = "TARGET_MATCH"
	ReasonRuleMatch          ReasonKind = "RULE_MATCH"
	ReasonFallthrough        ReasonKind = "FALLTHROUGH"
	ReasonError              ReasonKind = "ERROR"
)

// ErrorCode is the code of an ERROR reason: the evaluator's, then the SDK's own.
type ErrorCode string

// The error codes of docs/evaluation.md and contract section 4, spelled as they are written there.
const (
	CodeInvalidContext            ErrorCode = "INVALID_CONTEXT"
	CodeFlagNotFound              ErrorCode = "FLAG_NOT_FOUND"
	CodeSegmentNotFound           ErrorCode = "SEGMENT_NOT_FOUND"
	CodeMalformedFlag             ErrorCode = "MALFORMED_FLAG"
	CodeMalformedSegment          ErrorCode = "MALFORMED_SEGMENT"
	CodePrerequisiteCycle         ErrorCode = "PREREQUISITE_CYCLE"
	CodePrerequisiteDepthExceeded ErrorCode = "PREREQUISITE_DEPTH_EXCEEDED"
	CodeNotReady                  ErrorCode = "NOT_READY"
	CodeTypeMismatch              ErrorCode = "TYPE_MISMATCH"
	CodeEvaluationFailed          ErrorCode = "EVALUATION_FAILED"
)

// StreamState is how the stream is doing (contract section 8).
type StreamState string

// The stream states of contract section 8, spelled as the contract spells them.
const (
	StreamOff          StreamState = "off"
	StreamConnecting   StreamState = "connecting"
	StreamConnected    StreamState = "connected"
	StreamDisconnected StreamState = "disconnected"
	StreamClosed       StreamState = "closed"
)

// LastError is why the most recent ruleset or stream attempt failed (contract section 8). The three codes
// below are the fixed ones; an answer with another status is `http_<status>` (for example "http_429").
type LastError string

// The fixed lastError codes of contract section 8.
const (
	LastErrorTimeout    LastError = "timeout"
	LastErrorBadRuleset LastError = "bad_ruleset"
	LastErrorNetwork    LastError = "network"
)

// Reason is the evaluator's reason. Code is set for ReasonError, RuleIndex for ReasonRuleMatch (and only
// then) and FlagKey for ReasonPrerequisiteFailed.
type Reason struct {
	Kind      ReasonKind
	Code      ErrorCode
	RuleIndex int
	FlagKey   string
}

// Detail is what VariationDetail and the typed detail methods answer: the served value (or the caller's
// fallback), its variation index, and the reason. VariationIndex is nil whenever Value is the fallback.
type Detail struct {
	Value          any
	VariationIndex *int
	Reason         Reason
}

// Status is a snapshot of what the SDK has loaded. ETag and LastError are empty when there is none, and
// LastSyncAt is the zero time until the first sync. It never holds a URL, a key, an error message or a
// context.
type Status struct {
	ETag       string
	LastSyncAt time.Time
	Stream     StreamState
	LastError  LastError
}

// EvaluationEvent is what one evaluation did, handed to Hooks.AfterEvaluation. It carries the context the
// caller passed; the SDK itself sends none of it anywhere.
//
// Value is in the accessor's own type: an int64 for the integer accessor, a bool, a string, a float64 or a
// JSON value (any) for the others. FlagVersion is nil when the ruleset has no such flag or the flag is
// malformed.
type EvaluationEvent struct {
	FlagKey        string
	Context        Context
	Value          any
	VariationIndex *int
	Reason         Reason
	ETag           string
	FlagVersion    *int64
}

// Hooks are the SDK's observers. AfterEvaluation is called after every Variation and VariationDetail call, a
// fallback included, on the caller's goroutine, before the call returns. Its return value is ignored, and a
// panic in it goes to OnError.
type Hooks struct {
	AfterEvaluation func(EvaluationEvent)
}

// Context is a single context or a multi context, exactly as docs/evaluation.md defines them: a map whose
// values are nil, bool, string, the language's numeric types, []any or map[string]any. No "kind" means
// "user". An invalid context is not an error: the evaluation answers the fallback with INVALID_CONTEXT.
type Context = map[string]any

// Error is a failure the SDK absorbed and told OnError about. Code is the lastError code of a failed ruleset,
// stream or usage request ("timeout", "bad_ruleset", "network" or "http_<status>"), and empty for another
// failure (a hook that panicked, an evaluation that failed, counts lost at close). Its message never carries a
// URL, a key or anything about a context.
type Error struct {
	Message string
	Code    LastError
}

// Error returns the message alone, which is fixed text the SDK built.
func (e *Error) Error() string { return e.Message }

// Options builds a Client. URL and SDKKey are required. Stream, PollInterval, FlushInterval and
// RequestTimeout are optional: a nil Stream means on, and a zero (or negative) duration means the default.
type Options struct {
	// URL is the server's base URL. Trailing "/" characters are removed.
	URL string
	// SDKKey is a server key (fs_server_...).
	SDKKey string
	// Stream follows GET /sdk/v1/stream. nil means on; a pointer to false means poll only.
	Stream *bool
	// PollInterval is how often to poll when the stream is down or off, and the longest reconnect wait.
	// Zero means 30 s.
	PollInterval time.Duration
	// FlushInterval is how often usage counts are sent. Zero means 30 s.
	FlushInterval time.Duration
	// RequestTimeout is the longest any one request may take, body included. Zero means 10 s.
	RequestTimeout time.Duration

	// Hooks are called after each evaluation.
	Hooks Hooks
	// OnError is told about every failure the SDK absorbs. It is never needed for correctness.
	OnError func(error)
}
