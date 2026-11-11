package evaluator

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

// Loading checks a ruleset's shape once and builds what evaluation reads, so an evaluation validates nothing.
// docs/evaluation.md is normative for what is malformed. Null and absent are the same for an optional field.

type flagStatus uint8

const (
	statusMalformed flagStatus = iota
	statusBroken
	statusReady
)

type rollout struct {
	kind  string
	items []weighted
}

type weighted struct {
	variation int
	weight    int
}

// serve is a variation index, or a rollout when rollout is set.
type serve struct {
	variation int
	rollout   *rollout
}

type segmentClause struct {
	segments []*segment
	negate   bool
}

type clause struct {
	attr *attributeClause
	seg  *segmentClause
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
	variation float64 // a count, which may be larger than any index
}

type flagEntry struct {
	status     flagStatus
	code       ErrorCode // broken
	key        string
	on         bool
	variations []jsonvalue.Value
	off        int // -1 for none
	salt       string
	version    float64
	targets    map[string]map[string]targetHit
	prereqs    []prerequisite
	rules      []rule
	fallthru   serve
}

type keysByKind map[string]map[string]struct{}

type segmentRule struct {
	clauses  []*attributeClause
	weight   int
	weighted bool
	kind     string
}

type segment struct {
	malformed bool
	key       string
	salt      string
	included  keysByKind
	excluded  keysByKind
	rules     []segmentRule
}

// Ruleset is a loaded set of flags and segments. It is immutable, so any number of goroutines may evaluate
// against it.
type Ruleset struct {
	flags    map[string]*flagEntry
	segments map[string]*segment
}

func isCount(v jsonvalue.Value) bool {
	if !v.IsFiniteNumber() {
		return false
	}
	n := v.NumberValue()
	return n >= 0 && n == math.Trunc(n)
}

// index is an in-range variation index; it is a count first, so it is an exact integer.
func index(v jsonvalue.Value, limit int) (int, bool) {
	if !isCount(v) || v.NumberValue() >= float64(limit) {
		return 0, false
	}
	return int(v.NumberValue()), true
}

func isKey(v jsonvalue.Value) bool { return v.IsString() && v.StringValue() != "" }

func optionalKind(v jsonvalue.Value) (string, bool) {
	if v.IsNull() {
		return defaultKind, true
	}
	if v.IsString() && isKind(v.StringValue()) {
		return v.StringValue(), true
	}
	return "", false
}

func optionalBool(v jsonvalue.Value) (bool, bool) {
	if v.IsNull() {
		return false, true
	}
	return v.BoolValue(), v.IsBool()
}

func stringArray(v jsonvalue.Value) ([]string, bool) {
	if !v.IsArray() {
		return nil, false
	}
	items := v.Items()
	out := make([]string, len(items))
	for i, item := range items {
		if !item.IsString() {
			return nil, false
		}
		out[i] = item.StringValue()
	}
	return out, true
}

func isClauseValue(v jsonvalue.Value) bool {
	if v.IsNumber() {
		return v.IsFiniteNumber()
	}
	return v.IsString() || v.IsBool()
}

func attributeName(v jsonvalue.Value) ([]string, bool) {
	if !isKey(v) {
		return nil, false
	}
	name := v.StringValue()
	if name[0] != '/' {
		return []string{name}, true
	}
	return parseReference(name)
}

func compileAttributeClause(v jsonvalue.Value) (*attributeClause, bool) {
	if !v.IsObject() {
		return nil, false
	}
	op, ok := attributeOps[field(v, "op").StringValue()]
	if !ok || !field(v, "op").IsString() {
		return nil, false
	}
	path, ok := attributeName(field(v, "attribute"))
	if !ok {
		return nil, false
	}
	negate, ok := optionalBool(field(v, "negate"))
	if !ok {
		return nil, false
	}
	kind, ok := optionalKind(field(v, "contextKind"))
	if !ok || !field(v, "values").IsArray() {
		return nil, false
	}
	items := field(v, "values").Items()
	c := &attributeClause{op: op, path: path, negate: negate, kind: kind, values: make([]clauseValue, len(items))}
	for i, item := range items {
		if !isClauseValue(item) {
			return nil, false
		}
		c.values[i] = newClauseValue(op, item)
	}
	return c, true
}

// compileRolloutOrVariation reads a serve from the object that holds "variation" and "rollout".
func compileServe(v jsonvalue.Value, variationCount int) (serve, bool) {
	if !v.IsObject() {
		return serve{}, false
	}
	variation, rolloutValue := field(v, "variation"), field(v, "rollout")
	if rolloutValue.IsNull() {
		i, ok := index(variation, variationCount)
		return serve{variation: i}, ok
	}
	if !variation.IsNull() {
		return serve{}, false
	}
	r, ok := compileRollout(rolloutValue, variationCount)
	return serve{rollout: r}, ok
}

func compileRollout(v jsonvalue.Value, variationCount int) (*rollout, bool) {
	if !v.IsObject() {
		return nil, false
	}
	kind, ok := optionalKind(field(v, "contextKind"))
	items := field(v, "variations")
	if !ok || !items.IsArray() || len(items.Items()) == 0 {
		return nil, false
	}
	r := &rollout{kind: kind}
	total := 0
	for _, item := range items.Items() {
		if !item.IsObject() {
			return nil, false
		}
		variation, ok := index(field(item, "variation"), variationCount)
		weight := field(item, "weight")
		if !ok || !isCount(weight) || weight.NumberValue() > bucketCount {
			return nil, false
		}
		r.items = append(r.items, weighted{variation: variation, weight: int(weight.NumberValue())})
		total += int(weight.NumberValue())
		if total > math.MaxInt32 {
			return nil, false
		}
	}
	return r, total == bucketCount
}

func (rs *Ruleset) compileClause(v jsonvalue.Value) (clause, bool) {
	if v.IsObject() && field(v, "op").IsString() && field(v, "op").StringValue() == "segmentMatch" {
		negate, ok := optionalBool(field(v, "negate"))
		keys, ok2 := stringArray(field(v, "values"))
		if !ok || !ok2 {
			return clause{}, false
		}
		return clause{seg: &segmentClause{negate: negate, segments: make([]*segment, len(keys))}}, true
	}
	a, ok := compileAttributeClause(v)
	return clause{attr: a}, ok
}

func (rs *Ruleset) compileRule(v jsonvalue.Value, variationCount int) (rule, bool) {
	if !v.IsObject() || !field(v, "clauses").IsArray() {
		return rule{}, false
	}
	var r rule
	for _, item := range field(v, "clauses").Items() {
		c, ok := rs.compileClause(item)
		if !ok {
			return rule{}, false
		}
		r.clauses = append(r.clauses, c)
	}
	s, ok := compileServe(v, variationCount)
	r.serve = s
	return r, ok
}

// header checks what serving the off variation needs; it is apart so "on": false survives a bad rule.
func compileHeader(v jsonvalue.Value) (*flagEntry, bool) {
	if !v.IsObject() || !field(v, "on").IsBool() || !isKey(field(v, "key")) {
		return nil, false
	}
	variations := field(v, "variations")
	if !variations.IsArray() || len(variations.Items()) == 0 {
		return nil, false
	}
	e := &flagEntry{
		key: field(v, "key").StringValue(), on: field(v, "on").BoolValue(),
		variations: variations.Items(), off: -1,
	}
	if off := field(v, "offVariation"); !off.IsNull() {
		i, ok := index(off, len(e.variations))
		if !ok {
			return nil, false
		}
		e.off = i
	}
	return e, true
}

func (e *flagEntry) compileTargets(v jsonvalue.Value) bool {
	if !v.IsArray() {
		return false
	}
	e.targets = map[string]map[string]targetHit{}
	for order, t := range v.Items() {
		if !t.IsObject() {
			return false
		}
		variation, ok := index(field(t, "variation"), len(e.variations))
		keys, ok2 := stringArray(field(t, "contextKeys"))
		kind, ok3 := optionalKind(field(t, "contextKind"))
		if !ok || !ok2 || !ok3 {
			return false
		}
		byKey := e.targets[kind]
		if byKey == nil {
			byKey = map[string]targetHit{}
			e.targets[kind] = byKey
		}
		for _, k := range keys {
			if _, taken := byKey[k]; !taken {
				byKey[k] = targetHit{order: order, variation: variation}
			}
		}
	}
	return true
}

func (e *flagEntry) compilePrerequisites(v jsonvalue.Value) bool {
	if !v.IsArray() {
		return false
	}
	for _, p := range v.Items() {
		if !p.IsObject() || !isKey(field(p, "flagKey")) || !isCount(field(p, "variation")) {
			return false
		}
		e.prereqs = append(e.prereqs, prerequisite{
			flagKey: field(p, "flagKey").StringValue(), variation: field(p, "variation").NumberValue(),
		})
	}
	return true
}

func (e *flagEntry) compileBody(rs *Ruleset, v jsonvalue.Value) bool {
	salt, version := field(v, "salt"), field(v, "version")
	if !salt.IsString() || !isCount(version) {
		return false
	}
	e.salt = salt.StringValue()
	e.version = version.NumberValue()
	if !e.compileTargets(field(v, "targets")) || !e.compilePrerequisites(field(v, "prerequisites")) {
		return false
	}
	rules := field(v, "rules")
	if !rules.IsArray() {
		return false
	}
	for _, item := range rules.Items() {
		r, ok := rs.compileRule(item, len(e.variations))
		if !ok {
			return false
		}
		e.rules = append(e.rules, r)
	}
	s, ok := compileServe(field(v, "fallthrough"), len(e.variations))
	e.fallthru = s
	return ok
}

// resolveSegments fills each segment clause in rule, clause, value order and returns the first problem.
func (e *flagEntry) resolveSegments(rs *Ruleset, v jsonvalue.Value) ErrorCode {
	rules := field(v, "rules").Items()
	for i := range e.rules {
		clauses := field(rules[i], "clauses").Items()
		for j, c := range e.rules[i].clauses {
			if c.seg == nil {
				continue
			}
			for k, key := range field(clauses[j], "values").Items() {
				s := rs.segments[key.StringValue()]
				if s == nil {
					return ErrSegmentNotFound
				}
				if s.malformed {
					return ErrMalformedSegment
				}
				c.seg.segments[k] = s
			}
		}
	}
	return ""
}

func (rs *Ruleset) compileFlag(v jsonvalue.Value) *flagEntry {
	e, ok := compileHeader(v)
	if !ok {
		return &flagEntry{status: statusMalformed}
	}
	if !e.compileBody(rs, v) {
		return &flagEntry{status: statusBroken, code: ErrMalformedFlag, key: e.key, on: e.on, variations: e.variations, off: e.off}
	}
	if code := e.resolveSegments(rs, v); code != "" {
		return &flagEntry{status: statusBroken, code: code, key: e.key, on: e.on, variations: e.variations, off: e.off}
	}
	e.status = statusReady
	return e
}

func compileKeys(userKeys []string, lists jsonvalue.Value) (keysByKind, bool) {
	out := keysByKind{defaultKind: {}}
	for _, k := range userKeys {
		out[defaultKind][k] = struct{}{}
	}
	if lists.IsNull() {
		return out, true
	}
	if !lists.IsArray() {
		return nil, false
	}
	for _, item := range lists.Items() {
		kind := field(item, "contextKind")
		values, ok := stringArray(field(item, "values"))
		if !item.IsObject() || !kind.IsString() || !isKind(kind.StringValue()) || !ok {
			return nil, false
		}
		set := out[kind.StringValue()]
		if set == nil {
			set = map[string]struct{}{}
			out[kind.StringValue()] = set
		}
		for _, k := range values {
			set[k] = struct{}{}
		}
	}
	return out, true
}

func compileSegmentRule(v jsonvalue.Value) (segmentRule, bool) {
	if !v.IsObject() || !field(v, "clauses").IsArray() {
		return segmentRule{}, false
	}
	var r segmentRule
	if w := field(v, "weight"); !w.IsNull() {
		if !isCount(w) || w.NumberValue() > bucketCount {
			return segmentRule{}, false
		}
		r.weight, r.weighted = int(w.NumberValue()), true
	}
	kind, ok := optionalKind(field(v, "rolloutContextKind"))
	if !ok {
		return segmentRule{}, false
	}
	r.kind = kind
	for _, item := range field(v, "clauses").Items() {
		c, ok := compileAttributeClause(item)
		if !ok {
			return segmentRule{}, false
		}
		r.clauses = append(r.clauses, c)
	}
	return r, true
}

func compileSegment(v jsonvalue.Value) *segment {
	bad := &segment{malformed: true}
	salt := field(v, "salt")
	included, ok1 := stringArray(field(v, "included"))
	excluded, ok2 := stringArray(field(v, "excluded"))
	if !v.IsObject() || !isKey(field(v, "key")) || !salt.IsString() || !ok1 || !ok2 || !field(v, "rules").IsArray() {
		return bad
	}
	s := &segment{key: field(v, "key").StringValue(), salt: salt.StringValue()}
	var ok bool
	if s.included, ok = compileKeys(included, field(v, "includedContexts")); !ok {
		return bad
	}
	if s.excluded, ok = compileKeys(excluded, field(v, "excludedContexts")); !ok {
		return bad
	}
	for _, item := range field(v, "rules").Items() {
		r, ok := compileSegmentRule(item)
		if !ok {
			return bad
		}
		s.rules = append(s.rules, r)
	}
	return s
}

// NewRuleset loads a ruleset, an object with "flags" and "segments" arrays, as the parser hands it over.
// It never fails: a bad flag becomes an ERROR result when it is evaluated, and anything that is not a
// ruleset is an empty one. The first entry with a key holds it.
func NewRuleset(root jsonvalue.Value) *Ruleset {
	rs := &Ruleset{flags: map[string]*flagEntry{}, segments: map[string]*segment{}}
	if !root.IsObject() {
		return rs
	}
	for _, item := range field(root, "segments").Items() {
		key := field(item, "key")
		if item.IsObject() && key.IsString() && rs.segments[key.StringValue()] == nil {
			rs.segments[key.StringValue()] = compileSegment(item)
		}
	}
	for _, item := range field(root, "flags").Items() {
		key := field(item, "key")
		if item.IsObject() && key.IsString() && rs.flags[key.StringValue()] == nil {
			rs.flags[key.StringValue()] = rs.compileFlag(item)
		}
	}
	return rs
}
