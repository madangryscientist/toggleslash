// Package eval is the flag evaluator: docs/evaluation.md in Go. It has no network code and does not import
// the SDK's root package. A ruleset is loaded once from a jsonv.Value, and Evaluate never panics or errors:
// every failure is an ERROR result.
package eval

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"

// Reason kinds.
const (
	ReasonOff                = "OFF"
	ReasonTargetMatch        = "TARGET_MATCH"
	ReasonRuleMatch          = "RULE_MATCH"
	ReasonFallthrough        = "FALLTHROUGH"
	ReasonPrerequisiteFailed = "PREREQUISITE_FAILED"
	ReasonError              = "ERROR"
)

// Error codes the evaluator produces.
const (
	CodeInvalidContext            = "INVALID_CONTEXT"
	CodeFlagNotFound              = "FLAG_NOT_FOUND"
	CodeSegmentNotFound           = "SEGMENT_NOT_FOUND"
	CodeMalformedFlag             = "MALFORMED_FLAG"
	CodeMalformedSegment          = "MALFORMED_SEGMENT"
	CodePrerequisiteCycle         = "PREREQUISITE_CYCLE"
	CodePrerequisiteDepthExceeded = "PREREQUISITE_DEPTH_EXCEEDED"
)

// Reason says why a flag evaluated as it did.
type Reason struct {
	Kind      string
	RuleIndex int    // RULE_MATCH only
	FlagKey   string // PREREQUISITE_FAILED only
	Code      string // ERROR only
}

// Result is one evaluation. VariationIndex is -1 when nothing was served.
type Result struct {
	Value          jsonv.Value
	VariationIndex int
	Reason         Reason
}

func failure(code string) Result {
	return Result{VariationIndex: -1, Reason: Reason{Kind: ReasonError, Code: code}}
}
