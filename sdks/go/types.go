package toggleslash

import (
	"fmt"
	"time"
)

// ReasonKind is why a flag was served as it was. The spelling is the canonical one of docs/evaluation.md.
type ReasonKind string

// The reason kinds.
const (
	ReasonOff                ReasonKind = "OFF"
	ReasonPrerequisiteFailed ReasonKind = "PREREQUISITE_FAILED"
	ReasonTargetMatch        ReasonKind = "TARGET_MATCH"
	ReasonRuleMatch          ReasonKind = "RULE_MATCH"
	ReasonFallthrough        ReasonKind = "FALLTHROUGH"
	ReasonError              ReasonKind = "ERROR"
)

// ErrorCode is the code of an ERROR reason: the evaluator's, or one of the SDK's own.
type ErrorCode string

// The error codes.
const (
	ErrInvalidContext            ErrorCode = "INVALID_CONTEXT"
	ErrFlagNotFound              ErrorCode = "FLAG_NOT_FOUND"
	ErrSegmentNotFound           ErrorCode = "SEGMENT_NOT_FOUND"
	ErrMalformedFlag             ErrorCode = "MALFORMED_FLAG"
	ErrMalformedSegment          ErrorCode = "MALFORMED_SEGMENT"
	ErrPrerequisiteCycle         ErrorCode = "PREREQUISITE_CYCLE"
	ErrPrerequisiteDepthExceeded ErrorCode = "PREREQUISITE_DEPTH_EXCEEDED"
	// ErrNotReady: no ruleset has loaded yet.
	ErrNotReady ErrorCode = "NOT_READY"
	// ErrTypeMismatch: the flag served a value of a different kind than the fallback.
	ErrTypeMismatch ErrorCode = "TYPE_MISMATCH"
	// ErrEvaluationFailed: the evaluation panicked. The failure goes to the onError callback.
	ErrEvaluationFailed ErrorCode = "EVALUATION_FAILED"
)

// Reason says why a value was returned.
type Reason struct {
	Kind ReasonKind
	// Code is set when Kind is ReasonError.
	Code ErrorCode
	// RuleIndex is set when Kind is ReasonRuleMatch.
	RuleIndex *int
	// PrerequisiteKey is set when Kind is ReasonPrerequisiteFailed.
	PrerequisiteKey string
}

// Detail is what the ...VariationDetail methods return.
type Detail[T any] struct {
	// Value is the served value, or the caller's fallback.
	Value T
	// VariationIndex is nil whenever Value is the fallback.
	VariationIndex *int
	Reason         Reason
}

// StreamState is how the stream is doing. The spelling is the canonical one of the contract.
type StreamState string

// The stream states.
const (
	StreamOff          StreamState = "off"
	StreamConnecting   StreamState = "connecting"
	StreamConnected    StreamState = "connected"
	StreamDisconnected StreamState = "disconnected"
	StreamClosed       StreamState = "closed"
)

// The fixed LastError codes. The HTTP one is "http_<status>", for example "http_503".
const (
	LastErrorTimeout    = "timeout"
	LastErrorBadRuleset = "bad_ruleset"
	LastErrorNetwork    = "network"
)

// Status is one consistent snapshot of what the SDK has loaded and how its stream is doing. It holds no URL,
// header, key, message, context, flag key or count.
type Status struct {
	// ETag of the ruleset held, as received. Empty: none.
	ETag string
	// LastSyncAt is when the SDK last confirmed its ruleset matches the server. Zero: never.
	LastSyncAt time.Time
	Stream     StreamState
	// LastError is "", "timeout", "bad_ruleset", "network" or "http_<status>".
	LastError string
}

// EvaluationEvent is what the afterEvaluation hook receives.
type EvaluationEvent struct {
	FlagKey string
	// Context is the context the caller passed. The SDK sends none of it anywhere.
	Context Context
	// Value is what the caller is about to get.
	Value Value
	// VariationIndex is nil for a fallback.
	VariationIndex *int
	Reason         Reason
	// ETag of the ruleset held at this evaluation. Empty: none.
	ETag string
	// FlagVersion is the flag's version in that ruleset, or nil when the ruleset has no such flag or it is malformed.
	FlagVersion *int64
}

// Error is what the onError callback receives for a failure the SDK absorbed. Its text is fixed: it never
// holds a URL, a key, a context or the text of a network error.
type Error struct {
	// Op is "ruleset", "stream", "events", "evaluate", "hook" or "close".
	Op string
	// Code is a LastError code for a sync failure, else a short fixed phrase.
	Code string
}

func (e *Error) Error() string { return fmt.Sprintf("toggleslash: %s: %s", e.Op, e.Code) }
