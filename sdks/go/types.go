package toggleslash

import (
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/eval"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

// Value is a JSON value: what a flag serves and what a JSON fallback holds. It is not encoding/json's
// `any`, because that type cannot hold 1e999 or a lone surrogate, which docs/evaluation.md keeps. Build one
// with Null, Bool, Number, String, Array and Object, or read one with ParseJSON.
type Value = jsonvalue.Value

// Member is one key and value of an object Value.
type Member = jsonvalue.Member

// The constructors of a Value.
var (
	// Null is the JSON null. The zero Value is null too.
	Null = jsonvalue.Null
	// Bool is a JSON boolean.
	Bool = jsonvalue.Bool
	// Number is a JSON number (a float64, as the spec reads every number).
	Number = jsonvalue.Number
	// String is a JSON string.
	String = jsonvalue.String
	// Array is a JSON array.
	Array = jsonvalue.Array
	// Object is a JSON object.
	Object = jsonvalue.Object
)

// ParseJSON reads JSON text into a Value with the reader the SDK uses for rulesets: 1e999 is +Inf, a lone
// surrogate escape is kept, a number is a float64.
func ParseJSON(text []byte) (Value, error) { return jsonvalue.Parse(text) }

// NoVariation is the VariationIndex of a result that serves no variation: the fallback was returned, or
// the flag serves nothing.
const NoVariation = eval.NoVariation

// ReasonKind says why a result is what it is.
type ReasonKind string

// The reasons of docs/evaluation.md.
const (
	ReasonOff                = ReasonKind(eval.ReasonOff)
	ReasonPrerequisiteFailed = ReasonKind(eval.ReasonPrerequisiteFailed)
	ReasonTargetMatch        = ReasonKind(eval.ReasonTargetMatch)
	ReasonRuleMatch          = ReasonKind(eval.ReasonRuleMatch)
	ReasonFallthrough        = ReasonKind(eval.ReasonFallthrough)
	ReasonError              = ReasonKind(eval.ReasonError)
)

// ErrorCode is the Code of an ERROR reason: the evaluator's, and three of the SDK's own.
type ErrorCode string

// The error codes.
const (
	ErrorInvalidContext    ErrorCode = "INVALID_CONTEXT"
	ErrorFlagNotFound      ErrorCode = "FLAG_NOT_FOUND"
	ErrorMalformedFlag     ErrorCode = "MALFORMED_FLAG"
	ErrorMalformedSegment  ErrorCode = "MALFORMED_SEGMENT"
	ErrorSegmentNotFound   ErrorCode = "SEGMENT_NOT_FOUND"
	ErrorPrerequisiteCycle ErrorCode = "PREREQUISITE_CYCLE"
	ErrorPrerequisiteDepth ErrorCode = "PREREQUISITE_DEPTH_EXCEEDED"
	ErrorNotReady          ErrorCode = "NOT_READY"
	ErrorTypeMismatch      ErrorCode = "TYPE_MISMATCH"
	ErrorEvaluationFailed  ErrorCode = "EVALUATION_FAILED"
)

// Reason says why a result is what it is. RuleIndex means something for ReasonRuleMatch only, FlagKey for
// ReasonPrerequisiteFailed only (the direct prerequisite) and Code for ReasonError only.
type Reason struct {
	Kind      ReasonKind
	RuleIndex int
	FlagKey   string
	Code      ErrorCode
}

func reasonOf(r eval.Reason) Reason {
	return Reason{Kind: ReasonKind(r.Kind), RuleIndex: r.RuleIndex, FlagKey: r.FlagKey, Code: ErrorCode(r.Code)}
}

func errorReason(code ErrorCode) Reason { return Reason{Kind: ReasonError, Code: code} }

// Detail is the answer of a ...VariationDetail method: the value (the fallback when nothing usable was
// served), the variation served (NoVariation when Value is the fallback) and why.
type Detail[T any] struct {
	Value          T
	VariationIndex int
	Reason         Reason
}

// StreamState is where the stream stands.
type StreamState string

// The stream states. Off is stream disabled; Connecting is opening it (and the first load);
// Connected is after the server's connected event; Disconnected is down, waiting to retry; Closed is after Close.
const (
	StreamOff          StreamState = "off"
	StreamConnecting   StreamState = "connecting"
	StreamConnected    StreamState = "connected"
	StreamDisconnected StreamState = "disconnected"
	StreamClosed       StreamState = "closed"
)

// SyncErrorCode is why the last ruleset or stream attempt failed: "http_<status>", "timeout",
// "bad_ruleset" or "network". The empty string means it did not.
type SyncErrorCode string

// The three fixed codes; the fourth kind is "http_" followed by a status.
const (
	SyncTimeout    SyncErrorCode = "timeout"
	SyncBadRuleset SyncErrorCode = "bad_ruleset"
	SyncNetwork    SyncErrorCode = "network"
)

// Status is what this client has loaded and how its stream is doing. It is local state, read from memory,
// and holds no URL, header, key, message, context, flag key or count.
type Status struct {
	// ETag of the ruleset held, as the server sent it; nil before the first load.
	ETag *string
	// LastSyncAt is when the client last confirmed its ruleset matches the server; the zero time if never.
	LastSyncAt time.Time
	Stream     StreamState
	// LastError is why the most recent ruleset or stream attempt failed; empty if it did not.
	LastError SyncErrorCode
}

// EvaluationEvent is what one evaluation did, handed to the afterEvaluation hook. It carries the context
// the caller passed, so a hook that sends it anywhere is the caller's decision; the SDK sends none of it.
type EvaluationEvent struct {
	FlagKey string
	Context Context
	// Value is what the caller got: the served value, or its fallback.
	Value          Value
	VariationIndex int
	Reason         Reason
	// ETag of the ruleset the evaluation used, as the server sent it; nil before the first load.
	ETag *string
	// FlagVersion is the flag's version in that ruleset; nil when the flag is not in it or is malformed.
	FlagVersion *float64
}

// Error is what the SDK reports to OnError. Op is the operation ("sync", "stream", "usage", "hook",
// "evaluate" or "close"), Code a short stable word, and the message is built by the SDK without a context, a
// key or an attribute.
type Error struct {
	Op      string
	Code    string
	Message string
	// Err is what the SDK recovered from, for a callback that panicked; nil otherwise.
	Err error
}

func (e *Error) Error() string { return "toggleslash: " + e.Op + ": " + e.Message }

// Unwrap returns the recovered cause, if any.
func (e *Error) Unwrap() error { return e.Err }
