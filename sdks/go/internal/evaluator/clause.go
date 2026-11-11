package evaluator

import (
	"strings"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// readAttribute is the value a name or a "/" reference reaches, or null when it is missing. Only the first
// component is read like a literal name (and "key" is the context key); the rest step into objects, own
// properties only.
func readAttribute(c kindContext, components []string) jsonv.Value {
	current := jsonv.Null()
	if components[0] == "key" {
		current = jsonv.String(c.key)
	} else if c.attrs != nil {
		current, _ = c.attrs.Get(components[0])
	}
	for _, name := range components[1:] {
		o, ok := current.AsObject()
		if !ok {
			return jsonv.Null()
		}
		current, _ = o.Get(name)
	}
	return current
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

func compareUnits(a, b []uint16) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return signFloat(float64(len(a)), float64(len(b)))
}

// compareText orders two strings by UTF-16 code unit. UTF-8 bytes order by code point, which differs from
// that only where a lead byte of 0xED or more is involved (surrogates, and the planes above the first
// against U+E000 and up), so those take the UTF-16 route.
func compareText(a, b string) int {
	if jsonv.HasHighBytes(a) || jsonv.HasHighBytes(b) {
		return compareUnits(jsonv.UTF16(a), jsonv.UTF16(b))
	}
	return strings.Compare(a, b)
}

// compareValues is -1, 0 or 1 for two finite numbers or two strings; ok is false otherwise.
func compareValues(attribute, value jsonv.Value) (int, bool) {
	if a, ok := attribute.AsNumber(); ok {
		b, isNumber := value.AsNumber()
		if !isNumber || !jsonv.IsFinite(a) {
			return 0, false
		}
		return signFloat(a, b), true
	}
	if a, ok := attribute.AsString(); ok {
		if b, isString := value.AsString(); isString {
			return compareText(a, b), true
		}
	}
	return 0, false
}

func hasUnitSubsequence(a, b []uint16, mode op) bool {
	switch mode {
	case opStartsWith:
		return len(a) >= len(b) && compareUnits(a[:len(b)], b) == 0
	case opEndsWith:
		return len(a) >= len(b) && compareUnits(a[len(a)-len(b):], b) == 0
	}
	for i := 0; i+len(b) <= len(a); i++ {
		if compareUnits(a[i:i+len(b)], b) == 0 {
			return true
		}
	}
	return false
}

// matchesString is contains, startsWith and endsWith over UTF-16 code units. A value with no lone surrogate
// is well-formed UTF-8, whose bytes match exactly where its code units do; a value with one is compared as
// code units.
func matchesString(o op, attribute, value string) bool {
	if jsonv.HasLoneSurrogate(value) {
		return hasUnitSubsequence(jsonv.UTF16(attribute), jsonv.UTF16(value), o)
	}
	switch o {
	case opContains:
		return strings.Contains(attribute, value)
	case opStartsWith:
		return strings.HasPrefix(attribute, value)
	}
	return strings.HasSuffix(attribute, value)
}

func orderMatches(o op, order int) bool {
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

func matchesSemver(o op, attribute, value jsonv.Value) bool {
	a, ok1 := attribute.AsString()
	b, ok2 := value.AsString()
	if !ok1 || !ok2 {
		return false
	}
	candidate, ok1 := parseVersion(a)
	target, ok2 := parseVersion(b)
	if !ok1 || !ok2 {
		return false
	}
	order := compareVersions(candidate, target)
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

func matchesDate(o op, attribute, value jsonv.Value) bool {
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

func (c *clause) matchesValue(attribute jsonv.Value, index int) bool {
	value := c.values[index]
	switch c.op {
	case opIn:
		if a, ok := attribute.AsBool(); ok {
			b, isBool := value.AsBool()
			return isBool && a == b
		}
		order, ok := compareValues(attribute, value)
		return ok && order == 0
	case opContains, opStartsWith, opEndsWith:
		a, ok1 := attribute.AsString()
		b, ok2 := value.AsString()
		return ok1 && ok2 && matchesString(c.op, a, b)
	case opMatches:
		a, ok1 := attribute.AsString()
		_, ok2 := value.AsString()
		return ok1 && ok2 && c.patterns[index] != nil && c.patterns[index].matches(a)
	case opSemverEq, opSemverLt, opSemverLte, opSemverGt, opSemverGte:
		return matchesSemver(c.op, attribute, value)
	case opBefore, opAfter:
		return matchesDate(c.op, attribute, value)
	}
	order, ok := compareValues(attribute, value)
	return ok && orderMatches(c.op, order)
}

func (c *clause) matchesCandidate(attribute jsonv.Value) bool {
	for i := range c.values {
		if c.matchesValue(attribute, i) {
			return true
		}
	}
	return false
}

// attributeMatches reports whether one attribute clause holds. It reads the context of its kind: a missing
// attribute, or a missing kind, fails it, whatever negate says.
func (c *clause) attributeMatches(ctxs contexts) bool {
	kind := c.kind
	if kind == "" {
		kind = defaultKind
	}
	ctx, ok := ctxs.get(kind)
	if !ok {
		return false
	}
	attribute := readAttribute(ctx, c.components)
	if attribute.IsNull() {
		return false
	}
	matched := false
	if items, isArray := attribute.AsArray(); isArray {
		for _, item := range items {
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
