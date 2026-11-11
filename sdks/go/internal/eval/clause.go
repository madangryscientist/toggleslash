package eval

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// readAttribute returns the value a name or a "/" reference reaches, or ok false when it is missing.
func readAttribute(c kindContext, components []string) (jsonv.Value, bool) {
	first := components[0]
	var current jsonv.Value
	switch {
	case first == "key":
		current = jsonv.StringValue(c.key)
	case c.attributes != nil:
		v, ok := c.attributes.Get(first)
		if !ok {
			return jsonv.Value{}, false
		}
		current = v
	default:
		return jsonv.Value{}, false
	}
	for _, component := range components[1:] {
		o, ok := current.AsObject()
		if !ok {
			return jsonv.Value{}, false
		}
		if current, ok = o.Get(component); !ok {
			return jsonv.Value{}, false
		}
	}
	return current, true
}

func finite(n float64) bool { return !math.IsInf(n, 0) && !math.IsNaN(n) }

// order returns -1, 0 or 1 for two finite numbers or two strings (by UTF-16 code unit); ok false otherwise.
func order(attribute, v jsonv.Value) (int, bool) {
	if a, ok := attribute.AsNumber(); ok {
		b, ok := v.AsNumber()
		if !ok || !finite(a) {
			return 0, false
		}
		switch {
		case a < b:
			return -1, true
		case a > b:
			return 1, true
		}
		return 0, true
	}
	if a, ok := attribute.AsString(); ok {
		if b, ok := v.AsString(); ok {
			return jsonv.Compare(a, b), true
		}
	}
	return 0, false
}

func toInstant(v jsonv.Value) (float64, bool) {
	if n, ok := v.AsNumber(); ok {
		return n, finite(n)
	}
	if s, ok := v.AsString(); ok {
		return parseInstant(s)
	}
	return 0, false
}

func (c *clause) matchesValue(index int, attribute jsonv.Value) bool {
	v := c.values[index]
	switch c.op {
	case opIn:
		if a, ok := attribute.AsBool(); ok {
			b, isBool := v.AsBool()
			return isBool && a == b
		}
		o, ok := order(attribute, v)
		return ok && o == 0
	case opContains, opStartsWith, opEndsWith, opMatches:
		a, ok := attribute.AsString()
		if !ok {
			return false
		}
		s, ok := v.AsString()
		if !ok {
			return false
		}
		switch c.op {
		case opContains:
			return jsonv.Contains(a, s)
		case opStartsWith:
			return jsonv.HasPrefix(a, s)
		case opEndsWith:
			return jsonv.HasSuffix(a, s)
		}
		p := c.patterns[index]
		return p != nil && p.Matches(jsonv.CodePoints(a))
	case opSemverEq, opSemverLt, opSemverLte, opSemverGt, opSemverGte:
		a, ok := attribute.AsString()
		if !ok {
			return false
		}
		s, ok := v.AsString()
		if !ok {
			return false
		}
		candidate, ok := parseSemver(a)
		if !ok {
			return false
		}
		target, ok := parseSemver(s)
		if !ok {
			return false
		}
		o := compareSemver(candidate, target)
		switch c.op {
		case opSemverEq:
			return o == 0
		case opSemverLt:
			return o < 0
		case opSemverLte:
			return o <= 0
		case opSemverGt:
			return o > 0
		}
		return o >= 0
	case opBefore, opAfter:
		a, ok := toInstant(attribute)
		if !ok {
			return false
		}
		b, ok := toInstant(v)
		if !ok {
			return false
		}
		if c.op == opBefore {
			return a < b
		}
		return a > b
	}
	o, ok := order(attribute, v)
	if !ok {
		return false
	}
	switch c.op {
	case opLt:
		return o < 0
	case opLte:
		return o <= 0
	case opGt:
		return o > 0
	}
	return o >= 0
}

// attributeMatches reports whether one attribute clause holds. It reads the context of its kind. A missing
// or null attribute, or a missing kind, fails it whatever negate says.
func (c *clause) attributeMatches(ctx contexts) bool {
	kc, ok := ctx[c.kind]
	if !ok {
		return false
	}
	attribute, ok := readAttribute(kc, c.attribute)
	if !ok || attribute.IsNull() {
		return false
	}
	candidates := []jsonv.Value{attribute}
	if items, isArray := attribute.AsArray(); isArray {
		candidates = items
	}
	matched := false
search:
	for _, candidate := range candidates {
		for i := range c.values {
			if c.matchesValue(i, candidate) {
				matched = true
				break search
			}
		}
	}
	return matched != c.negate
}
