package toggleslash

import (
	"fmt"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

// Context is a ToggleSlash evaluation context: a single context or a multi context, as a JSON value
// (docs/evaluation.md, "Context"). It is the SDK's own JSON value, so a context can carry whatever the
// vectors carry, a number that is not finite and a lone surrogate included. Build one with NewContext,
// jsonvalue.Parse or jsonvalue.From.
type Context = jsonvalue.Value

// NewContext builds a single context. An empty kind means "user" (the key is then the only thing the
// evaluator needs). attrs may be nil; its values are the types jsonvalue.From takes.
func NewContext(kind, key string, attrs map[string]any) Context {
	members := make(map[string]jsonvalue.Value, len(attrs)+2)
	for name, value := range attrs {
		members[name] = jsonvalue.From(value)
	}
	if kind != "" {
		members["kind"] = jsonvalue.NewString(kind)
	}
	members["key"] = jsonvalue.NewString(key)
	return jsonvalue.NewObject(members)
}

// ReasonKind is why a flag evaluated as it did. The values are the canonical spelling.
type ReasonKind string

// The reason kinds.
const (
	ReasonKindOff                ReasonKind = "OFF"
	ReasonKindPrerequisiteFailed ReasonKind = "PREREQUISITE_FAILED"
	ReasonKindTargetMatch        ReasonKind = "TARGET_MATCH"
	ReasonKindRuleMatch          ReasonKind = "RULE_MATCH"
	ReasonKindFallthrough        ReasonKind = "FALLTHROUGH"
	ReasonKindError              ReasonKind = "ERROR"
)

// ErrorCode is the code of an ERROR reason. The values are the canonical spelling.
type ErrorCode string

// The error codes: the evaluator's, then the SDK's own.
const (
	ErrorCodeInvalidContext            ErrorCode = "INVALID_CONTEXT"
	ErrorCodeFlagNotFound              ErrorCode = "FLAG_NOT_FOUND"
	ErrorCodeSegmentNotFound           ErrorCode = "SEGMENT_NOT_FOUND"
	ErrorCodeMalformedFlag             ErrorCode = "MALFORMED_FLAG"
	ErrorCodeMalformedSegment          ErrorCode = "MALFORMED_SEGMENT"
	ErrorCodePrerequisiteCycle         ErrorCode = "PREREQUISITE_CYCLE"
	ErrorCodePrerequisiteDepthExceeded ErrorCode = "PREREQUISITE_DEPTH_EXCEEDED"
	ErrorCodeNotReady                  ErrorCode = "NOT_READY"
	ErrorCodeTypeMismatch              ErrorCode = "TYPE_MISMATCH"
	ErrorCodeEvaluationFailed          ErrorCode = "EVALUATION_FAILED"
)

// Reason says why an evaluation gave its value. Kind is always set; the other fields are set only for
// the kind that has them.
type Reason struct {
	Kind      ReasonKind
	RuleIndex *int      // RULE_MATCH: the index of the rule that matched.
	FlagKey   string    // PREREQUISITE_FAILED: the prerequisite flag that failed.
	ErrorCode ErrorCode // ERROR: why.
}

// Detail is an evaluation with its variation index and reason.
type Detail[T any] struct {
	Value T
	// VariationIndex is the index of the variation the value came from, or nil when the value is the
	// caller's fallback.
	VariationIndex *int
	Reason         Reason
}

// StreamState is where the SDK's stream is. The values are the canonical spelling.
type StreamState string

// The stream states.
const (
	StreamOff          StreamState = "off"
	StreamConnecting   StreamState = "connecting"
	StreamConnected    StreamState = "connected"
	StreamDisconnected StreamState = "disconnected"
	StreamClosed       StreamState = "closed"
)

// The fixed lastError codes. The other code is "http_<status>", for which no constant can exist.
const (
	LastErrorTimeout    = "timeout"
	LastErrorBadRuleset = "bad_ruleset"
	LastErrorNetwork    = "network"
)

// Status is what the SDK has loaded and how its stream is doing, as one consistent snapshot. It holds no
// URL, key, message, context, flag key or count.
type Status struct {
	// ETag of the ruleset held, as received. Empty before the first load.
	ETag string
	// LastSyncAt is when the SDK last confirmed its ruleset matches the server. The zero time if never.
	LastSyncAt time.Time
	Stream     StreamState
	// LastError is why the most recent ruleset or stream attempt failed: "http_<status>", "timeout",
	// "bad_ruleset" or "network". Empty when none.
	LastError string
}

// EvaluationEvent is what the afterEvaluation hook is called with, once per evaluation.
type EvaluationEvent struct {
	// FlagKey is the key the caller asked for.
	FlagKey string
	// Context is the context the caller passed. The SDK itself sends none of it anywhere.
	Context Context
	// Value is what the caller is about to get. An integer fallback larger than 2^53 is shown rounded to
	// the nearest float, because the event holds a JSON value.
	Value jsonvalue.Value
	// VariationIndex is nil for a fallback.
	VariationIndex *int
	Reason         Reason
	// ETag of the ruleset held at this evaluation. Empty before the first load.
	ETag string
	// FlagVersion is the flag's version in that ruleset, or nil when it has no such flag or the flag is
	// malformed.
	FlagVersion *int64
}

// Error is what onError receives: a failure the SDK absorbed. Its text is fixed words and codes: it never
// carries a URL, a key or any part of a context. Err is set when the failure is something the application's
// own code raised (a *PanicError), or the evaluator.
type Error struct {
	// Op is where it happened: "ruleset", "stream", "events", "evaluate", "hook" or "close".
	Op string
	// Code is "http_<status>", "timeout", "bad_ruleset" or "network" for a request, "EVALUATION_FAILED"
	// for an evaluation that panicked, and empty otherwise.
	Code    string
	Message string
	Err     error
}

func (e *Error) Error() string {
	text := e.Op
	switch {
	case e.Message != "":
		text += ": " + e.Message
	case e.Code != "":
		text += ": " + e.Code
	}
	if e.Err != nil {
		text += ": " + e.Err.Error()
	}
	return text
}

// Unwrap returns the underlying error, if any.
func (e *Error) Unwrap() error { return e.Err }

// PanicError is a panic recovered from a hook or the evaluator, passed to onError as raised.
type PanicError struct{ Value any }

func (p *PanicError) Error() string { return fmt.Sprintf("panic: %v", p.Value) }

// Unwrap returns the panic value when it was an error.
func (p *PanicError) Unwrap() error {
	if err, ok := p.Value.(error); ok {
		return err
	}
	return nil
}
