// Package evaluator is docs/evaluation.md: it turns a ruleset (flags and segments as JSON) and a context
// into a Result, and it never throws. It has no network code and no dependency but the standard library
// and internal/jsonvalue. The shared vectors in fixtures/evaluation/ are its executable form.
package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"

// ReasonKind is why a flag evaluated to what it did.
type ReasonKind string

// The reason kinds of docs/evaluation.md, "Result".
const (
	ReasonOff                ReasonKind = "OFF"
	ReasonPrerequisiteFailed ReasonKind = "PREREQUISITE_FAILED"
	ReasonTargetMatch        ReasonKind = "TARGET_MATCH"
	ReasonRuleMatch          ReasonKind = "RULE_MATCH"
	ReasonFallthrough        ReasonKind = "FALLTHROUGH"
	ReasonError              ReasonKind = "ERROR"
)

// ErrorCode is the code of an ERROR reason.
type ErrorCode string

// The error codes: the evaluator's own (docs/evaluation.md) and the two the SDK adds (contract 4).
const (
	ErrInvalidContext            ErrorCode = "INVALID_CONTEXT"
	ErrFlagNotFound              ErrorCode = "FLAG_NOT_FOUND"
	ErrSegmentNotFound           ErrorCode = "SEGMENT_NOT_FOUND"
	ErrMalformedFlag             ErrorCode = "MALFORMED_FLAG"
	ErrMalformedSegment          ErrorCode = "MALFORMED_SEGMENT"
	ErrPrerequisiteCycle         ErrorCode = "PREREQUISITE_CYCLE"
	ErrPrerequisiteDepthExceeded ErrorCode = "PREREQUISITE_DEPTH_EXCEEDED"
	ErrNotReady                  ErrorCode = "NOT_READY"
	ErrTypeMismatch              ErrorCode = "TYPE_MISMATCH"
	ErrEvaluationFailed          ErrorCode = "EVALUATION_FAILED"
)

// Reason says why. RuleIndex is meaningful for RULE_MATCH, FlagKey for PREREQUISITE_FAILED and Code for ERROR.
type Reason struct {
	Kind      ReasonKind
	RuleIndex int
	FlagKey   string
	Code      ErrorCode
}

// Result is one evaluation. VariationIndex is -1 when no variation was served (the value is then null).
type Result struct {
	Value          jsonvalue.Value
	VariationIndex int
	Reason         Reason
}

// Failure is the result of an evaluation that failed with code.
func Failure(code ErrorCode) Result {
	return Result{VariationIndex: -1, Reason: Reason{Kind: ReasonError, Code: code}}
}

// defaultKind is what a kind that is read and absent means.
const defaultKind = "user"

// bucketCount is the number of buckets, and the sum of a rollout's weights.
const bucketCount = 100000

// maxPrerequisiteDepth is the number of prerequisite links allowed from the requested flag.
const maxPrerequisiteDepth = 32
