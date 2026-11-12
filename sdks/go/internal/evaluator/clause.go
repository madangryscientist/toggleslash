package evaluator

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// readAttribute is the value a name or a `/` reference reaches. The second result is false when it is missing.
func readAttribute(entry kindContext, components []string) (jsonvalue.Value, bool) {
	var current jsonvalue.Value
	found := false
	if components[0] == "key" {
		current, found = jsonvalue.String(entry.key), true
	} else if entry.attrs.IsObject() {
		current, found = entry.attrs.Get(components[0])
	}
	for _, component := range components[1:] {
		if !found || !current.IsObject() {
			return jsonvalue.Value{}, false
		}
		current, found = current.Get(component)
	}
	return current, found
}

func finite(n float64) bool { return !math.IsInf(n, 0) && !math.IsNaN(n) }

func signFloat(a, b float64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// compare is -1, 0 or 1 for two finite numbers or two strings (by UTF-16 code unit), and false otherwise.
func compare(attribute, value jsonvalue.Value) (int, bool) {
	if a, ok := attribute.AsNumber(); ok {
		v, isNumber := value.AsNumber()
		if !isNumber || !finite(a) {
			return 0, false
		}
		return signFloat(a, v), true
	}
	a, ok := attribute.AsString()
	v, isString := value.AsString()
	if !ok || !isString {
		return 0, false
	}
	return wtf8.Compare(a, v), true
}

func matchesString(op attributeOp, attribute, value string) bool {
	switch op {
	case opContains:
		return wtf8.Contains(attribute, value)
	case opStartsWith:
		return wtf8.HasPrefix(attribute, value)
	}
	return wtf8.HasSuffix(attribute, value)
}

func matchesOrder(op attributeOp, order int) bool {
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

func matchesSemver(op attributeOp, attribute, value jsonvalue.Value) bool {
	a, ok1 := attribute.AsString()
	v, ok2 := value.AsString()
	if !ok1 || !ok2 {
		return false
	}
	candidate, ok1 := parseSemanticVersion(a)
	target, ok2 := parseSemanticVersion(v)
	if !ok1 || !ok2 {
		return false
	}
	order := compareSemanticVersions(candidate, target)
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

func instantOf(v jsonvalue.Value) (float64, bool) {
	n, isNumber := v.AsNumber()
	s, isString := v.AsString()
	return toInstant(n, isNumber, s, isString)
}

func matchesDate(op attributeOp, attribute, value jsonvalue.Value) bool {
	candidate, ok1 := instantOf(attribute)
	target, ok2 := instantOf(value)
	if !ok1 || !ok2 {
		return false
	}
	if op == opBefore {
		return candidate < target
	}
	return candidate > target
}

func matchesValue(c *clause, index int, attribute jsonvalue.Value) bool {
	value := c.values[index]
	switch c.op {
	case opIn:
		if a, ok := attribute.AsBool(); ok {
			v, isBool := value.AsBool()
			return isBool && a == v
		}
		order, ok := compare(attribute, value)
		return ok && order == 0
	case opContains, opStartsWith, opEndsWith:
		a, ok1 := attribute.AsString()
		v, ok2 := value.AsString()
		return ok1 && ok2 && matchesString(c.op, a, v)
	case opMatches:
		a, ok := attribute.AsString()
		return ok && c.patterns[index] != nil && c.patterns[index].matches(a)
	case opSemverEq, opSemverLt, opSemverLte, opSemverGt, opSemverGte:
		return matchesSemver(c.op, attribute, value)
	case opBefore, opAfter:
		return matchesDate(c.op, attribute, value)
	}
	order, ok := compare(attribute, value)
	return ok && matchesOrder(c.op, order)
}

// attributeClauseMatches reports whether one attribute clause holds. It reads the context of its kind. A
// missing attribute, or a missing kind, fails it, whatever `negate` says.
func attributeClauseMatches(c *clause, ctx contexts) bool {
	entry, ok := ctx[c.kind]
	if !ok {
		return false
	}
	attribute, found := readAttribute(entry, c.components)
	if !found || attribute.IsNull() {
		return false
	}
	candidates := []jsonvalue.Value{attribute}
	if items, isArray := attribute.AsArray(); isArray {
		candidates = items
	}
	matched := false
	for _, candidate := range candidates {
		for i := range c.values {
			if matchesValue(c, i, candidate) {
				matched = true
				break
			}
		}
		if matched {
			break
		}
	}
	return matched != c.negate
}
