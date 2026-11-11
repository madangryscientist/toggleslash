package toggleslash

import (
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// Value is a JSON value: null, a boolean, a number (a float64), a string, an array or an object. It is
// what a flag serves and what a Context is made of. The zero Value is null.
type Value = jsonv.Value

// Context is the end user an evaluation is for, in the shape docs/evaluation.md gives: an object with a
// "key" (and optionally a "kind" and "attributes"), or a multi-kind context. Build one with
// ParseContext, ContextFrom or NewUserContext.
type Context = Value

// NewNull returns the JSON null.
func NewNull() Value { return Value{} }

// NewBool returns a JSON boolean.
func NewBool(b bool) Value { return jsonv.NewBool(b) }

// NewNumber returns a JSON number.
func NewNumber(n float64) Value { return jsonv.NewNumber(n) }

// NewString returns a JSON string.
func NewString(s string) Value { return jsonv.NewString(s) }

// NewArray returns a JSON array.
func NewArray(items []Value) Value { return jsonv.NewArray(items) }

// NewObject returns a JSON object.
func NewObject(fields map[string]Value) Value { return jsonv.NewObject(fields) }

// ReasonKind is the kind of a Reason. The value of each constant is its canonical spelling.
type ReasonKind string

// The reason kinds of docs/evaluation.md.
const (
	ReasonOff                ReasonKind = "OFF"
	ReasonPrerequisiteFailed ReasonKind = "PREREQUISITE_FAILED"
	ReasonTargetMatch        ReasonKind = "TARGET_MATCH"
	ReasonRuleMatch          ReasonKind = "RULE_MATCH"
	ReasonFallthrough        ReasonKind = "FALLTHROUGH"
	ReasonError              ReasonKind = "ERROR"
)

// ErrorCode is the code of a Reason whose Kind is ReasonError. The value of each constant is its
// canonical spelling.
type ErrorCode string

// The error codes: those of docs/evaluation.md, then the three the SDK adds (contract section 4).
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

// Reason says why an evaluation answered as it did. Code is set when Kind is ReasonError, RuleIndex
// when it is ReasonRuleMatch, and FlagKey when it is ReasonPrerequisiteFailed; the other fields are
// their zero value.
type Reason struct {
	Kind      ReasonKind
	Code      ErrorCode
	RuleIndex int
	FlagKey   string
}

// Detail is the answer of a ...VariationDetail method. VariationIndex is nil whenever Value is the
// fallback (whatever the reason), and the index the flag served otherwise.
type Detail[T any] struct {
	Value          T
	VariationIndex *int
	Reason         Reason
}

// StreamState is the state of the live stream, in Status.
type StreamState string

// The stream states.
const (
	StreamOff          StreamState = "off"
	StreamConnecting   StreamState = "connecting"
	StreamConnected    StreamState = "connected"
	StreamDisconnected StreamState = "disconnected"
	StreamClosed       StreamState = "closed"
)

// LastError says why the most recent ruleset or stream attempt failed. The empty value means it did not.
type LastError string

// The fixed LastError values; an HTTP failure is "http_" followed by the status, such as "http_503".
const (
	LastErrorNone       LastError = ""
	LastErrorTimeout    LastError = "timeout"
	LastErrorBadRuleset LastError = "bad_ruleset"
	LastErrorNetwork    LastError = "network"
)

func httpError(status int) LastError { return LastError("http_" + itoa(status)) }

// Status is what the SDK knows about its own sync, as one consistent snapshot. It holds nothing about
// the server's URL, the key, any context or any flag.
type Status struct {
	// ETag is the tag of the ruleset held; HasETag is false before the first load and when the
	// server sent none.
	ETag    string
	HasETag bool
	// LastSyncAt is when the SDK last confirmed its ruleset matches the server; the zero time if never.
	LastSyncAt time.Time
	Stream     StreamState
	LastError  LastError
}

// EvaluationEvent is what WithAfterEvaluation's hook receives after every evaluation, fallbacks
// included. Value is the fallback when VariationIndex is nil. ETag is the tag of the ruleset held at
// that evaluation (HasETag false when none), and FlagVersion the flag's version when the flag is
// ready to evaluate, otherwise nil.
type EvaluationEvent struct {
	FlagKey        string
	Context        Context
	Value          Value
	VariationIndex *int
	Reason         Reason
	ETag           string
	HasETag        bool
	FlagVersion    *int64
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
