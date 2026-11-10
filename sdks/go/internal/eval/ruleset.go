package eval

import (
	"strings"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

// The shape of a ruleset, checked at runtime because rulesets arrive as JSON. docs/evaluation.md is
// normative for what counts as malformed. Null and absent are the same for an optional field.

type opcode uint8

const (
	opIn opcode = iota
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

var attributeOps = map[string]opcode{
	"in": opIn, "contains": opContains, "startsWith": opStartsWith, "endsWith": opEndsWith,
	"lt": opLt, "lte": opLte, "gt": opGt, "gte": opGte,
	"semverEq": opSemverEq, "semverLt": opSemverLt, "semverLte": opSemverLte,
	"semverGt": opSemverGt, "semverGte": opSemverGte,
	"before": opBefore, "after": opAfter, "matches": opMatches,
}

// clause is an attribute clause or a segmentMatch clause.
type clause struct {
	op          opcode
	negate      bool
	kind        string   // the context kind of an attribute clause
	components  []string // the attribute name, or the parts of its "/" reference
	values      []jsonvalue.Value
	segmentKeys []string
}

type serve struct {
	variation int
	rollout   *rollout
}

type weighted struct {
	variation int
	weight    int
}

type rollout struct {
	variations []weighted
	kind       string
}

type rule struct {
	clauses []clause
	serve
}

type prerequisite struct {
	flagKey   string
	variation float64
}

// targetHit is a target that lists a key: its place in targets and the variation it serves.
type targetHit struct {
	order     int
	variation int
}

// flag is a flag whose header and body are both valid.
type flag struct {
	key           string
	salt          string
	version       float64
	variations    []jsonvalue.Value
	targets       map[string]map[string]targetHit // by kind, then by key
	prerequisites []prerequisite
	rules         []rule
	defaultServe  serve
}

type entryStatus uint8

const (
	statusMalformed entryStatus = iota
	statusBroken
	statusReady
)

// flagEntry is a flag by its validity. A broken flag has a valid header, so it can still be off.
type flagEntry struct {
	status     entryStatus
	on         bool
	variations []jsonvalue.Value
	off        int // the off variation, or -1
	code       string
	flag       *flag
}

type keysByKind map[string]map[string]struct{}

type segmentRule struct {
	clauses []clause
	weight  int // -1 when the rule has no rollout
	kind    string
}

type segmentEntry struct {
	malformed bool
	key, salt string
	included  keysByKind
	excluded  keysByKind
	rules     []segmentRule
}

// Ruleset is a validated, indexed ruleset. It is immutable, so any number of goroutines may evaluate it.
type Ruleset struct {
	flags    map[string]*flagEntry
	segments map[string]*segmentEntry
}

func isKey(v jsonvalue.Value) (string, bool) {
	s, ok := v.Text()
	return s, ok && s != ""
}

func isOptionalKind(v jsonvalue.Value) (string, bool) {
	if v.IsNull() {
		return defaultKind, true
	}
	s, ok := v.Text()
	return s, ok && isKind(s)
}

func variationIndex(v jsonvalue.Value, count int) (int, bool) {
	n, _ := v.Number()
	if !v.IsCount() || n >= float64(count) {
		return 0, false
	}
	return int(n), true
}

func optionalBool(v jsonvalue.Value) (bool, bool) {
	if v.IsNull() {
		return false, true
	}
	return v.Bool()
}

func stringArray(v jsonvalue.Value) ([]string, bool) {
	items, ok := v.Items()
	if !ok {
		return nil, false
	}
	out := make([]string, len(items))
	for i, item := range items {
		if out[i], ok = item.Text(); !ok {
			return nil, false
		}
	}
	return out, true
}

// parseReference reads an attribute reference, to "Reading the attribute" in docs/evaluation.md: a name that
// starts with "/". The components are what is left after dropping the leading "/", splitting on "/" and
// decoding "~1" as "/" and "~0" as "~" in one left-to-right pass. It is false when a component is empty or
// a "~" is followed by anything but 0 or 1, or ends the component.
func parseReference(text string) ([]string, bool) {
	var components []string
	for _, part := range strings.Split(text[1:], "/") {
		if part == "" {
			return nil, false
		}
		var decoded strings.Builder
		for i := 0; i < len(part); i++ {
			if part[i] != '~' {
				decoded.WriteByte(part[i])
				continue
			}
			switch {
			case i+1 < len(part) && part[i+1] == '0':
				decoded.WriteByte('~')
			case i+1 < len(part) && part[i+1] == '1':
				decoded.WriteByte('/')
			default:
				return nil, false
			}
			i++
		}
		components = append(components, decoded.String())
	}
	return components, true
}

func attributeComponents(v jsonvalue.Value) ([]string, bool) {
	name, ok := isKey(v)
	if !ok {
		return nil, false
	}
	if !strings.HasPrefix(name, "/") {
		return []string{name}, true
	}
	return parseReference(name)
}

func isClauseValue(v jsonvalue.Value) bool {
	if n, ok := v.Number(); ok {
		return n-n == 0 // finite
	}
	return v.Kind() == jsonvalue.KindString || v.Kind() == jsonvalue.KindBool
}

func parseAttributeClause(v jsonvalue.Value) (clause, bool) {
	if !v.IsObject() {
		return clause{}, false
	}
	opName, _ := v.Field("op").Text()
	op, known := attributeOps[opName]
	if !known {
		return clause{}, false
	}
	components, ok := attributeComponents(v.Field("attribute"))
	if !ok {
		return clause{}, false
	}
	negate, ok := optionalBool(v.Field("negate"))
	if !ok {
		return clause{}, false
	}
	kind, ok := isOptionalKind(v.Field("contextKind"))
	if !ok {
		return clause{}, false
	}
	values, ok := v.Field("values").Items()
	if !ok {
		return clause{}, false
	}
	for _, value := range values {
		if !isClauseValue(value) {
			return clause{}, false
		}
	}
	return clause{op: op, negate: negate, kind: kind, components: components, values: values}, true
}

func parseSegmentClause(v jsonvalue.Value) (clause, bool) {
	if !v.IsObject() {
		return clause{}, false
	}
	if op, _ := v.Field("op").Text(); op != "segmentMatch" {
		return clause{}, false
	}
	negate, ok := optionalBool(v.Field("negate"))
	if !ok {
		return clause{}, false
	}
	keys, ok := stringArray(v.Field("values"))
	if !ok {
		return clause{}, false
	}
	return clause{op: opSegmentMatch, negate: negate, segmentKeys: keys}, true
}

func parseRollout(v jsonvalue.Value, count int) (*rollout, bool) {
	if !v.IsObject() {
		return nil, false
	}
	kind, ok := isOptionalKind(v.Field("contextKind"))
	if !ok {
		return nil, false
	}
	items, ok := v.Field("variations").Items()
	if !ok || len(items) == 0 {
		return nil, false
	}
	out := &rollout{kind: kind, variations: make([]weighted, len(items))}
	total := 0
	for i, item := range items {
		if !item.IsObject() {
			return nil, false
		}
		index, ok := variationIndex(item.Field("variation"), count)
		weight := item.Field("weight")
		w, _ := weight.Number()
		if !ok || !weight.IsCount() || w > bucketCount {
			return nil, false
		}
		out.variations[i] = weighted{variation: index, weight: int(w)}
		total += int(w)
	}
	return out, total == bucketCount
}

func parseServe(v jsonvalue.Value, count int) (serve, bool) {
	if !v.IsObject() {
		return serve{}, false
	}
	variation, rolloutValue := v.Field("variation"), v.Field("rollout")
	if rolloutValue.IsNull() {
		index, ok := variationIndex(variation, count)
		return serve{variation: index}, ok
	}
	if !variation.IsNull() {
		return serve{}, false
	}
	r, ok := parseRollout(rolloutValue, count)
	return serve{rollout: r}, ok
}

func parseRule(v jsonvalue.Value, count int) (rule, bool) {
	if !v.IsObject() {
		return rule{}, false
	}
	items, ok := v.Field("clauses").Items()
	if !ok {
		return rule{}, false
	}
	clauses := make([]clause, len(items))
	for i, item := range items {
		c, ok := parseAttributeClause(item)
		if !ok {
			c, ok = parseSegmentClause(item)
		}
		if !ok {
			return rule{}, false
		}
		clauses[i] = c
	}
	s, ok := parseServe(v, count)
	return rule{clauses: clauses, serve: s}, ok
}

// flagHeader is what serving the off variation needs. It is checked apart so "on": false survives a bad rule.
func flagHeader(v jsonvalue.Value) (*flagEntry, bool) {
	if !v.IsObject() {
		return nil, false
	}
	on, ok := v.Field("on").Bool()
	variations, hasVariations := v.Field("variations").Items()
	if _, hasKey := isKey(v.Field("key")); !ok || !hasKey || !hasVariations || len(variations) == 0 {
		return nil, false
	}
	off := -1
	if offValue := v.Field("offVariation"); !offValue.IsNull() {
		if off, ok = variationIndex(offValue, len(variations)); !ok {
			return nil, false
		}
	}
	return &flagEntry{on: on, variations: variations, off: off}, true
}

func parseTarget(v jsonvalue.Value, count int) (kind string, variation int, keys []string, ok bool) {
	if !v.IsObject() {
		return
	}
	if variation, ok = variationIndex(v.Field("variation"), count); !ok {
		return
	}
	if keys, ok = stringArray(v.Field("contextKeys")); !ok {
		return
	}
	kind, ok = isOptionalKind(v.Field("contextKind"))
	return
}

func parsePrerequisite(v jsonvalue.Value) (prerequisite, bool) {
	if !v.IsObject() {
		return prerequisite{}, false
	}
	key, ok := isKey(v.Field("flagKey"))
	n, _ := v.Field("variation").Number()
	return prerequisite{flagKey: key, variation: n}, ok && v.Field("variation").IsCount()
}

// parseFlagBody checks the body of a flag whose header was valid.
func parseFlagBody(v jsonvalue.Value, header *flagEntry) (*flag, bool) {
	count := len(header.variations)
	salt, ok := v.Field("salt").Text()
	version, _ := v.Field("version").Number()
	if !ok || !v.Field("version").IsCount() {
		return nil, false
	}
	f := &flag{salt: salt, version: version, variations: header.variations, targets: map[string]map[string]targetHit{}}
	f.key, _ = v.Field("key").Text()

	targets, ok := v.Field("targets").Items()
	if !ok {
		return nil, false
	}
	for order, t := range targets {
		kind, variation, keys, ok := parseTarget(t, count)
		if !ok {
			return nil, false
		}
		byKey := f.targets[kind]
		if byKey == nil {
			byKey = map[string]targetHit{}
			f.targets[kind] = byKey
		}
		for _, key := range keys {
			if _, held := byKey[key]; !held {
				byKey[key] = targetHit{order: order, variation: variation}
			}
		}
	}

	prerequisites, ok := v.Field("prerequisites").Items()
	if !ok {
		return nil, false
	}
	for _, p := range prerequisites {
		parsed, ok := parsePrerequisite(p)
		if !ok {
			return nil, false
		}
		f.prerequisites = append(f.prerequisites, parsed)
	}

	rules, ok := v.Field("rules").Items()
	if !ok {
		return nil, false
	}
	for _, r := range rules {
		parsed, ok := parseRule(r, count)
		if !ok {
			return nil, false
		}
		f.rules = append(f.rules, parsed)
	}

	if f.defaultServe, ok = parseServe(v.Field("fallthrough"), count); !ok {
		return nil, false
	}
	return f, true
}

func contextLists(v jsonvalue.Value, into keysByKind) bool {
	if v.IsNull() {
		return true
	}
	items, ok := v.Items()
	if !ok {
		return false
	}
	for _, item := range items {
		kindText, hasKind := item.Field("contextKind").Text()
		values, valid := stringArray(item.Field("values"))
		if !item.IsObject() || !hasKind || !isKind(kindText) || !valid {
			return false
		}
		addKeys(into, kindText, values)
	}
	return true
}

func addKeys(into keysByKind, kind string, keys []string) {
	set := into[kind]
	if set == nil {
		set = map[string]struct{}{}
		into[kind] = set
	}
	for _, key := range keys {
		set[key] = struct{}{}
	}
}

func parseSegmentRule(v jsonvalue.Value) (segmentRule, bool) {
	if !v.IsObject() {
		return segmentRule{}, false
	}
	weight := -1
	if w := v.Field("weight"); !w.IsNull() {
		n, _ := w.Number()
		if !w.IsCount() || n > bucketCount {
			return segmentRule{}, false
		}
		weight = int(n)
	}
	kind, ok := isOptionalKind(v.Field("rolloutContextKind"))
	items, hasClauses := v.Field("clauses").Items()
	if !ok || !hasClauses {
		return segmentRule{}, false
	}
	clauses := make([]clause, len(items))
	for i, item := range items {
		if clauses[i], ok = parseAttributeClause(item); !ok {
			return segmentRule{}, false
		}
	}
	return segmentRule{clauses: clauses, weight: weight, kind: kind}, true
}

func compileSegment(v jsonvalue.Value) *segmentEntry {
	bad := &segmentEntry{malformed: true}
	if !v.IsObject() {
		return bad
	}
	key, hasKey := isKey(v.Field("key"))
	salt, hasSalt := v.Field("salt").Text()
	included, ok1 := stringArray(v.Field("included"))
	excluded, ok2 := stringArray(v.Field("excluded"))
	rules, ok3 := v.Field("rules").Items()
	if !hasKey || !hasSalt || !ok1 || !ok2 || !ok3 {
		return bad
	}
	seg := &segmentEntry{key: key, salt: salt, included: keysByKind{}, excluded: keysByKind{}}
	addKeys(seg.included, defaultKind, included)
	addKeys(seg.excluded, defaultKind, excluded)
	if !contextLists(v.Field("includedContexts"), seg.included) || !contextLists(v.Field("excludedContexts"), seg.excluded) {
		return bad
	}
	for _, r := range rules {
		parsed, ok := parseSegmentRule(r)
		if !ok {
			return bad
		}
		seg.rules = append(seg.rules, parsed)
	}
	return seg
}

// segmentProblem is the first reference to a segment that is missing or malformed, in rule, clause, value order.
func segmentProblem(f *flag, segments map[string]*segmentEntry) string {
	for _, r := range f.rules {
		for _, c := range r.clauses {
			for _, key := range c.segmentKeys {
				seg, found := segments[key]
				if !found {
					return "SEGMENT_NOT_FOUND"
				}
				if seg.malformed {
					return "MALFORMED_SEGMENT"
				}
			}
		}
	}
	return ""
}

func compileFlag(v jsonvalue.Value, segments map[string]*segmentEntry) *flagEntry {
	entry, ok := flagHeader(v)
	if !ok {
		return &flagEntry{status: statusMalformed}
	}
	body, ok := parseFlagBody(v, entry)
	if !ok {
		entry.status, entry.code = statusBroken, "MALFORMED_FLAG"
		return entry
	}
	if code := segmentProblem(body, segments); code != "" {
		entry.status, entry.code = statusBroken, code
		return entry
	}
	entry.status, entry.flag = statusReady, body
	return entry
}

// indexByKey compiles each record of an array under its string key; the first of a repeated key wins.
func indexByKey[T any](items jsonvalue.Value, compile func(jsonvalue.Value) T) map[string]T {
	index := map[string]T{}
	list, _ := items.Items()
	for _, item := range list {
		key, ok := item.Field("key").Text()
		if !ok || !item.IsObject() {
			continue
		}
		if _, held := index[key]; !held {
			index[key] = compile(item)
		}
	}
	return index
}

// Compile validates and indexes a ruleset ({"flags": [...], "segments": [...]}, as JSON hands it over) once,
// so each Evaluate skips that work. It never fails: a bad flag becomes an ERROR result when it is evaluated,
// and anything that is not an array of flags or of segments counts as empty.
func Compile(input jsonvalue.Value) *Ruleset {
	segments := indexByKey(input.Field("segments"), compileSegment)
	flags := indexByKey(input.Field("flags"), func(v jsonvalue.Value) *flagEntry { return compileFlag(v, segments) })
	return &Ruleset{flags: flags, segments: segments}
}

// FlagVersion is the version of a flag that is fully valid; it is false for a missing or malformed flag.
func (r *Ruleset) FlagVersion(key string) (float64, bool) {
	entry, found := r.flags[key]
	if !found || entry.status != statusReady {
		return 0, false
	}
	return entry.flag.version, true
}
