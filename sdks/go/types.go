package toggleslash

import (
	"fmt"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// Value is one JSON value. It keeps what encoding/json cannot: a number such as 1e999 (positive infinity) and a
// string holding a lone surrogate. Build one with ParseJSON, or pass a map[string]any as a context.
type Value = jsonv.Value

// ParseJSON reads JSON text into a Value. Duplicate object names keep the last value.
func ParseJSON(data []byte) (Value, error) { return jsonv.Parse(data) }

// ReasonKind is the kind of a Reason, spelled as in docs/evaluation.md.
type ReasonKind = evaluator.ReasonKind

// The reason kinds.
const (
	ReasonOff                = evaluator.ReasonOff
	ReasonPrerequisiteFailed = evaluator.ReasonPrerequisiteFailed
	ReasonTargetMatch        = evaluator.ReasonTargetMatch
	ReasonRuleMatch          = evaluator.ReasonRuleMatch
	ReasonFallthrough        = evaluator.ReasonFallthrough
	ReasonError              = evaluator.ReasonError
)

// ErrorCode is the code of an ERROR reason.
type ErrorCode = evaluator.ErrorCode

// The evaluator's error codes, and the three the SDK adds.
const (
	ErrInvalidContext           = evaluator.ErrInvalidContext
	ErrFlagNotFound             = evaluator.ErrFlagNotFound
	ErrSegmentNotFound          = evaluator.ErrSegmentNotFound
	ErrMalformedFlag            = evaluator.ErrMalformedFlag
	ErrMalformedSegment         = evaluator.ErrMalformedSegment
	ErrPrerequisiteCycle        = evaluator.ErrPrerequisiteCycle
	ErrPrerequisiteDepthExceeds = evaluator.ErrPrerequisiteDepthExceeds

	// ErrNotReady: no ruleset has been loaded yet.
	ErrNotReady ErrorCode = "NOT_READY"
	// ErrTypeMismatch: the served value is not of the kind the caller asked for.
	ErrTypeMismatch ErrorCode = "TYPE_MISMATCH"
	// ErrEvaluationFailed: the evaluation, the context or a callback raised.
	ErrEvaluationFailed ErrorCode = "EVALUATION_FAILED"
)

// Reason says why a flag evaluated as it did. Kind is always set; RuleIndex is meaningful for ReasonRuleMatch,
// FlagKey for ReasonPrerequisiteFailed and Code for ReasonError.
type Reason = evaluator.Reason

// Detail is the answer of a ...VariationDetail method: the served value or the fallback, the variation index
// that was served (nil when the value is the fallback) and the reason.
type Detail[T any] struct {
	Value          T
	VariationIndex *int
	Reason         Reason
}

// Event is what the afterEvaluation hook receives, once per evaluation. Value is the Go value the caller got
// (bool, string, int64, float64 or Value). The hook runs on the caller's goroutine, after the answer is known.
type Event struct {
	FlagKey        string
	Context        any
	Value          any
	VariationIndex *int
	Reason         Reason
	// ETag names the ruleset the evaluation used; it is "" when none was held.
	ETag string
	// FlagVersion is the flag's version in that ruleset, nil when the ruleset has no such flag or the flag is
	// malformed.
	FlagVersion *int64
}

// StreamState is where the stream is.
type StreamState string

// The stream states.
const (
	StreamOff          StreamState = "off"
	StreamConnecting   StreamState = "connecting"
	StreamConnected    StreamState = "connected"
	StreamDisconnected StreamState = "disconnected"
	StreamClosed       StreamState = "closed"
)

// The fixed LastError codes. The others are "http_<status>".
const (
	LastErrorTimeout    = "timeout"
	LastErrorBadRuleset = "bad_ruleset"
	LastErrorNetwork    = "network"
)

// Status is what this SDK has loaded and how its stream is doing, as one consistent snapshot. It holds no URL,
// key, message, context or count.
type Status struct {
	// ETag of the ruleset held; "" when there is none.
	ETag string
	// LastSyncAt is when the SDK last confirmed its ruleset matches the server; the zero time when never.
	LastSyncAt time.Time
	Stream     StreamState
	// LastError is why the most recent ruleset or stream attempt failed; "" when it did not.
	LastError string
}

// PanicError is passed to the onError callback when a hook, or the evaluation itself, panicked. The SDK
// recovers the panic; the caller gets the fallback.
type PanicError struct {
	// Value is what was passed to panic. It is empty for a panic inside the SDK's own evaluation, which could
	// otherwise carry the context.
	Value any
	where string
}

func (e *PanicError) Error() string {
	if e.Value == nil {
		return "toggleslash: " + e.where + " panicked"
	}
	return fmt.Sprintf("toggleslash: %s panicked: %v", e.where, e.Value)
}

// syncError is a failure of a ruleset, stream or events request. Its text is fixed: it never carries a URL, a
// key or a message from the network stack, all of which can name the host or the key.
type syncError struct {
	code string
	what string
}

func (e *syncError) Error() string { return "toggleslash: " + e.what + ": " + e.code }

func newSyncError(what, code string) *syncError { return &syncError{code: code, what: what} }
