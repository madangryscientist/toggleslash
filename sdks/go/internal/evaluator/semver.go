package evaluator

import "strings"

// version is a parsed semantic version. The numeric parts stay digit strings, so nothing is ever converted to
// a number.
type version struct {
	major, minor, patch string
	prerelease          []string
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isIdentifierChar(c byte) bool {
	return isDigit(c) || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '-'
}

func allDigits(s string) bool {
	for i := range len(s) {
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

// identifiers splits dot-separated identifiers of ASCII letters, digits and "-", none empty.
func identifiers(s string) ([]string, bool) {
	parts := strings.Split(s, ".")
	for _, part := range parts {
		if part == "" {
			return nil, false
		}
		for i := range len(part) {
			if !isIdentifierChar(part[i]) {
				return nil, false
			}
		}
	}
	return parts, true
}

func splitAt(s string, sep byte) (before, after string, found bool) {
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

// parseVersion reads full SemVer 2.0.0 and the short forms MAJOR and MAJOR.MINOR; ok is false for anything else.
func parseVersion(text string) (version, bool) {
	withoutBuild, build, hasBuild := splitAt(text, '+')
	if hasBuild {
		if _, ok := identifiers(build); !ok {
			return version{}, false
		}
	}
	core, pre, hasPre := splitAt(withoutBuild, '-')
	var prerelease []string
	if hasPre {
		var ok bool
		if prerelease, ok = prereleaseIdentifiers(pre); !ok {
			return version{}, false
		}
	}
	numbers, ok := numericParts(core, !hasBuild && !hasPre)
	if !ok {
		return version{}, false
	}
	v := version{major: numbers[0], minor: "0", patch: "0", prerelease: prerelease}
	if len(numbers) > 1 {
		v.minor = numbers[1]
	}
	if len(numbers) > 2 {
		v.patch = numbers[2]
	}
	return v, true
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
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
	return compareStrings(a, b)
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
	return compareStrings(a, b)
}

func comparePrerelease(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		// No prerelease is above any prerelease of the same core.
		switch {
		case len(a) == len(b):
			return 0
		case len(a) == 0:
			return 1
		}
		return -1
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		if c := compareIdentifier(a[i], b[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

// compareVersions is -1, 0 or 1 by SemVer 2.0.0 section 11. Build metadata is already gone.
func compareVersions(a, b version) int {
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
