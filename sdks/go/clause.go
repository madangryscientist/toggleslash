package toggleslash

import "strings"

// Attribute clauses, to the "Clauses" section of docs/evaluation.md.

func own(record map[string]any, name string) (any, bool) {
	value, ok := record[name]
	return value, ok
}

// readAttribute reads a name or a "/" reference from a context. The first component reads the context's key
// (for "key") or its own attributes; each further component steps into an object.
func readAttribute(context kindContext, name string) (any, bool) {
	var components []string
	if strings.HasPrefix(name, "/") {
		parsed, ok := parseReference(name)
		if !ok {
			return nil, false
		}
		components = parsed
	} else {
		components = []string{name}
	}
	if len(components) == 0 {
		return nil, false
	}
	var current any
	present := false
	if components[0] == "key" {
		current, present = context.key, true
	} else if context.attributes != nil {
		current, present = own(context.attributes, components[0])
	}
	for _, component := range components[1:] {
		record, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, present = own(record, component)
		if !present {
			return nil, false
		}
	}
	if !present {
		return nil, false
	}
	return current, true
}

// clauseOrder orders two finite numbers, or two strings by UTF-16 code unit, and reports false for any other
// pair.
func clauseOrder(attribute, value any) (int, bool) {
	if left, ok := numericValue(attribute); ok {
		right, ok := numericValue(value)
		if !ok || !isFiniteNumber(left) || !isFiniteNumber(right) {
			return 0, false
		}
		switch {
		case left < right:
			return -1, true
		case left > right:
			return 1, true
		}
		return 0, true
	}
	left, ok := attribute.(string)
	if !ok {
		return 0, false
	}
	right, ok := value.(string)
	if !ok {
		return 0, false
	}
	return compareText(left, right), true
}

func matchesStringOp(op, attribute, value string) bool {
	switch op {
	case "matches":
		program, ok := compilePattern(value)
		return ok && patternMatches(program, attribute)
	case "contains":
		return containsText(attribute, value)
	case "startsWith":
		return hasPrefixText(attribute, value)
	}
	return hasSuffixText(attribute, value)
}

func matchesSemver(op string, attribute, value any) bool {
	left, ok := attribute.(string)
	if !ok {
		return false
	}
	right, ok := value.(string)
	if !ok {
		return false
	}
	candidate, ok := parseSemanticVersion(left)
	if !ok {
		return false
	}
	target, ok := parseSemanticVersion(right)
	if !ok {
		return false
	}
	order := compareSemanticVersions(candidate, target)
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

func matchesDate(op string, attribute, value any) bool {
	candidate, ok := instantOf(attribute)
	if !ok {
		return false
	}
	target, ok := instantOf(value)
	if !ok {
		return false
	}
	if op == "before" {
		return candidate < target
	}
	return candidate > target
}

// matchesValue is the base match for one attribute candidate against one clause value.
func matchesValue(op string, attribute, value any) bool {
	switch op {
	case "in":
		if left, ok := attribute.(bool); ok {
			right, ok := value.(bool)
			return ok && left == right
		}
		if order, ok := clauseOrder(attribute, value); ok {
			return order == 0
		}
		return false
	case "contains", "startsWith", "endsWith", "matches":
		left, ok := attribute.(string)
		if !ok {
			return false
		}
		right, ok := value.(string)
		return ok && matchesStringOp(op, left, right)
	case "semverEq", "semverLt", "semverLte", "semverGt", "semverGte":
		return matchesSemver(op, attribute, value)
	case "before", "after":
		return matchesDate(op, attribute, value)
	}
	order, ok := clauseOrder(attribute, value)
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
	}
	return order >= 0
}

// candidatesOf makes an array attribute one candidate per element and anything else a single candidate. A
// null inside an array never matches, and an empty array has no candidates.
func candidatesOf(attribute any) []any {
	if items, ok := attribute.([]any); ok {
		return items
	}
	return []any{attribute}
}

// attributeClauseMatches reports whether one attribute clause holds. A missing attribute, or a context of
// another kind, fails it whatever negate says.
func attributeClauseMatches(clause map[string]any, contexts map[string]kindContext) bool {
	kind := defaultKind
	if raw, present := clause["contextKind"]; present && raw != nil {
		text, ok := raw.(string)
		if !ok {
			return false
		}
		kind = text
	}
	context, ok := contexts[kind]
	if !ok {
		return false
	}
	name, ok := clause["attribute"].(string)
	if !ok {
		return false
	}
	attribute, ok := readAttribute(context, name)
	if !ok || attribute == nil {
		return false
	}
	op, ok := clause["op"].(string)
	if !ok {
		return false
	}
	values, ok := clause["values"].([]any)
	if !ok {
		return false
	}
	matched := false
	for _, candidate := range candidatesOf(attribute) {
		for _, value := range values {
			if matchesValue(op, candidate, value) {
				matched = true
				break
			}
		}
		if matched {
			break
		}
	}
	if negate, ok := clause["negate"].(bool); ok && negate {
		return !matched
	}
	return matched
}
