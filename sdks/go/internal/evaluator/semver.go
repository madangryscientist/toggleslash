package evaluator

import "strings"

// Semantic versions, to "Semantic versions" in docs/evaluation.md: a hand-written walk over the string, no
// regular expression and no library. A numeric part is never converted to a number.

type semver struct {
	major, minor, patch string
	prerelease          []string
}

func isDigitByte(c byte) bool { return c >= '0' && c <= '9' }

func isIdentifierByte(c byte) bool {
	return isDigitByte(c) || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '-'
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isDigitByte(s[i]) {
			return false
		}
	}
	return true
}

// isNumericPart is 0, or digits with no leading zero.
func isNumericPart(s string) bool {
	return s != "" && allDigits(s) && (s == "0" || s[0] != '0')
}

// identifiers are dot separated, none empty, each of ASCII letters, digits and "-".
func identifiers(s string) ([]string, bool) {
	parts := strings.Split(s, ".")
	for _, part := range parts {
		if part == "" {
			return nil, false
		}
		for i := 0; i < len(part); i++ {
			if !isIdentifierByte(part[i]) {
				return nil, false
			}
		}
	}
	return parts, true
}

// splitAt is the text before and after the first sep, and whether there was one.
func splitAt(s string, sep byte) (string, string, bool) {
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

// parseSemver is the version s spells, or nil.
func parseSemver(s string) *semver {
	withoutBuild, build, hasBuild := splitAt(s, '+')
	if hasBuild {
		if _, ok := identifiers(build); !ok {
			return nil
		}
	}
	core, pre, hasPre := splitAt(withoutBuild, '-')
	var prerelease []string
	if hasPre {
		var ok bool
		if prerelease, ok = prereleaseIdentifiers(pre); !ok {
			return nil
		}
	}
	numbers, ok := numericParts(core, !hasBuild && !hasPre)
	if !ok {
		return nil
	}
	v := &semver{major: numbers[0], minor: "0", patch: "0", prerelease: prerelease}
	if len(numbers) > 1 {
		v.minor = numbers[1]
	}
	if len(numbers) > 2 {
		v.patch = numbers[2]
	}
	return v
}

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
	for i := 0; i < min(len(a), len(b)); i++ {
		if order := compareIdentifier(a[i], b[i]); order != 0 {
			return order
		}
	}
	return sign(len(a) - len(b))
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

// compareSemver is -1, 0 or 1 by SemVer 2.0.0 section 11.
func compareSemver(a, b *semver) int {
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
