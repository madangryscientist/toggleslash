package evaluator

import "strings"

// semanticVersion is a parsed version; the numeric parts stay strings, so a part of any length compares
// without overflow.
type semanticVersion struct {
	major, minor, patch string
	prerelease          []string
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isIdentifierChar(c byte) bool {
	return isDigit(c) || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '-'
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

// identifiers splits on "." and requires every part to be non-empty ASCII letters, digits and "-".
func identifiers(s string) ([]string, bool) {
	parts := strings.Split(s, ".")
	for _, p := range parts {
		if p == "" {
			return nil, false
		}
		for i := 0; i < len(p); i++ {
			if !isIdentifierChar(p[i]) {
				return nil, false
			}
		}
	}
	return parts, true
}

// splitAt returns the text before and after the first sep, and whether there was one.
func splitAt(s string, sep byte) (string, string, bool) {
	if i := strings.IndexByte(s, sep); i >= 0 {
		return s[:i], s[i+1:], true
	}
	return s, "", false
}

func prereleaseIdentifiers(s string) ([]string, bool) {
	parts, ok := identifiers(s)
	if !ok {
		return nil, false
	}
	for _, p := range parts {
		if allDigits(p) && !isNumericPart(p) {
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

// parseSemanticVersion reads MAJOR.MINOR.PATCH[-prerelease][+build], or a short form (MAJOR, MAJOR.MINOR)
// that carries no suffix.
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

func sign(n int) int {
	if n < 0 {
		return -1
	}
	if n > 0 {
		return 1
	}
	return 0
}

// compareNumeric orders digit strings with no leading zero: the longer is larger, else byte order.
func compareNumeric(a, b string) int {
	if len(a) != len(b) {
		return sign(len(a) - len(b))
	}
	return strings.Compare(a, b)
}

func compareIdentifier(a, b string) int {
	an, bn := allDigits(a), allDigits(b)
	switch {
	case an && bn:
		return compareNumeric(a, b)
	case an:
		return -1
	case bn:
		return 1
	}
	return strings.Compare(a, b)
}

func comparePrerelease(a, b []string) int {
	// No prerelease is above any prerelease of the same core.
	if len(a) == 0 || len(b) == 0 {
		return sign(len(b) - len(a))
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		if c := compareIdentifier(a[i], b[i]); c != 0 {
			return c
		}
	}
	return sign(len(a) - len(b))
}

// compareSemanticVersions is -1, 0 or 1 by SemVer 2.0.0 section 11; build metadata is already gone.
func compareSemanticVersions(a, b semanticVersion) int {
	if c := compareNumeric(a.major, b.major); c != 0 {
		return c
	}
	if c := compareNumeric(a.minor, b.minor); c != 0 {
		return c
	}
	if c := compareNumeric(a.patch, b.patch); c != 0 {
		return c
	}
	return comparePrerelease(a.prerelease, b.prerelease)
}
