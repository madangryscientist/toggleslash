package evaluator

import (
	"math"
	"strings"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// readAttribute is the value a name or a "/" reference reaches in one kind's context, and false
// when it is missing.
func readAttribute(c kindContext, name string, reference []string) (jsonv.Value, bool) {
	components := reference
	if components == nil {
		components = []string{name}
	}
	var current jsonv.Value
	found := false
	if components[0] == "key" {
		current, found = jsonv.NewString(c.key), true
	} else if c.attrs != nil {
		current, found = c.attrs.Get(components[0])
	}
	for _, component := range components[1:] {
		if !found || current.Kind() != jsonv.Object {
			return jsonv.Value{}, false
		}
		current, found = current.Object().Get(component)
	}
	return current, found
}

// hasLone reports whether s holds a lone surrogate.
func hasLone(s string) bool {
	for i := 0; i+2 < len(s); i++ {
		if s[i] == 0xED && s[i+1] >= 0xA0 && s[i+1] <= 0xBF {
			return true
		}
	}
	return false
}

// compareStrings orders by UTF-16 code unit, which is not the byte order for a code point above
// U+FFFF against one in U+E000 to U+FFFF.
func compareStrings(a, b string) int {
	if a == b {
		return 0
	}
	ua, ub := wtf8.UTF16(a), wtf8.UTF16(b)
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			if ua[i] < ub[i] {
				return -1
			}
			return 1
		}
	}
	return sign(len(ua) - len(ub))
}

func compareNumbers(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// compareValues is -1, 0 or 1 for two finite numbers or two strings, and false otherwise.
func compareValues(attribute, value jsonv.Value) (int, bool) {
	switch {
	case attribute.Kind() == jsonv.Number && value.Kind() == jsonv.Number:
		if math.IsInf(attribute.Number(), 0) || math.IsNaN(attribute.Number()) {
			return 0, false
		}
		return compareNumbers(attribute.Number(), value.Number()), true
	case attribute.Kind() == jsonv.String && value.Kind() == jsonv.String:
		return compareStrings(attribute.String(), value.String()), true
	}
	return 0, false
}

// unitContains, unitHasPrefix and unitHasSuffix are the substring ops on UTF-16 code units, for the
// strings the byte forms get wrong: a lone surrogate may match half of a pair.
func unitIndex(h, n []uint16, from int) int {
	for i := from; i+len(n) <= len(h); i++ {
		match := true
		for j := range n {
			if h[i+j] != n[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func matchesString(o op, attribute, value string, p *pattern) bool {
	if o == opMatches {
		return p != nil && p.matches(attribute)
	}
	if hasLone(attribute) || hasLone(value) {
		h, n := wtf8.UTF16(attribute), wtf8.UTF16(value)
		switch o {
		case opContains:
			return unitIndex(h, n, 0) >= 0
		case opStartsWith:
			return len(n) <= len(h) && unitIndex(h[:len(n)], n, 0) == 0
		}
		return len(n) <= len(h) && unitIndex(h[len(h)-len(n):], n, 0) == 0
	}
	switch o {
	case opContains:
		return strings.Contains(attribute, value)
	case opStartsWith:
		return strings.HasPrefix(attribute, value)
	}
	return strings.HasSuffix(attribute, value)
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

func matchesSemver(o op, attribute, value jsonv.Value) bool {
	if attribute.Kind() != jsonv.String || value.Kind() != jsonv.String {
		return false
	}
	candidate, ok1 := parseVersion(attribute.String())
	target, ok2 := parseVersion(value.String())
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

// toInstant reads a finite number as Unix milliseconds and a string as a timestamp.
func toInstant(v jsonv.Value) (float64, bool) {
	switch v.Kind() {
	case jsonv.Number:
		n := v.Number()
		return n, !math.IsInf(n, 0) && !math.IsNaN(n)
	case jsonv.String:
		return parseInstant(v.String())
	}
	return 0, false
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

func matchesValue(c *clause, attribute jsonv.Value, index int) bool {
	value := c.values[index]
	switch c.op {
	case opIn:
		if attribute.Kind() == jsonv.Bool {
			return value.Kind() == jsonv.Bool && attribute.Bool() == value.Bool()
		}
		order, ok := compareValues(attribute, value)
		return ok && order == 0
	case opContains, opStartsWith, opEndsWith, opMatches:
		if attribute.Kind() != jsonv.String || value.Kind() != jsonv.String {
			return false
		}
		var p *pattern
		if c.patterns != nil {
			p = c.patterns[index]
		}
		return matchesString(c.op, attribute.String(), value.String(), p)
	case opSemverEq, opSemverLt, opSemverLte, opSemverGt, opSemverGte:
		return matchesSemver(c.op, attribute, value)
	case opBefore, opAfter:
		return matchesDate(c.op, attribute, value)
	}
	order, ok := compareValues(attribute, value)
	return ok && orderHolds(c.op, order)
}

// attributeClauseMatches reports whether one attribute clause holds. It reads the context of its
// kind. A missing attribute, or a missing kind, fails it, whatever negate says.
func attributeClauseMatches(c *clause, cs contexts) bool {
	kind := c.kind
	if kind == "" {
		kind = defaultKind
	}
	context, ok := cs.get(kind)
	if !ok {
		return false
	}
	attribute, found := readAttribute(context, c.attribute, c.reference)
	if !found || attribute.Kind() == jsonv.Null {
		return false
	}
	candidates := []jsonv.Value{attribute}
	if attribute.Kind() == jsonv.Array {
		candidates = attribute.Items()
	}
	matched := false
	for _, candidate := range candidates {
		for i := range c.values {
			if matchesValue(c, candidate, i) {
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
