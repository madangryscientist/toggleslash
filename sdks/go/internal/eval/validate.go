package eval

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

const bucketCount = 100000

// defaultKind is what a kind that is read and absent means.
const defaultKind = "user"

type value = jsonv.Value

func isRecord(v value) (*jsonv.Members, bool) { return v.AsObject() }

// field reads an own property of an object; a missing property reads as null, and null is absent.
func field(o *jsonv.Members, name string) value {
	v, _ := o.Get(name)
	return v
}

func isKey(v value) (string, bool) {
	s, ok := v.AsString()
	return s, ok && s != ""
}

// isCount reports an integer that is 0 or more, and returns it clamped to a float for comparison.
func isCount(v value) (float64, bool) {
	n, ok := v.AsNumber()
	if !ok || math.IsInf(n, 0) || math.IsNaN(n) || n != math.Trunc(n) || n < 0 {
		return 0, false
	}
	return n, true
}

// isIndex reports a count below limit, as an int.
func isIndex(v value, limit int) (int, bool) {
	n, ok := isCount(v)
	if !ok || n >= float64(limit) {
		return 0, false
	}
	return int(n), true
}

func isKindString(s string) bool {
	if s == "" || s == "kind" || s == "multi" {
		return false
	}
	for i := range len(s) {
		c := s[i]
		ok := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}

// optionalKind reads a kind that may be absent (absent is "user"). ok is false when present and invalid.
func optionalKind(v value) (string, bool) {
	if v.IsNull() {
		return defaultKind, true
	}
	s, ok := v.AsString()
	if !ok || !isKindString(s) {
		return "", false
	}
	return s, true
}

func stringArray(v value) ([]string, bool) {
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
	"semverEq": opSemverEq, "semverLt": opSemverLt, "semverLte": opSemverLte, "semverGt": opSemverGt, "semverGte": opSemverGte,
	"before": opBefore, "after": opAfter, "matches": opMatches,
}

// clause is an attribute clause or a segment clause, validated.
type clause struct {
	op        opKind
	attribute []string // the components; the first is read as a literal name
	values    []value
	patterns  []*Pattern // for matches: one per value, nil when not a string or not a valid pattern
	negate    bool
	kind      string
	segments  []string // segmentMatch only
}

// parseReference splits a name that starts with "/" into components, or reports an invalid reference.
func parseReference(text string) ([]string, bool) {
	var out []string
	rest := text[1:]
	for {
		part := rest
		next := -1
		for i := range len(rest) {
			if rest[i] == '/' {
				part = rest[:i]
				next = i + 1
				break
			}
		}
		if part == "" {
			return nil, false
		}
		decoded := make([]byte, 0, len(part))
		for i := 0; i < len(part); i++ {
			c := part[i]
			if c != '~' {
				decoded = append(decoded, c)
				continue
			}
			if i+1 >= len(part) {
				return nil, false
			}
			switch part[i+1] {
			case '0':
				decoded = append(decoded, '~')
			case '1':
				decoded = append(decoded, '/')
			default:
				return nil, false
			}
			i++
		}
		out = append(out, string(decoded))
		if next < 0 {
			return out, true
		}
		rest = rest[next:]
	}
}

func parseNegate(v value) (bool, bool) {
	if v.IsNull() {
		return false, true
	}
	b, ok := v.AsBool()
	return b, ok
}

func isClauseValue(v value) bool {
	switch v.Kind() {
	case jsonv.Number:
		n, _ := v.AsNumber()
		return !math.IsInf(n, 0) && !math.IsNaN(n)
	case jsonv.String, jsonv.Bool:
		return true
	}
	return false
}

func parseAttributeClause(v value) (*clause, bool) {
	o, ok := isRecord(v)
	if !ok {
		return nil, false
	}
	opName, ok := field(o, "op").AsString()
	if !ok {
		return nil, false
	}
	op, known := opNames[opName]
	if !known {
		return nil, false
	}
	name, ok := isKey(field(o, "attribute"))
	if !ok {
		return nil, false
	}
	components := []string{name}
	if name[0] == '/' {
		if components, ok = parseReference(name); !ok {
			return nil, false
		}
	}
	negate, ok := parseNegate(field(o, "negate"))
	if !ok {
		return nil, false
	}
	kind, ok := optionalKind(field(o, "contextKind"))
	if !ok {
		return nil, false
	}
	values, ok := field(o, "values").AsArray()
	if !ok {
		return nil, false
	}
	for _, item := range values {
		if !isClauseValue(item) {
			return nil, false
		}
	}
	c := &clause{op: op, attribute: components, values: values, negate: negate, kind: kind}
	if op == opMatches {
		c.patterns = make([]*Pattern, len(values))
		for i, item := range values {
			if s, ok := item.AsString(); ok {
				c.patterns[i] = CompilePattern(jsonv.CodePoints(s))
			}
		}
	}
	return c, true
}

func parseSegmentClause(o *jsonv.Members) (*clause, bool) {
	negate, ok := parseNegate(field(o, "negate"))
	if !ok {
		return nil, false
	}
	keys, ok := stringArray(field(o, "values"))
	if !ok {
		return nil, false
	}
	return &clause{op: opSegmentMatch, negate: negate, segments: keys}, true
}

// parseFlagClause reads a clause of a flag's rule: an attribute clause or a segment clause.
func parseFlagClause(v value) (*clause, bool) {
	o, ok := isRecord(v)
	if !ok {
		return nil, false
	}
	if op, _ := field(o, "op").AsString(); op == "segmentMatch" {
		return parseSegmentClause(o)
	}
	return parseAttributeClause(v)
}

type weighted struct {
	variation int
	weight    int
}

type rollout struct {
	variations []weighted
	kind       string
}

// serve is a variation index, or a rollout when rollout is non-nil.
type serve struct {
	variation int
	rollout   *rollout
}

func parseRollout(v value, variationCount int) (*rollout, bool) {
	o, ok := isRecord(v)
	if !ok {
		return nil, false
	}
	kind, ok := optionalKind(field(o, "contextKind"))
	if !ok {
		return nil, false
	}
	items, ok := field(o, "variations").AsArray()
	if !ok || len(items) == 0 {
		return nil, false
	}
	r := &rollout{kind: kind}
	total := 0.0
	for _, item := range items {
		io, ok := isRecord(item)
		if !ok {
			return nil, false
		}
		index, ok := isIndex(field(io, "variation"), variationCount)
		if !ok {
			return nil, false
		}
		weight, ok := isCount(field(io, "weight"))
		if !ok || weight > bucketCount {
			return nil, false
		}
		total += weight
		r.variations = append(r.variations, weighted{variation: index, weight: int(weight)})
	}
	if total != bucketCount {
		return nil, false
	}
	return r, true
}

func parseServe(o *jsonv.Members, variationCount int) (serve, bool) {
	variation, rolloutValue := field(o, "variation"), field(o, "rollout")
	if rolloutValue.IsNull() {
		index, ok := isIndex(variation, variationCount)
		return serve{variation: index}, ok
	}
	if !variation.IsNull() {
		return serve{}, false
	}
	r, ok := parseRollout(rolloutValue, variationCount)
	return serve{rollout: r}, ok
}

type rule struct {
	clauses []*clause
	serve   serve
}

type target struct {
	variation int
	keys      []string
	kind      string
}

type prerequisite struct {
	flagKey   string
	variation float64 // a count; not range checked
}

type header struct {
	key        string
	on         bool
	variations []value
	off        int // -1 when absent
}

type flag struct {
	header        header
	salt          string
	version       float64
	targets       []target
	prerequisites []prerequisite
	rules         []rule
	fallthrough_  serve
}

// parseHeader checks what serving the off variation needs.
func parseHeader(o *jsonv.Members) (header, bool) {
	on, ok := field(o, "on").AsBool()
	if !ok {
		return header{}, false
	}
	key, ok := isKey(field(o, "key"))
	if !ok {
		return header{}, false
	}
	variations, ok := field(o, "variations").AsArray()
	if !ok || len(variations) == 0 {
		return header{}, false
	}
	h := header{key: key, on: on, variations: variations, off: -1}
	if off := field(o, "offVariation"); !off.IsNull() {
		index, ok := isIndex(off, len(variations))
		if !ok {
			return header{}, false
		}
		h.off = index
	}
	return h, true
}

func parseTarget(v value, variationCount int) (target, bool) {
	o, ok := isRecord(v)
	if !ok {
		return target{}, false
	}
	index, ok := isIndex(field(o, "variation"), variationCount)
	if !ok {
		return target{}, false
	}
	keys, ok := stringArray(field(o, "contextKeys"))
	if !ok {
		return target{}, false
	}
	kind, ok := optionalKind(field(o, "contextKind"))
	return target{variation: index, keys: keys, kind: kind}, ok
}

func parseRule(v value, variationCount int) (rule, bool) {
	o, ok := isRecord(v)
	if !ok {
		return rule{}, false
	}
	items, ok := field(o, "clauses").AsArray()
	if !ok {
		return rule{}, false
	}
	var r rule
	for _, item := range items {
		c, ok := parseFlagClause(item)
		if !ok {
			return rule{}, false
		}
		r.clauses = append(r.clauses, c)
	}
	r.serve, ok = parseServe(o, variationCount)
	return r, ok
}

// parseBody checks the rest of a flag. It fills in f, whose header is already read.
func parseBody(o *jsonv.Members, f *flag) bool {
	var ok bool
	if f.salt, ok = field(o, "salt").AsString(); !ok {
		return false
	}
	if f.version, ok = isCount(field(o, "version")); !ok {
		return false
	}
	count := len(f.header.variations)
	targets, ok := field(o, "targets").AsArray()
	if !ok {
		return false
	}
	for _, item := range targets {
		t, ok := parseTarget(item, count)
		if !ok {
			return false
		}
		f.targets = append(f.targets, t)
	}
	prerequisites, ok := field(o, "prerequisites").AsArray()
	if !ok {
		return false
	}
	for _, item := range prerequisites {
		po, ok := isRecord(item)
		if !ok {
			return false
		}
		key, ok := isKey(field(po, "flagKey"))
		if !ok {
			return false
		}
		variation, ok := isCount(field(po, "variation"))
		if !ok {
			return false
		}
		f.prerequisites = append(f.prerequisites, prerequisite{flagKey: key, variation: variation})
	}
	rules, ok := field(o, "rules").AsArray()
	if !ok {
		return false
	}
	for _, item := range rules {
		r, ok := parseRule(item, count)
		if !ok {
			return false
		}
		f.rules = append(f.rules, r)
	}
	fo, ok := isRecord(field(o, "fallthrough"))
	if !ok {
		return false
	}
	f.fallthrough_, ok = parseServe(fo, count)
	return ok
}

// segment is a validated segment with its key lists indexed by kind.
type segment struct {
	key      string
	salt     string
	included map[string]map[string]struct{}
	excluded map[string]map[string]struct{}
	rules    []segmentRule
}

type segmentRule struct {
	clauses []*clause
	weight  int // -1 when absent
	kind    string
}

func keysByKind(userKeys []string, lists value) (map[string]map[string]struct{}, bool) {
	index := map[string]map[string]struct{}{defaultKind: {}}
	for _, k := range userKeys {
		index[defaultKind][k] = struct{}{}
	}
	if lists.IsNull() {
		return index, true
	}
	items, ok := lists.AsArray()
	if !ok {
		return nil, false
	}
	for _, item := range items {
		o, ok := isRecord(item)
		if !ok {
			return nil, false
		}
		kind, ok := field(o, "contextKind").AsString()
		if !ok || !isKindString(kind) {
			return nil, false
		}
		values, ok := stringArray(field(o, "values"))
		if !ok {
			return nil, false
		}
		set := index[kind]
		if set == nil {
			set = map[string]struct{}{}
			index[kind] = set
		}
		for _, k := range values {
			set[k] = struct{}{}
		}
	}
	return index, true
}

func parseSegment(v value) (*segment, bool) {
	o, ok := isRecord(v)
	if !ok {
		return nil, false
	}
	s := &segment{}
	if s.key, ok = isKey(field(o, "key")); !ok {
		return nil, false
	}
	if s.salt, ok = field(o, "salt").AsString(); !ok {
		return nil, false
	}
	included, ok := stringArray(field(o, "included"))
	if !ok {
		return nil, false
	}
	excluded, ok := stringArray(field(o, "excluded"))
	if !ok {
		return nil, false
	}
	if s.included, ok = keysByKind(included, field(o, "includedContexts")); !ok {
		return nil, false
	}
	if s.excluded, ok = keysByKind(excluded, field(o, "excludedContexts")); !ok {
		return nil, false
	}
	rules, ok := field(o, "rules").AsArray()
	if !ok {
		return nil, false
	}
	for _, item := range rules {
		ro, ok := isRecord(item)
		if !ok {
			return nil, false
		}
		r := segmentRule{weight: -1}
		if w := field(ro, "weight"); !w.IsNull() {
			weight, ok := isCount(w)
			if !ok || weight > bucketCount {
				return nil, false
			}
			r.weight = int(weight)
		}
		if r.kind, ok = optionalKind(field(ro, "rolloutContextKind")); !ok {
			return nil, false
		}
		clauses, ok := field(ro, "clauses").AsArray()
		if !ok {
			return nil, false
		}
		for _, ci := range clauses {
			c, ok := parseAttributeClause(ci)
			if !ok {
				return nil, false
			}
			r.clauses = append(r.clauses, c)
		}
		s.rules = append(s.rules, r)
	}
	return s, true
}
