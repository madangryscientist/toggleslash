package toggleslash

import (
	"fmt"
	"time"
)

// ReasonKind says why a flag evaluated as it did. The values are the canonical spellings of
// docs/evaluation.md.
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

// The evaluator's error codes.
const (
	ErrInvalidContext            ErrorCode = "INVALID_CONTEXT"
	ErrFlagNotFound              ErrorCode = "FLAG_NOT_FOUND"
	ErrSegmentNotFound           ErrorCode = "SEGMENT_NOT_FOUND"
	ErrMalformedFlag             ErrorCode = "MALFORMED_FLAG"
	ErrMalformedSegment          ErrorCode = "MALFORMED_SEGMENT"
	ErrPrerequisiteCycle         ErrorCode = "PREREQUISITE_CYCLE"
	ErrPrerequisiteDepthExceeded ErrorCode = "PREREQUISITE_DEPTH_EXCEEDED"
)

// The SDK's own error codes.
const (
	// ErrNotReady: no ruleset has loaded yet.
	ErrNotReady ErrorCode = "NOT_READY"
	// ErrTypeMismatch: the flag served a value of a different kind than the fallback.
	ErrTypeMismatch ErrorCode = "TYPE_MISMATCH"
	// ErrEvaluationFailed: the evaluation panicked; the failure went to the OnError function.
	ErrEvaluationFailed ErrorCode = "EVALUATION_FAILED"
)

// Reason is why a flag evaluated as it did. Only the fields of its Kind are set: RuleIndex for
// ReasonRuleMatch, FlagKey for ReasonPrerequisiteFailed and Code for ReasonError.
type Reason struct {
	Kind      ReasonKind
	RuleIndex int
	FlagKey   string
	Code      ErrorCode
}

// Detail is one evaluation: the value (the served value, or the fallback), the variation index (nil
// whenever the value is the fallback) and the reason.
type Detail[T any] struct {
	Value          T
	VariationIndex *int
	Reason         Reason
}

// StreamState is the state of the live-update stream.
type StreamState string

// The stream states.
const (
	// StreamOff: the client was built with WithStream(false). It stays off until Close.
	StreamOff StreamState = "off"
	// StreamConnecting: the first load, and each stream attempt while it is opening.
	StreamConnecting StreamState = "connecting"
	// StreamConnected: the stream's connected event arrived and the stream is still open.
	StreamConnected StreamState = "connected"
	// StreamDisconnected: the last stream attempt ended and the client is waiting to retry.
	StreamDisconnected StreamState = "disconnected"
	// StreamClosed: Close has been called, whatever the state was before.
	StreamClosed StreamState = "closed"
)

// SyncError is the lastError code of a failed ruleset or stream attempt: "http_<status>", "timeout",
// "bad_ruleset" or "network". The empty string is "none".
type SyncError string

// The fixed lastError codes. The others are "http_" and the status, such as "http_503".
const (
	SyncTimeout    SyncError = "timeout"
	SyncBadRuleset SyncError = "bad_ruleset"
	SyncNetwork    SyncError = "network"
)

// Status is what the client has loaded and how its stream is doing. It is one consistent snapshot.
type Status struct {
	// ETag of the ruleset held, as received; empty before the first load.
	ETag string
	// LastSyncAt is when the client last confirmed its ruleset matches the server; zero if never.
	LastSyncAt time.Time
	Stream     StreamState
	// LastError is why the most recent ruleset or stream attempt failed; empty if it did not.
	LastError SyncError
}

// EvaluationEvent is what one evaluation did, handed to the function given to WithAfterEvaluation.
type EvaluationEvent struct {
	// FlagKey is the key the caller asked for.
	FlagKey string
	// Context is the context the caller passed. The client sends none of it anywhere.
	Context Context
	// Value is what the caller is about to get: the served value, or the fallback.
	Value Value
	// VariationIndex is nil whenever Value is the fallback.
	VariationIndex *int
	Reason         Reason
	// ETag of the ruleset held at this evaluation, as received; empty before the first load.
	ETag string
	// FlagVersion is the flag's version in that ruleset; nil when the ruleset has no such flag or the
	// flag is malformed.
	FlagVersion *float64
}

// RequestError is the error a failed ruleset, stream or usage request reports to the OnError function.
// Its text is fixed: it never carries the URL, the key or anything about a context.
type RequestError struct {
	// Code is the lastError code: "http_<status>", "timeout", "bad_ruleset" or "network".
	Code SyncError
	// Message names the route and what went wrong.
	Message string
}

// Error implements error.
func (e *RequestError) Error() string { return e.Message }

// PanicError is a panic the client recovered, handed to the OnError function: in the AfterEvaluation
// function, in an evaluation or in the client's own goroutines.
type PanicError struct {
	// Where names what panicked.
	Where string
	// Value is what was passed to panic.
	Value any
}

// Error implements error.
func (e *PanicError) Error() string { return "panic in " + e.Where + ": " + fmt.Sprint(e.Value) }
