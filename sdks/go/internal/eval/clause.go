package eval

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// readAttribute is the value a name or a "/" reference reaches in one context; false when it is missing.
// "key" as the first component reads the context key; every other step reads an object's own members.
func readAttribute(c *kindContext, components []string) (jsonvalue.Value, bool) {
	var current jsonvalue.Value
	found := false
	if components[0] == "key" {
		current, found = c.keyValue, true
	} else if c.attrs.IsObject() {
		current, found = c.attrs.Get(components[0])
	}
	for _, component := range components[1:] {
		if !found || !current.IsObject() {
			return jsonvalue.Value{}, false
		}
		current, found = current.Get(component)
	}
	return current, found
}

func signFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// compare is -1, 0 or 1 for two finite numbers or two strings (by UTF-16 code unit); otherwise false.
func compare(attribute, value jsonvalue.Value) (int, bool) {
	if a, ok := attribute.Number(); ok {
		b, isNumber := value.Number()
		if !isNumber || math.IsInf(a, 0) {
			return 0, false
		}
		return signFloat(a, b), true
	}
	if a, ok := attribute.Text(); ok {
		if b, isText := value.Text(); isText {
			return wtf8.Compare(a, b), true
		}
	}
	return 0, false
}

// matchesPattern: a valid pattern and a match somewhere in the candidate. Compiled per call.
func matchesPattern(attribute, value string) bool {
	program, ok := compilePattern(value)
	return ok && patternMatches(program, attribute)
}

func matchesString(op opcode, attribute, value string) bool {
	switch op {
	case opMatches:
		return matchesPattern(attribute, value)
	case opContains:
		return wtf8.Contains(attribute, value)
	case opStartsWith:
		return wtf8.HasPrefix(attribute, value)
	}
	return wtf8.HasSuffix(attribute, value)
}

func matchesOrder(op opcode, order int) bool {
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

func matchesSemver(op opcode, attribute, value jsonvalue.Value) bool {
	a, okA := attribute.Text()
	b, okB := value.Text()
	if !okA || !okB {
		return false
	}
	candidate, okC := parseSemanticVersion(a)
	target, okT := parseSemanticVersion(b)
	if !okC || !okT {
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

func matchesDate(op opcode, attribute, value jsonvalue.Value) bool {
	candidate, okC := toInstant(attribute)
	target, okT := toInstant(value)
	if !okC || !okT {
		return false
	}
	if op == opBefore {
		return candidate < target
	}
	return candidate > target
}

func matchesValue(op opcode, attribute, value jsonvalue.Value) bool {
	switch op {
	case opIn:
		if a, ok := attribute.Bool(); ok {
			b, isBool := value.Bool()
			return isBool && a == b
		}
		order, ok := compare(attribute, value)
		return ok && order == 0
	case opContains, opStartsWith, opEndsWith, opMatches:
		a, okA := attribute.Text()
		b, okB := value.Text()
		return okA && okB && matchesString(op, a, b)
	case opSemverEq, opSemverLt, opSemverLte, opSemverGt, opSemverGte:
		return matchesSemver(op, attribute, value)
	case opBefore, opAfter:
		return matchesDate(op, attribute, value)
	}
	order, ok := compare(attribute, value)
	return ok && matchesOrder(op, order)
}

// attributeClauseMatches: does one attribute clause hold? It reads the context of its kind. A missing
// attribute, or a missing kind, fails it, whatever negate says.
func attributeClauseMatches(c *clause, ctxs contexts) bool {
	context := ctxs.get(c.kind)
	if context == nil {
		return false
	}
	attribute, found := readAttribute(context, c.components)
	if !found || attribute.IsNull() {
		return false
	}
	candidates, isArray := attribute.Items()
	if !isArray {
		candidates = []jsonvalue.Value{attribute}
	}
	matched := false
	for _, candidate := range candidates {
		for _, value := range c.values {
			if matchesValue(c.op, candidate, value) {
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
