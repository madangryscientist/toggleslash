package toggleslash

import (
	"fmt"
	"strconv"
	"time"
)

// ReasonKind says how a flag came to its value.
type ReasonKind string

// The reason kinds of docs/sdk-contract.md section 2, spelled as the contract spells them.
const (
	ReasonOff                ReasonKind = "OFF"
	ReasonTargetMatch        ReasonKind = "TARGET_MATCH"
	ReasonRuleMatch          ReasonKind = "RULE_MATCH"
	ReasonFallthrough        ReasonKind = "FALLTHROUGH"
	ReasonPrerequisiteFailed ReasonKind = "PREREQUISITE_FAILED"
	ReasonError              ReasonKind = "ERROR"
)

// ErrorCode says why the caller got its fallback, for a reason of kind ReasonError.
type ErrorCode string

// The error codes: the evaluator's seven, then the SDK's three.
const (
	ErrInvalidContext          ErrorCode = "INVALID_CONTEXT"
	ErrFlagNotFound            ErrorCode = "FLAG_NOT_FOUND"
	ErrSegmentNotFound         ErrorCode = "SEGMENT_NOT_FOUND"
	ErrMalformedFlag           ErrorCode = "MALFORMED_FLAG"
	ErrMalformedSegment        ErrorCode = "MALFORMED_SEGMENT"
	ErrPrerequisiteCycle       ErrorCode = "PREREQUISITE_CYCLE"
	ErrPrerequisiteDepthExceed ErrorCode = "PREREQUISITE_DEPTH_EXCEEDED"
	ErrNotReady                ErrorCode = "NOT_READY"
	ErrTypeMismatch            ErrorCode = "TYPE_MISMATCH"
	ErrEvaluationFailed        ErrorCode = "EVALUATION_FAILED"
)

// Reason says why an evaluation answered as it did. RuleIndex is meaningful only for ReasonRuleMatch, FlagKey
// only for ReasonPrerequisiteFailed (the prerequisite that failed) and Code only for ReasonError.
type Reason struct {
	Kind      ReasonKind
	RuleIndex int
	FlagKey   string
	Code      ErrorCode
}

// NoVariation is the variation index of an answer that served no variation: a fallback, or a flag that serves
// nothing.
const NoVariation = -1

// NoVersion is the flag version of an event for a flag the ruleset does not have, or has but cannot use.
const NoVersion int64 = -1

// Detail is what a …VariationDetail method answers: the value (the caller's fallback when nothing usable was
// served), the variation index (NoVariation for none) and the reason.
type Detail[T any] struct {
	Value          T
	VariationIndex int
	Reason         Reason
}

// StreamState is how the SDK's stream is doing.
type StreamState string

// The stream states of contract section 8.
const (
	StreamOff          StreamState = "off"
	StreamConnecting   StreamState = "connecting"
	StreamConnected    StreamState = "connected"
	StreamDisconnected StreamState = "disconnected"
	StreamClosed       StreamState = "closed"
)

// LastError is why the most recent ruleset or stream attempt failed, or empty if it did not. It is one of
// LastErrorTimeout, LastErrorBadRuleset, LastErrorNetwork, or "http_<status>" (see HTTPStatus).
type LastError string

// The fixed values of LastError.
const (
	LastErrorNone       LastError = ""
	LastErrorTimeout    LastError = "timeout"
	LastErrorBadRuleset LastError = "bad_ruleset"
	LastErrorNetwork    LastError = "network"
)

func httpError(status int) LastError { return LastError("http_" + strconv.Itoa(status)) }

// Status is what the SDK has loaded and how its stream is doing: a consistent snapshot of local state. It holds no
// URL, key, header or message, and reading it makes no request.
type Status struct {
	// ETag of the ruleset held, as the server sent it; empty before the first load.
	ETag string
	// LastSyncAt is when the SDK last confirmed its ruleset matches the server; the zero time if never.
	LastSyncAt time.Time
	Stream     StreamState
	LastError  LastError
}

// Event is what one evaluation did, handed to the function WithAfterEvaluation sets. It carries the context the
// caller passed, so a hook that sends it anywhere is the caller's decision.
type Event struct {
	FlagKey string
	Context Context
	// Value is what the caller got. For IntVariation it is the nearest double to the integer, so an integer
	// above 2^53 is rounded in the event; the caller still gets the exact one.
	Value Value
	// VariationIndex is the variation served, or NoVariation.
	VariationIndex int
	Reason         Reason
	// ETag of the ruleset this evaluation used, or empty before the first load.
	ETag string
	// FlagVersion is the flag's version in that ruleset, or NoVersion. A version above 2^63-1 is NoVersion.
	FlagVersion int64
}

// PanicError is what a panic in the evaluation or in the AfterEvaluation function becomes when it is passed to
// the OnError function. Value is what was passed to panic.
type PanicError struct {
	// Where says what panicked: "afterEvaluation hook" or "evaluation".
	Where string
	Value any
}

func (e *PanicError) Error() string { return fmt.Sprintf("%s panicked: %v", e.Where, e.Value) }

// Unwrap returns the panic value when it is itself an error.
func (e *PanicError) Unwrap() error {
	err, _ := e.Value.(error)
	return err
}

// sdkError is an error the SDK builds itself. Its text is fixed: it never carries a URL (a network error's text
// would), a header, a key or a context.
type sdkError struct {
	code LastError
	msg  string
}

func (e *sdkError) Error() string { return e.msg }
