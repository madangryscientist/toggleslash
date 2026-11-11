package eval

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

// The shape of a ruleset, checked at runtime because rulesets arrive as JSON. docs/evaluation.md is
// normative for what counts as malformed. Null and absent are the same for an optional field.

type attributeClause struct {
	op        string
	attribute string
	reference []string // the components of a "/" reference; nil for a literal name
	values    []jsonvalue.Value
	patterns  []pattern // per value, for the matches op; a nil entry is an invalid pattern
	negate    bool
	kind      string // "" is user
}

type clause struct {
	segment     bool
	attr        *attributeClause
	segmentKeys []string
	negate      bool
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
	serve
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
	key          string
	on           bool
	variations   []jsonvalue.Value
	offVariation int // -1 when absent
}

type flag struct {
	flagHeader
	salt             string
	version          float64
	prerequisites    []prerequisite
	rules            []rule
	fallthroughServe serve
	targets          targetIndex
}

type segmentRule struct {
	clauses     []*attributeClause
	weight      int // -1 when absent
	rolloutKind string
}

type keysByKind map[string]map[string]struct{}

type segment struct {
	key      string
	salt     string
	included keysByKind
	excluded keysByKind
	rules    []segmentRule
}

type targetHit struct {
	order     int
	variation int
}

// targetIndex is the targets by kind, then by key; the first target in array order holds each key.
type targetIndex map[string]map[string]targetHit

type entryStatus int

const (
	entryMalformed entryStatus = iota
	entryBroken
	entryReady
)

type flagEntry struct {
	status entryStatus
	header flagHeader
	code   string // for a broken entry
	flag   *flag
}

// segmentEntry is a compiled segment, or malformed when seg is nil.
type segmentEntry struct {
	seg *segment
}

// Ruleset is a validated and indexed ruleset. It is immutable, so any number of goroutines may evaluate on it.
type Ruleset struct {
	flags    map[string]*flagEntry
	segments map[string]segmentEntry
}

func isCount(v jsonvalue.Value) (float64, bool) {
	f, ok := v.AsNumber()
	if !ok || math.IsInf(f, 0) || math.IsNaN(f) || f < 0 || f != math.Trunc(f) {
		return 0, false
	}
	return f, true
}

func isAbsent(v jsonvalue.Value) bool { return v.IsNull() }

func isObject(v jsonvalue.Value) bool { return v.Kind() == jsonvalue.Object }

// optionalKind reads a kind that may be absent: "" for absent, ok false for an invalid kind.
func optionalKind(v jsonvalue.Value) (string, bool) {
	if isAbsent(v) {
		return "", true
	}
	s, ok := v.AsString()
	if !ok || !isKind(s) {
		return "", false
	}
	return s, true
}

func variationIndex(v jsonvalue.Value, count int) (int, bool) {
	f, ok := isCount(v)
	if !ok || f >= float64(count) {
		return 0, false
	}
	return int(f), true
}

func stringArray(v jsonvalue.Value) ([]string, bool) {
	arr, ok := v.AsArray()
	if !ok {
		return nil, false
	}
	out := make([]string, len(arr))
	for i, e := range arr {
		s, ok := e.AsString()
		if !ok {
			return nil, false
		}
		out[i] = s
	}
	return out, true
}

func negateOf(v jsonvalue.Value) (bool, bool) {
	if isAbsent(v) {
		return false, true
	}
	return v.AsBool()
}

func isClauseValue(v jsonvalue.Value) bool {
	switch v.Kind() {
	case jsonvalue.Number:
		f, _ := v.AsNumber()
		return !math.IsInf(f, 0) && !math.IsNaN(f)
	case jsonvalue.String, jsonvalue.Bool:
		return true
	}
	return false
}

var attributeOps = map[string]bool{
	"in": true, "contains": true, "startsWith": true, "endsWith": true,
	"lt": true, "lte": true, "gt": true, "gte": true,
	"semverEq": true, "semverLt": true, "semverLte": true, "semverGt": true, "semverGte": true,
	"before": true, "after": true, "matches": true,
}

func readAttributeClause(v jsonvalue.Value) (*attributeClause, bool) {
	if !isObject(v) {
		return nil, false
	}
	op, ok := field(v, "op").AsString()
	if !ok || !attributeOps[op] {
		return nil, false
	}
	name, ok := field(v, "attribute").AsString()
	if !ok || name == "" {
		return nil, false
	}
	c := &attributeClause{op: op, attribute: name}
	if name[0] == '/' {
		ref, ok := parseReference(name)
		if !ok {
			return nil, false
		}
		c.reference = ref
	}
	if c.negate, ok = negateOf(field(v, "negate")); !ok {
		return nil, false
	}
	if c.kind, ok = optionalKind(field(v, "contextKind")); !ok {
		return nil, false
	}
	values, ok := field(v, "values").AsArray()
	if !ok {
		return nil, false
	}
	for _, value := range values {
		if !isClauseValue(value) {
			return nil, false
		}
	}
	c.values = values
	if op == "matches" {
		c.patterns = make([]pattern, len(values))
		for i, value := range values {
			if s, ok := value.AsString(); ok {
				c.patterns[i] = compilePattern(s)
			}
		}
	}
	return c, true
}

func readSegmentClause(v jsonvalue.Value) (clause, bool) {
	if !isObject(v) {
		return clause{}, false
	}
	if op, ok := field(v, "op").AsString(); !ok || op != "segmentMatch" {
		return clause{}, false
	}
	negate, ok := negateOf(field(v, "negate"))
	if !ok {
		return clause{}, false
	}
	keys, ok := stringArray(field(v, "values"))
	if !ok {
		return clause{}, false
	}
	return clause{segment: true, segmentKeys: keys, negate: negate}, true
}

func readWeighted(v jsonvalue.Value, count int) (weighted, bool) {
	if !isObject(v) {
		return weighted{}, false
	}
	idx, ok := variationIndex(field(v, "variation"), count)
	w, ok2 := isCount(field(v, "weight"))
	if !ok || !ok2 || w > BucketCount {
		return weighted{}, false
	}
	return weighted{variation: idx, weight: int(w)}, true
}

func readRollout(v jsonvalue.Value, count int) (*rollout, bool) {
	if !isObject(v) {
		return nil, false
	}
	kind, ok := optionalKind(field(v, "contextKind"))
	if !ok {
		return nil, false
	}
	arr, ok := field(v, "variations").AsArray()
	if !ok || len(arr) == 0 {
		return nil, false
	}
	r := &rollout{kind: kind, variations: make([]weighted, len(arr))}
	total := 0
	for i, e := range arr {
		w, ok := readWeighted(e, count)
		if !ok {
			return nil, false
		}
		r.variations[i] = w
		total += w.weight
	}
	return r, total == BucketCount
}

func readServe(v jsonvalue.Value, count int) (serve, bool) {
	if !isObject(v) {
		return serve{}, false
	}
	variation, rolloutValue := field(v, "variation"), field(v, "rollout")
	if isAbsent(rolloutValue) {
		idx, ok := variationIndex(variation, count)
		return serve{variation: idx}, ok
	}
	if !isAbsent(variation) {
		return serve{}, false
	}
	r, ok := readRollout(rolloutValue, count)
	return serve{rollout: r}, ok
}

func readRule(v jsonvalue.Value, count int) (rule, bool) {
	if !isObject(v) {
		return rule{}, false
	}
	list, ok := field(v, "clauses").AsArray()
	if !ok {
		return rule{}, false
	}
	r := rule{clauses: make([]clause, len(list))}
	for i, e := range list {
		if attr, ok := readAttributeClause(e); ok {
			r.clauses[i] = clause{attr: attr}
		} else if seg, ok := readSegmentClause(e); ok {
			r.clauses[i] = seg
		} else {
			return rule{}, false
		}
	}
	r.serve, ok = readServe(v, count)
	return r, ok
}

func readTarget(v jsonvalue.Value, count int) (target, bool) {
	if !isObject(v) {
		return target{}, false
	}
	idx, ok := variationIndex(field(v, "variation"), count)
	keys, ok2 := stringArray(field(v, "contextKeys"))
	kind, ok3 := optionalKind(field(v, "contextKind"))
	return target{variation: idx, keys: keys, kind: kind}, ok && ok2 && ok3
}

func readPrerequisite(v jsonvalue.Value) (prerequisite, bool) {
	if !isObject(v) {
		return prerequisite{}, false
	}
	key, ok := field(v, "flagKey").AsString()
	variation, ok2 := isCount(field(v, "variation"))
	return prerequisite{flagKey: key, variation: variation}, ok && key != "" && ok2
}

// readHeader is what serving the off variation needs. It is checked apart so `on: false` survives a bad rule.
func readHeader(v jsonvalue.Value) (flagHeader, bool) {
	if !isObject(v) {
		return flagHeader{}, false
	}
	on, ok := field(v, "on").AsBool()
	key, ok2 := field(v, "key").AsString()
	variations, ok3 := field(v, "variations").AsArray()
	if !ok || !ok2 || key == "" || !ok3 || len(variations) == 0 {
		return flagHeader{}, false
	}
	h := flagHeader{key: key, on: on, variations: variations, offVariation: -1}
	if off := field(v, "offVariation"); !isAbsent(off) {
		idx, ok := variationIndex(off, len(variations))
		if !ok {
			return flagHeader{}, false
		}
		h.offVariation = idx
	}
	return h, true
}

func readList[T any](v jsonvalue.Value, read func(jsonvalue.Value) (T, bool)) ([]T, bool) {
	arr, ok := v.AsArray()
	if !ok {
		return nil, false
	}
	out := make([]T, len(arr))
	for i, e := range arr {
		item, ok := read(e)
		if !ok {
			return nil, false
		}
		out[i] = item
	}
	return out, true
}

// readBody reads everything past the header. It reports false for a flag that is malformed in its body.
func readBody(v jsonvalue.Value, h flagHeader) (*flag, bool) {
	count := len(h.variations)
	salt, ok := field(v, "salt").AsString()
	if !ok {
		return nil, false
	}
	version, ok := isCount(field(v, "version"))
	if !ok {
		return nil, false
	}
	targets, ok := readList(field(v, "targets"), func(e jsonvalue.Value) (target, bool) { return readTarget(e, count) })
	if !ok {
		return nil, false
	}
	prerequisites, ok := readList(field(v, "prerequisites"), readPrerequisite)
	if !ok {
		return nil, false
	}
	rules, ok := readList(field(v, "rules"), func(e jsonvalue.Value) (rule, bool) { return readRule(e, count) })
	if !ok {
		return nil, false
	}
	fall, ok := readServe(field(v, "fallthrough"), count)
	if !ok {
		return nil, false
	}
	return &flag{
		flagHeader: h, salt: salt, version: version, prerequisites: prerequisites, rules: rules,
		fallthroughServe: fall, targets: indexTargets(targets),
	}, true
}

func indexTargets(targets []target) targetIndex {
	index := targetIndex{}
	for order, t := range targets {
		kind := t.kind
		if kind == "" {
			kind = DefaultKind
		}
		keys := index[kind]
		if keys == nil {
			keys = map[string]targetHit{}
			index[kind] = keys
		}
		for _, k := range t.keys {
			if _, taken := keys[k]; !taken {
				keys[k] = targetHit{order: order, variation: t.variation}
			}
		}
	}
	return index
}

func readSegmentRule(v jsonvalue.Value) (segmentRule, bool) {
	if !isObject(v) {
		return segmentRule{}, false
	}
	r := segmentRule{weight: -1}
	if w := field(v, "weight"); !isAbsent(w) {
		n, ok := isCount(w)
		if !ok || n > BucketCount {
			return segmentRule{}, false
		}
		r.weight = int(n)
	}
	kind, ok := optionalKind(field(v, "rolloutContextKind"))
	if !ok {
		return segmentRule{}, false
	}
	r.rolloutKind = kind
	clauses, ok := readList(field(v, "clauses"), readAttributeClause)
	r.clauses = clauses
	return r, ok
}

// addContextLists reads an optional list of {contextKind, values} into index.
func addContextLists(v jsonvalue.Value, index keysByKind) bool {
	if isAbsent(v) {
		return true
	}
	arr, ok := v.AsArray()
	if !ok {
		return false
	}
	for _, e := range arr {
		if !isObject(e) {
			return false
		}
		kind, ok := field(e, "contextKind").AsString()
		values, ok2 := stringArray(field(e, "values"))
		if !ok || !isKind(kind) || !ok2 {
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

func readSegment(v jsonvalue.Value) segmentEntry {
	if !isObject(v) {
		return segmentEntry{}
	}
	key, ok := field(v, "key").AsString()
	salt, ok2 := field(v, "salt").AsString()
	included, ok3 := stringArray(field(v, "included"))
	excluded, ok4 := stringArray(field(v, "excluded"))
	if !ok || key == "" || !ok2 || !ok3 || !ok4 {
		return segmentEntry{}
	}
	seg := &segment{key: key, salt: salt, included: keysByKind{}, excluded: keysByKind{}}
	addKeys(seg.included, DefaultKind, included)
	addKeys(seg.excluded, DefaultKind, excluded)
	if !addContextLists(field(v, "includedContexts"), seg.included) ||
		!addContextLists(field(v, "excludedContexts"), seg.excluded) {
		return segmentEntry{}
	}
	rules, ok := readList(field(v, "rules"), readSegmentRule)
	if !ok {
		return segmentEntry{}
	}
	seg.rules = rules
	return segmentEntry{seg: seg}
}

// segmentProblem is the first reference to a segment that is missing or malformed, in rule, clause, value order.
func (r *Ruleset) segmentProblem(f *flag) string {
	for _, rl := range f.rules {
		for _, c := range rl.clauses {
			if !c.segment {
				continue
			}
			for _, key := range c.segmentKeys {
				entry, found := r.segments[key]
				if !found {
					return CodeSegmentNotFound
				}
				if entry.seg == nil {
					return CodeMalformedSegment
				}
			}
		}
	}
	return ""
}

// indexByKey calls compile for the first item of each string key; an item with no string key is dropped.
func indexByKey[T any](items jsonvalue.Value, compile func(jsonvalue.Value) T) map[string]T {
	index := map[string]T{}
	arr, _ := items.AsArray()
	for _, item := range arr {
		if !isObject(item) {
			continue
		}
		key, ok := field(item, "key").AsString()
		if !ok {
			continue
		}
		if _, taken := index[key]; !taken {
			index[key] = compile(item)
		}
	}
	return index
}

func (r *Ruleset) compileFlag(v jsonvalue.Value) *flagEntry {
	header, ok := readHeader(v)
	if !ok {
		return &flagEntry{status: entryMalformed}
	}
	f, ok := readBody(v, header)
	if !ok {
		return &flagEntry{status: entryBroken, header: header, code: CodeMalformedFlag}
	}
	if code := r.segmentProblem(f); code != "" {
		return &flagEntry{status: entryBroken, header: header, code: code}
	}
	return &flagEntry{status: entryReady, header: header, flag: f}
}

// NewRuleset validates and indexes a ruleset ({flags, segments}, as JSON hands it over) once, so each
// Evaluate skips that work. It never fails: a bad flag becomes an ERROR result when it is evaluated, and a
// root that is not an object, or a flags or segments that is not an array, is empty.
func NewRuleset(root jsonvalue.Value) *Ruleset {
	r := &Ruleset{}
	r.segments = indexByKey(field(root, "segments"), readSegment)
	r.flags = indexByKey(field(root, "flags"), r.compileFlag)
	return r
}

// FlagVersion is the version of a flag that is ready to evaluate.
func (r *Ruleset) FlagVersion(key string) (float64, bool) {
	e := r.flags[key]
	if e == nil || e.status != entryReady {
		return 0, false
	}
	return e.flag.version, true
}
