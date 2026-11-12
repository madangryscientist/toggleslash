package toggleslash

import "strings"

// Semantic versions, to the "Semantic versions" section of docs/evaluation.md.

// semanticVersion is a parsed version. Its numeric parts stay strings, so a part of any length compares
// exactly, and its build metadata is dropped because equality and order ignore it.
type semanticVersion struct {
	major, minor, patch string
	prerelease          []string
}

func isDigitChar(c byte) bool { return c >= '0' && c <= '9' }

func allDigits(text string) bool {
	for i := 0; i < len(text); i++ {
		if !isDigitChar(text[i]) {
			return false
		}
	}
	return true
}

func isIdentifierChar(c byte) bool {
	return isDigitChar(c) || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '-'
}

func isNumericPart(text string) bool {
	return text != "" && allDigits(text) && (text == "0" || text[0] != '0')
}

func identifiers(text string) ([]string, bool) {
	parts := strings.Split(text, ".")
	for _, part := range parts {
		if part == "" {
			return nil, false
		}
		for i := 0; i < len(part); i++ {
			if !isIdentifierChar(part[i]) {
				return nil, false
			}
		}
	}
	return parts, true
}

// splitAt splits at the first occurrence of c. A "-" or "+" inside prerelease or build identifiers is not a
// separator, so only the first is taken.
func splitAt(text string, c byte) (string, string, bool) {
	at := strings.IndexByte(text, c)
	if at < 0 {
		return text, "", false
	}
	return text[:at], text[at+1:], true
}

func prereleaseIdentifiers(text string) ([]string, bool) {
	parts, ok := identifiers(text)
	if !ok {
		return nil, false
	}
	for _, part := range parts {
		if allDigits(part) && !isNumericPart(part) {
			return nil, false
		}
	}
	return parts, true
}

func numericParts(text string, short bool) ([]string, bool) {
	numbers := strings.Split(text, ".")
	if len(numbers) > 3 || len(numbers) < 3 && !short {
		return nil, false
	}
	for _, part := range numbers {
		if !isNumericPart(part) {
			return nil, false
		}
	}
	return numbers, true
}

func parseSemanticVersion(text string) (semanticVersion, bool) {
	withoutBuild, build, hasBuild := splitAt(text, '+')
	if hasBuild {
		if _, ok := identifiers(build); !ok {
			return semanticVersion{}, false
		}
	}
	core, prerelease, hasPrerelease := splitAt(withoutBuild, '-')
	var parsed []string
	if hasPrerelease {
		var ok bool
		parsed, ok = prereleaseIdentifiers(prerelease)
		if !ok {
			return semanticVersion{}, false
		}
	}
	numbers, ok := numericParts(core, !hasBuild && !hasPrerelease)
	if !ok {
		return semanticVersion{}, false
	}
	for len(numbers) < 3 {
		numbers = append(numbers, "0")
	}
	return semanticVersion{
		major:      numbers[0],
		minor:      numbers[1],
		patch:      numbers[2],
		prerelease: parsed,
	}, true
}

func signOf(order int) int {
	switch {
	case order < 0:
		return -1
	case order > 0:
		return 1
	}
	return 0
}

func compareNumericParts(a, b string) int {
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}

func compareIdentifier(a, b string) int {
	aNumeric, bNumeric := allDigits(a), allDigits(b)
	switch {
	case aNumeric && bNumeric:
		return compareNumericParts(a, b)
	case aNumeric:
		return -1
	case bNumeric:
		return 1
	}
	return strings.Compare(a, b)
}

func comparePrerelease(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return signOf(len(b) - len(a))
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		if order := compareIdentifier(a[i], b[i]); order != 0 {
			return order
		}
	}
	return signOf(len(a) - len(b))
}

func compareSemanticVersions(a, b semanticVersion) int {
	if order := compareNumericParts(a.major, b.major); order != 0 {
		return order
	}
	if order := compareNumericParts(a.minor, b.minor); order != 0 {
		return order
	}
	if order := compareNumericParts(a.patch, b.patch); order != 0 {
		return order
	}
	return comparePrerelease(a.prerelease, b.prerelease)
}
