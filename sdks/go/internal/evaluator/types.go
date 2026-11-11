package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"

type opKind uint8

const (
	opIn opKind = iota
	opContains
	opStartsWith
	opEndsWith
	opLt
	opLte
	opGt
	opGte
	opSemverEq
	opSemverLt
	opSemverLte
	opSemverGt
	opSemverGte
	opBefore
	opAfter
	opMatches
	opSegmentMatch
)

// clauseValue is one value of a clause, with what its op needs parsed once at load.
type clauseValue struct {
	v          value
	pattern    *pattern // matches: nil when the pattern is not valid
	version    *semver  // semver ops: nil when the value is not a version
	instant    float64  // before, after
	hasInstant bool
}

type clause struct {
	op          opKind
	path        []string // the attribute: one literal name, or a reference's components
	negate      bool
	kind        string
	values      []clauseValue
	segmentKeys []string // segmentMatch
}

type weighted struct {
	variation int
	weight    int
}

type rollout struct {
	kind       string // "" is user
	variations []weighted
}

// serve is a variation, or a rollout when rollout is not nil.
type serve struct {
	variation int
	rollout   *rollout
}

type rule struct {
	clauses []*clause
	serve   serve
}

type target struct {
	variation int
	keys      []string
	kind      string
}

type prerequisite struct {
	flagKey   string
	variation float64
}

type flagHeader struct {
	on           bool
	variations   []jsonvalue.Value
	offVariation int // -1 when none
}

type flagBody struct {
	salt          string
	version       float64
	targets       []*target
	prerequisites []prerequisite
	rules         []*rule
	fallThrough   serve
}

type targetHit struct {
	order     int
	variation int
}

type flagStatus uint8

const (
	statusMalformed flagStatus = iota
	statusBroken
	statusReady
)

type flagEntry struct {
	status  flagStatus
	header  *flagHeader
	body    *flagBody
	code    string // broken: the error code
	targets map[string]map[string]targetHit
}

type segmentRule struct {
	clauses     []*clause
	weight      int // -1 when none
	rolloutKind string
}

type segment struct {
	key, salt string
	included  map[string]map[string]struct{}
	excluded  map[string]map[string]struct{}
	rules     []*segmentRule
}

type segmentEntry struct {
	malformed bool
	segment   *segment
}

// Ruleset is a validated, indexed ruleset. It is immutable after Compile and safe for concurrent
// use.
type Ruleset struct {
	flags    map[string]*flagEntry
	segments map[string]*segmentEntry
}

// Reason kinds.
const (
	ReasonOff                = "OFF"
	ReasonTargetMatch        = "TARGET_MATCH"
	ReasonRuleMatch          = "RULE_MATCH"
	ReasonFallthrough        = "FALLTHROUGH"
	ReasonPrerequisiteFailed = "PREREQUISITE_FAILED"
	ReasonError              = "ERROR"
)

// Reason is why a flag evaluated as it did.
type Reason struct {
	Kind      string
	RuleIndex *int   // RULE_MATCH
	FlagKey   string // PREREQUISITE_FAILED
	Code      string // ERROR
}

// Result is one evaluation. Value is null when nothing is served.
type Result struct {
	Value          jsonvalue.Value
	HasVariation   bool
	VariationIndex int
	Reason         Reason
}
