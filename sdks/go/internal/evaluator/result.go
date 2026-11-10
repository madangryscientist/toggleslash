package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"

// ErrorCode is the closed set of evaluation error codes (docs/evaluation.md).
type ErrorCode string

// The error codes the evaluator itself produces. The client adds NOT_READY, TYPE_MISMATCH and
// EVALUATION_FAILED.
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

// ReasonKind is the closed set of reason kinds.
type ReasonKind string

// The reason kinds.
const (
	ReasonOff                ReasonKind = "OFF"
	ReasonTargetMatch        ReasonKind = "TARGET_MATCH"
	ReasonRuleMatch          ReasonKind = "RULE_MATCH"
	ReasonFallthrough        ReasonKind = "FALLTHROUGH"
	ReasonPrerequisiteFailed ReasonKind = "PREREQUISITE_FAILED"
	ReasonError              ReasonKind = "ERROR"
)

// NoIndex is the variation index of a result that served no variation.
const NoIndex = -1

// Reason says why a flag served what it did. RuleIndex is -1 unless Kind is RULE_MATCH.
// PrerequisiteKey is set only for PREREQUISITE_FAILED and ErrorCode only for ERROR.
type Reason struct {
	Kind            ReasonKind
	RuleIndex       int
	PrerequisiteKey string
	ErrorCode       ErrorCode
}

// Result is one evaluation. VariationIndex is NoIndex (-1) when nothing was served, and Value is null
// then.
type Result struct {
	Value          jsonv.Value
	VariationIndex int
	Reason         Reason
}

func errorResult(code ErrorCode) Result {
	return Result{VariationIndex: NoIndex, Reason: Reason{Kind: ReasonError, RuleIndex: -1, ErrorCode: code}}
}
