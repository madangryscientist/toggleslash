package eval

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

// The shape of a ruleset, checked at runtime because rulesets arrive as JSON. docs/evaluation.md is
// normative for what counts as malformed. Null and absent are the same for an optional field. Each check
// returns the typed value it read, so the evaluator never looks at JSON again.

type op uint8

const (
	opIn op = iota
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

var attributeOps = map[string]op{
	"in": opIn, "contains": opContains, "startsWith": opStartsWith, "endsWith": opEndsWith,
	"lt": opLt, "lte": opLte, "gt": opGt, "gte": opGte,
	"semverEq": opSemverEq, "semverLt": opSemverLt, "semverLte": opSemverLte,
	"semverGt": opSemverGt, "semverGte": opSemverGte,
	"before": opBefore, "after": opAfter, "matches": opMatches,
}

type clause struct {
	op        op
	attribute string
	ref       []string // the components of a `/` reference; nil for a plain name
	values    []jsonvalue.Value
	patterns  []*program // for matches: one per value, nil when the value is not a valid pattern
	negate    bool
	kind      string   // "" is user
	segments  []string // for segmentMatch
}

type weighted struct {
	variation int
	weight    int
}

type rollout struct {
	kind    string
	entries []weighted
}

// serve is a variation, or a rollout when rollout is not nil.
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

type flagHeader struct {
	key        string
	on         bool
	variations []jsonvalue.Value
	off        int // -1 when there is none
}

type flag struct {
	flagHeader
	salt          string
	version       float64
	targets       []target
	prerequisites []prerequisite
	rules         []rule
	fallthru      serve
}

type segmentRule struct {
	clauses  []clause
	weight   int // -1 when there is none
	rollKind string
}

type contextList struct {
	kind   string
	values []string
}

type segment struct {
	key      string
	salt     string
	included []string
	excluded []string
	incCtx   []contextList
	excCtx   []contextList
	rules    []segmentRule
}

func isCount(v jsonvalue.Value) (float64, bool) {
	if v.Kind != jsonvalue.Number || math.IsInf(v.N, 0) || v.N != math.Trunc(v.N) || v.N < 0 {
		return 0, false
	}
	return v.N, true
}

func variationIndex(v jsonvalue.Value, count int) (int, bool) {
	n, ok := isCount(v)
	if !ok || n >= float64(count) {
		return 0, false
	}
	return int(n), true
}

func isKey(v jsonvalue.Value) (string, bool) {
	if v.Kind != jsonvalue.String || v.S == "" {
		return "", false
	}
	return v.S, true
}

func stringArray(v jsonvalue.Value) ([]string, bool) {
	if v.Kind != jsonvalue.Array {
		return nil, false
	}
	out := make([]string, 0, len(v.A))
	for _, item := range v.A {
		if item.Kind != jsonvalue.String {
			return nil, false
		}
		out = append(out, item.S)
	}
	return out, true
}

// optionalKind reads a kind that may be absent ("" then).
func optionalKind(v jsonvalue.Value) (string, bool) {
	if v.IsAbsent() {
		return "", true
	}
	if v.Kind == jsonvalue.String && isKind(v.S) {
		return v.S, true
	}
	return "", false
}

func isClauseValue(v jsonvalue.Value) bool {
	switch v.Kind {
	case jsonvalue.Number:
		return !math.IsInf(v.N, 0) && !math.IsNaN(v.N)
	case jsonvalue.String, jsonvalue.Bool:
		return true
	}
	return false
}

func readNegate(v jsonvalue.Value) (bool, bool) {
	switch v.Kind {
	case jsonvalue.Null:
		return false, true
	case jsonvalue.Bool:
		return v.B, true
	}
	return false, false
}

func attributeName(v jsonvalue.Value) (string, []string, bool) {
	name, ok := isKey(v)
	if !ok {
		return "", nil, false
	}
	if name[0] != '/' {
		return name, nil, true
	}
	ref, ok := parseReference(name)
	return name, ref, ok
}

func readAttributeClause(v jsonvalue.Value) (clause, bool) {
	if v.Kind != jsonvalue.Object {
		return clause{}, false
	}
	opName, _ := v.Get("op")
	o, known := attributeOps[opName.S]
	if opName.Kind != jsonvalue.String || !known {
		return clause{}, false
	}
	attr, _ := v.Get("attribute")
	name, ref, ok := attributeName(attr)
	if !ok {
		return clause{}, false
	}
	negateValue, _ := v.Get("negate")
	negate, ok := readNegate(negateValue)
	if !ok {
		return clause{}, false
	}
	kindValue, _ := v.Get("contextKind")
	kind, ok := optionalKind(kindValue)
	if !ok {
		return clause{}, false
	}
	values, _ := v.Get("values")
	if values.Kind != jsonvalue.Array {
		return clause{}, false
	}
	c := clause{op: o, attribute: name, ref: ref, values: values.A, negate: negate, kind: kind}
	for _, item := range values.A {
		if !isClauseValue(item) {
			return clause{}, false
		}
	}
	if o == opMatches {
		c.patterns = make([]*program, len(values.A))
		for i, item := range values.A {
			if item.Kind == jsonvalue.String {
				c.patterns[i] = compilePattern(item.S)
			}
		}
	}
	return c, true
}

func readSegmentClause(v jsonvalue.Value) (clause, bool) {
	if v.Kind != jsonvalue.Object {
		return clause{}, false
	}
	if opName, _ := v.Get("op"); opName.Kind != jsonvalue.String || opName.S != "segmentMatch" {
		return clause{}, false
	}
	negateValue, _ := v.Get("negate")
	negate, ok := readNegate(negateValue)
	if !ok {
		return clause{}, false
	}
	values, _ := v.Get("values")
	keys, ok := stringArray(values)
	if !ok {
		return clause{}, false
	}
	return clause{op: opSegmentMatch, negate: negate, segments: keys}, true
}

func readRollout(v jsonvalue.Value, count int) (*rollout, bool) {
	if v.Kind != jsonvalue.Object {
		return nil, false
	}
	kindValue, _ := v.Get("contextKind")
	kind, ok := optionalKind(kindValue)
	if !ok {
		return nil, false
	}
	variations, _ := v.Get("variations")
	if variations.Kind != jsonvalue.Array || len(variations.A) == 0 {
		return nil, false
	}
	r := &rollout{kind: kind}
	total := 0
	for _, item := range variations.A {
		if item.Kind != jsonvalue.Object {
			return nil, false
		}
		varValue, _ := item.Get("variation")
		index, ok := variationIndex(varValue, count)
		weightValue, _ := item.Get("weight")
		weight, wok := isCount(weightValue)
		if !ok || !wok || weight > bucketCount {
			return nil, false
		}
		r.entries = append(r.entries, weighted{variation: index, weight: int(weight)})
		total += int(weight)
	}
	if total != bucketCount {
		return nil, false
	}
	return r, true
}

func readServe(v jsonvalue.Value, count int) (serve, bool) {
	if v.Kind != jsonvalue.Object {
		return serve{}, false
	}
	variation, _ := v.Get("variation")
	rolloutValue, _ := v.Get("rollout")
	if rolloutValue.IsAbsent() {
		index, ok := variationIndex(variation, count)
		return serve{variation: index}, ok
	}
	if !variation.IsAbsent() {
		return serve{}, false
	}
	r, ok := readRollout(rolloutValue, count)
	return serve{rollout: r}, ok
}

func readRule(v jsonvalue.Value, count int) (rule, bool) {
	if v.Kind != jsonvalue.Object {
		return rule{}, false
	}
	clauses, _ := v.Get("clauses")
	if clauses.Kind != jsonvalue.Array {
		return rule{}, false
	}
	r := rule{}
	for _, item := range clauses.A {
		c, ok := readAttributeClause(item)
		if !ok {
			if c, ok = readSegmentClause(item); !ok {
				return rule{}, false
			}
		}
		r.clauses = append(r.clauses, c)
	}
	s, ok := readServe(v, count)
	r.serve = s
	return r, ok
}

func readTarget(v jsonvalue.Value, count int) (target, bool) {
	if v.Kind != jsonvalue.Object {
		return target{}, false
	}
	variation, _ := v.Get("variation")
	index, ok := variationIndex(variation, count)
	keysValue, _ := v.Get("contextKeys")
	keys, kok := stringArray(keysValue)
	kindValue, _ := v.Get("contextKind")
	kind, nok := optionalKind(kindValue)
	if !ok || !kok || !nok {
		return target{}, false
	}
	return target{variation: index, keys: keys, kind: kind}, true
}

func readPrerequisite(v jsonvalue.Value) (prerequisite, bool) {
	if v.Kind != jsonvalue.Object {
		return prerequisite{}, false
	}
	keyValue, _ := v.Get("flagKey")
	key, ok := isKey(keyValue)
	variationValue, _ := v.Get("variation")
	variation, vok := isCount(variationValue)
	if !ok || !vok {
		return prerequisite{}, false
	}
	return prerequisite{flagKey: key, variation: variation}, true
}

// readFlagHeader reads what serving the off variation needs. It is checked apart so `on: false` survives
// a bad rule.
func readFlagHeader(v jsonvalue.Value) (flagHeader, bool) {
	if v.Kind != jsonvalue.Object {
		return flagHeader{}, false
	}
	on, _ := v.Get("on")
	if on.Kind != jsonvalue.Bool {
		return flagHeader{}, false
	}
	keyValue, _ := v.Get("key")
	key, ok := isKey(keyValue)
	variations, _ := v.Get("variations")
	if !ok || variations.Kind != jsonvalue.Array || len(variations.A) == 0 {
		return flagHeader{}, false
	}
	h := flagHeader{key: key, on: on.B, variations: variations.A, off: -1}
	offValue, _ := v.Get("offVariation")
	if !offValue.IsAbsent() {
		index, ok := variationIndex(offValue, len(variations.A))
		if !ok {
			return flagHeader{}, false
		}
		h.off = index
	}
	return h, true
}

// readFlagBody checks the whole flag, not only the path a context would take.
func readFlagBody(v jsonvalue.Value, header flagHeader) (*flag, bool) {
	count := len(header.variations)
	f := &flag{flagHeader: header}
	saltValue, _ := v.Get("salt")
	versionValue, _ := v.Get("version")
	version, vok := isCount(versionValue)
	if saltValue.Kind != jsonvalue.String || !vok {
		return nil, false
	}
	f.salt, f.version = saltValue.S, version

	targets, _ := v.Get("targets")
	if targets.Kind != jsonvalue.Array {
		return nil, false
	}
	for _, item := range targets.A {
		t, ok := readTarget(item, count)
		if !ok {
			return nil, false
		}
		f.targets = append(f.targets, t)
	}
	prerequisites, _ := v.Get("prerequisites")
	if prerequisites.Kind != jsonvalue.Array {
		return nil, false
	}
	for _, item := range prerequisites.A {
		p, ok := readPrerequisite(item)
		if !ok {
			return nil, false
		}
		f.prerequisites = append(f.prerequisites, p)
	}
	rules, _ := v.Get("rules")
	if rules.Kind != jsonvalue.Array {
		return nil, false
	}
	for _, item := range rules.A {
		r, ok := readRule(item, count)
		if !ok {
			return nil, false
		}
		f.rules = append(f.rules, r)
	}
	fallthroughValue, _ := v.Get("fallthrough")
	s, ok := readServe(fallthroughValue, count)
	if !ok {
		return nil, false
	}
	f.fallthru = s
	return f, true
}

func readContextLists(v jsonvalue.Value) ([]contextList, bool) {
	if v.IsAbsent() {
		return nil, true
	}
	if v.Kind != jsonvalue.Array {
		return nil, false
	}
	out := make([]contextList, 0, len(v.A))
	for _, item := range v.A {
		if item.Kind != jsonvalue.Object {
			return nil, false
		}
		kindValue, _ := item.Get("contextKind")
		valuesValue, _ := item.Get("values")
		values, ok := stringArray(valuesValue)
		if kindValue.Kind != jsonvalue.String || !isKind(kindValue.S) || !ok {
			return nil, false
		}
		out = append(out, contextList{kind: kindValue.S, values: values})
	}
	return out, true
}

func readSegmentRule(v jsonvalue.Value) (segmentRule, bool) {
	if v.Kind != jsonvalue.Object {
		return segmentRule{}, false
	}
	r := segmentRule{weight: -1}
	weightValue, _ := v.Get("weight")
	if !weightValue.IsAbsent() {
		weight, ok := isCount(weightValue)
		if !ok || weight > bucketCount {
			return segmentRule{}, false
		}
		r.weight = int(weight)
	}
	kindValue, _ := v.Get("rolloutContextKind")
	kind, ok := optionalKind(kindValue)
	if !ok {
		return segmentRule{}, false
	}
	r.rollKind = kind
	clauses, _ := v.Get("clauses")
	if clauses.Kind != jsonvalue.Array {
		return segmentRule{}, false
	}
	for _, item := range clauses.A {
		c, ok := readAttributeClause(item)
		if !ok {
			return segmentRule{}, false
		}
		r.clauses = append(r.clauses, c)
	}
	return r, true
}

func readSegment(v jsonvalue.Value) (*segment, bool) {
	if v.Kind != jsonvalue.Object {
		return nil, false
	}
	keyValue, _ := v.Get("key")
	key, ok := isKey(keyValue)
	saltValue, _ := v.Get("salt")
	if !ok || saltValue.Kind != jsonvalue.String {
		return nil, false
	}
	s := &segment{key: key, salt: saltValue.S}
	included, _ := v.Get("included")
	excluded, _ := v.Get("excluded")
	if s.included, ok = stringArray(included); !ok {
		return nil, false
	}
	if s.excluded, ok = stringArray(excluded); !ok {
		return nil, false
	}
	incCtx, _ := v.Get("includedContexts")
	excCtx, _ := v.Get("excludedContexts")
	if s.incCtx, ok = readContextLists(incCtx); !ok {
		return nil, false
	}
	if s.excCtx, ok = readContextLists(excCtx); !ok {
		return nil, false
	}
	rules, _ := v.Get("rules")
	if rules.Kind != jsonvalue.Array {
		return nil, false
	}
	for _, item := range rules.A {
		r, ok := readSegmentRule(item)
		if !ok {
			return nil, false
		}
		s.rules = append(s.rules, r)
	}
	return s, true
}
