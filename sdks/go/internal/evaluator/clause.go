package evaluator

import (
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// readAttribute reads the value a name or a `/` reference reaches; a missing one is null.
func readAttribute(ctx kindContext, c *clause) jsonvalue.Value {
	name := c.attribute
	rest := []string(nil)
	if c.reference != nil {
		name = c.reference[0]
		rest = c.reference[1:]
	}
	var current jsonvalue.Value
	if name == "key" {
		current = jsonvalue.String(ctx.key)
	} else if ctx.attributes != nil {
		current, _ = ctx.attributes.Get(name)
	}
	for _, component := range rest {
		obj, ok := current.AsObject()
		if !ok {
			return jsonvalue.Null()
		}
		current, _ = obj.Get(component)
	}
	return current
}

func sign(order int) int {
	switch {
	case order < 0:
		return -1
	case order > 0:
		return 1
	}
	return 0
}

// compare is -1, 0 or 1 for two finite numbers or two strings (by UTF-16 code unit); ok is false otherwise.
func compare(attribute, value jsonvalue.Value) (int, bool) {
	if a, ok := attribute.AsNumber(); ok {
		v, isNumber := value.AsNumber()
		if !isNumber || !attribute.IsFiniteNumber() {
			return 0, false
		}
		switch {
		case a < v:
			return -1, true
		case a > v:
			return 1, true
		}
		return 0, true
	}
	if a, ok := attribute.AsString(); ok {
		if v, isString := value.AsString(); isString {
			return sign(wtf8.Compare(a, v)), true
		}
	}
	return 0, false
}

func matchesOrder(o op, order int) bool {
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

func matchesString(c *clause, index int, attribute, value string) bool {
	switch c.op {
	case opMatches:
		pattern := c.patterns[index]
		return pattern != nil && pattern.Matches(attribute)
	case opContains:
		return wtf8.Contains(attribute, value)
	case opStartsWith:
		return wtf8.HasPrefix(attribute, value)
	}
	return wtf8.HasSuffix(attribute, value)
}

func matchesSemver(o op, attribute, value jsonvalue.Value) bool {
	a, aok := attribute.AsString()
	v, vok := value.AsString()
	if !aok || !vok {
		return false
	}
	candidate, ok := parseSemver(a)
	if !ok {
		return false
	}
	target, ok := parseSemver(v)
	if !ok {
		return false
	}
	order := compareSemver(candidate, target)
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

func matchesDate(o op, attribute, value jsonvalue.Value) bool {
	candidate, ok := toInstant(attribute)
	if !ok {
		return false
	}
	target, ok := toInstant(value)
	if !ok {
		return false
	}
	if o == opBefore {
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
	case opContains, opStartsWith, opEndsWith, opMatches:
		a, aok := attribute.AsString()
		v, vok := value.AsString()
		return aok && vok && matchesString(c, index, a, v)
	case opSemverEq, opSemverLt, opSemverLte, opSemverGt, opSemverGte:
		return matchesSemver(c.op, attribute, value)
	case opBefore, opAfter:
		return matchesDate(c.op, attribute, value)
	}
	order, ok := compare(attribute, value)
	return ok && matchesOrder(c.op, order)
}

func candidateMatches(c *clause, candidate jsonvalue.Value) bool {
	for i := range c.values {
		if matchesValue(c, i, candidate) {
			return true
		}
	}
	return false
}

// attributeClauseMatches reports whether one attribute clause holds. It reads the context of its kind; a
// missing attribute or kind fails it, whatever negate says.
func attributeClauseMatches(c *clause, ctx contexts) bool {
	kc, ok := ctx[c.kind]
	if !ok {
		return false
	}
	attribute := readAttribute(kc, c)
	if attribute.IsNull() {
		return false
	}
	matched := false
	if items, isArray := attribute.AsArray(); isArray {
		for _, item := range items {
			if candidateMatches(c, item) {
				matched = true
				break
			}
		}
	} else {
		matched = candidateMatches(c, attribute)
	}
	return matched != c.negate
}
