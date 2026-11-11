package evaluator

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// This file validates and compiles a ruleset, to docs/evaluation.md: what counts as malformed is decided
// here, once, so an evaluation skips that work. Null and absent are the same for an optional field.

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
	"semverEq": opSemverEq, "semverLt": opSemverLt, "semverLte": opSemverLte, "semverGt": opSemverGt,
	"semverGte": opSemverGte, "before": opBefore, "after": opAfter, "matches": opMatches,
}

type clause struct {
	op     op
	negate bool
	// Attribute clauses.
	kind       string // "" is user
	components []string
	values     []jsonv.Value
	patterns   []*program // for matches, one per value; nil for an invalid pattern
	// Segment clauses.
	segments []string
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

type prerequisite struct {
	flagKey   string
	variation float64
}

type entryStatus int

const (
	statusMalformed entryStatus = iota
	statusBroken
	statusReady
)

type flagEntry struct {
	status entryStatus
	// Header: set when the status is not malformed.
	on           bool
	variations   []jsonv.Value
	offVariation int // -1 for none
	code         ErrorCode
	// Body: set when the status is ready, or broken by a segment problem.
	hasBody       bool
	version       float64
	salt          string
	key           string
	targets       map[string]map[string]targetHit
	prerequisites []prerequisite
	rules         []rule
	fallback      serve
}

type keysByKind map[string]map[string]struct{}

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

// Ruleset is a validated, indexed ruleset.
type Ruleset struct {
	flags    map[string]*flagEntry
	segments map[string]*segment // a nil segment is a malformed one
}

func isCount(v jsonv.Value) (int, bool) {
	n, ok := v.AsNumber()
	if !ok || !jsonv.IsFinite(n) || n != math.Trunc(n) || n < 0 {
		return 0, false
	}
	if n > math.MaxInt32 {
		return math.MaxInt32, true
	}
	return int(n), true
}

func isVariationIndex(v jsonv.Value, count int) (int, bool) {
	n, ok := isCount(v)
	if !ok || n >= count {
		return 0, false
	}
	return n, true
}

func optionalKind(v jsonv.Value) (string, bool) {
	if v.IsNull() {
		return "", true
	}
	s, ok := v.AsString()
	if !ok || !isKind(s) {
		return "", false
	}
	return s, true
}

func isNegate(v jsonv.Value) (bool, bool) {
	if v.IsNull() {
		return false, true
	}
	return v.AsBool()
}

func stringArray(v jsonv.Value) ([]string, bool) {
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

func isObject(v jsonv.Value) bool {
	_, ok := v.AsObject()
	return ok
}

func attributeClause(v jsonv.Value) (clause, bool) {
	if !isObject(v) {
		return clause{}, false
	}
	opName, ok := v.Get("op").AsString()
	if !ok {
		return clause{}, false
	}
	o, known := opNames[opName]
	if !known {
		return clause{}, false
	}
	name, ok := v.Get("attribute").AsString()
	if !ok || name == "" {
		return clause{}, false
	}
	components := []string{name}
	if name[0] == '/' {
		if components, ok = parseReference(name); !ok {
			return clause{}, false
		}
	}
	negate, ok := isNegate(v.Get("negate"))
	if !ok {
		return clause{}, false
	}
	kind, ok := optionalKind(v.Get("contextKind"))
	if !ok {
		return clause{}, false
	}
	values, ok := v.Get("values").AsArray()
	if !ok {
		return clause{}, false
	}
	c := clause{op: o, negate: negate, kind: kind, components: components, values: values}
	for _, value := range values {
		if n, isNumber := value.AsNumber(); isNumber {
			if !jsonv.IsFinite(n) {
				return clause{}, false
			}
		} else if _, isString := value.AsString(); !isString {
			if _, isBool := value.AsBool(); !isBool {
				return clause{}, false
			}
		}
	}
	if o == opMatches {
		c.patterns = make([]*program, len(values))
		for i, value := range values {
			if s, isString := value.AsString(); isString {
				c.patterns[i] = compilePattern(s)
			}
		}
	}
	return c, true
}

func segmentClause(v jsonv.Value) (clause, bool) {
	if !isObject(v) {
		return clause{}, false
	}
	if name, ok := v.Get("op").AsString(); !ok || name != "segmentMatch" {
		return clause{}, false
	}
	negate, ok := isNegate(v.Get("negate"))
	if !ok {
		return clause{}, false
	}
	keys, ok := stringArray(v.Get("values"))
	if !ok {
		return clause{}, false
	}
	return clause{op: opSegmentMatch, negate: negate, segments: keys}, true
}

func rolloutOf(v jsonv.Value, variationCount int) (*rollout, bool) {
	if !isObject(v) {
		return nil, false
	}
	kind, ok := optionalKind(v.Get("contextKind"))
	if !ok {
		return nil, false
	}
	items, ok := v.Get("variations").AsArray()
	if !ok || len(items) == 0 {
		return nil, false
	}
	r := &rollout{kind: kind}
	total := 0
	for _, item := range items {
		if !isObject(item) {
			return nil, false
		}
		index, ok := isVariationIndex(item.Get("variation"), variationCount)
		if !ok {
			return nil, false
		}
		weight, ok := isCount(item.Get("weight"))
		if !ok || weight > bucketCount {
			return nil, false
		}
		total += weight
		r.variations = append(r.variations, weighted{variation: index, weight: weight})
	}
	if total != bucketCount {
		return nil, false
	}
	return r, true
}

// serveOf reads the serve in v: a rule object or the fallthrough.
func serveOf(v jsonv.Value, variationCount int) (serve, bool) {
	if !isObject(v) {
		return serve{}, false
	}
	rolloutValue := v.Get("rollout")
	if rolloutValue.IsNull() {
		index, ok := isVariationIndex(v.Get("variation"), variationCount)
		return serve{variation: index}, ok
	}
	if !v.Get("variation").IsNull() {
		return serve{}, false
	}
	r, ok := rolloutOf(rolloutValue, variationCount)
	return serve{rollout: r}, ok
}

func ruleOf(v jsonv.Value, variationCount int) (rule, bool) {
	if !isObject(v) {
		return rule{}, false
	}
	items, ok := v.Get("clauses").AsArray()
	if !ok {
		return rule{}, false
	}
	r := rule{}
	for _, item := range items {
		c, ok := attributeClause(item)
		if !ok {
			if c, ok = segmentClause(item); !ok {
				return rule{}, false
			}
		}
		r.clauses = append(r.clauses, c)
	}
	s, ok := serveOf(v, variationCount)
	r.serve = s
	return r, ok
}

func targetsOf(v jsonv.Value, variationCount int) (map[string]map[string]targetHit, bool) {
	items, ok := v.AsArray()
	if !ok {
		return nil, false
	}
	index := map[string]map[string]targetHit{}
	for order, item := range items {
		if !isObject(item) {
			return nil, false
		}
		variation, ok := isVariationIndex(item.Get("variation"), variationCount)
		if !ok {
			return nil, false
		}
		keys, ok := stringArray(item.Get("contextKeys"))
		if !ok {
			return nil, false
		}
		kind, ok := optionalKind(item.Get("contextKind"))
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
		for _, key := range keys {
			if _, held := byKey[key]; !held {
				byKey[key] = targetHit{order: order, variation: variation}
			}
		}
	}
	return index, true
}

func prerequisitesOf(v jsonv.Value) ([]prerequisite, bool) {
	items, ok := v.AsArray()
	if !ok {
		return nil, false
	}
	out := make([]prerequisite, 0, len(items))
	for _, item := range items {
		if !isObject(item) {
			return nil, false
		}
		key, ok := item.Get("flagKey").AsString()
		if !ok || key == "" {
			return nil, false
		}
		if _, ok := isCount(item.Get("variation")); !ok {
			return nil, false
		}
		n, _ := item.Get("variation").AsNumber()
		out = append(out, prerequisite{flagKey: key, variation: n})
	}
	return out, true
}

// fillHeader reads what serving the off variation needs. It is checked apart so on: false survives a bad rule.
func fillHeader(v jsonv.Value, e *flagEntry) bool {
	if !isObject(v) {
		return false
	}
	on, ok := v.Get("on").AsBool()
	if !ok {
		return false
	}
	key, ok := v.Get("key").AsString()
	if !ok || key == "" {
		return false
	}
	variations, ok := v.Get("variations").AsArray()
	if !ok || len(variations) == 0 {
		return false
	}
	off := -1
	if o := v.Get("offVariation"); !o.IsNull() {
		if off, ok = isVariationIndex(o, len(variations)); !ok {
			return false
		}
	}
	e.on, e.key, e.variations, e.offVariation = on, key, variations, off
	return true
}

func fillBody(v jsonv.Value, e *flagEntry) bool {
	count := len(e.variations)
	salt, ok := v.Get("salt").AsString()
	if !ok {
		return false
	}
	version, ok := v.Get("version").AsNumber()
	if _, isCountValue := isCount(v.Get("version")); !ok || !isCountValue {
		return false
	}
	targets, ok := targetsOf(v.Get("targets"), count)
	if !ok {
		return false
	}
	prerequisites, ok := prerequisitesOf(v.Get("prerequisites"))
	if !ok {
		return false
	}
	ruleItems, ok := v.Get("rules").AsArray()
	if !ok {
		return false
	}
	rules := make([]rule, 0, len(ruleItems))
	for _, item := range ruleItems {
		r, ok := ruleOf(item, count)
		if !ok {
			return false
		}
		rules = append(rules, r)
	}
	fallbackServe, ok := serveOf(v.Get("fallthrough"), count)
	if !ok {
		return false
	}
	e.hasBody, e.salt, e.version, e.targets = true, salt, version, targets
	e.prerequisites, e.rules, e.fallback = prerequisites, rules, fallbackServe
	return true
}

type contextList struct {
	kind   string
	values []string
}

func contextLists(v jsonv.Value) ([]contextList, bool) {
	if v.IsNull() {
		return nil, true
	}
	items, ok := v.AsArray()
	if !ok {
		return nil, false
	}
	var out []contextList
	for _, item := range items {
		if !isObject(item) {
			return nil, false
		}
		kind, ok := item.Get("contextKind").AsString()
		if !ok || !isKind(kind) {
			return nil, false
		}
		values, ok := stringArray(item.Get("values"))
		if !ok {
			return nil, false
		}
		out = append(out, contextList{kind, values})
	}
	return out, true
}

func keysOf(userKeys []string, listsValue jsonv.Value) (keysByKind, bool) {
	lists, ok := contextLists(listsValue)
	if !ok {
		return nil, false
	}
	index := keysByKind{defaultKind: {}}
	for _, k := range userKeys {
		index[defaultKind][k] = struct{}{}
	}
	for _, l := range lists {
		set := index[l.kind]
		if set == nil {
			set = map[string]struct{}{}
			index[l.kind] = set
		}
		for _, k := range l.values {
			set[k] = struct{}{}
		}
	}
	return index, true
}

func segmentRuleOf(v jsonv.Value) (segmentRule, bool) {
	if !isObject(v) {
		return segmentRule{}, false
	}
	r := segmentRule{}
	if w := v.Get("weight"); !w.IsNull() {
		weight, ok := isCount(w)
		if !ok || weight > bucketCount {
			return segmentRule{}, false
		}
		r.hasWeight, r.weight = true, weight
	}
	kind, ok := optionalKind(v.Get("rolloutContextKind"))
	if !ok {
		return segmentRule{}, false
	}
	r.rolloutKind = kind
	items, ok := v.Get("clauses").AsArray()
	if !ok {
		return segmentRule{}, false
	}
	for _, item := range items {
		c, ok := attributeClause(item)
		if !ok {
			return segmentRule{}, false
		}
		r.clauses = append(r.clauses, c)
	}
	return r, true
}

// compileSegment is nil for a malformed segment.
func compileSegment(v jsonv.Value) *segment {
	if !isObject(v) {
		return nil
	}
	key, ok := v.Get("key").AsString()
	if !ok || key == "" {
		return nil
	}
	salt, ok := v.Get("salt").AsString()
	if !ok {
		return nil
	}
	included, ok := stringArray(v.Get("included"))
	if !ok {
		return nil
	}
	excluded, ok := stringArray(v.Get("excluded"))
	if !ok {
		return nil
	}
	s := &segment{key: key, salt: salt}
	if s.included, ok = keysOf(included, v.Get("includedContexts")); !ok {
		return nil
	}
	if s.excluded, ok = keysOf(excluded, v.Get("excludedContexts")); !ok {
		return nil
	}
	items, ok := v.Get("rules").AsArray()
	if !ok {
		return nil
	}
	for _, item := range items {
		r, ok := segmentRuleOf(item)
		if !ok {
			return nil
		}
		s.rules = append(s.rules, r)
	}
	return s
}

// segmentProblem is the first reference to a segment that is missing or malformed, in rule, clause, value
// order.
func segmentProblem(e *flagEntry, segments map[string]*segment) ErrorCode {
	for _, r := range e.rules {
		for _, c := range r.clauses {
			for _, key := range c.segments {
				s, found := segments[key]
				if !found {
					return ErrSegmentNotFound
				}
				if s == nil {
					return ErrMalformedSegment
				}
			}
		}
	}
	return ""
}

func compileFlag(v jsonv.Value, segments map[string]*segment) *flagEntry {
	e := &flagEntry{}
	if !fillHeader(v, e) {
		return &flagEntry{status: statusMalformed}
	}
	if !fillBody(v, e) {
		e.status, e.code = statusBroken, ErrMalformedFlag
		return e
	}
	if code := segmentProblem(e, segments); code != "" {
		e.status, e.code = statusBroken, code
		return e
	}
	e.status = statusReady
	return e
}

// indexByKey keeps the first item that is an object with a string key, whatever it holds.
func indexByKey[T any](items jsonv.Value, compile func(jsonv.Value) T) map[string]T {
	index := map[string]T{}
	list, _ := items.AsArray()
	for _, item := range list {
		if !isObject(item) {
			continue
		}
		key, ok := item.Get("key").AsString()
		if !ok {
			continue
		}
		if _, held := index[key]; !held {
			index[key] = compile(item)
		}
	}
	return index
}

// Load validates and indexes a ruleset ({flags, segments}, as JSON hands it over) once, so each Evaluate
// skips that work. It never fails: a bad flag becomes an ERROR result when it is evaluated, and a value that
// is not an object, or flags or segments that are not arrays, load as empty.
func Load(v jsonv.Value) *Ruleset {
	segments := indexByKey(v.Get("segments"), compileSegment)
	flags := indexByKey(v.Get("flags"), func(item jsonv.Value) *flagEntry { return compileFlag(item, segments) })
	return &Ruleset{flags: flags, segments: segments}
}

// FlagVersion is the flag's version in this ruleset. ok is false when the ruleset has no such flag, when the
// flag's header or body is malformed, and when the version does not fit a signed 64-bit integer (the contract
// lets an SDK report that as none).
func (r *Ruleset) FlagVersion(key string) (int64, bool) {
	e, found := r.flags[key]
	if !found || !e.hasBody || e.version >= 1<<63 {
		return 0, false
	}
	return int64(e.version), true
}
