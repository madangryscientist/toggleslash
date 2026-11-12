package evaluator

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

// The shape of a ruleset, checked at runtime because rulesets arrive as JSON. docs/evaluation.md is normative for
// what counts as malformed. Null and absent are the same for an optional field.

type attributeOp uint8

const (
	opIn attributeOp = iota
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
)

var attributeOps = map[string]attributeOp{
	"in": opIn, "contains": opContains, "startsWith": opStartsWith, "endsWith": opEndsWith,
	"lt": opLt, "lte": opLte, "gt": opGt, "gte": opGte,
	"semverEq": opSemverEq, "semverLt": opSemverLt, "semverLte": opSemverLte,
	"semverGt": opSemverGt, "semverGte": opSemverGte,
	"before": opBefore, "after": opAfter, "matches": opMatches,
}

// clause is an attribute clause, or a segmentMatch clause when segment is set.
type clause struct {
	segment bool
	negate  bool
	// attribute clauses
	op         attributeOp
	components []string // the attribute name as the components that are walked: one for a literal name
	kind       string   // the context kind, "user" when absent
	values     []jsonvalue.Value
	patterns   []pattern // for matches: the compiled pattern of each value, nil when it is not valid
	// segment clauses
	segmentKeys []string
}

type weighted struct {
	variation int
	weight    int
}

type rollout struct {
	entries []weighted
	kind    string
}

// serve is a variation, or a rollout when rollout is set.
type serve struct {
	variation int
	rollout   *rollout
}

type rule struct {
	clauses []clause
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

// flag is a validated flag. The body fields are set only when the whole flag is valid.
type flag struct {
	key           string
	on            bool
	variations    []jsonvalue.Value
	offVariation  int // NoVariation when there is none
	salt          string
	version       float64
	targets       []target
	prerequisites []prerequisite
	rules         []rule
	fall          serve
}

type segmentRule struct {
	clauses     []clause
	hasWeight   bool
	weight      int
	rolloutKind string
}

type segment struct {
	key      string
	salt     string
	included keysByKind
	excluded keysByKind
	rules    []segmentRule
}

// keysByKind is the kind, then the keys of that kind.
type keysByKind map[string]map[string]struct{}

func isKey(v jsonvalue.Value) (string, bool) {
	s, ok := v.AsString()
	return s, ok && s != ""
}

func isCount(v jsonvalue.Value) (float64, bool) {
	n, ok := v.AsNumber()
	if !ok || math.IsInf(n, 0) || math.IsNaN(n) || n != math.Trunc(n) || n < 0 {
		return 0, false
	}
	return n, true
}

// isVariationIndex reads an integer 0 or more and below count.
func isVariationIndex(v jsonvalue.Value, count int) (int, bool) {
	n, ok := isCount(v)
	if !ok || n >= float64(count) {
		return 0, false
	}
	return int(n), true
}

func stringArray(v jsonvalue.Value) ([]string, bool) {
	items, ok := v.AsArray()
	if !ok {
		return nil, false
	}
	out := make([]string, len(items))
	for i, item := range items {
		s, ok := item.AsString()
		if !ok {
			return nil, false
		}
		out[i] = s
	}
	return out, true
}

// optionalKind reads a kind that may be absent; an absent one is "user".
func optionalKind(v jsonvalue.Value) (string, bool) {
	if isAbsent(v) {
		return DefaultKind, true
	}
	s, ok := v.AsString()
	return s, ok && IsKind(s)
}

func isClauseValue(v jsonvalue.Value) bool {
	if n, ok := v.AsNumber(); ok {
		return !math.IsInf(n, 0) && !math.IsNaN(n)
	}
	_, isString := v.AsString()
	_, isBool := v.AsBool()
	return isString || isBool
}

func optionalBool(v jsonvalue.Value) (bool, bool) {
	if isAbsent(v) {
		return false, true
	}
	return v.AsBool()
}

func attributeComponents(name string) ([]string, bool) {
	if name == "" {
		return nil, false
	}
	if name[0] != '/' {
		return []string{name}, true
	}
	return parseReference(name)
}

func readAttributeClause(v jsonvalue.Value) (clause, bool) {
	if !v.IsObject() {
		return clause{}, false
	}
	opName, _ := field(v, "op").AsString()
	op, ok := attributeOps[opName]
	if !ok {
		return clause{}, false
	}
	name, _ := field(v, "attribute").AsString()
	components, ok := attributeComponents(name)
	if !ok {
		return clause{}, false
	}
	negate, ok := optionalBool(field(v, "negate"))
	if !ok {
		return clause{}, false
	}
	kind, ok := optionalKind(field(v, "contextKind"))
	if !ok {
		return clause{}, false
	}
	values, ok := field(v, "values").AsArray()
	if !ok {
		return clause{}, false
	}
	for _, value := range values {
		if !isClauseValue(value) {
			return clause{}, false
		}
	}
	c := clause{op: op, components: components, kind: kind, values: values, negate: negate}
	if op == opMatches {
		c.patterns = make([]pattern, len(values))
		for i, value := range values {
			if s, isString := value.AsString(); isString {
				c.patterns[i], _ = compilePattern(s)
			}
		}
	}
	return c, true
}

func readSegmentClause(v jsonvalue.Value) (clause, bool) {
	if !v.IsObject() {
		return clause{}, false
	}
	if op, _ := field(v, "op").AsString(); op != "segmentMatch" {
		return clause{}, false
	}
	negate, ok := optionalBool(field(v, "negate"))
	if !ok {
		return clause{}, false
	}
	keys, ok := stringArray(field(v, "values"))
	if !ok {
		return clause{}, false
	}
	return clause{segment: true, negate: negate, segmentKeys: keys}, true
}

func readRolloutEntry(v jsonvalue.Value, variationCount int) (weighted, bool) {
	if !v.IsObject() {
		return weighted{}, false
	}
	variation, ok := isVariationIndex(field(v, "variation"), variationCount)
	if !ok {
		return weighted{}, false
	}
	weight, ok := isCount(field(v, "weight"))
	if !ok || weight > BucketCount {
		return weighted{}, false
	}
	return weighted{variation: variation, weight: int(weight)}, true
}

func readRollout(v jsonvalue.Value, variationCount int) (*rollout, bool) {
	if !v.IsObject() {
		return nil, false
	}
	kind, ok := optionalKind(field(v, "contextKind"))
	if !ok {
		return nil, false
	}
	items, ok := field(v, "variations").AsArray()
	if !ok || len(items) == 0 {
		return nil, false
	}
	r := &rollout{kind: kind}
	total := 0
	for _, item := range items {
		entry, ok := readRolloutEntry(item, variationCount)
		if !ok {
			return nil, false
		}
		total += entry.weight
		r.entries = append(r.entries, entry)
	}
	return r, total == BucketCount
}

// readServe reads the `variation` or `rollout` of a rule or of the fallthrough: exactly one of them.
func readServe(v jsonvalue.Value, variationCount int) (serve, bool) {
	if !v.IsObject() {
		return serve{}, false
	}
	variation, rawRollout := field(v, "variation"), field(v, "rollout")
	if isAbsent(rawRollout) {
		index, ok := isVariationIndex(variation, variationCount)
		return serve{variation: index}, ok
	}
	if !isAbsent(variation) {
		return serve{}, false
	}
	r, ok := readRollout(rawRollout, variationCount)
	return serve{rollout: r}, ok
}

func readRule(v jsonvalue.Value, variationCount int) (rule, bool) {
	if !v.IsObject() {
		return rule{}, false
	}
	items, ok := field(v, "clauses").AsArray()
	if !ok {
		return rule{}, false
	}
	var r rule
	for _, item := range items {
		c, ok := readAttributeClause(item)
		if !ok {
			if c, ok = readSegmentClause(item); !ok {
				return rule{}, false
			}
		}
		r.clauses = append(r.clauses, c)
	}
	s, ok := readServe(v, variationCount)
	r.serve = s
	return r, ok
}

func readTarget(v jsonvalue.Value, variationCount int) (target, bool) {
	if !v.IsObject() {
		return target{}, false
	}
	variation, ok := isVariationIndex(field(v, "variation"), variationCount)
	if !ok {
		return target{}, false
	}
	keys, ok := stringArray(field(v, "contextKeys"))
	if !ok {
		return target{}, false
	}
	kind, ok := optionalKind(field(v, "contextKind"))
	return target{variation: variation, keys: keys, kind: kind}, ok
}

func readPrerequisite(v jsonvalue.Value) (prerequisite, bool) {
	if !v.IsObject() {
		return prerequisite{}, false
	}
	key, ok := isKey(field(v, "flagKey"))
	if !ok {
		return prerequisite{}, false
	}
	variation, ok := isCount(field(v, "variation"))
	return prerequisite{flagKey: key, variation: variation}, ok
}

// readFlagHeader reads what serving the off variation needs. It is checked apart so `on: false` survives a bad rule.
func readFlagHeader(v jsonvalue.Value) (*flag, bool) {
	if !v.IsObject() {
		return nil, false
	}
	on, ok := field(v, "on").AsBool()
	if !ok {
		return nil, false
	}
	key, ok := isKey(field(v, "key"))
	if !ok {
		return nil, false
	}
	variations, ok := field(v, "variations").AsArray()
	if !ok || len(variations) == 0 {
		return nil, false
	}
	f := &flag{key: key, on: on, variations: variations, offVariation: NoVariation}
	if off := field(v, "offVariation"); !isAbsent(off) {
		index, ok := isVariationIndex(off, len(variations))
		if !ok {
			return nil, false
		}
		f.offVariation = index
	}
	return f, true
}

// readFlagBody reads the rest of the flag into f. It reports false when anything in it is invalid.
func readFlagBody(v jsonvalue.Value, f *flag) bool {
	count := len(f.variations)
	salt, ok := field(v, "salt").AsString()
	if !ok {
		return false
	}
	version, ok := isCount(field(v, "version"))
	if !ok {
		return false
	}
	f.salt, f.version = salt, version
	targets, ok := field(v, "targets").AsArray()
	if !ok {
		return false
	}
	for _, item := range targets {
		t, ok := readTarget(item, count)
		if !ok {
			return false
		}
		f.targets = append(f.targets, t)
	}
	prerequisites, ok := field(v, "prerequisites").AsArray()
	if !ok {
		return false
	}
	for _, item := range prerequisites {
		p, ok := readPrerequisite(item)
		if !ok {
			return false
		}
		f.prerequisites = append(f.prerequisites, p)
	}
	rules, ok := field(v, "rules").AsArray()
	if !ok {
		return false
	}
	for _, item := range rules {
		r, ok := readRule(item, count)
		if !ok {
			return false
		}
		f.rules = append(f.rules, r)
	}
	fall, ok := readServe(field(v, "fallthrough"), count)
	f.fall = fall
	return ok
}

func readSegmentRule(v jsonvalue.Value) (segmentRule, bool) {
	if !v.IsObject() {
		return segmentRule{}, false
	}
	var r segmentRule
	if raw := field(v, "weight"); !isAbsent(raw) {
		weight, ok := isCount(raw)
		if !ok || weight > BucketCount {
			return segmentRule{}, false
		}
		r.hasWeight, r.weight = true, int(weight)
	}
	kind, ok := optionalKind(field(v, "rolloutContextKind"))
	if !ok {
		return segmentRule{}, false
	}
	r.rolloutKind = kind
	items, ok := field(v, "clauses").AsArray()
	if !ok {
		return segmentRule{}, false
	}
	for _, item := range items {
		c, ok := readAttributeClause(item)
		if !ok {
			return segmentRule{}, false
		}
		r.clauses = append(r.clauses, c)
	}
	return r, true
}

// readContextLists adds the keys of each `{ contextKind, values }` entry to index. Absent lists add nothing.
func readContextLists(v jsonvalue.Value, index keysByKind) bool {
	if isAbsent(v) {
		return true
	}
	items, ok := v.AsArray()
	if !ok {
		return false
	}
	for _, item := range items {
		if !item.IsObject() {
			return false
		}
		kind, ok := field(item, "contextKind").AsString()
		if !ok || !IsKind(kind) {
			return false
		}
		values, ok := stringArray(field(item, "values"))
		if !ok {
			return false
		}
		addKeys(index, kind, values)
	}
	return true
}

func addKeys(index keysByKind, kind string, keys []string) {
	set := index[kind]
	if set == nil {
		set = map[string]struct{}{}
		index[kind] = set
	}
	for _, k := range keys {
		set[k] = struct{}{}
	}
}

func readSegment(v jsonvalue.Value) (*segment, bool) {
	if !v.IsObject() {
		return nil, false
	}
	key, ok := isKey(field(v, "key"))
	if !ok {
		return nil, false
	}
	salt, ok := field(v, "salt").AsString()
	if !ok {
		return nil, false
	}
	included, ok1 := stringArray(field(v, "included"))
	excluded, ok2 := stringArray(field(v, "excluded"))
	if !ok1 || !ok2 {
		return nil, false
	}
	s := &segment{key: key, salt: salt, included: keysByKind{}, excluded: keysByKind{}}
	addKeys(s.included, DefaultKind, included)
	addKeys(s.excluded, DefaultKind, excluded)
	if !readContextLists(field(v, "includedContexts"), s.included) || !readContextLists(field(v, "excludedContexts"), s.excluded) {
		return nil, false
	}
	rules, ok := field(v, "rules").AsArray()
	if !ok {
		return nil, false
	}
	for _, item := range rules {
		r, ok := readSegmentRule(item)
		if !ok {
			return nil, false
		}
		s.rules = append(s.rules, r)
	}
	return s, true
}
