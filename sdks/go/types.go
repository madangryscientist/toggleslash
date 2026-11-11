package toggleslash

import (
	"fmt"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

// ReasonKind is the kind of an evaluation reason (docs/evaluation.md). The constants carry the canonical
// spelling as their value.
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

// ErrorCode is the code of an ERROR reason: the evaluator's or one of the SDK's own.
type ErrorCode string

// The error codes.
const (
	ErrInvalidContext    ErrorCode = "INVALID_CONTEXT"
	ErrFlagNotFound      ErrorCode = "FLAG_NOT_FOUND"
	ErrSegmentNotFound   ErrorCode = "SEGMENT_NOT_FOUND"
	ErrMalformedFlag     ErrorCode = "MALFORMED_FLAG"
	ErrMalformedSegment  ErrorCode = "MALFORMED_SEGMENT"
	ErrPrerequisiteCycle ErrorCode = "PREREQUISITE_CYCLE"
	ErrPrerequisiteDepth ErrorCode = "PREREQUISITE_DEPTH_EXCEEDED"
	ErrNotReady          ErrorCode = "NOT_READY"
	ErrTypeMismatch      ErrorCode = "TYPE_MISMATCH"
	ErrEvaluationFailed  ErrorCode = "EVALUATION_FAILED"
)

// Reason says why an evaluation answered as it did. Code is set for ERROR, RuleIndex for RULE_MATCH and
// FlagKey for PREREQUISITE_FAILED.
type Reason struct {
	Kind      ReasonKind
	Code      ErrorCode
	RuleIndex int
	FlagKey   string
}

// Detail is the answer of a VariationDetail call. VariationIndex is nil whenever Value is the fallback.
type Detail[T any] struct {
	Value          T
	VariationIndex *int
	Reason         Reason
}

// StreamState is the state of the live-update stream.
type StreamState string

// The stream states.
const (
	StreamOff          StreamState = "off"
	StreamConnecting   StreamState = "connecting"
	StreamConnected    StreamState = "connected"
	StreamDisconnected StreamState = "disconnected"
	StreamClosed       StreamState = "closed"
)

// The fixed LastError values besides http_<n>.
const (
	LastErrorTimeout    = "timeout"
	LastErrorBadRuleset = "bad_ruleset"
	LastErrorNetwork    = "network"
)

// Status is one consistent snapshot of the client. ETag is empty when none is held, LastSyncAt is the zero
// time when no sync has happened and LastError is empty when the last attempt did not fail.
type Status struct {
	ETag       string
	LastSyncAt time.Time
	Stream     StreamState
	LastError  string
}

// EvaluationEvent is what the afterEvaluation hook receives. ETag is empty before the first load and
// FlagVersion is nil when the ruleset has no such flag or the flag is malformed. Value is held as a
// jsonvalue.Value, whose numbers are float64: an integer above 2^53 reaches the hook as the nearest double,
// while Int64Variation still returns the exact integer.
type EvaluationEvent struct {
	FlagKey        string
	Context        Context
	Value          jsonvalue.Value
	VariationIndex *int
	Reason         Reason
	ETag           string
	FlagVersion    *int64
}

// SyncError is a failed ruleset, stream or events request. Its text is fixed: it never carries a URL, a
// header, a key, a context or a response body.
type SyncError struct {
	// Route is "ruleset", "stream" or "events".
	Route string
	// Detail is "network", "timeout", "bad_ruleset" or "http_<n>".
	Detail string
}

func (e *SyncError) Error() string { return "toggleslash: " + e.Route + " request failed: " + e.Detail }

// PanicError is passed to onError when a hook or the evaluator panicked. Value is what was passed to panic.
type PanicError struct {
	Where string
	Value any
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("toggleslash: %s panicked: %v", e.Where, e.Value)
}

// Unwrap returns the panic value when it is an error.
func (e *PanicError) Unwrap() error {
	err, _ := e.Value.(error)
	return err
}

// LostCountsError is passed to onError when counts were lost at Close.
type LostCountsError struct{ Message string }

func (e *LostCountsError) Error() string { return e.Message }
