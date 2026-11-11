package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"

// readAttribute is the value a clause's path reaches in one kind's context, or a null value when it is
// missing. The first component is "key" (the context's key) or an own attribute; later components step
// into objects, own properties only.
func readAttribute(c kindContext, path []string) jsonv.Value {
	var current jsonv.Value
	if path[0] == "key" {
		current = jsonv.NewString(c.key)
	} else if c.attrs != nil {
		current = c.attrs[path[0]]
	}
	for _, component := range path[1:] {
		obj, ok := current.Object()
		if !ok {
			return jsonv.Value{}
		}
		current = obj[component]
	}
	return current
}

func signOf(less, greater bool) int {
	switch {
	case less:
		return -1
	case greater:
		return 1
	}
	return 0
}

// compare is -1, 0 or 1 for two finite numbers or two strings (by UTF-16 code unit); ok is false for
// anything else.
func compare(attribute, value jsonv.Value) (int, bool) {
	if a, ok := attribute.Number(); ok {
		b, isNum := value.Number()
		if !isNum || !attribute.IsFinite() {
			return 0, false
		}
		return signOf(a < b, a > b), true
	}
	if a, ok := attribute.Str(); ok {
		b, isStr := value.Str()
		if !isStr {
			return 0, false
		}
		return jsonv.Compare(a, b), true
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

func semverHolds(o op, attribute, value jsonv.Value) bool {
	a, ok1 := attribute.Str()
	b, ok2 := value.Str()
	if !ok1 || !ok2 {
		return false
	}
	candidate, ok1 := parseSemver(a)
	target, ok2 := parseSemver(b)
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

func dateHolds(o op, attribute, value jsonv.Value) bool {
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

func stringHolds(o op, attribute string, value string, pattern *program) bool {
	switch o {
	case opMatches:
		return pattern != nil && pattern.matches(attribute)
	case opContains:
		return jsonv.Contains(attribute, value)
	case opStartsWith:
		return jsonv.HasPrefix(attribute, value)
	}
	return jsonv.HasSuffix(attribute, value)
}

// valueHolds is one candidate against one clause value. index is the value's position in the clause.
func (c *clause) valueHolds(candidate jsonv.Value, index int) bool {
	value := c.values[index]
	switch c.op {
	case opIn:
		if b, ok := candidate.Bool(); ok {
			vb, isBool := value.Bool()
			return isBool && b == vb
		}
		order, ok := compare(candidate, value)
		return ok && order == 0
	case opContains, opStartsWith, opEndsWith, opMatches:
		a, ok1 := candidate.Str()
		v, ok2 := value.Str()
		if !ok1 || !ok2 {
			return false
		}
		var pattern *program
		if c.op == opMatches {
			pattern = c.patterns[index]
		}
		return stringHolds(c.op, a, v, pattern)
	case opSemverEq, opSemverLt, opSemverLte, opSemverGt, opSemverGte:
		return semverHolds(c.op, candidate, value)
	case opBefore, opAfter:
		return dateHolds(c.op, candidate, value)
	}
	order, ok := compare(candidate, value)
	return ok && orderHolds(c.op, order)
}

// attributeMatches reports whether one attribute clause holds. It reads the context of its kind. A
// missing attribute, or a missing kind, fails it, whatever negate says.
func (c *clause) attributeMatches(ctx contexts) bool {
	kind := c.kind
	if kind == "" {
		kind = defaultKind
	}
	kc, ok := ctx.get(kind)
	if !ok {
		return false
	}
	attribute := readAttribute(kc, c.path)
	if attribute.IsNull() {
		return false
	}
	candidates := []jsonv.Value{attribute}
	if items, isArray := attribute.Array(); isArray {
		candidates = items
	}
	matched := false
search:
	for _, candidate := range candidates {
		for i := range c.values {
			if c.valueHolds(candidate, i) {
				matched = true
				break search
			}
		}
	}
	return matched != c.negate
}
