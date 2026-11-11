package toggleslash

import (
	"strconv"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
)

// ReasonKind says how a flag came to its value. The values keep the spelling of docs/evaluation.md.
type ReasonKind string

// The reason kinds.
const (
	ReasonOff                ReasonKind = evaluator.ReasonOff
	ReasonPrerequisiteFailed ReasonKind = evaluator.ReasonPrerequisiteFailed
	ReasonTargetMatch        ReasonKind = evaluator.ReasonTargetMatch
	ReasonRuleMatch          ReasonKind = evaluator.ReasonRuleMatch
	ReasonFallthrough        ReasonKind = evaluator.ReasonFallthrough
	ReasonError              ReasonKind = evaluator.ReasonError
)

// ErrorCode says why a reason of kind ReasonError is an error: the evaluator's codes and the SDK's own.
type ErrorCode string

// The error codes.
const (
	ErrInvalidContext           ErrorCode = evaluator.CodeInvalidContext
	ErrFlagNotFound             ErrorCode = evaluator.CodeFlagNotFound
	ErrSegmentNotFound          ErrorCode = evaluator.CodeSegmentNotFound
	ErrMalformedFlag            ErrorCode = evaluator.CodeMalformedFlag
	ErrMalformedSegment         ErrorCode = evaluator.CodeMalformedSegment
	ErrPrerequisiteCycle        ErrorCode = evaluator.CodePrerequisiteCycle
	ErrPrerequisiteDepthExceeds ErrorCode = evaluator.CodePrerequisiteDepthExceeds

	// ErrNotReady: no ruleset has loaded yet.
	ErrNotReady ErrorCode = "NOT_READY"
	// ErrTypeMismatch: the flag served a value of a different kind than the fallback.
	ErrTypeMismatch ErrorCode = "TYPE_MISMATCH"
	// ErrEvaluationFailed: the evaluation panicked. The failure went to the OnError callback.
	ErrEvaluationFailed ErrorCode = "EVALUATION_FAILED"
)

// StreamState is the state of the client's connection to the stream.
type StreamState string

// The stream states.
const (
	StreamOff          StreamState = "off"
	StreamConnecting   StreamState = "connecting"
	StreamConnected    StreamState = "connected"
	StreamDisconnected StreamState = "disconnected"
	StreamClosed       StreamState = "closed"
)

// SyncErrorCode is why the most recent ruleset or stream attempt failed. The empty value is none.
type SyncErrorCode string

// The codes that do not carry a number. An HTTP status is "http_<status>", for example "http_503".
const (
	SyncTimeout    SyncErrorCode = "timeout"
	SyncBadRuleset SyncErrorCode = "bad_ruleset"
	SyncNetwork    SyncErrorCode = "network"
)

func httpErrorCode(status int) SyncErrorCode {
	return SyncErrorCode("http_" + strconv.Itoa(status))
}

// NoVariation is the variation index of a result that is the caller's fallback.
const NoVariation = -1

// Reason says why a result is what it is. RuleIndex is set for ReasonRuleMatch, FlagKey for
// ReasonPrerequisiteFailed and Code for ReasonError; the other fields are zero.
type Reason struct {
	Kind      ReasonKind
	RuleIndex int
	FlagKey   string
	Code      ErrorCode
}

// Detail is a value with the variation index it came from and the reason.
type Detail[T any] struct {
	Value T
	// VariationIndex is NoVariation (-1) when Value is the fallback.
	VariationIndex int
	Reason         Reason
}

// Status is what the client has loaded and how its stream is doing, read at one moment.
type Status struct {
	// ETag of the ruleset held, as received; empty before the first load.
	ETag string
	// LastSyncAt is when the client last confirmed its ruleset matches the server; the zero time if never.
	LastSyncAt time.Time
	Stream     StreamState
	// LastError is why the most recent ruleset or stream attempt failed; empty is none.
	LastError SyncErrorCode
}

// Event is what the WithAfterEvaluation callback receives after each evaluation.
type Event struct {
	FlagKey string
	// Context is the context the caller passed.
	Context Context
	// Value is what the caller is about to get. For an integer accessor it is the nearest binary64.
	Value Value
	// VariationIndex is NoVariation (-1) for a fallback.
	VariationIndex int
	Reason         Reason
	// ETag of the ruleset held at this evaluation; empty before the first load.
	ETag string
	// FlagVersion is the flag's version in that ruleset; nil when the ruleset has no such flag, the flag
	// is malformed, or the version is above the largest int64.
	FlagVersion *int64
}

func fromEvaluator(r evaluator.Reason) Reason {
	return Reason{Kind: ReasonKind(r.Kind), RuleIndex: r.RuleIndex, FlagKey: r.FlagKey, Code: ErrorCode(r.Code)}
}

func errorReason(code ErrorCode) Reason {
	return Reason{Kind: ReasonError, Code: code}
}

// SyncError is the error the WithOnError callback receives for a failed ruleset or stream request
// (Usage is false) or a failed usage request (Usage is true). Its text holds the code and nothing else: no
// URL, key, header or context.
type SyncError struct {
	Code  SyncErrorCode
	Usage bool
}

func (e *SyncError) Error() string {
	if e.Usage {
		return "toggleslash: usage request failed: " + string(e.Code)
	}
	return "toggleslash: ruleset request failed: " + string(e.Code)
}
