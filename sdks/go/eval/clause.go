package eval

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonval"
)

// attributeMatches reports whether one attribute clause holds. A missing
// attribute, or a context without the clause's kind, fails it whatever negate
// says.
func (rn *run) attributeMatches(c clause) bool {
	context, ok := rn.contexts[c.contextKind]
	if !ok {
		return false
	}
	value, ok := readAttribute(context, c)
	if !ok || absent(value) {
		return false
	}
	candidates, isArray := asArray(value)
	var list []jsonval.Value
	if isArray {
		list = candidates
	} else {
		list = []jsonval.Value{value}
	}
	matched := false
	for _, candidate := range list {
		for _, wanted := range c.values {
			if matchesValue(c.op, candidate, wanted) {
				matched = true
				break
			}
		}
		if matched {
			break
		}
	}
	if c.negate {
		return !matched
	}
	return matched
}

// readAttribute reads the value an attribute name or `/` reference reaches.
func readAttribute(context kindContext, c clause) (jsonval.Value, bool) {
	components := c.components
	if components == nil {
		components = []string{c.attribute}
	}
	var current jsonval.Value
	switch first := components[0]; {
	case first == "key":
		current = jsonval.String(context.key)
	case context.attributes != nil:
		value, ok := context.attributes[first]
		if !ok {
			return nil, false
		}
		current = value
	default:
		return nil, false
	}
	for _, component := range components[1:] {
		obj, ok := asObject(current)
		if !ok {
			return nil, false
		}
		value, ok := obj[component]
		if !ok {
			return nil, false
		}
		current = value
	}
	return current, true
}

func matchesValue(op string, attribute, value jsonval.Value) bool {
	switch op {
	case "in":
		if candidate, ok := asBool(attribute); ok {
			wanted, ok := asBool(value)
			return ok && candidate == wanted
		}
		order, ok := compareValues(attribute, value)
		return ok && order == 0
	case "contains", "startsWith", "endsWith", "matches":
		candidate, ok := asString(attribute)
		if !ok {
			return false
		}
		wanted, ok := asString(value)
		if !ok {
			return false
		}
		switch op {
		case "contains":
			return containsUTF16(candidate, wanted)
		case "startsWith":
			return startsWithUTF16(candidate, wanted)
		case "endsWith":
			return endsWithUTF16(candidate, wanted)
		default:
			return patternMatches(candidate, wanted)
		}
	case "semverEq", "semverLt", "semverLte", "semverGt", "semverGte":
		return matchesSemver(op, attribute, value)
	case "before", "after":
		return matchesInstant(op, attribute, value)
	default:
		order, ok := compareValues(attribute, value)
		if !ok {
			return false
		}
		switch op {
		case "lt":
			return order < 0
		case "lte":
			return order <= 0
		case "gt":
			return order > 0
		default:
			return order >= 0
		}
	}
}

// compareValues orders two finite numbers, or two strings by UTF-16 code unit.
func compareValues(attribute, value jsonval.Value) (int, bool) {
	if left, ok := asNumber(attribute); ok {
		if math.IsInf(left, 0) || math.IsNaN(left) {
			return 0, false
		}
		right, ok := asNumber(value)
		if !ok || math.IsInf(right, 0) || math.IsNaN(right) {
			return 0, false
		}
		return sign(left, right), true
	}
	if left, ok := asString(attribute); ok {
		right, ok := asString(value)
		if !ok {
			return 0, false
		}
		return compareUTF16(left, right), true
	}
	return 0, false
}

func sign(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// The three substring ops are ordinal over UTF-16 code units too
// (docs/evaluation.md, "String equality and the three substring ops are
// ordinal"), so a lone surrogate in the needle can match half of a pair in the
// string. Byte comparison would miss that, and would compare a pair above the
// basic plane against the wrong units.
func containsUTF16(candidate, wanted string) bool {
	haystack := utf16Units(candidate)
	needle := utf16Units(wanted)
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		found := true
		for j, unit := range needle {
			if haystack[i+j] != unit {
				found = false
				break
			}
		}
		if found {
			return true
		}
	}
	return false
}

func startsWithUTF16(candidate, wanted string) bool {
	haystack := utf16Units(candidate)
	needle := utf16Units(wanted)
	if len(needle) > len(haystack) {
		return false
	}
	for i, unit := range needle {
		if haystack[i] != unit {
			return false
		}
	}
	return true
}

func endsWithUTF16(candidate, wanted string) bool {
	haystack := utf16Units(candidate)
	needle := utf16Units(wanted)
	if len(needle) > len(haystack) {
		return false
	}
	offset := len(haystack) - len(needle)
	for i, unit := range needle {
		if haystack[offset+i] != unit {
			return false
		}
	}
	return true
}

// compareUTF16 orders two strings by UTF-16 code unit, which is not their byte
// order above the basic multilingual plane.
func compareUTF16(a, b string) int {
	left := utf16Units(a)
	right := utf16Units(b)
	shared := len(left)
	if len(right) < shared {
		shared = len(right)
	}
	for i := 0; i < shared; i++ {
		switch {
		case left[i] < right[i]:
			return -1
		case left[i] > right[i]:
			return 1
		}
	}
	switch {
	case len(left) < len(right):
		return -1
	case len(left) > len(right):
		return 1
	default:
		return 0
	}
}

// utf16Units is the UTF-16 encoding of s, one unit per BMP code point and two
// for each code point above it. A lone surrogate is one unit, as it is in
// UTF-16.
func utf16Units(s string) []uint16 {
	points := jsonval.CodePoints(s)
	out := make([]uint16, 0, len(points))
	for _, cp := range points {
		if cp >= 0x10000 {
			offset := cp - 0x10000
			out = append(out, uint16(0xD800+(offset>>10)), uint16(0xDC00+(offset&0x3FF)))
			continue
		}
		out = append(out, uint16(cp))
	}
	return out
}
