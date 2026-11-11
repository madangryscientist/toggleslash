package toggleslash

import (
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

// Value is a JSON value. It keeps what the evaluation spec needs and Go's encoding/json does not: a number
// too large for a float64 stays infinite instead of failing, and a lone surrogate in a string survives.
// The zero Value is JSON null.
type Value = jsonvalue.Value

// ParseValue reads one JSON value from data.
func ParseValue(data []byte) (Value, error) { return jsonvalue.Parse(data) }

// ValueFrom converts plain Go data to a Value: nil, bool, string, every integer and float type, []any,
// []string, map[string]any and Value.
func ValueFrom(x any) (Value, error) { return jsonvalue.FromGo(x) }

// ReasonKind is why a flag evaluated to what it did; the text is the canonical spelling.
type ReasonKind = evaluator.ReasonKind

// Reason says why. RuleIndex is meaningful for RULE_MATCH, FlagKey for PREREQUISITE_FAILED and Code for ERROR.
type Reason = evaluator.Reason

// ErrorCode is the code of an ERROR reason.
type ErrorCode = evaluator.ErrorCode

// The reason kinds.
const (
	ReasonOff                = evaluator.ReasonOff
	ReasonPrerequisiteFailed = evaluator.ReasonPrerequisiteFailed
	ReasonTargetMatch        = evaluator.ReasonTargetMatch
	ReasonRuleMatch          = evaluator.ReasonRuleMatch
	ReasonFallthrough        = evaluator.ReasonFallthrough
	ReasonError              = evaluator.ReasonError
)

// The error codes of an ERROR reason: the evaluator's own, then the three the SDK adds.
const (
	ErrInvalidContext            = evaluator.ErrInvalidContext
	ErrFlagNotFound              = evaluator.ErrFlagNotFound
	ErrSegmentNotFound           = evaluator.ErrSegmentNotFound
	ErrMalformedFlag             = evaluator.ErrMalformedFlag
	ErrMalformedSegment          = evaluator.ErrMalformedSegment
	ErrPrerequisiteCycle         = evaluator.ErrPrerequisiteCycle
	ErrPrerequisiteDepthExceeded = evaluator.ErrPrerequisiteDepthExceeded
	ErrNotReady                  = evaluator.ErrNotReady
	ErrTypeMismatch              = evaluator.ErrTypeMismatch
	ErrEvaluationFailed          = evaluator.ErrEvaluationFailed
)

// Detail is an evaluation with its variation index and reason. VariationIndex is -1 when Value is the
// fallback or the flag serves nothing.
type Detail[T any] struct {
	Value          T
	VariationIndex int
	Reason         Reason
}

// StreamState is how the stream is doing.
type StreamState string

// The stream states. Off is WithStream(false); Connecting is opening it (or the first load); Connected is
// the server's connected event having arrived; Disconnected is down and waiting to retry; Closed is after Close.
const (
	StreamOff          StreamState = "off"
	StreamConnecting   StreamState = "connecting"
	StreamConnected    StreamState = "connected"
	StreamDisconnected StreamState = "disconnected"
	StreamClosed       StreamState = "closed"
)

// LastError says why the most recent ruleset or stream attempt failed: LastErrorTimeout, LastErrorBadRuleset,
// LastErrorNetwork or "http_<status>". The empty string is none.
type LastError string

// The fixed lastError codes. An HTTP failure is "http_" and the status, such as "http_503".
const (
	LastErrorTimeout    LastError = "timeout"
	LastErrorBadRuleset LastError = "bad_ruleset"
	LastErrorNetwork    LastError = "network"
)

// Status is what the SDK has loaded, read from memory in one consistent snapshot.
type Status struct {
	// ETag is the tag of the ruleset held, as the server sent it; empty before the first load.
	ETag string
	// LastSyncAt is when the SDK last confirmed its ruleset matches the server; the zero time is never.
	LastSyncAt time.Time
	Stream     StreamState
	// LastError is empty when the most recent attempt worked.
	LastError LastError
}

// EvaluationEvent is what one evaluation did, handed to the WithAfterEvaluation function. It carries the
// Context the caller passed; the SDK itself sends none of it anywhere.
type EvaluationEvent struct {
	FlagKey string
	Context Context
	// Value is what the caller got: the served value, or its fallback.
	Value Value
	// VariationIndex is the variation served, or -1 when Value is the fallback.
	VariationIndex int
	Reason         Reason
	// ETag is the tag of the ruleset held at this evaluation (the same string as Status().ETag), or empty.
	ETag string
	// FlagVersion is the flag's version in that ruleset, or nil when the flag is not in it or did not load.
	FlagVersion *float64
}
