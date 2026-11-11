package evaluator

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

// Validation and indexing of a ruleset, checked at runtime because rulesets arrive as JSON.
// docs/evaluation.md is normative for what counts as malformed. Null and absent are the same for an
// optional field.

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

var opNames = map[string]op{
	"in": opIn, "contains": opContains, "startsWith": opStartsWith, "endsWith": opEndsWith,
	"lt": opLt, "lte": opLte, "gt": opGt, "gte": opGte,
	"semverEq": opSemverEq, "semverLt": opSemverLt, "semverLte": opSemverLte,
	"semverGt": opSemverGt, "semverGte": opSemverGte,
	"before": opBefore, "after": opAfter, "matches": opMatches,
}

// clause is an attribute clause or a segmentMatch clause.
type clause struct {
	op        op
	attribute string   // the literal name, or the first component of a reference
	reference []string // the components of a `/` reference; nil for a literal name
	values    []jsonvalue.Value
	patterns  []*Pattern // for matches: one per value, nil for a value that is not a valid pattern
	segments  []string   // for segmentMatch
	negate    bool
	kind      string
}

type weighted struct {
	variation int
	weight    int
}

type rollout struct {
	variations []weighted
	kind       string
}

// serve is a variation index, or a rollout when roll is not nil.
type serve struct {
	variation int
	roll      *rollout
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
	variation float64 // not range-checked: it indexes the prerequisite's variations
}

type flag struct {
	key              string
	on               bool
	variations       []jsonvalue.Value
	offVariation     int // -1 when absent
	salt             string
	version          float64
	targets          []target
	prerequisites    []prerequisite
	rules            []rule
	fallthroughServe serve
	targetIndex      map[string]map[string]targetHit
}

type targetHit struct {
	order     int
	variation int
}

type segmentRule struct {
	clauses []clause
	weight  int // -1 when absent
	kind    string
}

type segment struct {
	key      string
	salt     string
	included map[string]map[string]struct{}
	excluded map[string]map[string]struct{}
	rules    []segmentRule
}

type flagStatus uint8

const (
	flagReady flagStatus = iota
	flagBroken
	flagMalformed
)

// header is what serving the off variation needs.
type header struct {
	key          string
	on           bool
	variations   []jsonvalue.Value
	offVariation int
}

type flagEntry struct {
	status flagStatus
	header header
	flag   *flag
	code   string // flagBroken
}

// Ruleset is one environment's flags and segments, validated and indexed once.
type Ruleset struct {
	flags    map[string]*flagEntry
	segments map[string]*segment // nil value: malformed
}

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

// isVariationIndex: a count below variationCount.
func isVariationIndex(v jsonvalue.Value, variationCount int) (int, bool) {
	n, ok := isCount(v)
	if !ok || n >= float64(variationCount) {
		return 0, false
	}
	return int(n), true
}

func isOptionalKind(v jsonvalue.Value) (string, bool) {
	if isAbsent(v) {
		return "", true
	}
	s, ok := v.AsString()
	if !ok || !isKind(s) {
		return "", false
	}
	return s, true
}

func stringArray(v jsonvalue.Value) ([]string, bool) {
	items, ok := v.AsArray()
	if !ok {
		return nil, false
	}
	out := make([]string, len(items))
	for i, item := range items {
		s, isString := item.AsString()
		if !isString {
			return nil, false
		}
		out[i] = s
	}
	return out, true
}

func isNegate(v jsonvalue.Value) (bool, bool) {
	if isAbsent(v) {
		return false, true
	}
	return v.AsBool()
}

func field(v jsonvalue.Value, name string) jsonvalue.Value {
	f, _ := v.Field(name)
	return f
}

func isClauseValue(v jsonvalue.Value) bool {
	switch v.Kind() {
	case jsonvalue.KindNumber:
		return v.IsFiniteNumber()
	case jsonvalue.KindString, jsonvalue.KindBool:
		return true
	}
	return false
}

func parseAttributeClause(v jsonvalue.Value) (clause, bool) {
	if v.Kind() != jsonvalue.KindObject {
		return clause{}, false
	}
	opName, _ := field(v, "op").AsString()
	o, known := opNames[opName]
	if !known {
		return clause{}, false
	}
	name, ok := isKey(field(v, "attribute"))
	if !ok {
		return clause{}, false
	}
	c := clause{op: o, attribute: name, kind: defaultKind}
	if name[0] == '/' {
		ref, valid := parseReference(name)
		if !valid {
			return clause{}, false
		}
		c.reference = ref
	}
	if c.negate, ok = isNegate(field(v, "negate")); !ok {
		return clause{}, false
	}
	kind, ok := isOptionalKind(field(v, "contextKind"))
	if !ok {
		return clause{}, false
	}
	if kind != "" {
		c.kind = kind
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
	c.values = values
	if o == opMatches {
		c.patterns = make([]*Pattern, len(values))
		for i, value := range values {
			if s, isString := value.AsString(); isString {
				c.patterns[i] = CompilePattern(s)
			}
		}
	}
	return c, true
}

func parseSegmentClause(v jsonvalue.Value) (clause, bool) {
	if v.Kind() != jsonvalue.KindObject {
		return clause{}, false
	}
	if name, _ := field(v, "op").AsString(); name != "segmentMatch" {
		return clause{}, false
	}
	negate, ok := isNegate(field(v, "negate"))
	if !ok {
		return clause{}, false
	}
	keys, ok := stringArray(field(v, "values"))
	if !ok {
		return clause{}, false
	}
	return clause{op: opSegmentMatch, segments: keys, negate: negate, kind: defaultKind}, true
}

func parseWeighted(v jsonvalue.Value, variationCount int) (weighted, bool) {
	if v.Kind() != jsonvalue.KindObject {
		return weighted{}, false
	}
	index, ok := isVariationIndex(field(v, "variation"), variationCount)
	if !ok {
		return weighted{}, false
	}
	w, ok := isCount(field(v, "weight"))
	if !ok || w > bucketCount {
		return weighted{}, false
	}
	return weighted{variation: index, weight: int(w)}, true
}

func parseRollout(v jsonvalue.Value, variationCount int) (*rollout, bool) {
	if v.Kind() != jsonvalue.KindObject {
		return nil, false
	}
	kind, ok := isOptionalKind(field(v, "contextKind"))
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
		w, valid := parseWeighted(item, variationCount)
		if !valid {
			return nil, false
		}
		total += w.weight
		r.variations = append(r.variations, w)
	}
	if total != bucketCount {
		return nil, false
	}
	return r, true
}

// parseServe reads exactly one of `variation` or `rollout` from v.
func parseServe(v jsonvalue.Value, variationCount int) (serve, bool) {
	if v.Kind() != jsonvalue.KindObject {
		return serve{}, false
	}
	variation, rolloutValue := field(v, "variation"), field(v, "rollout")
	if isAbsent(rolloutValue) {
		index, ok := isVariationIndex(variation, variationCount)
		return serve{variation: index}, ok
	}
	if !isAbsent(variation) {
		return serve{}, false
	}
	r, ok := parseRollout(rolloutValue, variationCount)
	return serve{roll: r}, ok
}

func parseRule(v jsonvalue.Value, variationCount int) (rule, bool) {
	if v.Kind() != jsonvalue.KindObject {
		return rule{}, false
	}
	items, ok := field(v, "clauses").AsArray()
	if !ok {
		return rule{}, false
	}
	r := rule{}
	for _, item := range items {
		c, valid := parseAttributeClause(item)
		if !valid {
			if c, valid = parseSegmentClause(item); !valid {
				return rule{}, false
			}
		}
		r.clauses = append(r.clauses, c)
	}
	if r.serve, ok = parseServe(v, variationCount); !ok {
		return rule{}, false
	}
	return r, true
}

func parseTarget(v jsonvalue.Value, variationCount int) (target, bool) {
	if v.Kind() != jsonvalue.KindObject {
		return target{}, false
	}
	index, ok := isVariationIndex(field(v, "variation"), variationCount)
	if !ok {
		return target{}, false
	}
	keys, ok := stringArray(field(v, "contextKeys"))
	if !ok {
		return target{}, false
	}
	kind, ok := isOptionalKind(field(v, "contextKind"))
	if !ok {
		return target{}, false
	}
	if kind == "" {
		kind = defaultKind
	}
	return target{variation: index, keys: keys, kind: kind}, true
}

func parsePrerequisite(v jsonvalue.Value) (prerequisite, bool) {
	if v.Kind() != jsonvalue.KindObject {
		return prerequisite{}, false
	}
	key, ok := isKey(field(v, "flagKey"))
	if !ok {
		return prerequisite{}, false
	}
	n, ok := isCount(field(v, "variation"))
	return prerequisite{flagKey: key, variation: n}, ok
}

// parseHeader checks what serving the off variation needs. It is apart so `on: false` survives a bad rule.
func parseHeader(v jsonvalue.Value) (header, bool) {
	if v.Kind() != jsonvalue.KindObject {
		return header{}, false
	}
	on, ok := field(v, "on").AsBool()
	if !ok {
		return header{}, false
	}
	key, ok := isKey(field(v, "key"))
	variations, isArray := field(v, "variations").AsArray()
	if !ok || !isArray || len(variations) == 0 {
		return header{}, false
	}
	h := header{key: key, on: on, variations: variations, offVariation: -1}
	if off := field(v, "offVariation"); !isAbsent(off) {
		if h.offVariation, ok = isVariationIndex(off, len(variations)); !ok {
			return header{}, false
		}
	}
	return h, true
}

func parseBody(v jsonvalue.Value, h header) (*flag, bool) {
	count := len(h.variations)
	f := &flag{key: h.key, on: h.on, variations: h.variations, offVariation: h.offVariation}
	var ok bool
	if f.salt, ok = field(v, "salt").AsString(); !ok {
		return nil, false
	}
	if f.version, ok = isCount(field(v, "version")); !ok {
		return nil, false
	}
	targets, ok := field(v, "targets").AsArray()
	if !ok {
		return nil, false
	}
	for _, item := range targets {
		t, valid := parseTarget(item, count)
		if !valid {
			return nil, false
		}
		f.targets = append(f.targets, t)
	}
	prerequisites, ok := field(v, "prerequisites").AsArray()
	if !ok {
		return nil, false
	}
	for _, item := range prerequisites {
		p, valid := parsePrerequisite(item)
		if !valid {
			return nil, false
		}
		f.prerequisites = append(f.prerequisites, p)
	}
	rules, ok := field(v, "rules").AsArray()
	if !ok {
		return nil, false
	}
	for _, item := range rules {
		r, valid := parseRule(item, count)
		if !valid {
			return nil, false
		}
		f.rules = append(f.rules, r)
	}
	if f.fallthroughServe, ok = parseServe(field(v, "fallthrough"), count); !ok {
		return nil, false
	}
	return f, true
}

func indexTargets(f *flag) map[string]map[string]targetHit {
	index := map[string]map[string]targetHit{}
	for order, t := range f.targets {
		keys := index[t.kind]
		if keys == nil {
			keys = map[string]targetHit{}
			index[t.kind] = keys
		}
		for _, key := range t.keys {
			if _, taken := keys[key]; !taken {
				keys[key] = targetHit{order: order, variation: t.variation}
			}
		}
	}
	return index
}

// segmentProblem is the first reference to a segment that is missing or malformed, in rule, clause,
// value order, as an error code; empty when there is none.
func segmentProblem(f *flag, segments map[string]*segment) string {
	for _, r := range f.rules {
		for _, c := range r.clauses {
			if c.op != opSegmentMatch {
				continue
			}
			for _, key := range c.segments {
				s, found := segments[key]
				if !found {
					return CodeSegmentNotFound
				}
				if s == nil {
					return CodeMalformedSegment
				}
			}
		}
	}
	return ""
}

func compileFlag(v jsonvalue.Value, segments map[string]*segment) *flagEntry {
	h, ok := parseHeader(v)
	if !ok {
		return &flagEntry{status: flagMalformed}
	}
	f, ok := parseBody(v, h)
	if !ok {
		return &flagEntry{status: flagBroken, header: h, code: CodeMalformedFlag}
	}
	if code := segmentProblem(f, segments); code != "" {
		return &flagEntry{status: flagBroken, header: h, code: code}
	}
	f.targetIndex = indexTargets(f)
	return &flagEntry{status: flagReady, header: h, flag: f}
}

func parseSegmentRule(v jsonvalue.Value) (segmentRule, bool) {
	if v.Kind() != jsonvalue.KindObject {
		return segmentRule{}, false
	}
	r := segmentRule{weight: -1}
	if weight := field(v, "weight"); !isAbsent(weight) {
		w, ok := isCount(weight)
		if !ok || w > bucketCount {
			return segmentRule{}, false
		}
		r.weight = int(w)
	}
	kind, ok := isOptionalKind(field(v, "rolloutContextKind"))
	if !ok {
		return segmentRule{}, false
	}
	r.kind = kind
	items, ok := field(v, "clauses").AsArray()
	if !ok {
		return segmentRule{}, false
	}
	for _, item := range items {
		c, valid := parseAttributeClause(item)
		if !valid {
			return segmentRule{}, false
		}
		r.clauses = append(r.clauses, c)
	}
	return r, true
}

// keysByKind reads the user keys and the optional context lists into kind, then keys.
func keysByKind(userKeys []string, lists jsonvalue.Value) (map[string]map[string]struct{}, bool) {
	index := map[string]map[string]struct{}{defaultKind: {}}
	for _, key := range userKeys {
		index[defaultKind][key] = struct{}{}
	}
	if isAbsent(lists) {
		return index, true
	}
	items, ok := lists.AsArray()
	if !ok {
		return nil, false
	}
	for _, item := range items {
		if item.Kind() != jsonvalue.KindObject {
			return nil, false
		}
		kindString, isString := field(item, "contextKind").AsString()
		if !isString || !isKind(kindString) {
			return nil, false
		}
		values, valid := stringArray(field(item, "values"))
		if !valid {
			return nil, false
		}
		keys := index[kindString]
		if keys == nil {
			keys = map[string]struct{}{}
			index[kindString] = keys
		}
		for _, value := range values {
			keys[value] = struct{}{}
		}
	}
	return index, true
}

// compileSegment returns nil for a malformed segment.
func compileSegment(v jsonvalue.Value) *segment {
	if v.Kind() != jsonvalue.KindObject {
		return nil
	}
	key, ok := isKey(field(v, "key"))
	if !ok {
		return nil
	}
	salt, ok := field(v, "salt").AsString()
	if !ok {
		return nil
	}
	included, ok := stringArray(field(v, "included"))
	if !ok {
		return nil
	}
	excluded, ok := stringArray(field(v, "excluded"))
	if !ok {
		return nil
	}
	s := &segment{key: key, salt: salt}
	if s.included, ok = keysByKind(included, field(v, "includedContexts")); !ok {
		return nil
	}
	if s.excluded, ok = keysByKind(excluded, field(v, "excludedContexts")); !ok {
		return nil
	}
	rules, ok := field(v, "rules").AsArray()
	if !ok {
		return nil
	}
	for _, item := range rules {
		r, valid := parseSegmentRule(item)
		if !valid {
			return nil
		}
		s.rules = append(s.rules, r)
	}
	return s
}

// New validates and indexes a ruleset ({flags, segments}, as JSON hands it over) once, so each Evaluate
// skips that work. It never fails: a bad flag becomes an ERROR result when it is evaluated, and a `flags`
// or `segments` that is not an array counts as empty, as does a ruleset that is not an object. When two
// share a key the first wins, even a malformed one; an entry with no string key is dropped.
func New(input jsonvalue.Value) *Ruleset {
	rs := &Ruleset{flags: map[string]*flagEntry{}, segments: map[string]*segment{}}
	if input.Kind() != jsonvalue.KindObject {
		return rs
	}
	if items, ok := field(input, "segments").AsArray(); ok {
		for _, item := range items {
			key, isString := field(item, "key").AsString()
			if _, taken := rs.segments[key]; isString && !taken {
				rs.segments[key] = compileSegment(item)
			}
		}
	}
	if items, ok := field(input, "flags").AsArray(); ok {
		for _, item := range items {
			key, isString := field(item, "key").AsString()
			if _, taken := rs.flags[key]; isString && !taken {
				rs.flags[key] = compileFlag(item, rs.segments)
			}
		}
	}
	return rs
}

// FlagCount is the number of flags the ruleset holds, malformed ones included.
func (rs *Ruleset) FlagCount() int { return len(rs.flags) }

// FlagVersion is the `version` of a flag that is ready to evaluate; ok is false for a flag the
// ruleset does not hold and for one that is malformed or broken.
func (rs *Ruleset) FlagVersion(key string) (float64, bool) {
	entry, found := rs.flags[key]
	if !found || entry.status != flagReady {
		return 0, false
	}
	return entry.flag.version, true
}
