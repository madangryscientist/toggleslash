package evaluator

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

// Error codes an evaluation can end in.
const (
	CodeInvalidContext   = "INVALID_CONTEXT"
	CodeFlagNotFound     = "FLAG_NOT_FOUND"
	CodeSegmentNotFound  = "SEGMENT_NOT_FOUND"
	CodeMalformedFlag    = "MALFORMED_FLAG"
	CodeMalformedSegment = "MALFORMED_SEGMENT"
	CodePrereqCycle      = "PREREQUISITE_CYCLE"
	CodePrereqDepth      = "PREREQUISITE_DEPTH_EXCEEDED"
)

const maxWeight = 100000

// ---- compiled shapes ----

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

var opNames = map[string]opKind{
	"in": opIn, "contains": opContains, "startsWith": opStartsWith, "endsWith": opEndsWith,
	"lt": opLt, "lte": opLte, "gt": opGt, "gte": opGte,
	"semverEq": opSemverEq, "semverLt": opSemverLt, "semverLte": opSemverLte, "semverGt": opSemverGt,
	"semverGte": opSemverGte, "before": opBefore, "after": opAfter, "matches": opMatches,
}

// operand is one clause value with whatever its op needs worked out once, at load.
type operand struct {
	value      jsonvalue.Value
	version    semver
	hasVersion bool
	instant    float64
	hasInstant bool
	pattern    *Pattern
}

type clause struct {
	op       opKind
	negate   bool
	kind     string
	ref      attributeRef
	operands []operand
	segments []string // segmentMatch
}

type weighted struct {
	variation int
	weight    float64
}

type serve struct {
	variation int
	rollout   []weighted // nil when the serve is one variation
	hasRoll   bool
	kind      string
}

type rule struct {
	clauses []*clause
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

type flagStatus uint8

const (
	statusMalformed flagStatus = iota
	statusBroken
	statusReady
)

type flag struct {
	status       flagStatus
	brokenCode   string
	key          string
	on           bool
	variations   []jsonvalue.Value
	offVariation int // -1 when there is none
	salt         string
	version      jsonvalue.Value
	prereqs      []prerequisite
	rules        []rule
	fallthrough_ serve
	targets      map[string]map[string]targetHit // kind, key to the first target listing it
}

type segmentRule struct {
	clauses   []*clause
	weight    float64
	hasWeight bool
	kind      string
}

type keySets map[string]map[string]struct{}

func (k keySets) add(kind, key string) {
	m := k[kind]
	if m == nil {
		m = map[string]struct{}{}
		k[kind] = m
	}
	m[key] = struct{}{}
}

type segment struct {
	key, salt string
	included  keySets
	excluded  keySets
	rules     []segmentRule
}

// Ruleset is one environment's flags and segments, compiled once at load.
type Ruleset struct {
	flags    map[string]*flag
	segments map[string]*segment // a malformed segment is nil
}

// ---- reading helpers ----

func asCount(v jsonvalue.Value) (float64, bool) {
	n, ok := v.AsNumber()
	if !ok || math.IsInf(n, 0) || math.IsNaN(n) || n < 0 || n != math.Trunc(n) {
		return 0, false
	}
	return n, true
}

func asIndex(v jsonvalue.Value, count int) (int, bool) {
	n, ok := asCount(v)
	if !ok || n >= float64(count) {
		return 0, false
	}
	return int(n), true
}

func stringList(v jsonvalue.Value) ([]string, bool) {
	if v.Kind() != jsonvalue.KindArray {
		return nil, false
	}
	out := make([]string, 0, len(v.Elements()))
	for _, e := range v.Elements() {
		s, ok := e.AsString()
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func optionalBool(o jsonvalue.Value, name string) (value, ok bool) {
	v, present := optional(o, name)
	if !present {
		return false, true
	}
	b, isBool := v.AsBool()
	return b, isBool
}

// ---- clauses ----

func compileOperand(op opKind, v jsonvalue.Value) operand {
	o := operand{value: v}
	s, isString := v.AsString()
	switch op {
	case opSemverEq, opSemverLt, opSemverLte, opSemverGt, opSemverGte:
		if isString {
			o.version, o.hasVersion = parseSemver(s)
		}
	case opBefore, opAfter:
		o.instant, o.hasInstant = instantOf(v)
	case opMatches:
		if isString {
			o.pattern = CompilePattern(s)
		}
	}
	return o
}

func instantOf(v jsonvalue.Value) (float64, bool) {
	if v.Kind() == jsonvalue.KindNumber {
		n, _ := v.AsNumber()
		return n, v.IsFinite()
	}
	if s, ok := v.AsString(); ok {
		return parseDate(s)
	}
	return 0, false
}

func compileClause(v jsonvalue.Value, inSegment bool) (*clause, bool) {
	if v.Kind() != jsonvalue.KindObject {
		return nil, false
	}
	opv, ok := v.Get("op")
	opName, isString := opv.AsString()
	if !ok || !isString {
		return nil, false
	}
	negate, ok := optionalBool(v, "negate")
	if !ok {
		return nil, false
	}
	values, ok := v.Get("values")
	if !ok || values.Kind() != jsonvalue.KindArray {
		return nil, false
	}
	if opName == "segmentMatch" {
		if inSegment {
			return nil, false
		}
		keys, ok := stringList(values)
		if !ok {
			return nil, false
		}
		return &clause{op: opSegmentMatch, negate: negate, segments: keys}, true
	}
	op, known := opNames[opName]
	if !known {
		return nil, false
	}
	attr, ok := v.Get("attribute")
	name, isString := attr.AsString()
	if !ok || !isString {
		return nil, false
	}
	ref, ok := parseAttribute(name)
	if !ok {
		return nil, false
	}
	kind, ok := optionalKind(v, "contextKind")
	if !ok {
		return nil, false
	}
	c := &clause{op: op, negate: negate, kind: kind, ref: ref}
	for _, e := range values.Elements() {
		switch e.Kind() {
		case jsonvalue.KindString, jsonvalue.KindBool:
		case jsonvalue.KindNumber:
			if !e.IsFinite() {
				return nil, false
			}
		default:
			return nil, false
		}
		c.operands = append(c.operands, compileOperand(op, e))
	}
	return c, true
}

func compileClauses(v jsonvalue.Value, inSegment bool) ([]*clause, bool) {
	if v.Kind() != jsonvalue.KindArray {
		return nil, false
	}
	out := make([]*clause, 0, len(v.Elements()))
	for _, e := range v.Elements() {
		c, ok := compileClause(e, inSegment)
		if !ok {
			return nil, false
		}
		out = append(out, c)
	}
	return out, true
}

// ---- segments ----

func contextLists(o jsonvalue.Value, name string, into keySets) bool {
	v, present := optional(o, name)
	if !present {
		return true
	}
	if v.Kind() != jsonvalue.KindArray {
		return false
	}
	for _, e := range v.Elements() {
		if e.Kind() != jsonvalue.KindObject {
			return false
		}
		kv, ok := e.Get("contextKind")
		kind, isString := kv.AsString()
		if !ok || !isString || !validKind(kind) {
			return false
		}
		vals, ok := e.Get("values")
		if !ok {
			return false
		}
		keys, ok := stringList(vals)
		if !ok {
			return false
		}
		for _, k := range keys {
			into.add(kind, k)
		}
	}
	return true
}

func compileSegment(v jsonvalue.Value) *segment {
	keyv, _ := v.Get("key")
	key, _ := keyv.AsString()
	if !isKey(keyv) {
		return nil
	}
	saltv, ok := v.Get("salt")
	salt, isString := saltv.AsString()
	if !ok || !isString {
		return nil
	}
	s := &segment{key: key, salt: salt, included: keySets{}, excluded: keySets{}}
	for _, side := range []struct {
		name string
		into keySets
	}{{"included", s.included}, {"excluded", s.excluded}} {
		lv, ok := v.Get(side.name)
		if !ok {
			return nil
		}
		keys, ok := stringList(lv)
		if !ok {
			return nil
		}
		for _, k := range keys {
			side.into.add(defaultKind, k)
		}
	}
	if !contextLists(v, "includedContexts", s.included) || !contextLists(v, "excludedContexts", s.excluded) {
		return nil
	}
	rulesv, ok := v.Get("rules")
	if !ok || rulesv.Kind() != jsonvalue.KindArray {
		return nil
	}
	for _, rv := range rulesv.Elements() {
		if rv.Kind() != jsonvalue.KindObject {
			return nil
		}
		r := segmentRule{}
		if wv, present := optional(rv, "weight"); present {
			w, ok := asCount(wv)
			if !ok || w > maxWeight {
				return nil
			}
			r.weight, r.hasWeight = w, true
		}
		kind, ok := optionalKind(rv, "rolloutContextKind")
		if !ok {
			return nil
		}
		r.kind = kind
		cv, ok := rv.Get("clauses")
		if !ok {
			return nil
		}
		r.clauses, ok = compileClauses(cv, true)
		if !ok {
			return nil
		}
		s.rules = append(s.rules, r)
	}
	return s
}

// ---- flags ----

func compileServe(v jsonvalue.Value, variationCount int) (serve, bool) {
	if v.Kind() != jsonvalue.KindObject {
		return serve{}, false
	}
	if rv, present := optional(v, "rollout"); present {
		if _, hasVariation := optional(v, "variation"); hasVariation {
			return serve{}, false
		}
		if rv.Kind() != jsonvalue.KindObject {
			return serve{}, false
		}
		kind, ok := optionalKind(rv, "contextKind")
		if !ok {
			return serve{}, false
		}
		vs, ok := rv.Get("variations")
		if !ok || vs.Kind() != jsonvalue.KindArray || len(vs.Elements()) == 0 {
			return serve{}, false
		}
		s := serve{hasRoll: true, kind: kind}
		sum := 0.0
		for _, e := range vs.Elements() {
			if e.Kind() != jsonvalue.KindObject {
				return serve{}, false
			}
			iv, _ := e.Get("variation")
			idx, ok := asIndex(iv, variationCount)
			if !ok {
				return serve{}, false
			}
			wv, _ := e.Get("weight")
			w, ok := asCount(wv)
			if !ok || w > maxWeight {
				return serve{}, false
			}
			sum += w
			s.rollout = append(s.rollout, weighted{idx, w})
		}
		if sum != maxWeight {
			return serve{}, false
		}
		return s, true
	}
	iv, _ := v.Get("variation")
	idx, ok := asIndex(iv, variationCount)
	if !ok {
		return serve{}, false
	}
	return serve{variation: idx}, true
}

// compileHeader reads the fields serving the off variation needs.
func compileHeader(v jsonvalue.Value, f *flag) bool {
	keyv, _ := v.Get("key")
	if !isKey(keyv) {
		return false
	}
	f.key, _ = keyv.AsString()
	onv, _ := v.Get("on")
	on, ok := onv.AsBool()
	if !ok {
		return false
	}
	f.on = on
	vars, _ := v.Get("variations")
	if vars.Kind() != jsonvalue.KindArray || len(vars.Elements()) == 0 {
		return false
	}
	f.variations = vars.Elements()
	f.offVariation = -1
	if ov, present := optional(v, "offVariation"); present {
		idx, ok := asIndex(ov, len(f.variations))
		if !ok {
			return false
		}
		f.offVariation = idx
	}
	return true
}

func compileBody(v jsonvalue.Value, f *flag) bool {
	saltv, _ := v.Get("salt")
	salt, ok := saltv.AsString()
	if !ok {
		return false
	}
	f.salt = salt
	ver, _ := v.Get("version")
	if _, ok := asCount(ver); !ok {
		return false
	}
	f.version = ver
	n := len(f.variations)

	tv, _ := v.Get("targets")
	if tv.Kind() != jsonvalue.KindArray {
		return false
	}
	f.targets = map[string]map[string]targetHit{}
	for order, e := range tv.Elements() {
		if e.Kind() != jsonvalue.KindObject {
			return false
		}
		iv, _ := e.Get("variation")
		idx, ok := asIndex(iv, n)
		if !ok {
			return false
		}
		kv, _ := e.Get("contextKeys")
		keys, ok := stringList(kv)
		if !ok {
			return false
		}
		kind, ok := optionalKind(e, "contextKind")
		if !ok {
			return false
		}
		m := f.targets[kind]
		if m == nil {
			m = map[string]targetHit{}
			f.targets[kind] = m
		}
		for _, k := range keys {
			if _, seen := m[k]; !seen {
				m[k] = targetHit{order, idx}
			}
		}
	}

	pv, _ := v.Get("prerequisites")
	if pv.Kind() != jsonvalue.KindArray {
		return false
	}
	for _, e := range pv.Elements() {
		if e.Kind() != jsonvalue.KindObject {
			return false
		}
		kv, _ := e.Get("flagKey")
		if !isKey(kv) {
			return false
		}
		key, _ := kv.AsString()
		vv, _ := e.Get("variation")
		want, ok := asCount(vv)
		if !ok {
			return false
		}
		f.prereqs = append(f.prereqs, prerequisite{key, want})
	}

	rv, _ := v.Get("rules")
	if rv.Kind() != jsonvalue.KindArray {
		return false
	}
	for _, e := range rv.Elements() {
		if e.Kind() != jsonvalue.KindObject {
			return false
		}
		cv, _ := e.Get("clauses")
		clauses, ok := compileClauses(cv, false)
		if !ok {
			return false
		}
		s, ok := compileServe(e, n)
		if !ok {
			return false
		}
		f.rules = append(f.rules, rule{clauses, s})
	}

	fv, _ := v.Get("fallthrough")
	s, ok := compileServe(fv, n)
	if !ok {
		return false
	}
	f.fallthrough_ = s
	return true
}

// segmentProblem finds the first segmentMatch value that names a missing or malformed segment, in rule,
// clause and value order.
func (rs *Ruleset) segmentProblem(f *flag) string {
	for _, r := range f.rules {
		for _, c := range r.clauses {
			if c.op != opSegmentMatch {
				continue
			}
			for _, key := range c.segments {
				seg, exists := rs.segments[key]
				if !exists {
					return CodeSegmentNotFound
				}
				if seg == nil {
					return CodeMalformedSegment
				}
			}
		}
	}
	return ""
}

func (rs *Ruleset) compileFlag(v jsonvalue.Value) *flag {
	f := &flag{}
	if !compileHeader(v, f) {
		f.status = statusMalformed
		return f
	}
	if !compileBody(v, f) {
		f.status = statusBroken
		f.brokenCode = CodeMalformedFlag
		return f
	}
	if code := rs.segmentProblem(f); code != "" {
		f.status = statusBroken
		f.brokenCode = code
		return f
	}
	f.status = statusReady
	return f
}

// Load compiles a ruleset. Loading never fails: a bad flag or segment becomes an error when it is
// evaluated, a flags or segments that is not an array counts as empty, and the first entry with a key wins.
func Load(v jsonvalue.Value) *Ruleset {
	rs := &Ruleset{flags: map[string]*flag{}, segments: map[string]*segment{}}
	entries := func(name string) []jsonvalue.Value {
		if v.Kind() != jsonvalue.KindObject {
			return nil
		}
		a, _ := v.Get(name)
		return a.Elements()
	}
	for _, e := range entries("segments") {
		kv, ok := e.Get("key")
		if e.Kind() != jsonvalue.KindObject || !ok {
			continue
		}
		key, isString := kv.AsString()
		if !isString {
			continue
		}
		if _, seen := rs.segments[key]; !seen {
			rs.segments[key] = compileSegment(e)
		}
	}
	for _, e := range entries("flags") {
		kv, ok := e.Get("key")
		if e.Kind() != jsonvalue.KindObject || !ok {
			continue
		}
		key, isString := kv.AsString()
		if !isString {
			continue
		}
		if _, seen := rs.flags[key]; !seen {
			rs.flags[key] = rs.compileFlag(e)
			if rs.flags[key].key == "" {
				rs.flags[key].key = key
			}
		}
	}
	return rs
}

// FlagCount is the number of flags the ruleset holds, malformed ones included.
func (rs *Ruleset) FlagCount() int { return len(rs.flags) }

// FlagVersion returns the `version` of a flag when the flag is not malformed and has a valid one.
func (rs *Ruleset) FlagVersion(key string) (float64, bool) {
	f, ok := rs.flags[key]
	if !ok || f.status == statusMalformed || f.status == statusBroken && f.version.IsNull() {
		return 0, false
	}
	return asCount(f.version)
}
