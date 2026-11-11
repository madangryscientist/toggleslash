// Package evaluator is the Go implementation of docs/evaluation.md: it checks
// a ruleset once, then answers "what does this flag serve for this context".
//
// It has no network, clock or randomness. Strings are WTF-8 (see package wtf8),
// so a lone surrogate in a key or an attribute compares as it does in the
// UTF-16 world the spec is written in. Pattern matching is its own Pike VM:
// the package imports no regexp.
package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/wjson"

// ErrorCode names why an evaluation failed.
type ErrorCode string

// The evaluator's error codes. The SDK adds codes of its own on top of these.
const (
	ErrInvalidContext           ErrorCode = "INVALID_CONTEXT"
	ErrFlagNotFound             ErrorCode = "FLAG_NOT_FOUND"
	ErrSegmentNotFound          ErrorCode = "SEGMENT_NOT_FOUND"
	ErrMalformedFlag            ErrorCode = "MALFORMED_FLAG"
	ErrMalformedSegment         ErrorCode = "MALFORMED_SEGMENT"
	ErrPrerequisiteCycle        ErrorCode = "PREREQUISITE_CYCLE"
	ErrPrerequisiteDepthExceeds ErrorCode = "PREREQUISITE_DEPTH_EXCEEDED"
)

// ReasonKind says how a value was chosen.
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

// Reason is why a flag served what it did. Only the field its Kind names is
// set: RuleIndex for RULE_MATCH, FlagKey for PREREQUISITE_FAILED, Code for ERROR.
type Reason struct {
	Kind      ReasonKind
	Code      ErrorCode
	RuleIndex int
	FlagKey   string
}

// Result is one evaluation. VariationIndex is nil when nothing was served.
type Result struct {
	Value          wjson.Value
	VariationIndex *int
	Reason         Reason
}

func failure(code ErrorCode) Result {
	return Result{Reason: Reason{Kind: ReasonError, Code: code}}
}
