package toggleslash

import (
	"fmt"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/eval"
	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

// Reason says why a flag evaluated as it did. Kind is one of the Reason* constants. Code is set only when Kind is
// ReasonError, and is one of the Error* constants. RuleIndex is the matching rule for ReasonRuleMatch and -1
// otherwise. FlagKey is the failed prerequisite for ReasonPrerequisiteFailed and empty otherwise.
type Reason = eval.Reason

// Reason kinds, spelled as docs/evaluation.md spells them.
const (
	ReasonOff                = eval.KindOff
	ReasonTargetMatch        = eval.KindTargetMatch
	ReasonRuleMatch          = eval.KindRuleMatch
	ReasonFallthrough        = eval.KindFallthrough
	ReasonPrerequisiteFailed = eval.KindPrerequisiteFailed
	ReasonError              = eval.KindError
)

// Error codes of a Reason whose Kind is ReasonError: the evaluator's, then the three the SDK adds.
const (
	ErrorInvalidContext            = eval.CodeInvalidContext
	ErrorFlagNotFound              = eval.CodeFlagNotFound
	ErrorSegmentNotFound           = eval.CodeSegmentNotFound
	ErrorMalformedFlag             = eval.CodeMalformedFlag
	ErrorMalformedSegment          = eval.CodeMalformedSegment
	ErrorPrerequisiteCycle         = eval.CodePrerequisiteCycle
	ErrorPrerequisiteDepthExceeded = eval.CodePrerequisiteDepthExceeds
	// ErrorNotReady: the first ruleset has not loaded, so the caller got its fallback.
	ErrorNotReady = "NOT_READY"
	// ErrorTypeMismatch: the flag served a value of another JSON kind than the fallback's.
	ErrorTypeMismatch = "TYPE_MISMATCH"
	// ErrorEvaluationFailed: the evaluator itself failed; the caller got its fallback.
	ErrorEvaluationFailed = "EVALUATION_FAILED"
)

// Stream states of a Status.
const (
	StreamOff          = "off"
	StreamConnecting   = "connecting"
	StreamConnected    = "connected"
	StreamDisconnected = "disconnected"
	StreamClosed       = "closed"
)

// Codes of a Status.LastError (the other is "http_<status>", for example "http_503").
const (
	SyncTimeout    = "timeout"
	SyncBadRuleset = "bad_ruleset"
	SyncNetwork    = "network"
)

// Detail is an evaluation with its reason. Value is the served value, or the fallback when nothing usable was
// served. VariationIndex is the variation served, or -1 when Value is the fallback or the flag serves nothing.
type Detail[T any] struct {
	Value          T
	VariationIndex int
	Reason         Reason
}

// Status is what the client has loaded and how its stream is doing, as one consistent snapshot. It is read from
// memory and the server never sees it.
type Status struct {
	// ETag of the ruleset held, as the server sent it; "" before the first load.
	ETag string
	// LastSyncAt is when the client last confirmed its ruleset matches the server, as whole epoch milliseconds;
	// -1 if never.
	LastSyncAt int64
	// Stream is one of the Stream* constants.
	Stream string
	// LastError is why the most recent ruleset or stream attempt failed ("http_<status>", SyncTimeout,
	// SyncBadRuleset or SyncNetwork); "" if it did not.
	LastError string
}

// EvaluationEvent is what one evaluation did, handed to the function WithAfterEvaluation sets. It carries the
// context the caller passed, so a hook that sends it anywhere is the caller's decision: the SDK sends none of it.
type EvaluationEvent struct {
	FlagKey string
	Context jsonvalue.Value
	// Value is what the caller got: the served value, or its fallback.
	Value jsonvalue.Value
	// VariationIndex is the variation served, or -1 when Value is the fallback or the flag serves nothing.
	VariationIndex int
	Reason         Reason
	// ETag of the ruleset this evaluation used, as Status().ETag spells it; "" before the first load.
	ETag string
	// FlagVersion is the flag's version in that ruleset; -1 when the flag is not in it or is malformed. A version
	// is a non-negative integer that may exceed int64, so it is a float64, as in the ruleset.
	FlagVersion float64
}

// Error is every error the SDK hands to the function WithOnError sets. Its text is fixed per Op and Code, and
// carries no URL, key or context, so it is safe to log.
type Error struct {
	// Op is "ruleset", "stream", "usage", "hook", "evaluate" or "close".
	Op string
	// Code is "http_<status>", "timeout", "bad_ruleset", "network", "panic", "failed" or "unsent".
	Code string
	// Count is the number of usage entries lost, for Op "close".
	Count int
}

// Error implements error.
func (e *Error) Error() string {
	if e.Op == "close" {
		return fmt.Sprintf("closed with %d usage counts unsent: the server asked to wait", e.Count)
	}
	return "toggleslash: " + e.Op + ": " + e.Code
}
