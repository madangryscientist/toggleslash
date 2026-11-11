// Package evaluator implements docs/evaluation.md for the Go SDK: it loads a ruleset and evaluates a flag for a
// context. It has no network code. Strings are WTF-8 (see internal/jsonv), and nothing here uses a platform
// regular expression.
package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"

// ReasonKind is the kind of a Reason, spelled as in the spec.
type ReasonKind string

// The reason kinds the evaluator produces. The SDK adds none of its own: its ERROR codes live in ErrorCode.
const (
	ReasonOff                ReasonKind = "OFF"
	ReasonPrerequisiteFailed ReasonKind = "PREREQUISITE_FAILED"
	ReasonTargetMatch        ReasonKind = "TARGET_MATCH"
	ReasonRuleMatch          ReasonKind = "RULE_MATCH"
	ReasonFallthrough        ReasonKind = "FALLTHROUGH"
	ReasonError              ReasonKind = "ERROR"
)

// ErrorCode is the code of an ERROR reason, spelled as in the spec.
type ErrorCode string

// The evaluator's error codes.
const (
	ErrInvalidContext           ErrorCode = "INVALID_CONTEXT"
	ErrFlagNotFound             ErrorCode = "FLAG_NOT_FOUND"
	ErrSegmentNotFound          ErrorCode = "SEGMENT_NOT_FOUND"
	ErrMalformedFlag            ErrorCode = "MALFORMED_FLAG"
	ErrMalformedSegment         ErrorCode = "MALFORMED_SEGMENT"
	ErrPrerequisiteCycle        ErrorCode = "PREREQUISITE_CYCLE"
	ErrPrerequisiteDepthExceeds ErrorCode = "PREREQUISITE_DEPTH_EXCEEDED"
)

// Reason says why a flag evaluated as it did. Only the field its Kind names is meaningful.
type Reason struct {
	Kind ReasonKind
	// RuleIndex is the index in the flag's rules, for RULE_MATCH.
	RuleIndex int
	// FlagKey is the direct prerequisite that failed, for PREREQUISITE_FAILED.
	FlagKey string
	// Code is the error code, for ERROR.
	Code ErrorCode
}

// Result is what Evaluate returns. Index is -1 when nothing was served, and Value is then null.
type Result struct {
	Value  jsonv.Value
	Index  int
	Reason Reason
}

func failure(code ErrorCode) Result {
	return Result{Index: -1, Reason: Reason{Kind: ReasonError, Code: code}}
}

const (
	// bucketCount is the number of buckets: a rollout weight of 1000 is 1%.
	bucketCount = 100000
	// maxPrerequisiteDepth is the most prerequisite links from the requested flag.
	maxPrerequisiteDepth = 32
	// defaultKind is the kind an absent kind means.
	defaultKind = "user"
)
