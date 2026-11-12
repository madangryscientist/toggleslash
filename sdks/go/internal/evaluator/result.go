// Package evaluator is the Go implementation of docs/evaluation.md: it loads a ruleset and evaluates one flag
// for one context. It has no network code and imports no HTTP package, and it never panics at a caller.
package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"

// Reason kinds, as the spec spells them.
const (
	ReasonOff                = "OFF"
	ReasonTargetMatch        = "TARGET_MATCH"
	ReasonRuleMatch          = "RULE_MATCH"
	ReasonFallthrough        = "FALLTHROUGH"
	ReasonPrerequisiteFailed = "PREREQUISITE_FAILED"
	ReasonError              = "ERROR"
)

// Error codes the evaluator produces, as the spec spells them.
const (
	CodeInvalidContext          = "INVALID_CONTEXT"
	CodeFlagNotFound            = "FLAG_NOT_FOUND"
	CodeSegmentNotFound         = "SEGMENT_NOT_FOUND"
	CodeMalformedFlag           = "MALFORMED_FLAG"
	CodeMalformedSegment        = "MALFORMED_SEGMENT"
	CodePrerequisiteCycle       = "PREREQUISITE_CYCLE"
	CodePrerequisiteDepthExceed = "PREREQUISITE_DEPTH_EXCEEDED"
)

// Reason says why a flag evaluated the way it did. RuleIndex is meaningful only for a rule match, FlagKey only
// for a failed prerequisite and Code only for an error.
type Reason struct {
	Kind      string
	RuleIndex int
	FlagKey   string
	Code      string
}

// NoVariation is the variation index of a result that served nothing.
const NoVariation = -1

// Result is what an evaluation answers. VariationIndex is NoVariation when nothing was served, and then Value is null.
type Result struct {
	Value          jsonvalue.Value
	VariationIndex int
	Reason         Reason
}

func failure(code string) Result {
	return Result{VariationIndex: NoVariation, Reason: Reason{Kind: ReasonError, Code: code}}
}
