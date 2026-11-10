package evaluator

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// A ruleset is read once into typed values. What counts as malformed is docs/evaluation.md ("Loading");
// the checks here follow it, and a flag that fails one is answered at evaluation time, never at load.

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
)

var opNames = map[string]op{
	"in": opIn, "contains": opContains, "startsWith": opStartsWith, "endsWith": opEndsWith,
	"lt": opLt, "lte": opLte, "gt": opGt, "gte": opGte,
	"semverEq": opSemverEq, "semverLt": opSemverLt, "semverLte": opSemverLte, "semverGt": opSemverGt,
	"semverGte": opSemverGte, "before": opBefore, "after": opAfter, "matches": opMatches,
}

// clauseValue is one value of an attribute clause, with whatever its op needs worked out once.
type clauseValue struct {
	v jsonv.Value // a finite number, a string or a boolean

	pattern   *pattern        // matches: nil when the pattern is not valid
	semver    semanticVersion // semver ops
	semverOK  bool
	instant   float64 // before and after
	instantOK bool
	isBool    bool
	boolValue bool
	isNumber  bool
	numValue  float64
	isString  bool
	strValue  string
}

type attributeClause struct {
	op          op
	attribute   string
	components  []string // the path; one component for a plain name
	pathOK      bool     // false when a "/" reference is not a valid reference
	values      []clauseValue
	negate      bool
	contextKind string
}

type segmentClause struct {
	values []string
	negate bool
}

type clause struct {
	attr *attributeClause
	seg  *segmentClause
}

type weightedVariation struct {
	variation int
	weight    int
}

type rollout struct {
	variations  []weightedVariation
	contextKind string
}

// serve is a variation index, or a rollout when rollout is not nil.
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

// targetHit is a target that lists a key: its place in the targets and the variation it serves.
type targetHit struct {
	order     int
	variation int
}

type entryStatus uint8

const (
	statusMalformed entryStatus = iota
	statusBroken
	statusReady
)

// flagEntry is one flag as loaded. A malformed entry has nothing but its status; a broken one has its
// header and the error code it answers with; a ready one has everything.
type flagEntry struct {
	status entryStatus
	code   ErrorCode // broken

	on           bool
	variations   []jsonv.Value
	offVariation int // -1 when absent

	key           string
	salt          string
	version       float64
	prerequisites []prerequisite
	rules         []rule
	fallthru      serve
	targets       map[string]map[string]targetHit
}

type segmentRule struct {
	clauses     []*attributeClause
	weight      int // -1 when absent
	rolloutKind string
}

type keysByKind map[string]map[string]struct{}

type segment struct {
	key, salt string
	included  keysByKind
	excluded  keysByKind
	rules     []segmentRule
}

// Ruleset is a ruleset read once, ready to evaluate against. It is immutable and safe for concurrent use.
type Ruleset struct {
	flags    map[string]*flagEntry
	segments map[string]*segment // a nil value is a malformed segment
}

// ---- reading helpers ----

func isKey(v jsonv.Value) (string, bool) {
	s, ok := v.AsString()
	return s, ok && s != ""
}

// isCount is a finite integer >= 0.
func isCount(v jsonv.Value) (float64, bool) {
	n, ok := v.AsNumber()
	if !ok || math.IsInf(n, 0) || math.IsNaN(n) || n != math.Trunc(n) || n < 0 {
		return 0, false
	}
	return n, true
}

func variationIndex(v jsonv.Value, count int) (int, bool) {
	n, ok := isCount(v)
	if !ok || n >= float64(count) {
		return 0, false
	}
	return int(n), true
}

func member(obj jsonv.Value, name string) jsonv.Value {
	v, _ := obj.Get(name)
	return v
}

// optionalKind reads a kind that may be absent (null or missing): absent is "user".
func optionalKind(obj jsonv.Value, name string) (string, bool) {
	v := member(obj, name)
	if v.IsNull() {
		return defaultKind, true
	}
	s, ok := v.AsString()
	if !ok || !isKind(s) {
		return "", false
	}
	return s, true
}

func optionalNegate(obj jsonv.Value) (bool, bool) {
	v := member(obj, "negate")
	if v.IsNull() {
		return false, true
	}
	b, ok := v.AsBool()
	return b, ok
}

func stringArray(v jsonv.Value) ([]string, bool) {
	if !v.IsArray() {
		return nil, false
	}
	items := v.Items()
	out := make([]string, len(items))
	for i, it := range items {
		s, ok := it.AsString()
		if !ok {
			return nil, false
		}
		out[i] = s
	}
	return out, true
}

func readClauseValue(o op, v jsonv.Value) (clauseValue, bool) {
	cv := clauseValue{v: v}
	switch v.Kind() {
	case jsonv.KindNumber:
		n, _ := v.AsNumber()
		if math.IsInf(n, 0) || math.IsNaN(n) {
			return cv, false
		}
		cv.isNumber, cv.numValue = true, n
	case jsonv.KindString:
		cv.isString = true
		cv.strValue, _ = v.AsString()
	case jsonv.KindBool:
		cv.isBool = true
		cv.boolValue, _ = v.AsBool()
	default:
		return cv, false
	}
	switch o {
	case opMatches:
		if cv.isString {
			if p, ok := compilePattern(cv.strValue); ok {
				cv.pattern = p
			}
		}
	case opSemverEq, opSemverLt, opSemverLte, opSemverGt, opSemverGte:
		if cv.isString {
			cv.semver, cv.semverOK = parseSemanticVersion(cv.strValue)
		}
	case opBefore, opAfter:
		cv.instant, cv.instantOK = toInstant(v)
	}
	return cv, true
}

func readAttributeName(name string) (components []string, pathOK, valid bool) {
	if name == "" {
		return nil, false, false
	}
	if name[0] != '/' {
		return []string{name}, true, true
	}
	c, ok := parseReference(name)
	if !ok {
		return nil, false, false
	}
	return c, true, true
}

func readAttributeClause(v jsonv.Value) (*attributeClause, bool) {
	if !v.IsObject() {
		return nil, false
	}
	opName, ok := member(v, "op").AsString()
	if !ok {
		return nil, false
	}
	o, known := opNames[opName]
	if !known {
		return nil, false
	}
	name, ok := member(v, "attribute").AsString()
	if !ok {
		return nil, false
	}
	components, pathOK, valid := readAttributeName(name)
	if !valid {
		return nil, false
	}
	negate, ok := optionalNegate(v)
	if !ok {
		return nil, false
	}
	kind, ok := optionalKind(v, "contextKind")
	if !ok {
		return nil, false
	}
	vals := member(v, "values")
	if !vals.IsArray() {
		return nil, false
	}
	c := &attributeClause{op: o, attribute: name, components: components, pathOK: pathOK, negate: negate, contextKind: kind}
	for _, item := range vals.Items() {
		cv, ok := readClauseValue(o, item)
		if !ok {
			return nil, false
		}
		c.values = append(c.values, cv)
	}
	return c, true
}

func readSegmentClause(v jsonv.Value) (*segmentClause, bool) {
	if !v.IsObject() {
		return nil, false
	}
	if s, ok := member(v, "op").AsString(); !ok || s != "segmentMatch" {
		return nil, false
	}
	negate, ok := optionalNegate(v)
	if !ok {
		return nil, false
	}
	values, ok := stringArray(member(v, "values"))
	if !ok {
		return nil, false
	}
	return &segmentClause{values: values, negate: negate}, true
}

func readClause(v jsonv.Value) (clause, bool) {
	if a, ok := readAttributeClause(v); ok {
		return clause{attr: a}, true
	}
	if s, ok := readSegmentClause(v); ok {
		return clause{seg: s}, true
	}
	return clause{}, false
}

func readRollout(v jsonv.Value, count int) (*rollout, bool) {
	if !v.IsObject() {
		return nil, false
	}
	kind, ok := optionalKind(v, "contextKind")
	if !ok {
		return nil, false
	}
	list := member(v, "variations")
	if !list.IsArray() || list.Len() == 0 {
		return nil, false
	}
	r := &rollout{contextKind: kind}
	total := 0
	for _, item := range list.Items() {
		if !item.IsObject() {
			return nil, false
		}
		idx, ok := variationIndex(member(item, "variation"), count)
		if !ok {
			return nil, false
		}
		w, ok := isCount(member(item, "weight"))
		if !ok || w > BucketCount {
			return nil, false
		}
		total += int(w)
		r.variations = append(r.variations, weightedVariation{variation: idx, weight: int(w)})
	}
	if total != BucketCount {
		return nil, false
	}
	return r, true
}

func readServe(v jsonv.Value, count int) (serve, bool) {
	if !v.IsObject() {
		return serve{}, false
	}
	ro := member(v, "rollout")
	va := member(v, "variation")
	if ro.IsNull() {
		idx, ok := variationIndex(va, count)
		return serve{variation: idx}, ok
	}
	if !va.IsNull() {
		return serve{}, false
	}
	r, ok := readRollout(ro, count)
	return serve{rollout: r}, ok
}

func readRule(v jsonv.Value, count int) (rule, bool) {
	if !v.IsObject() {
		return rule{}, false
	}
	list := member(v, "clauses")
	if !list.IsArray() {
		return rule{}, false
	}
	var r rule
	for _, item := range list.Items() {
		c, ok := readClause(item)
		if !ok {
			return rule{}, false
		}
		r.clauses = append(r.clauses, c)
	}
	s, ok := readServe(v, count)
	r.serve = s
	return r, ok
}

func readTarget(v jsonv.Value, count int) (target, bool) {
	if !v.IsObject() {
		return target{}, false
	}
	idx, ok := variationIndex(member(v, "variation"), count)
	if !ok {
		return target{}, false
	}
	keys, ok := stringArray(member(v, "contextKeys"))
	if !ok {
		return target{}, false
	}
	kind, ok := optionalKind(v, "contextKind")
	if !ok {
		return target{}, false
	}
	return target{variation: idx, keys: keys, kind: kind}, true
}

func readPrerequisite(v jsonv.Value) (prerequisite, bool) {
	if !v.IsObject() {
		return prerequisite{}, false
	}
	key, ok := isKey(member(v, "flagKey"))
	if !ok {
		return prerequisite{}, false
	}
	n, ok := isCount(member(v, "variation"))
	return prerequisite{flagKey: key, variation: n}, ok
}

// readHeader checks what serving the off variation needs, apart so on:false survives a bad rule.
func readHeader(v jsonv.Value, e *flagEntry) bool {
	if !v.IsObject() {
		return false
	}
	on, ok := member(v, "on").AsBool()
	if !ok {
		return false
	}
	key, ok := isKey(member(v, "key"))
	if !ok {
		return false
	}
	vars := member(v, "variations")
	if !vars.IsArray() || vars.Len() == 0 {
		return false
	}
	off := -1
	if ov := member(v, "offVariation"); !ov.IsNull() {
		idx, ok := variationIndex(ov, vars.Len())
		if !ok {
			return false
		}
		off = idx
	}
	e.on, e.key, e.variations, e.offVariation = on, key, vars.Items(), off
	return true
}

func readBody(v jsonv.Value, e *flagEntry) bool {
	count := len(e.variations)
	salt, ok := member(v, "salt").AsString()
	if !ok {
		return false
	}
	version, ok := isCount(member(v, "version"))
	if !ok {
		return false
	}
	targets := member(v, "targets")
	prereqs := member(v, "prerequisites")
	rules := member(v, "rules")
	if !targets.IsArray() || !prereqs.IsArray() || !rules.IsArray() {
		return false
	}
	var ts []target
	for _, item := range targets.Items() {
		t, ok := readTarget(item, count)
		if !ok {
			return false
		}
		ts = append(ts, t)
	}
	for _, item := range prereqs.Items() {
		p, ok := readPrerequisite(item)
		if !ok {
			return false
		}
		e.prerequisites = append(e.prerequisites, p)
	}
	for _, item := range rules.Items() {
		r, ok := readRule(item, count)
		if !ok {
			return false
		}
		e.rules = append(e.rules, r)
	}
	ft, ok := readServe(member(v, "fallthrough"), count)
	if !ok {
		return false
	}
	e.salt, e.version, e.fallthru = salt, version, ft
	e.targets = indexTargets(ts)
	return true
}

func indexTargets(ts []target) map[string]map[string]targetHit {
	index := map[string]map[string]targetHit{}
	for order, t := range ts {
		keys := index[t.kind]
		if keys == nil {
			keys = map[string]targetHit{}
			index[t.kind] = keys
		}
		for _, k := range t.keys {
			if _, taken := keys[k]; !taken {
				keys[k] = targetHit{order: order, variation: t.variation}
			}
		}
	}
	return index
}

// ---- segments ----

func readContextLists(v jsonv.Value, into keysByKind) bool {
	if v.IsNull() {
		return true
	}
	if !v.IsArray() {
		return false
	}
	for _, item := range v.Items() {
		if !item.IsObject() {
			return false
		}
		kind, ok := member(item, "contextKind").AsString()
		if !ok || !isKind(kind) {
			return false
		}
		values, ok := stringArray(member(item, "values"))
		if !ok {
			return false
		}
		addKeys(into, kind, values)
	}
	return true
}

func addKeys(into keysByKind, kind string, values []string) {
	set := into[kind]
	if set == nil {
		set = map[string]struct{}{}
		into[kind] = set
	}
	for _, v := range values {
		set[v] = struct{}{}
	}
}

func readSegmentRule(v jsonv.Value) (segmentRule, bool) {
	if !v.IsObject() {
		return segmentRule{}, false
	}
	r := segmentRule{weight: -1}
	if w := member(v, "weight"); !w.IsNull() {
		n, ok := isCount(w)
		if !ok || n > BucketCount {
			return segmentRule{}, false
		}
		r.weight = int(n)
	}
	kind, ok := optionalKind(v, "rolloutContextKind")
	if !ok {
		return segmentRule{}, false
	}
	r.rolloutKind = kind
	list := member(v, "clauses")
	if !list.IsArray() {
		return segmentRule{}, false
	}
	for _, item := range list.Items() {
		c, ok := readAttributeClause(item)
		if !ok {
			return segmentRule{}, false
		}
		r.clauses = append(r.clauses, c)
	}
	return r, true
}

func readSegment(v jsonv.Value) *segment {
	if !v.IsObject() {
		return nil
	}
	key, ok := isKey(member(v, "key"))
	if !ok {
		return nil
	}
	salt, ok := member(v, "salt").AsString()
	if !ok {
		return nil
	}
	inc, ok1 := stringArray(member(v, "included"))
	exc, ok2 := stringArray(member(v, "excluded"))
	if !ok1 || !ok2 {
		return nil
	}
	s := &segment{key: key, salt: salt, included: keysByKind{defaultKind: {}}, excluded: keysByKind{defaultKind: {}}}
	addKeys(s.included, defaultKind, inc)
	addKeys(s.excluded, defaultKind, exc)
	if !readContextLists(member(v, "includedContexts"), s.included) || !readContextLists(member(v, "excludedContexts"), s.excluded) {
		return nil
	}
	rules := member(v, "rules")
	if !rules.IsArray() {
		return nil
	}
	for _, item := range rules.Items() {
		r, ok := readSegmentRule(item)
		if !ok {
			return nil
		}
		s.rules = append(s.rules, r)
	}
	return s
}

// segmentProblem is the first reference to a segment that is missing or malformed, in rule, clause,
// value order.
func segmentProblem(e *flagEntry, segments map[string]*segment) (ErrorCode, bool) {
	for _, r := range e.rules {
		for _, c := range r.clauses {
			if c.seg == nil {
				continue
			}
			for _, key := range c.seg.values {
				s, found := segments[key]
				if !found {
					return ErrSegmentNotFound, true
				}
				if s == nil {
					return ErrMalformedSegment, true
				}
			}
		}
	}
	return "", false
}

func compileFlag(v jsonv.Value, segments map[string]*segment) *flagEntry {
	e := &flagEntry{}
	if !readHeader(v, e) {
		return &flagEntry{status: statusMalformed}
	}
	if !readBody(v, e) {
		return &flagEntry{status: statusBroken, code: ErrMalformedFlag, on: e.on, variations: e.variations, offVariation: e.offVariation}
	}
	if code, bad := segmentProblem(e, segments); bad {
		return &flagEntry{status: statusBroken, code: code, on: e.on, variations: e.variations, offVariation: e.offVariation}
	}
	e.status = statusReady
	return e
}

// indexByKey indexes the entries of an array that have a string key; the first of a key wins, even
// when it is malformed. Anything but an array is an empty list.
func indexByKey(list jsonv.Value, each func(jsonv.Value, string)) {
	seen := map[string]struct{}{}
	for _, item := range list.Items() {
		kv, ok := item.Get("key")
		if !ok {
			continue
		}
		key, ok := kv.AsString()
		if !ok {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		each(item, key)
	}
}

// NewRuleset reads a ruleset ({flags, segments}, as JSON hands it over) once, so each Evaluate skips
// that work. It never fails: a bad flag becomes an ERROR result when it is evaluated, and a value that
// is not a ruleset is an empty one.
func NewRuleset(root jsonv.Value) *Ruleset {
	r := &Ruleset{flags: map[string]*flagEntry{}, segments: map[string]*segment{}}
	indexByKey(member(root, "segments"), func(item jsonv.Value, key string) {
		r.segments[key] = readSegment(item)
	})
	indexByKey(member(root, "flags"), func(item jsonv.Value, key string) {
		r.flags[key] = compileFlag(item, r.segments)
	})
	return r
}

// FlagVersion is the version of a flag that loaded cleanly, and false for any other entry.
func (r *Ruleset) FlagVersion(key string) (float64, bool) {
	e, ok := r.flags[key]
	if !ok || e.status != statusReady {
		return 0, false
	}
	return e.version, true
}
