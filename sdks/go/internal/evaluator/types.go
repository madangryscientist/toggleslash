// Package evaluator is the Go implementation of docs/evaluation.md: it loads a ruleset and evaluates one
// flag for one context. It has no network code and no dependency but the standard library and
// internal/jsonv, and it never panics at a caller of Evaluate.
package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"

// ErrorCode is the evaluator's error code.
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

// ReasonKind is the kind of a reason.
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

// Reason says why a flag evaluated as it did. Code is set for an ERROR, RuleIndex for a RULE_MATCH and
// FlagKey for a PREREQUISITE_FAILED.
type Reason struct {
	Kind      ReasonKind
	Code      ErrorCode
	RuleIndex int
	FlagKey   string
}

// Result is the outcome of one evaluation. Index is the served variation, or -1 when nothing is served
// (an ERROR, or an off flag with no off variation), and then Value is null.
type Result struct {
	Value  jsonv.Value
	Index  int
	Reason Reason
}

func failure(code ErrorCode) Result {
	return Result{Index: -1, Reason: Reason{Kind: ReasonError, Code: code}}
}
