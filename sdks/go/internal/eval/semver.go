package eval

import "strings"

// Semantic versions, to the "Semantic versions" section of docs/evaluation.md. A hand-written walk over the
// string: no regular expression and no library. Nothing here converts a numeric part to a number.

type semanticVersion struct {
	major, minor, patch string
	prerelease          []string
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

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

// splitAt splits at the first sep; found is false when there is none.
func splitAt(s string, sep byte) (before, after string, found bool) {
	at := strings.IndexByte(s, sep)
	if at < 0 {
		return s, "", false
	}
	return s[:at], s[at+1:], true
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

// numericParts reads MAJOR.MINOR.PATCH, or a short form (MAJOR, MAJOR.MINOR) when short is allowed.
func numericParts(s string, short bool) ([]string, bool) {
	numbers := strings.Split(s, ".")
	if len(numbers) > 3 || len(numbers) < 3 && !short {
		return nil, false
	}
	for _, n := range numbers {
		if !isNumericPart(n) {
			return nil, false
		}
	}
	return numbers, true
}

// parseSemanticVersion is the version s spells, or false when it is not one.
func parseSemanticVersion(s string) (semanticVersion, bool) {
	withoutBuild, build, hasBuild := splitAt(s, '+')
	// Build metadata: identifiers only, and leading zeros are fine.
	if hasBuild {
		if _, ok := identifiers(build); !ok {
			return semanticVersion{}, false
		}
	}
	core, prerelease, hasPrerelease := splitAt(withoutBuild, '-')
	var pre []string
	if hasPrerelease {
		var ok bool
		if pre, ok = prereleaseIdentifiers(prerelease); !ok {
			return semanticVersion{}, false
		}
	}
	// The short forms carry no suffix.
	numbers, ok := numericParts(core, !hasBuild && !hasPrerelease)
	if !ok {
		return semanticVersion{}, false
	}
	v := semanticVersion{major: numbers[0], minor: "0", patch: "0", prerelease: pre}
	if len(numbers) > 1 {
		v.minor = numbers[1]
	}
	if len(numbers) > 2 {
		v.patch = numbers[2]
	}
	return v, true
}

func sign(order int) int {
	switch {
	case order < 0:
		return -1
	case order > 0:
		return 1
	}
	return 0
}

// compareNumeric orders digit strings with no leading zero: the longer is larger, else by code unit.
func compareNumeric(a, b string) int {
	if len(a) != len(b) {
		return sign(len(a) - len(b))
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

// compareSemanticVersions is -1, 0 or 1 by SemVer 2.0.0 section 11; build metadata is already gone.
func compareSemanticVersions(a, b semanticVersion) int {
	for _, order := range [3]int{
		compareNumeric(a.major, b.major),
		compareNumeric(a.minor, b.minor),
		compareNumeric(a.patch, b.patch),
	} {
		if order != 0 {
			return order
		}
	}
	return comparePrerelease(a.prerelease, b.prerelease)
}
