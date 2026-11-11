package eval

import "github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"

// Reason kinds and error codes are exact-spelling strings, as docs/evaluation.md spells them.
const (
	KindOff                = "OFF"
	KindTargetMatch        = "TARGET_MATCH"
	KindRuleMatch          = "RULE_MATCH"
	KindFallthrough        = "FALLTHROUGH"
	KindPrerequisiteFailed = "PREREQUISITE_FAILED"
	KindError              = "ERROR"
)

// The error codes of a Reason of kind ERROR, spelt as docs/evaluation.md spells them.
const (
	CodeInvalidContext           = "INVALID_CONTEXT"
	CodeFlagNotFound             = "FLAG_NOT_FOUND"
	CodeSegmentNotFound          = "SEGMENT_NOT_FOUND"
	CodeMalformedFlag            = "MALFORMED_FLAG"
	CodeMalformedSegment         = "MALFORMED_SEGMENT"
	CodePrerequisiteCycle        = "PREREQUISITE_CYCLE"
	CodePrerequisiteDepthExceeds = "PREREQUISITE_DEPTH_EXCEEDED"
)

// Reason says why a flag evaluated as it did. RuleIndex is -1 when the reason has no rule, and Code and
// FlagKey are empty when it has none.
type Reason struct {
	Kind      string
	Code      string
	RuleIndex int
	FlagKey   string
}

// Result is one evaluation. VariationIndex is -1 when nothing is served, and Value is then null.
type Result struct {
	Value          jsonvalue.Value
	VariationIndex int
	Reason         Reason
}

func failure(code string) Result {
	return Result{VariationIndex: -1, Reason: Reason{Kind: KindError, Code: code, RuleIndex: -1}}
}
