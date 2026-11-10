package toggleslash

import (
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
)

// ReasonKind says how a flag came to serve what it did. The text is the canonical spelling.
type ReasonKind = evaluator.ReasonKind

// The reason kinds.
const (
	ReasonOff                = evaluator.ReasonOff
	ReasonTargetMatch        = evaluator.ReasonTargetMatch
	ReasonRuleMatch          = evaluator.ReasonRuleMatch
	ReasonFallthrough        = evaluator.ReasonFallthrough
	ReasonPrerequisiteFailed = evaluator.ReasonPrerequisiteFailed
	ReasonError              = evaluator.ReasonError
)

// ErrorCode is the code an ERROR reason carries. The text is the canonical spelling.
type ErrorCode = evaluator.ErrorCode

// The error codes: the evaluator's, then the SDK's own.
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

// NoVariation is the VariationIndex of a result whose value is the caller's fallback.
const NoVariation = evaluator.NoIndex

// Reason says why a flag served what it did. RuleIndex is -1 unless Kind is ReasonRuleMatch;
// PrerequisiteKey is set only for ReasonPrerequisiteFailed and ErrorCode only for ReasonError.
type Reason = evaluator.Reason

// Detail is an evaluation with its variation index and reason. VariationIndex is NoVariation (-1)
// whenever Value is the caller's fallback.
type Detail[T any] struct {
	Value          T
	VariationIndex int
	Reason         Reason
}

// StreamState is where the stream stands. The text is the canonical spelling.
type StreamState string

// The stream states.
const (
	StreamOff          StreamState = "off"
	StreamConnecting   StreamState = "connecting"
	StreamConnected    StreamState = "connected"
	StreamDisconnected StreamState = "disconnected"
	StreamClosed       StreamState = "closed"
)

// The fixed LastError codes, besides "http_<status>".
const (
	LastErrorTimeout    = "timeout"
	LastErrorBadRuleset = "bad_ruleset"
	LastErrorNetwork    = "network"
)

// Status is one consistent snapshot of what the SDK has loaded. It holds no URL, key, message,
// context or flag key.
type Status struct {
	// ETag is the ETag of the ruleset held, as received; empty before the first load.
	ETag string
	// LastSyncAt is when the SDK last confirmed its ruleset matches the server; the zero time if never.
	LastSyncAt time.Time
	// Stream is one of the Stream constants.
	Stream StreamState
	// LastError is "http_<status>", "timeout", "bad_ruleset" or "network"; empty if none.
	LastError string
}

// EvaluationEvent is what the afterEvaluation hook receives, once per evaluation.
type EvaluationEvent struct {
	// FlagKey is the key the caller asked for.
	FlagKey string
	// Context is the context the caller passed.
	Context Context
	// Value is what the caller is about to get.
	Value Value
	// VariationIndex is NoVariation for a fallback.
	VariationIndex int
	Reason         Reason
	// ETag is the ETag of the ruleset this evaluation used; empty before the first load.
	ETag string
	// FlagVersion is the flag's version in that ruleset, or nil when the ruleset has no such flag or
	// the flag is malformed.
	FlagVersion *float64
}
