package toggleslash

import (
	"fmt"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
)

// Reason says why an evaluation answered what it did. Kind is one of the Reason* constants; for an
// ERROR, Code says which. The spellings are the ones of docs/evaluation.md and the SDK contract.
type Reason = evaluator.Reason

// ReasonKind is the kind of a Reason.
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

// ErrorCode is the code of an ERROR reason.
type ErrorCode = evaluator.ErrorCode

// The error codes: the evaluator's, then the SDK's own.
const (
	InvalidContext            = evaluator.InvalidContext
	FlagNotFound              = evaluator.FlagNotFound
	SegmentNotFound           = evaluator.SegmentNotFound
	MalformedFlag             = evaluator.MalformedFlag
	MalformedSegment          = evaluator.MalformedSegment
	PrerequisiteCycle         = evaluator.PrerequisiteCycle
	PrerequisiteDepthExceeded = evaluator.PrerequisiteDepthExceeded

	// NotReady: no ruleset has loaded yet.
	NotReady ErrorCode = "NOT_READY"
	// TypeMismatch: the flag served a value of a different kind than the fallback.
	TypeMismatch ErrorCode = "TYPE_MISMATCH"
	// EvaluationFailed: the evaluation raised something it should not have. It went to onError.
	EvaluationFailed ErrorCode = "EVALUATION_FAILED"
)

// NoVariation is the variation index of an answer that is the caller's fallback.
const NoVariation = evaluator.NoVariation

// Detail is the full answer of VariationDetail.
type Detail struct {
	// Value is the served value, or the caller's fallback.
	Value Value
	// VariationIndex is the served variation, or NoVariation whenever Value is the fallback.
	VariationIndex int
	Reason         Reason
}

// TypedDetail is the full answer of a typed accessor's Detail method.
type TypedDetail[T any] struct {
	Value          T
	VariationIndex int
	Reason         Reason
}

// StreamState is the state of the live-update connection.
type StreamState string

// The stream states.
const (
	StreamOff          StreamState = "off"
	StreamConnecting   StreamState = "connecting"
	StreamConnected    StreamState = "connected"
	StreamDisconnected StreamState = "disconnected"
	StreamClosed       StreamState = "closed"
)

// The lastError codes of Status. A rejected status is "http_<n>" with the status code in place of
// <n>, such as "http_503".
const (
	SyncTimeout    = "timeout"
	SyncBadRuleset = "bad_ruleset"
	SyncNetwork    = "network"
)

// Status is one snapshot of what the SDK knows about the server. It holds no URL, header, key,
// message, context, flag key or count.
type Status struct {
	// ETag is the tag of the held ruleset; empty when there is none.
	ETag string
	// LastSyncAt is when the held ruleset was last confirmed current; nil before the first sync.
	LastSyncAt *time.Time
	Stream     StreamState
	// LastError is the code of the last failed sync; empty when there is none.
	LastError string
}

// EvaluationEvent is what the afterEvaluation hook receives after every evaluation.
type EvaluationEvent struct {
	FlagKey        string
	Context        Context
	Value          Value
	VariationIndex int
	Reason         Reason
	// ETag is the tag of the ruleset the answer came from; empty when it had none.
	ETag string
	// FlagVersion is the flag's version in that ruleset; nil when it had none.
	FlagVersion *int64
}

// ErrorSource says where an Error came from.
type ErrorSource string

// The sources of an Error.
const (
	SourceRuleset   ErrorSource = "ruleset"
	SourceStream    ErrorSource = "stream"
	SourceEvents    ErrorSource = "events"
	SourceHook      ErrorSource = "hook"
	SourceEvaluator ErrorSource = "evaluator"
	SourceClose     ErrorSource = "close"
)

// Error is what the SDK passes to the onError callback. Its text is fixed: it holds no URL, key,
// header or context data. An error a hook or the evaluator raised is Err, as raised.
type Error struct {
	Source ErrorSource
	// Code is the sync error code ("http_503", "timeout", "bad_ruleset", "network") when there is
	// one, and empty otherwise.
	Code    string
	Message string
	Err     error
}

func (e *Error) Error() string { return e.Message }

// Unwrap returns the error a hook or the evaluator raised.
func (e *Error) Unwrap() error { return e.Err }

// PanicError carries the value a hook, the evaluator or a callback panicked with.
type PanicError struct{ Value any }

func (p *PanicError) Error() string { return fmt.Sprintf("panic: %v", p.Value) }

// Unwrap returns the panic value when it was an error.
func (p *PanicError) Unwrap() error {
	if err, ok := p.Value.(error); ok {
		return err
	}
	return nil
}

func errorReason(code ErrorCode) Reason { return Reason{Kind: ReasonError, Code: code} }
