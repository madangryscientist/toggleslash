package evaluator

import "strings"

// Semantic versions, to the "Semantic versions" section of docs/evaluation.md:
// a hand-written walk over the string. Nothing converts a numeric part to a
// number, so a part of any length compares.

type semanticVersion struct {
	major, minor, patch string
	prerelease          []string
}

func isIdentifierChar(c byte) bool {
	return isDigit(c) || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '-'
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return false
		}
	}
	return true
}

// isNumericPart is "0" or digits with no leading zero.
func isNumericPart(s string) bool {
	return s != "" && allDigits(s) && (s == "0" || s[0] != '0')
}

// identifiers splits on ".": none empty, each of ASCII letters, digits and "-".
func identifiers(s string) ([]string, bool) {
	parts := strings.Split(s, ".")
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

// splitAt is the text before and after the first sep, and false when there is none.
func splitAt(s, sep string) (before, after string, found bool) {
	return strings.Cut(s, sep)
}

func prereleaseIdentifiers(s string) ([]string, bool) {
	parts, ok := identifiers(s)
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

func numericParts(s string, short bool) ([]string, bool) {
	numbers := strings.Split(s, ".")
	if len(numbers) > 3 || (len(numbers) < 3 && !short) {
		return nil, false
	}
	for _, n := range numbers {
		if !isNumericPart(n) {
			return nil, false
		}
	}
	return numbers, true
}

// parseSemanticVersion reads the version s spells.
func parseSemanticVersion(s string) (semanticVersion, bool) {
	withoutBuild, build, hasBuild := splitAt(s, "+")
	if hasBuild {
		// Build metadata: identifiers only, and leading zeros are fine.
		if _, ok := identifiers(build); !ok {
			return semanticVersion{}, false
		}
	}
	core, pre, hasPre := splitAt(withoutBuild, "-")
	var prerelease []string
	if hasPre {
		var ok bool
		if prerelease, ok = prereleaseIdentifiers(pre); !ok {
			return semanticVersion{}, false
		}
	}
	numbers, ok := numericParts(core, !hasBuild && !hasPre)
	if !ok {
		return semanticVersion{}, false
	}
	v := semanticVersion{major: numbers[0], minor: "0", patch: "0", prerelease: prerelease}
	if len(numbers) > 1 {
		v.minor = numbers[1]
	}
	if len(numbers) > 2 {
		v.patch = numbers[2]
	}
	return v, true
}

// compareNumeric orders digit strings with no leading zero: the longer is
// larger, else by code unit.
func compareNumeric(a, b string) int {
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
		return compareNumeric(a, b)
	case aNumeric:
		return -1
	case bNumeric:
		return 1
	}
	return strings.Compare(a, b)
}

func comparePrerelease(a, b []string) int {
	// No prerelease is above any prerelease of the same core.
	if len(a) == 0 || len(b) == 0 {
		return sign(len(b) - len(a))
	}
	for i := 0; i < min(len(a), len(b)); i++ {
		if order := compareIdentifier(a[i], b[i]); order != 0 {
			return order
		}
	}
	return sign(len(a) - len(b))
}

// compareSemanticVersions is -1, 0 or 1 by SemVer 2.0.0 section 11.
func compareSemanticVersions(a, b semanticVersion) int {
	if order := compareNumeric(a.major, b.major); order != 0 {
		return order
	}
	if order := compareNumeric(a.minor, b.minor); order != 0 {
		return order
	}
	if order := compareNumeric(a.patch, b.patch); order != 0 {
		return order
	}
	return comparePrerelease(a.prerelease, b.prerelease)
}
