package eval

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"

// ErrorCode is the code of an ERROR reason.
type ErrorCode string

// The error codes of docs/evaluation.md.
const (
	InvalidContext            ErrorCode = "INVALID_CONTEXT"
	FlagNotFound              ErrorCode = "FLAG_NOT_FOUND"
	SegmentNotFound           ErrorCode = "SEGMENT_NOT_FOUND"
	MalformedFlag             ErrorCode = "MALFORMED_FLAG"
	MalformedSegment          ErrorCode = "MALFORMED_SEGMENT"
	PrerequisiteCycle         ErrorCode = "PREREQUISITE_CYCLE"
	PrerequisiteDepthExceeded ErrorCode = "PREREQUISITE_DEPTH_EXCEEDED"
)

// ReasonKind is the kind of a Reason.
type ReasonKind string

// The reason kinds of docs/evaluation.md.
const (
	ReasonOff                ReasonKind = "OFF"
	ReasonTargetMatch        ReasonKind = "TARGET_MATCH"
	ReasonRuleMatch          ReasonKind = "RULE_MATCH"
	ReasonFallthrough        ReasonKind = "FALLTHROUGH"
	ReasonPrerequisiteFailed ReasonKind = "PREREQUISITE_FAILED"
	ReasonError              ReasonKind = "ERROR"
)

// Reason says why a flag was served as it was.
type Reason struct {
	Kind      ReasonKind
	RuleIndex int       // RULE_MATCH
	FlagKey   string    // PREREQUISITE_FAILED
	Code      ErrorCode // ERROR
}

// Result is one evaluation. VariationIndex is -1 when nothing was served, and Value is then null.
type Result struct {
	Value          jsonvalue.Value
	VariationIndex int
	Reason         Reason
}
