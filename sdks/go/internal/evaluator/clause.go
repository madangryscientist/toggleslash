package evaluator

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wjson"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// readAttribute is the value a name or a "/" reference reaches in one kind's
// context. The first component "key" reads the key; any other is an own
// attribute. Each later component needs the value so far to be an object.
func readAttribute(ctx *kindContext, components []string) (wjson.Value, bool) {
	first := components[0]
	var current wjson.Value
	switch {
	case first == "key":
		current = wjson.String(ctx.key)
	case ctx.attributes != nil:
		v, ok := ctx.attributes.Get(first)
		if !ok {
			return wjson.Value{}, false
		}
		current = v
	default:
		return wjson.Value{}, false
	}
	for _, component := range components[1:] {
		o := current.Object()
		if o == nil {
			return wjson.Value{}, false
		}
		v, ok := o.Get(component)
		if !ok {
			return wjson.Value{}, false
		}
		current = v
	}
	return current, true
}

func sign(n int) int {
	if n < 0 {
		return -1
	}
	if n > 0 {
		return 1
	}
	return 0
}

// compare is -1, 0 or 1 for two finite numbers or two strings (by UTF-16 code
// unit), and false for anything else.
func compare(attribute, value wjson.Value) (int, bool) {
	switch {
	case attribute.Kind() == wjson.KindNumber && value.Kind() == wjson.KindNumber:
		a, b := attribute.Number(), value.Number()
		if math.IsInf(a, 0) || math.IsNaN(a) {
			return 0, false
		}
		switch {
		case a < b:
			return -1, true
		case a > b:
			return 1, true
		}
		return 0, true
	case attribute.Kind() == wjson.KindString && value.Kind() == wjson.KindString:
		return wtf8.CompareUTF16(attribute.String(), value.String()), true
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

func matchesSemver(o op, attribute, value wjson.Value) bool {
	if attribute.Kind() != wjson.KindString || value.Kind() != wjson.KindString {
		return false
	}
	candidate, ok := parseSemanticVersion(attribute.String())
	if !ok {
		return false
	}
	target, ok := parseSemanticVersion(value.String())
	if !ok {
		return false
	}
	order := compareSemanticVersions(candidate, target)
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

func matchesDate(o op, attribute, value wjson.Value) bool {
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

func matchesString(c *attributeClause, index int, attribute, value string) bool {
	switch c.op {
	case opMatches:
		p := c.patterns[index]
		return p != nil && p.matches(attribute)
	case opContains:
		return wtf8.Contains(attribute, value)
	case opStartsWith:
		return wtf8.HasPrefix(attribute, value)
	}
	return wtf8.HasSuffix(attribute, value)
}

func matchesValue(c *attributeClause, index int, attribute wjson.Value) bool {
	value := c.values[index]
	switch c.op {
	case opIn:
		if attribute.Kind() == wjson.KindBool {
			return value.Kind() == wjson.KindBool && attribute.Bool() == value.Bool()
		}
		order, ok := compare(attribute, value)
		return ok && order == 0
	case opContains, opStartsWith, opEndsWith, opMatches:
		return attribute.Kind() == wjson.KindString && value.Kind() == wjson.KindString &&
			matchesString(c, index, attribute.String(), value.String())
	case opSemverEq, opSemverLt, opSemverLte, opSemverGt, opSemverGte:
		return matchesSemver(c.op, attribute, value)
	case opBefore, opAfter:
		return matchesDate(c.op, attribute, value)
	}
	order, ok := compare(attribute, value)
	return ok && matchesOrder(c.op, order)
}

func (c *attributeClause) matchesCandidate(candidate wjson.Value) bool {
	for i := range c.values {
		if matchesValue(c, i, candidate) {
			return true
		}
	}
	return false
}

// matches reports whether the clause holds. It reads the context of its
// contextKind. A missing attribute, or a missing kind, fails it, whatever
// negate says.
func (c *attributeClause) matches(cs contexts) bool {
	kind := c.contextKind
	if kind == "" {
		kind = defaultKind
	}
	ctx, ok := cs.get(kind)
	if !ok {
		return false
	}
	attribute, ok := readAttribute(ctx, c.components)
	if !ok || attribute.IsNull() {
		return false
	}
	matched := false
	if attribute.Kind() == wjson.KindArray {
		for _, candidate := range attribute.Array() {
			if c.matchesCandidate(candidate) {
				matched = true
				break
			}
		}
	} else {
		matched = c.matchesCandidate(attribute)
	}
	return matched != c.negate
}
