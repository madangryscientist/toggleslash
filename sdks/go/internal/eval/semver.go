package eval

import "strings"

// Semantic versions, to "Semantic versions" in docs/evaluation.md: a hand-written walk over the bytes (every
// valid character is ASCII, so a multi-byte character is simply invalid). Nothing converts a numeric part
// to a number.

type semver struct {
	major, minor, patch string
	prerelease          []string
}

func isAllDigits(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// isNumericPart is "0" or digits with no leading zero.
func isNumericPart(s string) bool {
	return s != "" && isAllDigits(s) && (s == "0" || s[0] != '0')
}

func isIdentifierByte(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '-'
}

// identifiers splits dot-separated identifiers, none empty, each of ASCII letters, digits and `-`.
func identifiers(s string) ([]string, bool) {
	parts := strings.Split(s, ".")
	for _, part := range parts {
		if part == "" {
			return nil, false
		}
		for i := range len(part) {
			if !isIdentifierByte(part[i]) {
				return nil, false
			}
		}
	}
	return parts, true
}

func parseSemver(text string) (semver, bool) {
	withoutBuild, build, hasBuild := strings.Cut(text, "+")
	if hasBuild {
		if _, ok := identifiers(build); !ok {
			return semver{}, false
		}
	}
	core, pre, hasPre := strings.Cut(withoutBuild, "-")
	var prerelease []string
	if hasPre {
		parts, ok := identifiers(pre)
		if !ok {
			return semver{}, false
		}
		for _, part := range parts {
			if isAllDigits(part) && !isNumericPart(part) {
				return semver{}, false
			}
		}
		prerelease = parts
	}
	numbers := strings.Split(core, ".")
	short := !hasBuild && !hasPre
	if len(numbers) > 3 || (len(numbers) < 3 && !short) {
		return semver{}, false
	}
	for _, n := range numbers {
		if !isNumericPart(n) {
			return semver{}, false
		}
	}
	v := semver{major: numbers[0], minor: "0", patch: "0", prerelease: prerelease}
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

// compareNumeric compares digit strings with no leading zero: the longer is larger, else by byte.
func compareNumeric(a, b string) int {
	if len(a) != len(b) {
		return sign(len(a) - len(b))
	}
	return strings.Compare(a, b)
}

func compareIdentifier(a, b string) int {
	an, bn := isAllDigits(a), isAllDigits(b)
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
	for i := range min(len(a), len(b)) {
		if c := compareIdentifier(a[i], b[i]); c != 0 {
			return c
		}
	}
	return sign(len(a) - len(b))
}

// compareSemver returns -1, 0 or 1 by SemVer 2.0.0 section 11; build metadata is already gone.
func compareSemver(a, b semver) int {
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
