// Package evaluator evaluates flags to docs/evaluation.md. It depends on nothing but the JSON
// value and string helpers of this module: no HTTP, no clock, no logging.
package evaluator

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

type value = jsonvalue.Value

const (
	bucketCount = 100_000
	defaultKind = "user"
)

// isCount is a finite integer >= 0 (a huge integer such as 1e300 counts).
func isCount(v value) (float64, bool) {
	f, ok := v.Number()
	if !ok || math.IsInf(f, 0) || math.IsNaN(f) || math.Trunc(f) != f || f < 0 {
		return 0, false
	}
	return f, true
}

func isVariationIndex(v value, count int) (int, bool) {
	f, ok := isCount(v)
	if !ok || f >= float64(count) {
		return 0, false
	}
	return int(f), true
}

func isKey(v value) (string, bool) {
	s, ok := v.String()
	return s, ok && s != ""
}

// isKindName is ^[A-Za-z0-9._-]+$ and neither "kind" nor "multi". ASCII only, byte by byte.
func isKindName(s string) bool {
	if s == "" || s == "kind" || s == "multi" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '.' || c == '_' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}

// optionalKind reads a kind that may be absent (null). ok is false when it is present and invalid;
// kind is "" when absent.
func optionalKind(v value) (kind string, ok bool) {
	if v.IsNull() {
		return "", true
	}
	s, isString := v.String()
	if !isString || !isKindName(s) {
		return "", false
	}
	return s, true
}

func stringArray(v value) ([]string, bool) {
	items, ok := v.Array()
	if !ok {
		return nil, false
	}
	out := make([]string, len(items))
	for i, item := range items {
		s, isString := item.String()
		if !isString {
			return nil, false
		}
		out[i] = s
	}
	return out, true
}

func optionalBool(v value) (negate, ok bool) {
	if v.IsNull() {
		return false, true
	}
	b, isBool := v.Bool()
	return b, isBool
}

// parseReference reads an attribute reference (a name that starts with '/'). The components are
// what is left after dropping the leading '/', splitting on '/' and decoding ~1 as '/' and ~0 as
// '~' in one left-to-right pass. It is false when a component is empty or a '~' is followed by
// anything but 0 or 1, or ends the component.
func parseReference(text string) ([]string, bool) {
	var components []string
	rest := text[1:]
	for {
		end := 0
		for end < len(rest) && rest[end] != '/' {
			end++
		}
		part := rest[:end]
		if part == "" {
			return nil, false
		}
		decoded := make([]byte, 0, len(part))
		for i := 0; i < len(part); i++ {
			if part[i] != '~' {
				decoded = append(decoded, part[i])
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
		components = append(components, string(decoded))
		if end == len(rest) {
			return components, true
		}
		rest = rest[end+1:]
	}
}

var attributeOps = map[string]opKind{
	"in": opIn, "contains": opContains, "startsWith": opStartsWith, "endsWith": opEndsWith,
	"lt": opLt, "lte": opLte, "gt": opGt, "gte": opGte,
	"semverEq": opSemverEq, "semverLt": opSemverLt, "semverLte": opSemverLte,
	"semverGt": opSemverGt, "semverGte": opSemverGte,
	"before": opBefore, "after": opAfter, "matches": opMatches,
}

func readAttributeClause(v value) (*clause, bool) {
	members, ok := v.Object()
	if !ok {
		return nil, false
	}
	opName, isString := members["op"].String()
	if !isString {
		return nil, false
	}
	op, known := attributeOps[opName]
	if !known {
		return nil, false
	}
	name, ok := isKey(members["attribute"])
	if !ok {
		return nil, false
	}
	path := []string{name}
	if name[0] == '/' {
		if path, ok = parseReference(name); !ok {
			return nil, false
		}
	}
	negate, ok := optionalBool(members["negate"])
	if !ok {
		return nil, false
	}
	kind, ok := optionalKind(members["contextKind"])
	if !ok {
		return nil, false
	}
	if kind == "" {
		kind = defaultKind
	}
	items, ok := members["values"].Array()
	if !ok {
		return nil, false
	}
	c := &clause{op: op, path: path, negate: negate, kind: kind, values: make([]clauseValue, len(items))}
	for i, item := range items {
		cv, ok := readClauseValue(op, item)
		if !ok {
			return nil, false
		}
		c.values[i] = cv
	}
	return c, true
}

func readSegmentClause(v value) (*clause, bool) {
	members, ok := v.Object()
	if !ok {
		return nil, false
	}
	if op, isString := members["op"].String(); !isString || op != "segmentMatch" {
		return nil, false
	}
	negate, ok := optionalBool(members["negate"])
	if !ok {
		return nil, false
	}
	keys, ok := stringArray(members["values"])
	if !ok {
		return nil, false
	}
	return &clause{op: opSegmentMatch, negate: negate, segmentKeys: keys}, true
}

func readRollout(v value, variationCount int) (*rollout, bool) {
	members, ok := v.Object()
	if !ok {
		return nil, false
	}
	kind, ok := optionalKind(members["contextKind"])
	if !ok {
		return nil, false
	}
	items, ok := members["variations"].Array()
	if !ok || len(items) == 0 {
		return nil, false
	}
	r := &rollout{kind: kind, variations: make([]weighted, len(items))}
	total := 0.0
	for i, item := range items {
		entry, ok := item.Object()
		if !ok {
			return nil, false
		}
		index, ok := isVariationIndex(entry["variation"], variationCount)
		if !ok {
			return nil, false
		}
		weight, ok := isCount(entry["weight"])
		if !ok || weight > bucketCount {
			return nil, false
		}
		r.variations[i] = weighted{variation: index, weight: int(weight)}
		total += weight
	}
	if total != bucketCount {
		return nil, false
	}
	return r, true
}

// readServe reads the variation or rollout of members (a rule, or the fallthrough).
func readServe(members map[string]value, variationCount int) (serve, bool) {
	rolloutValue := members["rollout"]
	if rolloutValue.IsNull() {
		index, ok := isVariationIndex(members["variation"], variationCount)
		return serve{variation: index}, ok
	}
	if !members["variation"].IsNull() {
		return serve{}, false
	}
	r, ok := readRollout(rolloutValue, variationCount)
	return serve{rollout: r}, ok
}

func readRule(v value, variationCount int) (*rule, bool) {
	members, ok := v.Object()
	if !ok {
		return nil, false
	}
	items, ok := members["clauses"].Array()
	if !ok {
		return nil, false
	}
	r := &rule{clauses: make([]*clause, len(items))}
	for i, item := range items {
		c, ok := readAttributeClause(item)
		if !ok {
			c, ok = readSegmentClause(item)
		}
		if !ok {
			return nil, false
		}
		r.clauses[i] = c
	}
	s, ok := readServe(members, variationCount)
	r.serve = s
	return r, ok
}

func readTarget(v value, variationCount int) (*target, bool) {
	members, ok := v.Object()
	if !ok {
		return nil, false
	}
	index, ok := isVariationIndex(members["variation"], variationCount)
	if !ok {
		return nil, false
	}
	keys, ok := stringArray(members["contextKeys"])
	if !ok {
		return nil, false
	}
	kind, ok := optionalKind(members["contextKind"])
	if !ok {
		return nil, false
	}
	if kind == "" {
		kind = defaultKind
	}
	return &target{variation: index, keys: keys, kind: kind}, true
}

// readHeader checks what serving the off variation needs.
func readHeader(members map[string]value) (*flagHeader, bool) {
	on, ok := members["on"].Bool()
	if !ok {
		return nil, false
	}
	if _, ok := isKey(members["key"]); !ok {
		return nil, false
	}
	variations, ok := members["variations"].Array()
	if !ok || len(variations) == 0 {
		return nil, false
	}
	h := &flagHeader{on: on, variations: variations, offVariation: -1}
	if off := members["offVariation"]; !off.IsNull() {
		index, ok := isVariationIndex(off, len(variations))
		if !ok {
			return nil, false
		}
		h.offVariation = index
	}
	return h, true
}

// readBody checks the rest of a flag. It returns false for any malformed part.
func readBody(members map[string]value, count int) (*flagBody, bool) {
	salt, ok := members["salt"].String()
	if !ok {
		return nil, false
	}
	version, ok := isCount(members["version"])
	if !ok {
		return nil, false
	}
	b := &flagBody{salt: salt, version: version}
	targets, ok := members["targets"].Array()
	if !ok {
		return nil, false
	}
	for _, item := range targets {
		t, ok := readTarget(item, count)
		if !ok {
			return nil, false
		}
		b.targets = append(b.targets, t)
	}
	prerequisites, ok := members["prerequisites"].Array()
	if !ok {
		return nil, false
	}
	for _, item := range prerequisites {
		entry, ok := item.Object()
		if !ok {
			return nil, false
		}
		flagKey, ok := isKey(entry["flagKey"])
		if !ok {
			return nil, false
		}
		variation, ok := isCount(entry["variation"])
		if !ok {
			return nil, false
		}
		b.prerequisites = append(b.prerequisites, prerequisite{flagKey: flagKey, variation: variation})
	}
	rules, ok := members["rules"].Array()
	if !ok {
		return nil, false
	}
	for _, item := range rules {
		r, ok := readRule(item, count)
		if !ok {
			return nil, false
		}
		b.rules = append(b.rules, r)
	}
	fall, ok := members["fallthrough"].Object()
	if !ok {
		return nil, false
	}
	if b.fallThrough, ok = readServe(fall, count); !ok {
		return nil, false
	}
	return b, true
}

func readContextLists(v value) (map[string]map[string]struct{}, bool) {
	out := map[string]map[string]struct{}{}
	if v.IsNull() {
		return out, true
	}
	items, ok := v.Array()
	if !ok {
		return nil, false
	}
	// Validate every entry before using any.
	for _, item := range items {
		entry, ok := item.Object()
		if !ok {
			return nil, false
		}
		kind, isString := entry["contextKind"].String()
		if !isString || !isKindName(kind) {
			return nil, false
		}
		values, ok := stringArray(entry["values"])
		if !ok {
			return nil, false
		}
		keys := out[kind]
		if keys == nil {
			keys = map[string]struct{}{}
			out[kind] = keys
		}
		for _, k := range values {
			keys[k] = struct{}{}
		}
	}
	return out, true
}

func readSegmentRule(v value) (*segmentRule, bool) {
	members, ok := v.Object()
	if !ok {
		return nil, false
	}
	r := &segmentRule{weight: -1}
	if w := members["weight"]; !w.IsNull() {
		weight, ok := isCount(w)
		if !ok || weight > bucketCount {
			return nil, false
		}
		r.weight = int(weight)
	}
	kind, ok := optionalKind(members["rolloutContextKind"])
	if !ok {
		return nil, false
	}
	r.rolloutKind = kind
	items, ok := members["clauses"].Array()
	if !ok {
		return nil, false
	}
	for _, item := range items {
		c, ok := readAttributeClause(item)
		if !ok {
			return nil, false
		}
		r.clauses = append(r.clauses, c)
	}
	return r, true
}

func readSegment(v value) (*segment, bool) {
	members, ok := v.Object()
	if !ok {
		return nil, false
	}
	key, ok := isKey(members["key"])
	if !ok {
		return nil, false
	}
	salt, ok := members["salt"].String()
	if !ok {
		return nil, false
	}
	included, ok1 := stringArray(members["included"])
	excluded, ok2 := stringArray(members["excluded"])
	if !ok1 || !ok2 {
		return nil, false
	}
	includedContexts, ok1 := readContextLists(members["includedContexts"])
	excludedContexts, ok2 := readContextLists(members["excludedContexts"])
	if !ok1 || !ok2 {
		return nil, false
	}
	s := &segment{key: key, salt: salt, included: includedContexts, excluded: excludedContexts}
	addUsers(s.included, included)
	addUsers(s.excluded, excluded)
	rules, ok := members["rules"].Array()
	if !ok {
		return nil, false
	}
	for _, item := range rules {
		r, ok := readSegmentRule(item)
		if !ok {
			return nil, false
		}
		s.rules = append(s.rules, r)
	}
	return s, true
}

func addUsers(index map[string]map[string]struct{}, userKeys []string) {
	keys := index[defaultKind]
	if keys == nil {
		keys = map[string]struct{}{}
		index[defaultKind] = keys
	}
	for _, k := range userKeys {
		keys[k] = struct{}{}
	}
}

// Compile validates and indexes a ruleset ({flags, segments}) once, so each Evaluate skips that
// work. It never panics: a bad flag becomes an ERROR result when it is evaluated.
func Compile(input jsonvalue.Value) *Ruleset {
	rs := &Ruleset{flags: map[string]*flagEntry{}, segments: map[string]*segmentEntry{}}
	members, _ := input.Object()
	segmentItems, _ := members["segments"].Array()
	for _, item := range segmentItems {
		key, ok := keyOf(item)
		if !ok {
			continue
		}
		if _, dup := rs.segments[key]; dup {
			continue
		}
		s, ok := readSegment(item)
		rs.segments[key] = &segmentEntry{malformed: !ok, segment: s}
	}
	flagItems, _ := members["flags"].Array()
	for _, item := range flagItems {
		key, ok := keyOf(item)
		if !ok {
			continue
		}
		if _, dup := rs.flags[key]; dup {
			continue
		}
		rs.flags[key] = rs.compileFlag(item)
	}
	return rs
}

// keyOf is the string key of an object item; items without one are dropped.
func keyOf(item value) (string, bool) {
	members, ok := item.Object()
	if !ok {
		return "", false
	}
	return members["key"].String()
}

func (rs *Ruleset) compileFlag(item value) *flagEntry {
	members, _ := item.Object()
	header, ok := readHeader(members)
	if !ok {
		return &flagEntry{status: statusMalformed}
	}
	body, ok := readBody(members, len(header.variations))
	if !ok {
		return &flagEntry{status: statusBroken, header: header, code: "MALFORMED_FLAG"}
	}
	entry := &flagEntry{header: header, body: body}
	if code := rs.segmentProblem(body); code != "" {
		entry.status = statusBroken
		entry.code = code
		return entry
	}
	entry.status = statusReady
	entry.targets = indexTargets(body)
	return entry
}

// segmentProblem is the first reference to a segment that is missing or malformed, in rule, clause,
// value order.
func (rs *Ruleset) segmentProblem(body *flagBody) string {
	for _, r := range body.rules {
		for _, c := range r.clauses {
			if c.op != opSegmentMatch {
				continue
			}
			for _, key := range c.segmentKeys {
				s, ok := rs.segments[key]
				if !ok {
					return "SEGMENT_NOT_FOUND"
				}
				if s.malformed {
					return "MALFORMED_SEGMENT"
				}
			}
		}
	}
	return ""
}

func indexTargets(body *flagBody) map[string]map[string]targetHit {
	index := map[string]map[string]targetHit{}
	for order, t := range body.targets {
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

// FlagVersion is the version of the flag named key, when the flag's body is well formed. A flag
// that is malformed, or whose body is, has none.
func (rs *Ruleset) FlagVersion(key string) (float64, bool) {
	entry, ok := rs.flags[key]
	if !ok || entry.body == nil {
		return 0, false
	}
	return entry.body.version, true
}
