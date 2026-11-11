package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"

// The reason kinds docs/evaluation.md names.
const (
	ReasonOff                = "OFF"
	ReasonTargetMatch        = "TARGET_MATCH"
	ReasonRuleMatch          = "RULE_MATCH"
	ReasonFallthrough        = "FALLTHROUGH"
	ReasonPrerequisiteFailed = "PREREQUISITE_FAILED"
	ReasonError              = "ERROR"
)

// The error codes docs/evaluation.md names.
const (
	CodeInvalidContext           = "INVALID_CONTEXT"
	CodeFlagNotFound             = "FLAG_NOT_FOUND"
	CodeSegmentNotFound          = "SEGMENT_NOT_FOUND"
	CodeMalformedFlag            = "MALFORMED_FLAG"
	CodeMalformedSegment         = "MALFORMED_SEGMENT"
	CodePrerequisiteCycle        = "PREREQUISITE_CYCLE"
	CodePrerequisiteDepthExceeds = "PREREQUISITE_DEPTH_EXCEEDED"
)

// Reason says why a result is what it is. RuleIndex is set for RULE_MATCH, FlagKey for
// PREREQUISITE_FAILED and Code for ERROR; the other fields are zero.
type Reason struct {
	Kind      string
	RuleIndex int
	FlagKey   string
	Code      string
}

// Result is one evaluation. VariationIndex is -1 when nothing is served, and Value is then null.
type Result struct {
	Value          jsonvalue.Value
	VariationIndex int
	Reason         Reason
}

func failure(code string) Result {
	return Result{VariationIndex: -1, Reason: Reason{Kind: ReasonError, Code: code}}
}

func served(variations []jsonvalue.Value, index int, reason Reason) Result {
	return Result{Value: variations[index], VariationIndex: index, Reason: reason}
}
