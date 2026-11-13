// Package eval is the evaluator of docs/evaluation.md: a ruleset, a flag key
// and a context in, a result out. It has no network code and imports only
// jsonval, so the client can hold a ruleset and evaluate synchronously.
package eval

import (
	"math"
	"strings"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonval"
)

// DefaultKind is what a kind that is read and absent means.
const DefaultKind = "user"

// The range a reported flag version must fit: a version above 2^63 - 1 reads
// as 2^63 and is reported as none (docs/sdk-contract.md section 10).
const (
	minInt64 = -9223372036854775808.0
	maxInt64 = 9223372036854775808.0
)

// BucketCount is how many buckets a rollout or a weighted segment rule aims at.
const BucketCount = 100000

// Error codes, as docs/evaluation.md "Error codes" spells them.
const (
	CodeInvalidContext            = "INVALID_CONTEXT"
	CodeFlagNotFound              = "FLAG_NOT_FOUND"
	CodeSegmentNotFound           = "SEGMENT_NOT_FOUND"
	CodeMalformedFlag             = "MALFORMED_FLAG"
	CodeMalformedSegment          = "MALFORMED_SEGMENT"
	CodePrerequisiteCycle         = "PREREQUISITE_CYCLE"
	CodePrerequisiteDepthExceeded = "PREREQUISITE_DEPTH_EXCEEDED"
)

// Ruleset is one environment's flags and segments, checked once at load.
type Ruleset struct {
	flags    map[string]*flagEntry
	segments map[string]*segmentEntry
}

type flagEntry struct {
	headerOK bool
	header   flagHeader
	// version is the flag's `version` for the afterEvaluation event, nil when
	// it is absent or not a signed 64-bit integer.
	version *int64
	// code is empty when the body is valid, and otherwise the error a valid
	// header with a bad body reports.
	code    string
	body    *flagBody
	targets map[string]map[string]targetHit
}

type flagHeader struct {
	on           bool
	variations   []jsonval.Value
	offVariation *int
}

type flagBody struct {
	key           string
	salt          string
	targets       []target
	prerequisites []prerequisite
	rules         []rule
	fallthroughSv serve
}

type serve struct {
	variation *int
	rollout   *rollout
}

type rollout struct {
	entries []weightedVariation
	kind    string
}

type weightedVariation struct {
	variation int
	weight    int
}

type rule struct {
	clauses []clause
	serve   serve
}

type target struct {
	variation   int
	contextKeys []string
	kind        string
}

type targetHit struct {
	order     int
	variation int
}

type prerequisite struct {
	flagKey   string
	variation float64
}

type clause struct {
	op          string
	attribute   string
	components  []string
	values      []jsonval.Value
	segmentKeys []string
	negate      bool
	contextKind string
}

type segmentEntry struct {
	ready    bool
	segment  *segment
	included map[string]map[string]struct{}
	excluded map[string]map[string]struct{}
}

type segment struct {
	key   string
	salt  string
	rules []segmentRule
}

type segmentRule struct {
	clauses     []clause
	weight      *int
	rolloutKind string
}

type contextList struct {
	kind   string
	values []string
}

// NewRuleset loads flags and segments, as the ruleset's JSON holds them. It
// never fails: a bad entry reports an error when it is evaluated. Absent or
// non-array arguments count as empty.
func NewRuleset(flags, segments jsonval.Value) *Ruleset {
	r := &Ruleset{
		flags:    map[string]*flagEntry{},
		segments: map[string]*segmentEntry{},
	}
	for _, item := range arrayOf(segments) {
		obj, ok := asObject(item)
		if !ok {
			continue
		}
		key, ok := asString(obj["key"])
		if !ok {
			continue
		}
		if _, taken := r.segments[key]; taken {
			continue
		}
		r.segments[key] = compileSegment(obj, key)
	}
	for _, item := range arrayOf(flags) {
		obj, ok := asObject(item)
		if !ok {
			continue
		}
		key, ok := asString(obj["key"])
		if !ok {
			continue
		}
		if _, taken := r.flags[key]; taken {
			continue
		}
		r.flags[key] = compileFlag(obj, r.segments)
	}
	return r
}

// FlagVersion answers the flag's `version` in this ruleset for the
// afterEvaluation event, or nil when the ruleset has no such flag or the flag
// is malformed at load (docs/sdk-contract.md section 10). A flag whose problem
// shows only at evaluation keeps its version.
func (r *Ruleset) FlagVersion(key string) *int64 {
	entry, ok := r.flags[key]
	if !ok || !entry.headerOK || entry.code != "" {
		return nil
	}
	return entry.version
}

// versionOf reads a flag's `version` as a signed 64-bit integer. A number with
// a fraction or outside the range is none.
func versionOf(value jsonval.Value) *int64 {
	number, ok := asNumber(value)
	if !ok || number != math.Trunc(number) || number < minInt64 || number >= maxInt64 {
		return nil
	}
	version := int64(number)
	return &version
}

func compileFlag(obj jsonval.Object, segments map[string]*segmentEntry) *flagEntry {
	header, ok := readHeader(obj)
	if !ok {
		return &flagEntry{}
	}
	entry := &flagEntry{headerOK: true, header: header, version: versionOf(obj["version"])}
	body, ok := readBody(obj, len(header.variations))
	if !ok {
		entry.code = CodeMalformedFlag
		return entry
	}
	if code := segmentProblem(body, segments); code != "" {
		entry.code = code
		return entry
	}
	entry.body = body
	entry.targets = targetIndex(body)
	return entry
}

func readHeader(obj jsonval.Object) (flagHeader, bool) {
	on, ok := asBool(obj["on"])
	if !ok {
		return flagHeader{}, false
	}
	variations, ok := asArray(obj["variations"])
	if !ok || len(variations) == 0 {
		return flagHeader{}, false
	}
	if _, ok := keyOf(obj["key"]); !ok {
		return flagHeader{}, false
	}
	h := flagHeader{on: on, variations: variations}
	if v := obj["offVariation"]; !absent(v) {
		index, ok := variationIndex(v, len(variations))
		if !ok {
			return flagHeader{}, false
		}
		h.offVariation = &index
	}
	return h, true
}

func readBody(obj jsonval.Object, variationCount int) (*flagBody, bool) {
	key, ok := keyOf(obj["key"])
	if !ok {
		return nil, false
	}
	salt, ok := asString(obj["salt"])
	if !ok {
		return nil, false
	}
	if _, ok := countValue(obj["version"]); !ok {
		return nil, false
	}
	targets, ok := readTargets(obj["targets"], variationCount)
	if !ok {
		return nil, false
	}
	prerequisites, ok := readPrerequisites(obj["prerequisites"])
	if !ok {
		return nil, false
	}
	rules, ok := readRules(obj["rules"], variationCount)
	if !ok {
		return nil, false
	}
	fallthroughSv, ok := readServe(obj["fallthrough"], variationCount)
	if !ok {
		return nil, false
	}
	return &flagBody{
		key:           key,
		salt:          salt,
		targets:       targets,
		prerequisites: prerequisites,
		rules:         rules,
		fallthroughSv: fallthroughSv,
	}, true
}

func readTargets(value jsonval.Value, variationCount int) ([]target, bool) {
	items, ok := asArray(value)
	if !ok {
		return nil, false
	}
	out := make([]target, 0, len(items))
	for _, item := range items {
		obj, ok := asObject(item)
		if !ok {
			return nil, false
		}
		variation, ok := variationIndex(obj["variation"], variationCount)
		if !ok {
			return nil, false
		}
		keys, ok := stringArray(obj["contextKeys"])
		if !ok {
			return nil, false
		}
		kind, ok := optionalKind(obj, "contextKind")
		if !ok {
			return nil, false
		}
		out = append(out, target{variation: variation, contextKeys: keys, kind: kind})
	}
	return out, true
}

func readPrerequisites(value jsonval.Value) ([]prerequisite, bool) {
	items, ok := asArray(value)
	if !ok {
		return nil, false
	}
	out := make([]prerequisite, 0, len(items))
	for _, item := range items {
		obj, ok := asObject(item)
		if !ok {
			return nil, false
		}
		flagKey, ok := keyOf(obj["flagKey"])
		if !ok {
			return nil, false
		}
		variation, ok := countValue(obj["variation"])
		if !ok {
			return nil, false
		}
		out = append(out, prerequisite{flagKey: flagKey, variation: variation})
	}
	return out, true
}

func readRules(value jsonval.Value, variationCount int) ([]rule, bool) {
	items, ok := asArray(value)
	if !ok {
		return nil, false
	}
	out := make([]rule, 0, len(items))
	for _, item := range items {
		obj, ok := asObject(item)
		if !ok {
			return nil, false
		}
		clauseItems, ok := asArray(obj["clauses"])
		if !ok {
			return nil, false
		}
		clauses := make([]clause, 0, len(clauseItems))
		for _, clauseItem := range clauseItems {
			parsed, isAttribute := readAttributeClause(clauseItem)
			if !isAttribute {
				var isSegment bool
				parsed, isSegment = readSegmentClause(clauseItem)
				if !isSegment {
					return nil, false
				}
			}
			clauses = append(clauses, parsed)
		}
		sv, ok := readServe(obj, variationCount)
		if !ok {
			return nil, false
		}
		out = append(out, rule{clauses: clauses, serve: sv})
	}
	return out, true
}

func readServe(value jsonval.Value, variationCount int) (serve, bool) {
	obj, ok := asObject(value)
	if !ok {
		return serve{}, false
	}
	sv := serve{}
	if v := obj["rollout"]; absent(v) {
		index, ok := variationIndex(obj["variation"], variationCount)
		if !ok {
			return serve{}, false
		}
		sv.variation = &index
		return sv, true
	}
	if !absent(obj["variation"]) {
		return serve{}, false
	}
	parsed, ok := readRollout(obj["rollout"], variationCount)
	if !ok {
		return serve{}, false
	}
	sv.rollout = parsed
	return sv, true
}

func readRollout(value jsonval.Value, variationCount int) (*rollout, bool) {
	obj, ok := asObject(value)
	if !ok {
		return nil, false
	}
	kind, ok := optionalKind(obj, "contextKind")
	if !ok {
		return nil, false
	}
	items, ok := asArray(obj["variations"])
	if !ok || len(items) == 0 {
		return nil, false
	}
	entries := make([]weightedVariation, 0, len(items))
	total := 0
	for _, item := range items {
		entry, ok := asObject(item)
		if !ok {
			return nil, false
		}
		variation, ok := variationIndex(entry["variation"], variationCount)
		if !ok {
			return nil, false
		}
		weight, ok := countInt(entry["weight"], BucketCount)
		if !ok {
			return nil, false
		}
		total += weight
		entries = append(entries, weightedVariation{variation: variation, weight: weight})
	}
	if total != BucketCount {
		return nil, false
	}
	return &rollout{entries: entries, kind: kind}, true
}

func readAttributeClause(value jsonval.Value) (clause, bool) {
	obj, ok := asObject(value)
	if !ok {
		return clause{}, false
	}
	op, ok := asString(obj["op"])
	if !ok || !attributeOps[op] {
		return clause{}, false
	}
	attribute, ok := keyOf(obj["attribute"])
	if !ok {
		return clause{}, false
	}
	negate, ok := readNegate(obj["negate"])
	if !ok {
		return clause{}, false
	}
	kind, ok := optionalKind(obj, "contextKind")
	if !ok {
		return clause{}, false
	}
	values, ok := readClauseValues(obj["values"])
	if !ok {
		return clause{}, false
	}
	c := clause{op: op, attribute: attribute, values: values, negate: negate, contextKind: kind}
	if strings.HasPrefix(attribute, "/") {
		components, ok := parseReference(attribute)
		if !ok {
			return clause{}, false
		}
		c.components = components
	}
	return c, true
}

func readSegmentClause(value jsonval.Value) (clause, bool) {
	obj, ok := asObject(value)
	if !ok {
		return clause{}, false
	}
	op, ok := asString(obj["op"])
	if !ok || op != "segmentMatch" {
		return clause{}, false
	}
	negate, ok := readNegate(obj["negate"])
	if !ok {
		return clause{}, false
	}
	keys, ok := stringArray(obj["values"])
	if !ok {
		return clause{}, false
	}
	return clause{op: op, segmentKeys: keys, negate: negate}, true
}

func readClauseValues(value jsonval.Value) ([]jsonval.Value, bool) {
	items, ok := asArray(value)
	if !ok {
		return nil, false
	}
	out := make([]jsonval.Value, 0, len(items))
	for _, item := range items {
		if !isClauseValue(item) {
			return nil, false
		}
		out = append(out, item)
	}
	return out, true
}

func isClauseValue(v jsonval.Value) bool {
	switch typed := v.(type) {
	case jsonval.String, jsonval.Bool:
		return true
	case jsonval.Number:
		n := float64(typed)
		return !math.IsInf(n, 0) && !math.IsNaN(n)
	default:
		return false
	}
}

func readNegate(value jsonval.Value) (bool, bool) {
	if absent(value) {
		return false, true
	}
	return asBool(value)
}

func segmentProblem(body *flagBody, segments map[string]*segmentEntry) string {
	for _, r := range body.rules {
		for _, c := range r.clauses {
			for _, key := range c.segmentKeys {
				entry, ok := segments[key]
				if !ok {
					return CodeSegmentNotFound
				}
				if !entry.ready {
					return CodeMalformedSegment
				}
			}
		}
	}
	return ""
}

func targetIndex(body *flagBody) map[string]map[string]targetHit {
	index := map[string]map[string]targetHit{}
	for order, t := range body.targets {
		keys, ok := index[t.kind]
		if !ok {
			keys = map[string]targetHit{}
			index[t.kind] = keys
		}
		for _, contextKey := range t.contextKeys {
			if _, taken := keys[contextKey]; !taken {
				keys[contextKey] = targetHit{order: order, variation: t.variation}
			}
		}
	}
	return index
}

func compileSegment(obj jsonval.Object, key string) *segmentEntry {
	salt, ok := asString(obj["salt"])
	if !ok {
		return &segmentEntry{}
	}
	included, ok := stringArray(obj["included"])
	if !ok {
		return &segmentEntry{}
	}
	excluded, ok := stringArray(obj["excluded"])
	if !ok {
		return &segmentEntry{}
	}
	includedLists, ok := readContextLists(obj["includedContexts"])
	if !ok {
		return &segmentEntry{}
	}
	excludedLists, ok := readContextLists(obj["excludedContexts"])
	if !ok {
		return &segmentEntry{}
	}
	rules, ok := readSegmentRules(obj["rules"])
	if !ok {
		return &segmentEntry{}
	}
	if _, ok := keyOf(obj["key"]); !ok {
		return &segmentEntry{}
	}
	return &segmentEntry{
		ready:    true,
		segment:  &segment{key: key, salt: salt, rules: rules},
		included: keysByKind(included, includedLists),
		excluded: keysByKind(excluded, excludedLists),
	}
}

func readContextLists(value jsonval.Value) ([]contextList, bool) {
	if absent(value) {
		return nil, true
	}
	items, ok := asArray(value)
	if !ok {
		return nil, false
	}
	out := make([]contextList, 0, len(items))
	for _, item := range items {
		obj, ok := asObject(item)
		if !ok {
			return nil, false
		}
		kind, ok := asString(obj["contextKind"])
		if !ok || !isKind(kind) {
			return nil, false
		}
		values, ok := stringArray(obj["values"])
		if !ok {
			return nil, false
		}
		out = append(out, contextList{kind: kind, values: values})
	}
	return out, true
}

func readSegmentRules(value jsonval.Value) ([]segmentRule, bool) {
	items, ok := asArray(value)
	if !ok {
		return nil, false
	}
	out := make([]segmentRule, 0, len(items))
	for _, item := range items {
		obj, ok := asObject(item)
		if !ok {
			return nil, false
		}
		kind, ok := optionalKind(obj, "rolloutContextKind")
		if !ok {
			return nil, false
		}
		sr := segmentRule{rolloutKind: kind}
		if v := obj["weight"]; !absent(v) {
			weight, ok := countInt(v, BucketCount)
			if !ok {
				return nil, false
			}
			sr.weight = &weight
		}
		clauseItems, ok := asArray(obj["clauses"])
		if !ok {
			return nil, false
		}
		clauses := make([]clause, 0, len(clauseItems))
		for _, clauseItem := range clauseItems {
			parsed, ok := readAttributeClause(clauseItem)
			if !ok {
				return nil, false
			}
			clauses = append(clauses, parsed)
		}
		sr.clauses = clauses
		out = append(out, sr)
	}
	return out, true
}

func keysByKind(userKeys []string, lists []contextList) map[string]map[string]struct{} {
	index := map[string]map[string]struct{}{DefaultKind: {}}
	for _, key := range userKeys {
		index[DefaultKind][key] = struct{}{}
	}
	for _, list := range lists {
		keys, ok := index[list.kind]
		if !ok {
			keys = map[string]struct{}{}
			index[list.kind] = keys
		}
		for _, value := range list.values {
			keys[value] = struct{}{}
		}
	}
	return index
}

var attributeOps = map[string]bool{
	"in":         true,
	"contains":   true,
	"startsWith": true,
	"endsWith":   true,
	"lt":         true,
	"lte":        true,
	"gt":         true,
	"gte":        true,
	"semverEq":   true,
	"semverLt":   true,
	"semverLte":  true,
	"semverGt":   true,
	"semverGte":  true,
	"before":     true,
	"after":      true,
	"matches":    true,
}

func arrayOf(v jsonval.Value) jsonval.Array {
	items, _ := asArray(v)
	return items
}

func asObject(v jsonval.Value) (jsonval.Object, bool) {
	obj, ok := v.(jsonval.Object)
	return obj, ok
}

func asArray(v jsonval.Value) (jsonval.Array, bool) {
	items, ok := v.(jsonval.Array)
	return items, ok
}

func asString(v jsonval.Value) (string, bool) {
	s, ok := v.(jsonval.String)
	return string(s), ok
}

func asNumber(v jsonval.Value) (float64, bool) {
	n, ok := v.(jsonval.Number)
	return float64(n), ok
}

func asBool(v jsonval.Value) (bool, bool) {
	b, ok := v.(jsonval.Bool)
	return bool(b), ok
}

// absent reports a missing field: null and absent are the same everywhere.
func absent(v jsonval.Value) bool {
	if v == nil {
		return true
	}
	_, ok := v.(jsonval.Null)
	return ok
}

// keyOf reads a key: a non-empty string.
func keyOf(v jsonval.Value) (string, bool) {
	s, ok := asString(v)
	if !ok || s == "" {
		return "", false
	}
	return s, true
}

// countValue reads a count: an integer that is 0 or more.
func countValue(v jsonval.Value) (float64, bool) {
	n, ok := asNumber(v)
	if !ok || math.IsInf(n, 0) || math.IsNaN(n) || n < 0 || n != math.Trunc(n) {
		return 0, false
	}
	return n, true
}

// countInt reads a count that is no larger than limit, as an int.
func countInt(v jsonval.Value, limit int) (int, bool) {
	n, ok := countValue(v)
	if !ok || n > float64(limit) {
		return 0, false
	}
	return int(n), true
}

// variationIndex reads an index into variationCount variations.
func variationIndex(v jsonval.Value, variationCount int) (int, bool) {
	n, ok := countValue(v)
	if !ok || n >= float64(variationCount) {
		return 0, false
	}
	return int(n), true
}

func stringArray(v jsonval.Value) ([]string, bool) {
	items, ok := asArray(v)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := asString(item)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// optionalKind reads a kind that may be absent, where absent is DefaultKind.
func optionalKind(obj jsonval.Object, name string) (string, bool) {
	v := obj[name]
	if absent(v) {
		return DefaultKind, true
	}
	kind, ok := asString(v)
	if !ok || !isKind(kind) {
		return "", false
	}
	return kind, true
}

// isKind reports `^[A-Za-z0-9._-]+$`, excluding the two reserved names.
func isKind(s string) bool {
	if s == "" || s == "kind" || s == "multi" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '.' || c == '_' || c == '-':
		default:
			return false
		}
	}
	return true
}

// parseReference reads a `/` reference into its decoded components. A component
// that is empty, or a `~` followed by anything but `0` or `1`, is invalid.
func parseReference(text string) ([]string, bool) {
	parts := strings.Split(text[1:], "/")
	components := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			return nil, false
		}
		var decoded strings.Builder
		decoded.Grow(len(part))
		for i := 0; i < len(part); i++ {
			if part[i] != '~' {
				decoded.WriteByte(part[i])
				continue
			}
			if i+1 >= len(part) {
				return nil, false
			}
			switch part[i+1] {
			case '0':
				decoded.WriteByte('~')
			case '1':
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
