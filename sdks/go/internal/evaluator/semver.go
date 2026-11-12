package evaluator

import "strings"

// semanticVersion is a parsed version. The numeric parts stay strings: nothing here converts one to a number.
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

// identifiers splits on '.', and every part must be non-empty and made of ASCII letters, digits and '-'.
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

func splitAt(s string, sep byte) (before, after string, found bool) {
	i := strings.IndexByte(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+1:], true
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

// parseSemanticVersion reads MAJOR.MINOR.PATCH with an optional -prerelease and +build, or the short forms
// MAJOR and MAJOR.MINOR with no suffix. It reports false for anything else.
func parseSemanticVersion(text string) (semanticVersion, bool) {
	withoutBuild, build, hasBuild := splitAt(text, '+')
	if hasBuild {
		if _, ok := identifiers(build); !ok {
			return semanticVersion{}, false
		}
	}
	core, pre, hasPre := splitAt(withoutBuild, '-')
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

func sign(order int) int {
	if order < 0 {
		return -1
	}
	if order > 0 {
		return 1
	}
	return 0
}

// compareNumeric orders digit strings with no leading zero: the longer is larger, else by code unit.
func compareNumeric(a, b string) int {
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return sign(strings.Compare(a, b))
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
	return sign(strings.Compare(a, b))
}

func comparePrerelease(a, b []string) int {
	// No prerelease is above any prerelease of the same core.
	if len(a) == 0 || len(b) == 0 {
		return sign(len(b) - len(a))
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		if order := compareIdentifier(a[i], b[i]); order != 0 {
			return order
		}
	}
	return sign(len(a) - len(b))
}

// compareSemanticVersions is -1, 0 or 1 by SemVer 2.0.0 section 11. Build metadata is already gone.
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
