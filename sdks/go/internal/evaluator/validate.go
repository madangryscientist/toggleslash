package evaluator

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wjson"
)

// The checks below follow docs/evaluation.md ("what counts as malformed"):
// null and absent are the same for an optional field. Each check also builds
// the typed value the evaluator reads, so a ruleset is walked once.

// absent reports whether o has no name, or holds null there.
func absent(o *wjson.Object, name string) bool {
	v, _ := o.Get(name)
	return v.IsNull()
}

func field(o *wjson.Object, name string) wjson.Value {
	v, _ := o.Get(name)
	return v
}

// isKind is ^[A-Za-z0-9._-]+$ and neither "kind" nor "multi".
func isKind(s string) bool {
	if s == "" || s == "kind" || s == "multi" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}

// optionalKind reads a context kind that may be absent; absent is "".
func optionalKind(o *wjson.Object, name string) (string, bool) {
	v := field(o, name)
	if v.IsNull() {
		return "", true
	}
	if v.Kind() != wjson.KindString || !isKind(v.String()) {
		return "", false
	}
	return v.String(), true
}

func key(v wjson.Value) (string, bool) {
	if v.Kind() == wjson.KindString && v.String() != "" {
		return v.String(), true
	}
	return "", false
}

// count reads an integer that is zero or more. A finite double with no
// fraction counts, however large.
func count(v wjson.Value) (float64, bool) {
	if v.Kind() != wjson.KindNumber {
		return 0, false
	}
	n := v.Number()
	if math.IsInf(n, 0) || math.IsNaN(n) || n != math.Trunc(n) || n < 0 {
		return 0, false
	}
	return n, true
}

func variationIndex(v wjson.Value, variations int) (int, bool) {
	n, ok := count(v)
	if !ok || n >= float64(variations) {
		return 0, false
	}
	return int(n), true
}

func stringArray(v wjson.Value) ([]string, bool) {
	if v.Kind() != wjson.KindArray {
		return nil, false
	}
	items := v.Array()
	out := make([]string, len(items))
	for i, item := range items {
		if item.Kind() != wjson.KindString {
			return nil, false
		}
		out[i] = item.String()
	}
	return out, true
}

func clauseValue(v wjson.Value) bool {
	switch v.Kind() {
	case wjson.KindNumber:
		return v.IsFinite()
	case wjson.KindString, wjson.KindBool:
		return true
	}
	return false
}

func negate(o *wjson.Object) (bool, bool) {
	v := field(o, "negate")
	if v.IsNull() {
		return false, true
	}
	if v.Kind() != wjson.KindBool {
		return false, false
	}
	return v.Bool(), true
}

// op is a clause operator.
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

var attributeOps = map[string]op{
	"in": opIn, "contains": opContains, "startsWith": opStartsWith, "endsWith": opEndsWith,
	"lt": opLt, "lte": opLte, "gt": opGt, "gte": opGte,
	"semverEq": opSemverEq, "semverLt": opSemverLt, "semverLte": opSemverLte,
	"semverGt": opSemverGt, "semverGte": opSemverGte,
	"before": opBefore, "after": opAfter, "matches": opMatches,
}

// attributeClause is "attribute op values", on the context of one kind.
type attributeClause struct {
	op          op
	components  []string // the name, or the parsed reference
	values      []wjson.Value
	patterns    []*pattern // for matches: one per value, nil when invalid
	negate      bool
	contextKind string
}

// clause is an attribute clause or a segmentMatch clause.
type clause struct {
	attribute *attributeClause
	segments  []string
	negate    bool
}

func compileAttributeClause(v wjson.Value) (*attributeClause, bool) {
	o := v.Object()
	if o == nil {
		return nil, false
	}
	opName := field(o, "op")
	if opName.Kind() != wjson.KindString {
		return nil, false
	}
	operator, ok := attributeOps[opName.String()]
	if !ok {
		return nil, false
	}
	name, ok := key(field(o, "attribute"))
	if !ok {
		return nil, false
	}
	components := []string{name}
	if name[0] == '/' {
		if components, ok = parseReference(name); !ok {
			return nil, false
		}
	}
	neg, ok := negate(o)
	if !ok {
		return nil, false
	}
	kind, ok := optionalKind(o, "contextKind")
	if !ok {
		return nil, false
	}
	raw := field(o, "values")
	if raw.Kind() != wjson.KindArray {
		return nil, false
	}
	values := raw.Array()
	for _, value := range values {
		if !clauseValue(value) {
			return nil, false
		}
	}
	c := &attributeClause{op: operator, components: components, values: values, negate: neg, contextKind: kind}
	if operator == opMatches {
		c.patterns = make([]*pattern, len(values))
		for i, value := range values {
			if value.Kind() == wjson.KindString {
				c.patterns[i] = compilePattern(value.String())
			}
		}
	}
	return c, true
}

func compileSegmentClause(v wjson.Value) (*clause, bool) {
	o := v.Object()
	if o == nil {
		return nil, false
	}
	if opName := field(o, "op"); opName.Kind() != wjson.KindString || opName.String() != "segmentMatch" {
		return nil, false
	}
	neg, ok := negate(o)
	if !ok {
		return nil, false
	}
	keys, ok := stringArray(field(o, "values"))
	if !ok {
		return nil, false
	}
	return &clause{segments: keys, negate: neg}, true
}

// compileClause reads an attribute clause or a segment clause.
func compileClause(v wjson.Value) (clause, bool) {
	if c, ok := compileAttributeClause(v); ok {
		return clause{attribute: c}, true
	}
	if c, ok := compileSegmentClause(v); ok {
		return *c, true
	}
	return clause{}, false
}

type weighted struct {
	variation int
	weight    int
}

type rollout struct {
	contextKind string
	variations  []weighted
}

// serve is one variation, or a rollout over several.
type serve struct {
	variation int
	rollout   *rollout
}

func compileRollout(v wjson.Value, variations int) (*rollout, bool) {
	o := v.Object()
	if o == nil {
		return nil, false
	}
	kind, ok := optionalKind(o, "contextKind")
	if !ok {
		return nil, false
	}
	raw := field(o, "variations")
	if raw.Kind() != wjson.KindArray || len(raw.Array()) == 0 {
		return nil, false
	}
	r := &rollout{contextKind: kind}
	total := 0
	for _, item := range raw.Array() {
		io := item.Object()
		if io == nil {
			return nil, false
		}
		index, ok := variationIndex(field(io, "variation"), variations)
		if !ok {
			return nil, false
		}
		weight, ok := count(field(io, "weight"))
		if !ok || weight > BucketCount {
			return nil, false
		}
		total += int(weight)
		r.variations = append(r.variations, weighted{variation: index, weight: int(weight)})
	}
	if total != BucketCount {
		return nil, false
	}
	return r, true
}

// compileServe reads the variation or rollout that sits on o itself, which is
// how a rule and a flag's fallthrough both carry one.
func compileServe(o *wjson.Object, variations int) (serve, bool) {
	rawRollout := field(o, "rollout")
	if rawRollout.IsNull() {
		index, ok := variationIndex(field(o, "variation"), variations)
		return serve{variation: index}, ok
	}
	if !absent(o, "variation") {
		return serve{}, false
	}
	r, ok := compileRollout(rawRollout, variations)
	return serve{rollout: r}, ok
}

type rule struct {
	clauses []clause
	serve   serve
}

func compileRule(v wjson.Value, variations int) (rule, bool) {
	o := v.Object()
	if o == nil {
		return rule{}, false
	}
	raw := field(o, "clauses")
	if raw.Kind() != wjson.KindArray {
		return rule{}, false
	}
	var r rule
	for _, item := range raw.Array() {
		c, ok := compileClause(item)
		if !ok {
			return rule{}, false
		}
		r.clauses = append(r.clauses, c)
	}
	var ok bool
	r.serve, ok = compileServe(o, variations)
	return r, ok
}

type target struct {
	variation   int
	contextKeys []string
	contextKind string
}

func compileTarget(v wjson.Value, variations int) (target, bool) {
	o := v.Object()
	if o == nil {
		return target{}, false
	}
	index, ok := variationIndex(field(o, "variation"), variations)
	if !ok {
		return target{}, false
	}
	keys, ok := stringArray(field(o, "contextKeys"))
	if !ok {
		return target{}, false
	}
	kind, ok := optionalKind(o, "contextKind")
	if !ok {
		return target{}, false
	}
	return target{variation: index, contextKeys: keys, contextKind: kind}, true
}

type prerequisite struct {
	flagKey   string
	variation float64
}

func compilePrerequisite(v wjson.Value) (prerequisite, bool) {
	o := v.Object()
	if o == nil {
		return prerequisite{}, false
	}
	k, ok := key(field(o, "flagKey"))
	if !ok {
		return prerequisite{}, false
	}
	n, ok := count(field(o, "variation"))
	return prerequisite{flagKey: k, variation: n}, ok
}

// flagHeader is what serving the off variation needs. It is checked apart so
// "on": false survives a bad rule.
type flagHeader struct {
	key          string
	on           bool
	variations   []wjson.Value
	offVariation int // -1 for none
}

func compileHeader(o *wjson.Object) (*flagHeader, bool) {
	on := field(o, "on")
	if on.Kind() != wjson.KindBool {
		return nil, false
	}
	k, ok := key(field(o, "key"))
	if !ok {
		return nil, false
	}
	vs := field(o, "variations")
	if vs.Kind() != wjson.KindArray || len(vs.Array()) == 0 {
		return nil, false
	}
	h := &flagHeader{key: k, on: on.Bool(), variations: vs.Array(), offVariation: -1}
	if off := field(o, "offVariation"); !off.IsNull() {
		index, ok := variationIndex(off, len(h.variations))
		if !ok {
			return nil, false
		}
		h.offVariation = index
	}
	return h, true
}

type flagBody struct {
	salt          string
	version       float64
	targets       []target
	prerequisites []prerequisite
	rules         []rule
	fallThrough   serve
}

func compileBody(o *wjson.Object, variations int) (*flagBody, bool) {
	salt := field(o, "salt")
	if salt.Kind() != wjson.KindString {
		return nil, false
	}
	version, ok := count(field(o, "version"))
	if !ok {
		return nil, false
	}
	b := &flagBody{salt: salt.String(), version: version}
	targets := field(o, "targets")
	prerequisites := field(o, "prerequisites")
	rules := field(o, "rules")
	if targets.Kind() != wjson.KindArray || prerequisites.Kind() != wjson.KindArray || rules.Kind() != wjson.KindArray {
		return nil, false
	}
	for _, item := range targets.Array() {
		t, ok := compileTarget(item, variations)
		if !ok {
			return nil, false
		}
		b.targets = append(b.targets, t)
	}
	for _, item := range prerequisites.Array() {
		p, ok := compilePrerequisite(item)
		if !ok {
			return nil, false
		}
		b.prerequisites = append(b.prerequisites, p)
	}
	for _, item := range rules.Array() {
		r, ok := compileRule(item, variations)
		if !ok {
			return nil, false
		}
		b.rules = append(b.rules, r)
	}
	ft := field(o, "fallthrough").Object()
	if ft == nil {
		return nil, false
	}
	if b.fallThrough, ok = compileServe(ft, variations); !ok {
		return nil, false
	}
	return b, true
}

type segmentRule struct {
	clauses     []*attributeClause
	weight      int // -1 for none
	rolloutKind string
}

// keysByKind maps a context kind to the keys listed for it.
type keysByKind map[string]map[string]struct{}

func (k keysByKind) add(kind string, keys []string) {
	set := k[kind]
	if set == nil {
		set = make(map[string]struct{}, len(keys))
		k[kind] = set
	}
	for _, key := range keys {
		set[key] = struct{}{}
	}
}

type segment struct {
	key      string
	salt     string
	included keysByKind
	excluded keysByKind
	rules    []segmentRule
}

func compileSegmentRule(v wjson.Value) (segmentRule, bool) {
	o := v.Object()
	if o == nil {
		return segmentRule{}, false
	}
	r := segmentRule{weight: -1}
	if w := field(o, "weight"); !w.IsNull() {
		n, ok := count(w)
		if !ok || n > BucketCount {
			return segmentRule{}, false
		}
		r.weight = int(n)
	}
	kind, ok := optionalKind(o, "rolloutContextKind")
	if !ok {
		return segmentRule{}, false
	}
	r.rolloutKind = kind
	raw := field(o, "clauses")
	if raw.Kind() != wjson.KindArray {
		return segmentRule{}, false
	}
	for _, item := range raw.Array() {
		c, ok := compileAttributeClause(item)
		if !ok {
			return segmentRule{}, false
		}
		r.clauses = append(r.clauses, c)
	}
	return r, true
}

// addContextLists reads includedContexts or excludedContexts, which may be
// absent, into keys.
func addContextLists(v wjson.Value, into keysByKind) bool {
	if v.IsNull() {
		return true
	}
	if v.Kind() != wjson.KindArray {
		return false
	}
	type list struct {
		kind   string
		values []string
	}
	var lists []list
	for _, item := range v.Array() {
		o := item.Object()
		if o == nil {
			return false
		}
		kind := field(o, "contextKind")
		if kind.Kind() != wjson.KindString || !isKind(kind.String()) {
			return false
		}
		values, ok := stringArray(field(o, "values"))
		if !ok {
			return false
		}
		lists = append(lists, list{kind.String(), values})
	}
	for _, l := range lists {
		into.add(l.kind, l.values)
	}
	return true
}

func compileSegment(v wjson.Value) (*segment, bool) {
	o := v.Object()
	if o == nil {
		return nil, false
	}
	k, ok := key(field(o, "key"))
	if !ok {
		return nil, false
	}
	salt := field(o, "salt")
	if salt.Kind() != wjson.KindString {
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
	s := &segment{key: k, salt: salt.String(), included: keysByKind{}, excluded: keysByKind{}}
	s.included.add(defaultKind, included)
	s.excluded.add(defaultKind, excluded)
	if !addContextLists(field(o, "includedContexts"), s.included) ||
		!addContextLists(field(o, "excludedContexts"), s.excluded) {
		return nil, false
	}
	rules := field(o, "rules")
	if rules.Kind() != wjson.KindArray {
		return nil, false
	}
	for _, item := range rules.Array() {
		r, ok := compileSegmentRule(item)
		if !ok {
			return nil, false
		}
		s.rules = append(s.rules, r)
	}
	return s, true
}
