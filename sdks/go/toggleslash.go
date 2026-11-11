// Package toggleslash is the Go server SDK for ToggleSlash.
//
// A Client holds one server key, keeps that environment's ruleset in memory,
// evaluates flags locally to docs/evaluation.md for a context the caller
// passes, and reports how often each variation was served. Create one with
// New and close it with Close:
//
//	client := toggleslash.New("https://flags.example.com", os.Getenv("TOGGLESLASH_SDK_KEY"))
//	defer client.Close()
//	client.Ready()
//	on := client.BoolVariation("new-checkout", toggleslash.NewContext("user-42"), false)
//
// Nothing a Client does panics at the caller: a failure becomes the caller's
// fallback with an ERROR reason, or goes to the OnError callback, or both.
// DESIGN.md holds the mapping from the contract's operations to this API.
package toggleslash

import (
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
)

// ReasonKind says how a value was chosen. The spelling is the spec's.
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

// ErrorCode says why a result is an ERROR. The spelling is the spec's.
type ErrorCode = evaluator.ErrorCode

// The evaluator's error codes.
const (
	ErrInvalidContext            = evaluator.ErrInvalidContext
	ErrFlagNotFound              = evaluator.ErrFlagNotFound
	ErrSegmentNotFound           = evaluator.ErrSegmentNotFound
	ErrMalformedFlag             = evaluator.ErrMalformedFlag
	ErrMalformedSegment          = evaluator.ErrMalformedSegment
	ErrPrerequisiteCycle         = evaluator.ErrPrerequisiteCycle
	ErrPrerequisiteDepthExceeded = evaluator.ErrPrerequisiteDepthExceeds
)

// The SDK's own error codes.
const (
	// ErrNotReady: no ruleset has loaded yet.
	ErrNotReady ErrorCode = "NOT_READY"
	// ErrTypeMismatch: the flag served a value of a different kind than the fallback.
	ErrTypeMismatch ErrorCode = "TYPE_MISMATCH"
	// ErrEvaluationFailed: the evaluation panicked. The failure went to OnError.
	ErrEvaluationFailed ErrorCode = "EVALUATION_FAILED"
)

// Reason is why a flag served what it did. Code is set for ERROR, RuleIndex
// for RULE_MATCH and FlagKey for PREREQUISITE_FAILED.
type Reason = evaluator.Reason

// Detail is a value with the variation that served it and the reason.
// VariationIndex is nil whenever Value is the caller's fallback.
type Detail[T any] struct {
	Value          T
	VariationIndex *int
	Reason         Reason
}

// StreamState is how the stream is doing.
type StreamState string

// The stream states.
const (
	// StreamOff: the stream option is off. It stays off until Close.
	StreamOff StreamState = "off"
	// StreamConnecting: the first load, and each stream attempt while it opens.
	StreamConnecting StreamState = "connecting"
	// StreamConnected: the stream's connected event arrived and it is still open.
	StreamConnected StreamState = "connected"
	// StreamDisconnected: the last attempt ended and the SDK is waiting to retry.
	StreamDisconnected StreamState = "disconnected"
	// StreamClosed: Close has been called.
	StreamClosed StreamState = "closed"
)

// Status is what the SDK has loaded and how its stream is doing. It holds no
// URL, key, context or error message, and it is local: the server never sees it.
type Status struct {
	// ETag of the ruleset held, as received; empty before the first load.
	ETag string
	// LastSyncAt is when the SDK last confirmed its ruleset matches the
	// server; the zero time if never. A keep-alive does not move it.
	LastSyncAt time.Time
	Stream     StreamState
	// LastError is "http_<status>", "timeout", "bad_ruleset" or "network"; empty for none.
	LastError string
}

// EvaluationEvent is what one evaluation did, handed to the AfterEvaluation
// hook. It carries the context the caller passed: a hook that sends it
// anywhere is the application's decision, the SDK itself sends none of it.
type EvaluationEvent struct {
	FlagKey string
	Context Context
	// Value is what the caller is about to get. For IntVariation it is a JSON
	// number, so above 2^53 it is the nearest double; the caller still gets the
	// exact int64.
	Value          Value
	VariationIndex *int
	Reason         Reason
	// ETag of the ruleset this evaluation used, as received; empty before the first load.
	ETag string
	// FlagVersion is the flag's version in that ruleset; nil when the ruleset
	// has no such flag or the flag is malformed.
	FlagVersion *int
}

// RequestError is a failed request the SDK absorbed, as OnError receives it.
// It holds a route and a code, never a URL, a header or a key.
type RequestError struct {
	// Route is "ruleset", "stream" or "events".
	Route string
	// Code is "http_<status>", "timeout", "bad_ruleset" or "network".
	Code string
}

func (e *RequestError) Error() string {
	return "toggleslash: " + e.Route + " request failed: " + e.Code
}
