package eval

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// readAttribute returns the value a name or a `/` reference reaches; missing is the zero (null) Value.
func readAttribute(c kindContext, cl *clause) jsonvalue.Value {
	components := cl.ref
	if components == nil {
		components = []string{cl.attribute}
	}
	first, rest := components[0], components[1:]
	var current jsonvalue.Value
	if first == "key" {
		current = jsonvalue.Str(c.key)
	} else if c.attributes.Kind == jsonvalue.Object {
		current = c.attributes.O.M[first]
	}
	for _, component := range rest {
		if current.Kind != jsonvalue.Object {
			return jsonvalue.Value{}
		}
		current = current.O.M[component]
	}
	return current
}

func signOf[T ~float64](a, b T) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// compare is -1, 0 or 1 for two finite numbers or two strings (by UTF-16 code unit); ok is false
// otherwise.
func compare(attribute, value jsonvalue.Value) (int, bool) {
	if attribute.Kind == jsonvalue.Number && value.Kind == jsonvalue.Number {
		if math.IsInf(attribute.N, 0) {
			return 0, false
		}
		return signOf(attribute.N, value.N), true
	}
	if attribute.Kind == jsonvalue.String && value.Kind == jsonvalue.String {
		return wtf8.Compare(attribute.S, value.S), true
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

func matchesSemver(o op, attribute, value jsonvalue.Value) bool {
	if attribute.Kind != jsonvalue.String || value.Kind != jsonvalue.String {
		return false
	}
	candidate, ok1 := parseSemver(attribute.S)
	target, ok2 := parseSemver(value.S)
	if !ok1 || !ok2 {
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

// toInstant reads a finite number as Unix milliseconds and a string as an instant.
func toInstant(v jsonvalue.Value) (float64, bool) {
	switch v.Kind {
	case jsonvalue.Number:
		if math.IsInf(v.N, 0) {
			return 0, false
		}
		return v.N, true
	case jsonvalue.String:
		return parseInstant(v.S)
	}
	return 0, false
}

func matchesDate(o op, attribute, value jsonvalue.Value) bool {
	candidate, ok1 := toInstant(attribute)
	target, ok2 := toInstant(value)
	if !ok1 || !ok2 {
		return false
	}
	if o == opBefore {
		return candidate < target
	}
	return candidate > target
}

func matchesValue(cl *clause, index int, attribute jsonvalue.Value) bool {
	value := cl.values[index]
	switch cl.op {
	case opIn:
		if attribute.Kind == jsonvalue.Bool {
			return value.Kind == jsonvalue.Bool && attribute.B == value.B
		}
		order, ok := compare(attribute, value)
		return ok && order == 0
	case opContains, opStartsWith, opEndsWith, opMatches:
		if attribute.Kind != jsonvalue.String || value.Kind != jsonvalue.String {
			return false
		}
		switch cl.op {
		case opMatches:
			p := cl.patterns[index]
			return p != nil && p.matches(attribute.S)
		case opContains:
			return wtf8.Contains(attribute.S, value.S)
		case opStartsWith:
			return wtf8.HasPrefix(attribute.S, value.S)
		}
		return wtf8.HasSuffix(attribute.S, value.S)
	case opSemverEq, opSemverLt, opSemverLte, opSemverGt, opSemverGte:
		return matchesSemver(cl.op, attribute, value)
	case opBefore, opAfter:
		return matchesDate(cl.op, attribute, value)
	}
	order, ok := compare(attribute, value)
	return ok && matchesOrder(cl.op, order)
}

func matchesCandidate(cl *clause, candidate jsonvalue.Value) bool {
	for i := range cl.values {
		if matchesValue(cl, i, candidate) {
			return true
		}
	}
	return false
}

// attributeClauseMatches reports whether one attribute clause holds. It reads the context of its
// contextKind. A missing attribute, or a missing kind, fails it, whatever negate says.
func attributeClauseMatches(cl *clause, contexts Contexts) bool {
	kind := cl.kind
	if kind == "" {
		kind = defaultKind
	}
	c, ok := contexts[kind]
	if !ok {
		return false
	}
	attribute := readAttribute(c, cl)
	if attribute.IsAbsent() {
		return false
	}
	matched := false
	if attribute.Kind == jsonvalue.Array {
		for _, candidate := range attribute.A {
			if matchesCandidate(cl, candidate) {
				matched = true
				break
			}
		}
	} else {
		matched = matchesCandidate(cl, attribute)
	}
	if cl.negate {
		return !matched
	}
	return matched
}
