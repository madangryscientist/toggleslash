package eval

import (
	"math"
	"strings"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

// readAttribute is the value a name or a "/" reference reaches, or null when it is missing.
func readAttribute(kc kindContext, c *attributeClause) jsonvalue.Value {
	name := c.attribute
	var rest []string
	if c.reference != nil {
		name, rest = c.reference[0], c.reference[1:]
	}
	var current jsonvalue.Value
	if name == "key" {
		current = jsonvalue.FromString(kc.key)
	} else if kc.attrs.Kind() == jsonvalue.Object {
		current, _ = kc.attrs.Get(name)
	}
	for _, component := range rest {
		if current.Kind() != jsonvalue.Object {
			return jsonvalue.Value{}
		}
		current, _ = current.Get(component)
	}
	return current
}

func signOf(c int) int {
	switch {
	case c < 0:
		return -1
	case c > 0:
		return 1
	}
	return 0
}

// compare is -1, 0 or 1 for two finite numbers or two strings (by UTF-16 code unit); ok is false otherwise.
func compare(attribute, value jsonvalue.Value) (int, bool) {
	if a, ok := attribute.AsNumber(); ok {
		v, isNum := value.AsNumber()
		if !isNum || math.IsInf(a, 0) || math.IsNaN(a) {
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
		if v, isStr := value.AsString(); isStr {
			return signOf(jsonvalue.CompareUTF16(a, v)), true
		}
	}
	return 0, false
}

// hasLoneSurrogate reports whether s (WTF-8) holds a lone surrogate: the bytes ED A0..BF xx.
func hasLoneSurrogate(s string) bool {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == 0xED && s[i+1] >= 0xA0 {
			return true
		}
	}
	return false
}

func indexUnits(hay, needle []uint16) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if equalUnits(hay[i:i+len(needle)], needle) {
			return true
		}
	}
	return false
}

func equalUnits(a, b []uint16) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return len(a) == len(b)
}

// stringOp is contains, startsWith or endsWith on UTF-16 code units. Without a lone surrogate in either
// string, UTF-8 bytes order and match exactly as the units do, so the byte functions answer.
func stringOp(op, attribute, value string) bool {
	if !hasLoneSurrogate(attribute) && !hasLoneSurrogate(value) {
		switch op {
		case "contains":
			return strings.Contains(attribute, value)
		case "startsWith":
			return strings.HasPrefix(attribute, value)
		}
		return strings.HasSuffix(attribute, value)
	}
	a, v := jsonvalue.UTF16(attribute), jsonvalue.UTF16(value)
	switch op {
	case "contains":
		return indexUnits(a, v)
	case "startsWith":
		return len(v) <= len(a) && equalUnits(a[:len(v)], v)
	}
	return len(v) <= len(a) && equalUnits(a[len(a)-len(v):], v)
}

func matchesOrder(op string, order int) bool {
	switch op {
	case "lt":
		return order < 0
	case "lte":
		return order <= 0
	case "gt":
		return order > 0
	}
	return order >= 0
}

func matchesSemver(op string, attribute, value jsonvalue.Value) bool {
	a, ok := attribute.AsString()
	v, ok2 := value.AsString()
	if !ok || !ok2 {
		return false
	}
	candidate, ok := parseSemver(a)
	target, ok2 := parseSemver(v)
	if !ok || !ok2 {
		return false
	}
	order := compareSemver(candidate, target)
	switch op {
	case "semverEq":
		return order == 0
	case "semverLt":
		return order < 0
	case "semverLte":
		return order <= 0
	case "semverGt":
		return order > 0
	}
	return order >= 0
}

// toInstant is a finite number as itself, a string parsed as an instant, and anything else not an instant.
func toInstant(v jsonvalue.Value) (float64, bool) {
	if f, ok := v.AsNumber(); ok {
		return f, !math.IsInf(f, 0) && !math.IsNaN(f)
	}
	if s, ok := v.AsString(); ok {
		return parseInstant(s)
	}
	return 0, false
}

func matchesDate(op string, attribute, value jsonvalue.Value) bool {
	candidate, ok := toInstant(attribute)
	target, ok2 := toInstant(value)
	if !ok || !ok2 {
		return false
	}
	if op == "before" {
		return candidate < target
	}
	return candidate > target
}

func (c *attributeClause) matchesValue(attribute jsonvalue.Value, index int) bool {
	value := c.values[index]
	switch c.op {
	case "in":
		if b, ok := attribute.AsBool(); ok {
			vb, isBool := value.AsBool()
			return isBool && b == vb
		}
		order, ok := compare(attribute, value)
		return ok && order == 0
	case "contains", "startsWith", "endsWith", "matches":
		a, ok := attribute.AsString()
		v, ok2 := value.AsString()
		if !ok || !ok2 {
			return false
		}
		if c.op == "matches" {
			p := c.patterns[index]
			return p != nil && p.matches(a)
		}
		return stringOp(c.op, a, v)
	case "semverEq", "semverLt", "semverLte", "semverGt", "semverGte":
		return matchesSemver(c.op, attribute, value)
	case "before", "after":
		return matchesDate(c.op, attribute, value)
	}
	order, ok := compare(attribute, value)
	return ok && matchesOrder(c.op, order)
}

// matches reports whether one attribute clause holds. It reads the context of its kind. A missing
// attribute, or a missing kind, fails it, whatever negate says.
func (c *attributeClause) matches(ctxs contexts) bool {
	kind := c.kind
	if kind == "" {
		kind = DefaultKind
	}
	kc, ok := ctxs[kind]
	if !ok {
		return false
	}
	attribute := readAttribute(kc, c)
	if attribute.IsNull() {
		return false
	}
	candidates := []jsonvalue.Value{attribute}
	if arr, isArr := attribute.AsArray(); isArr {
		candidates = arr
	}
	matched := false
	for _, candidate := range candidates {
		for i := range c.values {
			if c.matchesValue(candidate, i) {
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
