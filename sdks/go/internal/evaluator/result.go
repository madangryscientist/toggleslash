// Package evaluator is the SDK's implementation of docs/evaluation.md. It has no network code and
// never panics on a ruleset or a context: every failure is an ERROR result.
package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"

// ErrorCode is the code of an ERROR reason. The spelling is the spec's.
type ErrorCode string

// The evaluator's error codes.
const (
	InvalidContext            ErrorCode = "INVALID_CONTEXT"
	FlagNotFound              ErrorCode = "FLAG_NOT_FOUND"
	SegmentNotFound           ErrorCode = "SEGMENT_NOT_FOUND"
	MalformedFlag             ErrorCode = "MALFORMED_FLAG"
	MalformedSegment          ErrorCode = "MALFORMED_SEGMENT"
	PrerequisiteCycle         ErrorCode = "PREREQUISITE_CYCLE"
	PrerequisiteDepthExceeded ErrorCode = "PREREQUISITE_DEPTH_EXCEEDED"
)

// ReasonKind is the kind of a Reason. The spelling is the spec's.
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

// NoVariation is the variation index of a result that served nothing.
const NoVariation = -1

// Reason says why a result is what it is. RuleIndex is set for RULE_MATCH, FlagKey for
// PREREQUISITE_FAILED and Code for ERROR; the other fields are zero.
type Reason struct {
	Kind      ReasonKind
	RuleIndex int
	FlagKey   string
	Code      ErrorCode
}

// Result is one evaluation. VariationIndex is NoVariation (and Value null) when nothing is served.
type Result struct {
	Value          jsonv.Value
	VariationIndex int
	Reason         Reason
}

func failure(code ErrorCode) Result {
	return Result{VariationIndex: NoVariation, Reason: Reason{Kind: ReasonError, Code: code}}
}
