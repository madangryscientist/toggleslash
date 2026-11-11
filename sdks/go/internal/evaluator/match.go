package evaluator

import (
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

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
)

var attributeOps = map[string]opKind{
	"in": opIn, "contains": opContains, "startsWith": opStartsWith, "endsWith": opEndsWith,
	"lt": opLt, "lte": opLte, "gt": opGt, "gte": opGte,
	"semverEq": opSemverEq, "semverLt": opSemverLt, "semverLte": opSemverLte,
	"semverGt": opSemverGt, "semverGte": opSemverGte,
	"before": opBefore, "after": opAfter, "matches": opMatches,
}

// clauseValue is one value of a clause with what its op needs from it worked out once, at load.
type clauseValue struct {
	v          jsonvalue.Value
	prog       program // matches: nil when the pattern is not valid, so it matches nothing
	ver        *semver // semver ops: nil when the value is not a version
	instant    float64
	hasInstant bool
}

// attributeClause is a clause on an attribute of one kind.
type attributeClause struct {
	op     opKind
	path   []string // one component for a plain name, the decoded components of a "/" reference
	negate bool
	kind   string
	values []clauseValue
}

func newClauseValue(op opKind, v jsonvalue.Value) clauseValue {
	cv := clauseValue{v: v}
	switch op {
	case opMatches:
		if v.IsString() {
			cv.prog = compilePattern(v.StringValue())
		}
	case opSemverEq, opSemverLt, opSemverLte, opSemverGt, opSemverGte:
		if v.IsString() {
			cv.ver = parseSemver(v.StringValue())
		}
	case opBefore, opAfter:
		cv.instant, cv.hasInstant = toInstant(v)
	}
	return cv
}

// toInstant reads a finite number as Unix milliseconds and a string as an instant.
func toInstant(v jsonvalue.Value) (float64, bool) {
	switch {
	case v.IsNumber():
		return v.NumberValue(), v.IsFiniteNumber()
	case v.IsString():
		return parseInstant(v.StringValue())
	}
	return 0, false
}

func ownValue(v jsonvalue.Value, name string) (jsonvalue.Value, bool) {
	if !v.IsObject() {
		return jsonvalue.Value{}, false
	}
	return v.Object().Get(name)
}

// readAttribute is the value a name or a reference reaches in the context of one kind, if there is one.
func readAttribute(kc *kindCtx, path []string) (jsonvalue.Value, bool) {
	var current jsonvalue.Value
	var ok bool
	if path[0] == "key" {
		current, ok = jsonvalue.String(kc.key), true
	} else if kc.attrs != nil {
		current, ok = kc.attrs.Get(path[0])
	}
	for _, component := range path[1:] {
		if !ok {
			return jsonvalue.Value{}, false
		}
		current, ok = ownValue(current, component)
	}
	return current, ok
}

func sign64(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// compare is -1, 0 or 1 for two finite numbers or two strings (by UTF-16 code unit), and false otherwise.
func compare(attribute, value jsonvalue.Value) (int, bool) {
	switch {
	case attribute.IsNumber() && value.IsNumber():
		if !attribute.IsFiniteNumber() {
			return 0, false
		}
		return sign64(attribute.NumberValue(), value.NumberValue()), true
	case attribute.IsString() && value.IsString():
		return jsonvalue.CompareUTF16(attribute.StringValue(), value.StringValue()), true
	}
	return 0, false
}

func matchesIn(attribute jsonvalue.Value, cv *clauseValue) bool {
	if attribute.IsBool() {
		return cv.v.IsBool() && attribute.BoolValue() == cv.v.BoolValue()
	}
	if attribute.IsString() {
		return cv.v.IsString() && attribute.StringValue() == cv.v.StringValue()
	}
	if attribute.IsNumber() && cv.v.IsNumber() && attribute.IsFiniteNumber() {
		return attribute.NumberValue() == cv.v.NumberValue()
	}
	return false
}

func matchesString(op opKind, attribute string, cv *clauseValue) bool {
	if !cv.v.IsString() {
		return false
	}
	value := cv.v.StringValue()
	switch op {
	case opMatches:
		return cv.prog != nil && cv.prog.matches(attribute)
	case opContains:
		return jsonvalue.ContainsUTF16(attribute, value)
	case opStartsWith:
		return jsonvalue.HasPrefixUTF16(attribute, value)
	}
	return jsonvalue.HasSuffixUTF16(attribute, value)
}

func matchesOrder(op opKind, order int) bool {
	switch op {
	case opLt:
		return order < 0
	case opLte:
		return order <= 0
	case opGt:
		return order > 0
	}
	return order >= 0
}

func matchesSemver(op opKind, attribute jsonvalue.Value, cv *clauseValue) bool {
	if !attribute.IsString() || cv.ver == nil {
		return false
	}
	candidate := parseSemver(attribute.StringValue())
	if candidate == nil {
		return false
	}
	order := compareSemver(candidate, cv.ver)
	switch op {
	case opSemverEq:
		return order == 0
	case opSemverLt:
		return order < 0
	case opSemverLte:
		return order <= 0
	case opSemverGt:
		return order > 0
	}
	return order >= 0
}

func matchesDate(op opKind, attribute jsonvalue.Value, cv *clauseValue) bool {
	if !cv.hasInstant {
		return false
	}
	candidate, ok := toInstant(attribute)
	if !ok {
		return false
	}
	if op == opBefore {
		return candidate < cv.instant
	}
	return candidate > cv.instant
}

func (c *attributeClause) matchesValue(attribute jsonvalue.Value, cv *clauseValue) bool {
	switch c.op {
	case opIn:
		return matchesIn(attribute, cv)
	case opContains, opStartsWith, opEndsWith, opMatches:
		return attribute.IsString() && matchesString(c.op, attribute.StringValue(), cv)
	case opSemverEq, opSemverLt, opSemverLte, opSemverGt, opSemverGte:
		return matchesSemver(c.op, attribute, cv)
	case opBefore, opAfter:
		return matchesDate(c.op, attribute, cv)
	}
	order, ok := compare(attribute, cv.v)
	return ok && matchesOrder(c.op, order)
}

func (c *attributeClause) matchesCandidate(candidate jsonvalue.Value) bool {
	for i := range c.values {
		if c.matchesValue(candidate, &c.values[i]) {
			return true
		}
	}
	return false
}

// matches reports whether the clause holds. It reads the context of its kind; a missing attribute, or a
// missing kind, fails it whatever negate says.
func (c *attributeClause) matches(ctxs *contexts) bool {
	kc := ctxs.get(c.kind)
	if kc == nil {
		return false
	}
	attribute, ok := readAttribute(kc, c.path)
	if !ok || attribute.IsNull() {
		return false
	}
	matched := false
	if attribute.IsArray() {
		for _, item := range attribute.Items() {
			if c.matchesCandidate(item) {
				matched = true
				break
			}
		}
	} else {
		matched = c.matchesCandidate(attribute)
	}
	return matched != c.negate
}
