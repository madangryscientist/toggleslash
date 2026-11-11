package evaluator

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

func readClauseValue(op opKind, v value) (clauseValue, bool) {
	cv := clauseValue{v: v}
	if f, ok := v.Number(); ok {
		if math.IsInf(f, 0) || math.IsNaN(f) {
			return cv, false
		}
		if op == opBefore || op == opAfter {
			cv.instant, cv.hasInstant = f, true
		}
		return cv, true
	}
	if s, ok := v.String(); ok {
		switch op {
		case opMatches:
			cv.pattern = compilePattern(s)
		case opSemverEq, opSemverLt, opSemverLte, opSemverGt, opSemverGte:
			cv.version = parseSemver(s)
		case opBefore, opAfter:
			cv.instant, cv.hasInstant = parseInstant(s)
		}
		return cv, true
	}
	_, ok := v.Bool()
	return cv, ok
}

// readAttribute is the value a name or a reference reaches; null when it is missing.
func readAttribute(kc kindContext, path []string) value {
	var current value
	if path[0] == "key" {
		current = jsonvalueString(kc.key)
	} else if kc.attrs != nil {
		current = kc.attrs[path[0]]
	}
	for _, component := range path[1:] {
		members, ok := current.Object()
		if !ok {
			return value{}
		}
		current = members[component]
	}
	return current
}

func sign(a, b float64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// compareValues is -1, 0 or 1 for two finite numbers or two strings (by UTF-16 code unit); it is
// false for anything else.
func compareValues(attribute, v value) (int, bool) {
	if a, ok := attribute.Number(); ok {
		b, ok := v.Number()
		if !ok || math.IsInf(a, 0) || math.IsNaN(a) {
			return 0, false
		}
		return sign(a, b), true
	}
	if a, ok := attribute.String(); ok {
		b, ok := v.String()
		if !ok {
			return 0, false
		}
		return wtf8.Compare(a, b), true
	}
	return 0, false
}

func orderHolds(op opKind, order int) bool {
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

func toInstant(v value) (float64, bool) {
	if f, ok := v.Number(); ok {
		return f, !math.IsInf(f, 0) && !math.IsNaN(f)
	}
	if s, ok := v.String(); ok {
		return parseInstant(s)
	}
	return 0, false
}

func matchesValue(op opKind, attribute value, cv *clauseValue) bool {
	switch op {
	case opIn:
		if b, ok := attribute.Bool(); ok {
			vb, isBool := cv.v.Bool()
			return isBool && vb == b
		}
		order, ok := compareValues(attribute, cv.v)
		return ok && order == 0
	case opContains, opStartsWith, opEndsWith, opMatches:
		a, ok := attribute.String()
		if !ok {
			return false
		}
		s, ok := cv.v.String()
		if !ok {
			return false
		}
		switch op {
		case opMatches:
			return cv.pattern != nil && cv.pattern.matches(a)
		case opContains:
			return wtf8.Contains(a, s)
		case opStartsWith:
			return wtf8.HasPrefix(a, s)
		}
		return wtf8.HasSuffix(a, s)
	case opSemverEq, opSemverLt, opSemverLte, opSemverGt, opSemverGte:
		a, ok := attribute.String()
		if !ok || cv.version == nil {
			return false
		}
		candidate := parseSemver(a)
		if candidate == nil {
			return false
		}
		order := compareSemver(candidate, cv.version)
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
	case opBefore, opAfter:
		candidate, ok := toInstant(attribute)
		if !ok || !cv.hasInstant {
			return false
		}
		if op == opBefore {
			return candidate < cv.instant
		}
		return candidate > cv.instant
	}
	order, ok := compareValues(attribute, cv.v)
	return ok && orderHolds(op, order)
}

// attributeClauseMatches reports whether one attribute clause holds. It reads the context of its
// kind. A missing attribute, or a missing kind, fails it, whatever negate says.
func attributeClauseMatches(c *clause, cs contexts) bool {
	kc, ok := cs[c.kind]
	if !ok {
		return false
	}
	attribute := readAttribute(kc, c.path)
	if attribute.IsNull() {
		return false
	}
	candidates := []value{attribute}
	if items, isArray := attribute.Array(); isArray {
		candidates = items
	}
	matched := false
search:
	for _, candidate := range candidates {
		for i := range c.values {
			if matchesValue(c.op, candidate, &c.values[i]) {
				matched = true
				break search
			}
		}
	}
	return matched != c.negate
}
