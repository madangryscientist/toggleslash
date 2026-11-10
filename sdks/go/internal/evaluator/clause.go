package evaluator

import (
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// readAttribute is the value a name or a "/" reference reaches, and false when it is missing.
func readAttribute(c *kindContext, components []string) (jsonv.Value, bool) {
	first := components[0]
	var current jsonv.Value
	switch {
	case first == "key":
		current = jsonv.String(c.key)
	case c.attrs.IsObject():
		v, ok := c.attrs.Get(first)
		if !ok {
			return jsonv.Value{}, false
		}
		current = v
	default:
		return jsonv.Value{}, false
	}
	for _, component := range components[1:] {
		if !current.IsObject() {
			return jsonv.Value{}, false
		}
		v, ok := current.Get(component)
		if !ok {
			return jsonv.Value{}, false
		}
		current = v
	}
	return current, true
}

func signFloat(a, b float64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// compareValues is -1, 0 or 1 for two finite numbers or two strings (by UTF-16 code unit), and false
// for anything else.
func compareValues(candidate jsonv.Value, v *clauseValue) (int, bool) {
	if n, ok := candidate.AsNumber(); ok && v.isNumber {
		if n != n || n > 1.7976931348623157e308 || n < -1.7976931348623157e308 {
			return 0, false
		}
		return signFloat(n, v.numValue), true
	}
	if s, ok := candidate.AsString(); ok && v.isString {
		return wtf8.Compare(s, v.strValue), true
	}
	return 0, false
}

func orderHolds(o op, order int) bool {
	switch o {
	case opLt:
		return order < 0
	case opLte:
		return order <= 0
	case opGt:
		return order > 0
	}
	return order >= 0
}

func semverHolds(o op, candidate jsonv.Value, v *clauseValue) bool {
	text, ok := candidate.AsString()
	if !ok || !v.isString || !v.semverOK {
		return false
	}
	parsed, ok := parseSemanticVersion(text)
	if !ok {
		return false
	}
	order := compareSemanticVersions(parsed, v.semver)
	switch o {
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

func dateHolds(o op, candidate jsonv.Value, v *clauseValue) bool {
	if !v.instantOK {
		return false
	}
	at, ok := toInstant(candidate)
	if !ok {
		return false
	}
	if o == opBefore {
		return at < v.instant
	}
	return at > v.instant
}

func stringOpHolds(o op, candidate jsonv.Value, v *clauseValue) bool {
	text, ok := candidate.AsString()
	if !ok || !v.isString {
		return false
	}
	switch o {
	case opMatches:
		return v.pattern != nil && v.pattern.matches(text)
	case opContains:
		return wtf8.Contains(text, v.strValue)
	case opStartsWith:
		return wtf8.HasPrefix(text, v.strValue)
	}
	return wtf8.HasSuffix(text, v.strValue)
}

func valueHolds(o op, candidate jsonv.Value, v *clauseValue) bool {
	switch o {
	case opIn:
		if b, ok := candidate.AsBool(); ok {
			return v.isBool && v.boolValue == b
		}
		order, ok := compareValues(candidate, v)
		return ok && order == 0
	case opMatches, opContains, opStartsWith, opEndsWith:
		return stringOpHolds(o, candidate, v)
	case opSemverEq, opSemverLt, opSemverLte, opSemverGt, opSemverGte:
		return semverHolds(o, candidate, v)
	case opBefore, opAfter:
		return dateHolds(o, candidate, v)
	}
	order, ok := compareValues(candidate, v)
	return ok && orderHolds(o, order)
}

func anyValueHolds(c *attributeClause, candidate jsonv.Value) bool {
	for i := range c.values {
		if valueHolds(c.op, candidate, &c.values[i]) {
			return true
		}
	}
	return false
}

// attributeClauseMatches: does one attribute clause hold? It reads the context of its kind. A missing
// attribute, or a missing kind, fails it, whatever negate says.
func attributeClauseMatches(c *attributeClause, ctxs contexts) bool {
	ctx, ok := ctxs.find(c.contextKind)
	if !ok {
		return false
	}
	attribute, ok := readAttribute(ctx, c.components)
	if !ok || attribute.IsNull() {
		return false
	}
	matched := false
	if attribute.IsArray() {
		for _, item := range attribute.Items() {
			if anyValueHolds(c, item) {
				matched = true
				break
			}
		}
	} else {
		matched = anyValueHolds(c, attribute)
	}
	return matched != c.negate
}
