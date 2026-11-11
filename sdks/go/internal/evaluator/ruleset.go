package evaluator

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// A Ruleset is `{flags, segments}` validated and indexed once, so each Evaluate skips that work.
// docs/evaluation.md is normative for what is malformed. Null and absent are the same for an
// optional field. Load never fails: a bad flag becomes an ERROR result when it is evaluated.

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

// clause is an attribute clause, or a segmentMatch when op is opSegmentMatch.
type clause struct {
	op        op
	attribute string
	reference []string // the components of a "/" reference; nil for a literal name
	kind      string   // the context kind to read; empty is "user"
	values    []jsonv.Value
	patterns  []*pattern // for matches: one per value, nil for a value that is not a pattern
	segments  []string   // for segmentMatch: the segment keys
	negate    bool
}

type weighted struct {
	variation int
	weight    int
}

type rollout struct {
	kind       string
	variations []weighted
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

type prerequisite struct {
	flagKey   string
	variation int
}

type targetHit struct{ order, variation int }

// targetIndex is the targets by kind, then by key; the first target in array order holds a key.
type targetIndex map[string]map[string]targetHit

type flag struct {
	key          string
	salt         string
	on           bool
	variations   []jsonv.Value
	offVariation int // -1 for none
	version      float64
	targets      targetIndex
	prereqs      []prerequisite
	rules        []rule
	fallthrough_ serve
}

type entryStatus uint8

const (
	statusMalformed entryStatus = iota
	statusBroken
	statusReady
)

// flagEntry is what the ruleset holds for a flag key. A broken flag keeps the header it needs to
// serve its off variation.
type flagEntry struct {
	status entryStatus
	code   ErrorCode // for broken
	flag   *flag     // the header fields for broken, all of it for ready
}

type keySets map[string]map[string]struct{}

type segmentRule struct {
	clauses []clause
	weight  int // -1 for none
	kind    string
}

type segment struct {
	key      string
	salt     string
	included keySets
	excluded keySets
	rules    []segmentRule
}

// segmentEntry is a segment, or nil when the stored segment is malformed.
type segmentEntry struct{ segment *segment }

// Ruleset is a loaded `{flags, segments}`.
type Ruleset struct {
	flags    map[string]*flagEntry
	segments map[string]segmentEntry
}

// FlagCount is the number of flags the ruleset holds, malformed ones included.
func (r *Ruleset) FlagCount() int { return len(r.flags) }

// FlagVersion is the version of a fully valid flag, and false for a missing, malformed or broken one.
func (r *Ruleset) FlagVersion(key string) (int64, bool) {
	e, ok := r.flags[key]
	if !ok || e.status != statusReady {
		return 0, false
	}
	if e.flag.version >= 9.223372036854775807e18 {
		return math.MaxInt64, true
	}
	return int64(e.flag.version), true
}

func isRecord(v jsonv.Value) bool { return v.Kind() == jsonv.Object }

func isString(v jsonv.Value) bool { return v.Kind() == jsonv.String }

func isKey(v jsonv.Value) bool { return isString(v) && v.String() != "" }

func isCount(v jsonv.Value) bool {
	if v.Kind() != jsonv.Number {
		return false
	}
	n := v.Number()
	return !math.IsInf(n, 0) && !math.IsNaN(n) && n >= 0 && n == math.Trunc(n)
}

// toInt reads a count already checked by isCount, saturating far above any index.
func toInt(v jsonv.Value) int {
	if n := v.Number(); n < 1<<40 {
		return int(n)
	}
	return 1 << 40
}

func isOptionalKind(v jsonv.Value) bool {
	return absent(v) || (isString(v) && isKind(v.String()))
}

func optionalKind(v jsonv.Value) string {
	if absent(v) {
		return ""
	}
	return v.String()
}

func isIndex(v jsonv.Value, n int) bool { return isCount(v) && toInt(v) < n }

func isStringArray(v jsonv.Value) bool {
	if v.Kind() != jsonv.Array {
		return false
	}
	for _, item := range v.Items() {
		if !isString(item) {
			return false
		}
	}
	return true
}

func stringsOf(v jsonv.Value) []string {
	out := make([]string, 0, len(v.Items()))
	for _, item := range v.Items() {
		out = append(out, item.String())
	}
	return out
}

func isNegate(v jsonv.Value) bool { return absent(v) || v.Kind() == jsonv.Bool }

func isClauseValue(v jsonv.Value) bool {
	switch v.Kind() {
	case jsonv.Number:
		return !math.IsInf(v.Number(), 0) && !math.IsNaN(v.Number())
	case jsonv.String, jsonv.Bool:
		return true
	}
	return false
}

func isAttributeName(v jsonv.Value) bool {
	if !isKey(v) {
		return false
	}
	if v.String()[0] != '/' {
		return true
	}
	_, ok := parseReference(v.String())
	return ok
}

// readAttributeClause returns the clause, or false when v is not a valid attribute clause.
func readAttributeClause(v jsonv.Value) (clause, bool) {
	if !isRecord(v) || v.Field("op").Kind() != jsonv.String {
		return clause{}, false
	}
	o, known := attributeOps[v.Field("op").String()]
	values := v.Field("values")
	if !known || !isAttributeName(v.Field("attribute")) || !isNegate(v.Field("negate")) ||
		!isOptionalKind(v.Field("contextKind")) || values.Kind() != jsonv.Array {
		return clause{}, false
	}
	for _, item := range values.Items() {
		if !isClauseValue(item) {
			return clause{}, false
		}
	}
	c := clause{
		op:        o,
		attribute: v.Field("attribute").String(),
		kind:      optionalKind(v.Field("contextKind")),
		values:    values.Items(),
		negate:    v.Field("negate").Kind() == jsonv.Bool && v.Field("negate").Bool(),
	}
	if c.attribute[0] == '/' {
		c.reference, _ = parseReference(c.attribute)
	}
	if o == opMatches {
		c.patterns = make([]*pattern, len(c.values))
		for i, item := range c.values {
			if item.Kind() == jsonv.String {
				c.patterns[i] = compilePattern(item.String())
			}
		}
	}
	return c, true
}

func readSegmentClause(v jsonv.Value) (clause, bool) {
	if !isRecord(v) || v.Field("op").Kind() != jsonv.String || v.Field("op").String() != "segmentMatch" {
		return clause{}, false
	}
	if !isNegate(v.Field("negate")) || !isStringArray(v.Field("values")) {
		return clause{}, false
	}
	return clause{
		op:       opSegmentMatch,
		segments: stringsOf(v.Field("values")),
		negate:   v.Field("negate").Kind() == jsonv.Bool && v.Field("negate").Bool(),
	}, true
}

func readServe(v jsonv.Value, n int) (serve, bool) {
	if !isRecord(v) {
		return serve{}, false
	}
	variation, ro := v.Field("variation"), v.Field("rollout")
	if absent(ro) {
		if !isIndex(variation, n) {
			return serve{}, false
		}
		return serve{variation: toInt(variation)}, true
	}
	if !absent(variation) {
		return serve{}, false
	}
	r, ok := readRollout(ro, n)
	if !ok {
		return serve{}, false
	}
	return serve{rollout: r}, true
}

func readRollout(v jsonv.Value, n int) (*rollout, bool) {
	if !isRecord(v) || !isOptionalKind(v.Field("contextKind")) {
		return nil, false
	}
	vars := v.Field("variations")
	if vars.Kind() != jsonv.Array || len(vars.Items()) == 0 {
		return nil, false
	}
	r := &rollout{kind: optionalKind(v.Field("contextKind"))}
	total := 0
	for _, item := range vars.Items() {
		if !isRecord(item) || !isIndex(item.Field("variation"), n) {
			return nil, false
		}
		w := item.Field("weight")
		if !isCount(w) || w.Number() > BucketCount {
			return nil, false
		}
		r.variations = append(r.variations, weighted{variation: toInt(item.Field("variation")), weight: toInt(w)})
		total += toInt(w)
	}
	return r, total == BucketCount
}

func readRule(v jsonv.Value, n int) (rule, bool) {
	if !isRecord(v) {
		return rule{}, false
	}
	clauses := v.Field("clauses")
	if clauses.Kind() != jsonv.Array {
		return rule{}, false
	}
	var r rule
	for _, item := range clauses.Items() {
		c, ok := readAttributeClause(item)
		if !ok {
			c, ok = readSegmentClause(item)
		}
		if !ok {
			return rule{}, false
		}
		r.clauses = append(r.clauses, c)
	}
	s, ok := readServe(v, n)
	r.serve = s
	return r, ok
}

func readTarget(v jsonv.Value, n int) (kind string, keys []string, variation int, ok bool) {
	if !isRecord(v) || !isIndex(v.Field("variation"), n) || !isStringArray(v.Field("contextKeys")) ||
		!isOptionalKind(v.Field("contextKind")) {
		return "", nil, 0, false
	}
	kind = optionalKind(v.Field("contextKind"))
	if kind == "" {
		kind = defaultKind
	}
	return kind, stringsOf(v.Field("contextKeys")), toInt(v.Field("variation")), true
}

// readHeader reads what serving the off variation needs, so `on: false` survives a bad rule.
func readHeader(v jsonv.Value) (*flag, bool) {
	if !isRecord(v) || v.Field("on").Kind() != jsonv.Bool || !isKey(v.Field("key")) {
		return nil, false
	}
	variations := v.Field("variations")
	if variations.Kind() != jsonv.Array || len(variations.Items()) == 0 {
		return nil, false
	}
	off := v.Field("offVariation")
	if !absent(off) && !isIndex(off, len(variations.Items())) {
		return nil, false
	}
	f := &flag{
		key:          v.Field("key").String(),
		on:           v.Field("on").Bool(),
		variations:   variations.Items(),
		offVariation: -1,
	}
	if !absent(off) {
		f.offVariation = toInt(off)
	}
	return f, true
}

// readBody fills in the rest of f, or reports that the flag's body is malformed.
func readBody(f *flag, v jsonv.Value) bool {
	n := len(f.variations)
	salt, version := v.Field("salt"), v.Field("version")
	targets, prereqs, rules := v.Field("targets"), v.Field("prerequisites"), v.Field("rules")
	if !isString(salt) || !isCount(version) || targets.Kind() != jsonv.Array ||
		prereqs.Kind() != jsonv.Array || rules.Kind() != jsonv.Array {
		return false
	}
	f.salt, f.version = salt.String(), version.Number()
	f.targets = targetIndex{}
	for order, item := range targets.Items() {
		kind, keys, variation, ok := readTarget(item, n)
		if !ok {
			return false
		}
		byKey := f.targets[kind]
		if byKey == nil {
			byKey = map[string]targetHit{}
			f.targets[kind] = byKey
		}
		for _, k := range keys {
			if _, held := byKey[k]; !held {
				byKey[k] = targetHit{order: order, variation: variation}
			}
		}
	}
	for _, item := range prereqs.Items() {
		if !isRecord(item) || !isKey(item.Field("flagKey")) || !isCount(item.Field("variation")) {
			return false
		}
		f.prereqs = append(f.prereqs, prerequisite{item.Field("flagKey").String(), toInt(item.Field("variation"))})
	}
	for _, item := range rules.Items() {
		r, ok := readRule(item, n)
		if !ok {
			return false
		}
		f.rules = append(f.rules, r)
	}
	s, ok := readServe(v.Field("fallthrough"), n)
	f.fallthrough_ = s
	return ok
}

func readKeySets(userKeys []string, lists jsonv.Value) keySets {
	sets := keySets{defaultKind: {}}
	for _, k := range userKeys {
		sets[defaultKind][k] = struct{}{}
	}
	for _, list := range lists.Items() {
		kind := list.Field("contextKind").String()
		if sets[kind] == nil {
			sets[kind] = map[string]struct{}{}
		}
		for _, k := range stringsOf(list.Field("values")) {
			sets[kind][k] = struct{}{}
		}
	}
	return sets
}

func isContextLists(v jsonv.Value) bool {
	if absent(v) {
		return true
	}
	if v.Kind() != jsonv.Array {
		return false
	}
	for _, item := range v.Items() {
		kind := item.Field("contextKind")
		if !isRecord(item) || !isString(kind) || !isKind(kind.String()) || !isStringArray(item.Field("values")) {
			return false
		}
	}
	return true
}

func readSegment(v jsonv.Value) segmentEntry {
	if !isRecord(v) || !isKey(v.Field("key")) || !isString(v.Field("salt")) ||
		!isStringArray(v.Field("included")) || !isStringArray(v.Field("excluded")) ||
		!isContextLists(v.Field("includedContexts")) || !isContextLists(v.Field("excludedContexts")) ||
		v.Field("rules").Kind() != jsonv.Array {
		return segmentEntry{}
	}
	s := &segment{
		key:      v.Field("key").String(),
		salt:     v.Field("salt").String(),
		included: readKeySets(stringsOf(v.Field("included")), v.Field("includedContexts")),
		excluded: readKeySets(stringsOf(v.Field("excluded")), v.Field("excludedContexts")),
	}
	for _, item := range v.Field("rules").Items() {
		r, ok := readSegmentRule(item)
		if !ok {
			return segmentEntry{}
		}
		s.rules = append(s.rules, r)
	}
	return segmentEntry{segment: s}
}

func readSegmentRule(v jsonv.Value) (segmentRule, bool) {
	if !isRecord(v) {
		return segmentRule{}, false
	}
	w := v.Field("weight")
	if !absent(w) && (!isCount(w) || w.Number() > BucketCount) {
		return segmentRule{}, false
	}
	clauses := v.Field("clauses")
	if !isOptionalKind(v.Field("rolloutContextKind")) || clauses.Kind() != jsonv.Array {
		return segmentRule{}, false
	}
	r := segmentRule{weight: -1, kind: optionalKind(v.Field("rolloutContextKind"))}
	if !absent(w) {
		r.weight = toInt(w)
	}
	for _, item := range clauses.Items() {
		c, ok := readAttributeClause(item)
		if !ok {
			return segmentRule{}, false
		}
		r.clauses = append(r.clauses, c)
	}
	return r, true
}

// segmentProblem is the first reference to a segment that is missing or malformed, in rule,
// clause, value order.
func segmentProblem(f *flag, segments map[string]segmentEntry) (ErrorCode, bool) {
	for _, r := range f.rules {
		for _, c := range r.clauses {
			for _, key := range c.segments {
				s, ok := segments[key]
				if !ok {
					return SegmentNotFound, true
				}
				if s.segment == nil {
					return MalformedSegment, true
				}
			}
		}
	}
	return "", false
}

func compileFlag(v jsonv.Value, segments map[string]segmentEntry) *flagEntry {
	f, ok := readHeader(v)
	if !ok {
		return &flagEntry{status: statusMalformed}
	}
	if !readBody(f, v) {
		return &flagEntry{status: statusBroken, code: MalformedFlag, flag: f}
	}
	if code, bad := segmentProblem(f, segments); bad {
		return &flagEntry{status: statusBroken, code: code, flag: f}
	}
	return &flagEntry{status: statusReady, flag: f}
}

// Load validates and indexes a ruleset (`{flags, segments}`). A flags or segments value that is
// not an array counts as empty, an entry with no string key is dropped, and the first entry with a
// key holds it.
func Load(ruleset jsonv.Value) *Ruleset {
	rs := &Ruleset{flags: map[string]*flagEntry{}, segments: map[string]segmentEntry{}}
	if !isRecord(ruleset) {
		return rs
	}
	for _, item := range ruleset.Field("segments").Items() {
		key := item.Field("key")
		if !isRecord(item) || !isString(key) {
			continue
		}
		if _, held := rs.segments[key.String()]; !held {
			rs.segments[key.String()] = readSegment(item)
		}
	}
	for _, item := range ruleset.Field("flags").Items() {
		key := item.Field("key")
		if !isRecord(item) || !isString(key) {
			continue
		}
		if _, held := rs.flags[key.String()]; !held {
			rs.flags[key.String()] = compileFlag(item, rs.segments)
		}
	}
	return rs
}
