package evaluator

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// The ruleset as the evaluator holds it: validated and indexed once at Load, so Evaluate does none of
// that work. docs/evaluation.md is normative for what is malformed. Null and absent are the same for an
// optional field.

type op int

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

var opNames = map[string]op{
	"in": opIn, "contains": opContains, "startsWith": opStartsWith, "endsWith": opEndsWith,
	"lt": opLt, "lte": opLte, "gt": opGt, "gte": opGte,
	"semverEq": opSemverEq, "semverLt": opSemverLt, "semverLte": opSemverLte,
	"semverGt": opSemverGt, "semverGte": opSemverGte,
	"before": opBefore, "after": opAfter, "matches": opMatches,
}

// clause is an attribute clause, or a segmentMatch clause when op is opSegmentMatch.
type clause struct {
	op       op
	negate   bool
	kind     string // "" is "user"
	path     []string
	values   []jsonv.Value
	patterns []*program // for matches: one per value, nil for a pattern that is not valid
	segments []string   // for segmentMatch
}

type weighted struct {
	variation int
	weight    int
}

type rollout struct {
	kind       string
	variations []weighted
}

type serve struct {
	variation int
	rollout   *rollout
}

type rule struct {
	clauses []clause
	serve   serve
}

type targetHit struct {
	order     int
	variation int
}

// targetIndex is the targets by kind, then by key; the first target in array order holds each key.
type targetIndex map[string]map[string]targetHit

type prerequisite struct {
	flagKey   string
	variation float64
}

type status int

const (
	statusMalformed status = iota
	statusBroken
	statusReady
)

type flagEntry struct {
	status status
	code   ErrorCode // for statusBroken
	// The header: what serving the off variation needs.
	on           bool
	variations   []jsonv.Value
	offVariation int // -1 when there is none
	// The body, for statusReady.
	key           string
	salt          string
	version       float64
	targets       targetIndex
	prerequisites []prerequisite
	rules         []rule
	fall          serve
}

type segmentRule struct {
	clauses     []clause
	weight      int // -1 when there is none
	rolloutKind string
}

// keysByKind is the keys a segment lists, by kind.
type keysByKind map[string]map[string]struct{}

type segmentEntry struct {
	ok       bool
	key      string
	salt     string
	included keysByKind
	excluded keysByKind
	rules    []segmentRule
}

// Ruleset is a validated and indexed ruleset. It is immutable, so any number of goroutines may evaluate
// against one.
type Ruleset struct {
	flags    map[string]*flagEntry
	segments map[string]*segmentEntry
}

func isCount(v jsonv.Value) (float64, bool) {
	n, ok := v.Number()
	if !ok || math.IsInf(n, 0) || math.IsNaN(n) || n != math.Trunc(n) || n < 0 {
		return 0, false
	}
	return n, true
}

func isVariationIndex(v jsonv.Value, count int) (int, bool) {
	n, ok := isCount(v)
	if !ok || n >= float64(count) {
		return 0, false
	}
	return int(n), true
}

// optionalKind reads a contextKind that may be absent: "" for absent, and false when it is invalid.
func optionalKind(v jsonv.Value) (string, bool) {
	if v.IsNull() {
		return "", true
	}
	s, ok := v.Str()
	if !ok || !isKind(s) {
		return "", false
	}
	return s, true
}

func optionalBool(v jsonv.Value) (bool, bool) {
	if v.IsNull() {
		return false, true
	}
	return v.Bool()
}

func stringArray(v jsonv.Value) ([]string, bool) {
	items, ok := v.Array()
	if !ok {
		return nil, false
	}
	out := make([]string, len(items))
	for i, item := range items {
		s, ok := item.Str()
		if !ok {
			return nil, false
		}
		out[i] = s
	}
	return out, true
}

func isClauseValue(v jsonv.Value) bool {
	switch v.Kind() {
	case jsonv.Number:
		return v.IsFinite()
	case jsonv.String, jsonv.Bool:
		return true
	}
	return false
}

// attributeName reads a clause's attribute: a literal name, or a `/` reference.
func attributeName(v jsonv.Value) ([]string, bool) {
	s, ok := v.Str()
	if !ok || s == "" {
		return nil, false
	}
	if s[0] != '/' {
		return []string{s}, true
	}
	return parseReference(s)
}

func readAttributeClause(v jsonv.Value) (clause, bool) {
	obj, ok := v.Object()
	if !ok {
		return clause{}, false
	}
	opName, _ := obj["op"].Str()
	o, known := opNames[opName]
	if !known {
		return clause{}, false
	}
	path, ok := attributeName(obj["attribute"])
	if !ok {
		return clause{}, false
	}
	negate, ok := optionalBool(obj["negate"])
	if !ok {
		return clause{}, false
	}
	kind, ok := optionalKind(obj["contextKind"])
	if !ok {
		return clause{}, false
	}
	values, ok := obj["values"].Array()
	if !ok {
		return clause{}, false
	}
	c := clause{op: o, negate: negate, kind: kind, path: path, values: values}
	for _, val := range values {
		if !isClauseValue(val) {
			return clause{}, false
		}
	}
	if o == opMatches {
		c.patterns = make([]*program, len(values))
		for i, val := range values {
			if s, isStr := val.Str(); isStr {
				c.patterns[i] = compilePattern(s)
			}
		}
	}
	return c, true
}

func readSegmentClause(v jsonv.Value) (clause, bool) {
	obj, ok := v.Object()
	if !ok {
		return clause{}, false
	}
	if name, _ := obj["op"].Str(); name != "segmentMatch" {
		return clause{}, false
	}
	negate, ok := optionalBool(obj["negate"])
	if !ok {
		return clause{}, false
	}
	keys, ok := stringArray(obj["values"])
	if !ok {
		return clause{}, false
	}
	return clause{op: opSegmentMatch, negate: negate, segments: keys}, true
}

func readRollout(v jsonv.Value, variationCount int) (*rollout, bool) {
	obj, ok := v.Object()
	if !ok {
		return nil, false
	}
	kind, ok := optionalKind(obj["contextKind"])
	if !ok {
		return nil, false
	}
	items, ok := obj["variations"].Array()
	if !ok || len(items) == 0 {
		return nil, false
	}
	r := &rollout{kind: kind, variations: make([]weighted, 0, len(items))}
	total := 0
	for _, item := range items {
		o, ok := item.Object()
		if !ok {
			return nil, false
		}
		variation, ok := isVariationIndex(o["variation"], variationCount)
		if !ok {
			return nil, false
		}
		w, ok := isCount(o["weight"])
		if !ok || w > bucketCount {
			return nil, false
		}
		r.variations = append(r.variations, weighted{variation: variation, weight: int(w)})
		total += int(w)
	}
	if total != bucketCount {
		return nil, false
	}
	return r, true
}

// readServe reads the variation or rollout of a rule or a fallthrough: exactly one of them.
func readServe(obj map[string]jsonv.Value, variationCount int) (serve, bool) {
	ro := obj["rollout"]
	if ro.IsNull() {
		i, ok := isVariationIndex(obj["variation"], variationCount)
		return serve{variation: i}, ok
	}
	if !obj["variation"].IsNull() {
		return serve{}, false
	}
	r, ok := readRollout(ro, variationCount)
	return serve{rollout: r}, ok
}

func readRule(v jsonv.Value, variationCount int) (rule, bool) {
	obj, ok := v.Object()
	if !ok {
		return rule{}, false
	}
	items, ok := obj["clauses"].Array()
	if !ok {
		return rule{}, false
	}
	r := rule{clauses: make([]clause, 0, len(items))}
	for _, item := range items {
		c, ok := readAttributeClause(item)
		if !ok {
			c, ok = readSegmentClause(item)
		}
		if !ok {
			return rule{}, false
		}
		r.clauses = append(r.clauses, c)
	}
	s, ok := readServe(obj, variationCount)
	r.serve = s
	return r, ok
}

func readTargets(v jsonv.Value, variationCount int) (targetIndex, bool) {
	items, ok := v.Array()
	if !ok {
		return nil, false
	}
	index := targetIndex{}
	for order, item := range items {
		obj, ok := item.Object()
		if !ok {
			return nil, false
		}
		variation, ok := isVariationIndex(obj["variation"], variationCount)
		if !ok {
			return nil, false
		}
		keys, ok := stringArray(obj["contextKeys"])
		if !ok {
			return nil, false
		}
		kind, ok := optionalKind(obj["contextKind"])
		if !ok {
			return nil, false
		}
		if kind == "" {
			kind = defaultKind
		}
		byKey := index[kind]
		if byKey == nil {
			byKey = map[string]targetHit{}
			index[kind] = byKey
		}
		for _, k := range keys {
			if _, held := byKey[k]; !held {
				byKey[k] = targetHit{order: order, variation: variation}
			}
		}
	}
	return index, true
}

func readPrerequisites(v jsonv.Value) ([]prerequisite, bool) {
	items, ok := v.Array()
	if !ok {
		return nil, false
	}
	out := make([]prerequisite, 0, len(items))
	for _, item := range items {
		obj, ok := item.Object()
		if !ok {
			return nil, false
		}
		key, ok := obj["flagKey"].Str()
		if !ok || key == "" {
			return nil, false
		}
		n, ok := isCount(obj["variation"])
		if !ok {
			return nil, false
		}
		out = append(out, prerequisite{flagKey: key, variation: n})
	}
	return out, true
}

// readHeader checks what serving the off variation needs. It is checked apart so `on: false` survives a
// bad rule.
func readHeader(obj map[string]jsonv.Value) (*flagEntry, bool) {
	on, ok := obj["on"].Bool()
	if !ok {
		return nil, false
	}
	key, ok := obj["key"].Str()
	if !ok || key == "" {
		return nil, false
	}
	variations, ok := obj["variations"].Array()
	if !ok || len(variations) == 0 {
		return nil, false
	}
	off := -1
	if ov := obj["offVariation"]; !ov.IsNull() {
		i, ok := isVariationIndex(ov, len(variations))
		if !ok {
			return nil, false
		}
		off = i
	}
	return &flagEntry{on: on, key: key, variations: variations, offVariation: off}, true
}

func readBody(e *flagEntry, obj map[string]jsonv.Value) bool {
	n := len(e.variations)
	salt, ok := obj["salt"].Str()
	if !ok {
		return false
	}
	version, ok := isCount(obj["version"])
	if !ok {
		return false
	}
	targets, ok := readTargets(obj["targets"], n)
	if !ok {
		return false
	}
	prereqs, ok := readPrerequisites(obj["prerequisites"])
	if !ok {
		return false
	}
	ruleItems, ok := obj["rules"].Array()
	if !ok {
		return false
	}
	rules := make([]rule, 0, len(ruleItems))
	for _, item := range ruleItems {
		r, ok := readRule(item, n)
		if !ok {
			return false
		}
		rules = append(rules, r)
	}
	ft, ok := obj["fallthrough"].Object()
	if !ok {
		return false
	}
	fall, ok := readServe(ft, n)
	if !ok {
		return false
	}
	e.salt, e.version, e.targets, e.prerequisites, e.rules, e.fall = salt, version, targets, prereqs, rules, fall
	return true
}

// segmentProblem is the first reference to a segment that is missing or malformed, in rule, clause,
// value order.
func segmentProblem(e *flagEntry, segments map[string]*segmentEntry) (ErrorCode, bool) {
	for _, r := range e.rules {
		for _, c := range r.clauses {
			for _, key := range c.segments {
				seg, found := segments[key]
				if !found {
					return ErrSegmentNotFound, true
				}
				if !seg.ok {
					return ErrMalformedSegment, true
				}
			}
		}
	}
	return "", false
}

func compileFlag(v jsonv.Value, segments map[string]*segmentEntry) *flagEntry {
	obj, _ := v.Object()
	e, ok := readHeader(obj)
	if !ok {
		return &flagEntry{status: statusMalformed}
	}
	if !readBody(e, obj) {
		e.status, e.code = statusBroken, ErrMalformedFlag
		return e
	}
	if code, bad := segmentProblem(e, segments); bad {
		e.status, e.code = statusBroken, code
		return e
	}
	e.status = statusReady
	return e
}

func readKeysByKind(users []string, lists jsonv.Value) (keysByKind, bool) {
	out := keysByKind{defaultKind: {}}
	for _, k := range users {
		out[defaultKind][k] = struct{}{}
	}
	if lists.IsNull() {
		return out, true
	}
	items, ok := lists.Array()
	if !ok {
		return nil, false
	}
	for _, item := range items {
		obj, ok := item.Object()
		if !ok {
			return nil, false
		}
		kind, isStr := obj["contextKind"].Str()
		if !isStr || !isKind(kind) {
			return nil, false
		}
		values, ok := stringArray(obj["values"])
		if !ok {
			return nil, false
		}
		set := out[kind]
		if set == nil {
			set = map[string]struct{}{}
			out[kind] = set
		}
		for _, val := range values {
			set[val] = struct{}{}
		}
	}
	return out, true
}

func readSegmentRule(v jsonv.Value) (segmentRule, bool) {
	obj, ok := v.Object()
	if !ok {
		return segmentRule{}, false
	}
	r := segmentRule{weight: -1}
	if w := obj["weight"]; !w.IsNull() {
		n, ok := isCount(w)
		if !ok || n > bucketCount {
			return segmentRule{}, false
		}
		r.weight = int(n)
	}
	kind, ok := optionalKind(obj["rolloutContextKind"])
	if !ok {
		return segmentRule{}, false
	}
	r.rolloutKind = kind
	items, ok := obj["clauses"].Array()
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

func compileSegment(v jsonv.Value) *segmentEntry {
	bad := &segmentEntry{}
	obj, _ := v.Object()
	key, ok := obj["key"].Str()
	if !ok || key == "" {
		return bad
	}
	salt, ok := obj["salt"].Str()
	if !ok {
		return bad
	}
	included, ok := stringArray(obj["included"])
	if !ok {
		return bad
	}
	excluded, ok := stringArray(obj["excluded"])
	if !ok {
		return bad
	}
	in, ok := readKeysByKind(included, obj["includedContexts"])
	if !ok {
		return bad
	}
	ex, ok := readKeysByKind(excluded, obj["excludedContexts"])
	if !ok {
		return bad
	}
	items, ok := obj["rules"].Array()
	if !ok {
		return bad
	}
	rules := make([]segmentRule, 0, len(items))
	for _, item := range items {
		r, ok := readSegmentRule(item)
		if !ok {
			return bad
		}
		rules = append(rules, r)
	}
	return &segmentEntry{ok: true, key: key, salt: salt, included: in, excluded: ex, rules: rules}
}

// keyOf is the string `key` of an object entry, if it has one. The first entry with a key holds it.
func keyOf(v jsonv.Value) (string, bool) {
	obj, ok := v.Object()
	if !ok {
		return "", false
	}
	return obj["key"].Str()
}

// Load validates and indexes a ruleset ({flags, segments}, as JSON hands it over) once. It never fails:
// a bad flag becomes an ERROR result when it is evaluated, and anything that is not an object with arrays
// counts as empty.
func Load(raw jsonv.Value) *Ruleset {
	rs := &Ruleset{flags: map[string]*flagEntry{}, segments: map[string]*segmentEntry{}}
	obj, ok := raw.Object()
	if !ok {
		return rs
	}
	if items, ok := obj["segments"].Array(); ok {
		for _, item := range items {
			if key, ok := keyOf(item); ok {
				if _, held := rs.segments[key]; !held {
					rs.segments[key] = compileSegment(item)
				}
			}
		}
	}
	if items, ok := obj["flags"].Array(); ok {
		for _, item := range items {
			if key, ok := keyOf(item); ok {
				if _, held := rs.flags[key]; !held {
					rs.flags[key] = compileFlag(item, rs.segments)
				}
			}
		}
	}
	return rs
}

// FlagVersion is the version of a flag that validated all the way, and false for a flag the ruleset does
// not have or one that is malformed.
func (rs *Ruleset) FlagVersion(key string) (float64, bool) {
	e, ok := rs.flags[key]
	if !ok || e.status != statusReady {
		return 0, false
	}
	return e.version, true
}
