package evaluator

import "strings"

// version is a parsed semantic version. The numeric parts stay digit strings: nothing converts them
// to a number, so a part of any length compares correctly.
type version struct {
	major, minor, patch string
	pre                 []string
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return false
		}
	}
	return true
}

func isIdentifierChar(c byte) bool {
	return isDigit(c) || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '-'
}

// isNumericPart is "0" or digits with no leading zero.
func isNumericPart(s string) bool {
	return s != "" && allDigits(s) && (s == "0" || s[0] != '0')
}

// identifiers splits on "." and requires each part non-empty and made of ASCII letters, digits, "-".
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
	nums := strings.Split(s, ".")
	if len(nums) > 3 || (len(nums) < 3 && !short) {
		return nil, false
	}
	for _, n := range nums {
		if !isNumericPart(n) {
			return nil, false
		}
	}
	return nums, true
}

// parseVersion reads MAJOR.MINOR.PATCH[-prerelease][+build], or the short forms MAJOR and
// MAJOR.MINOR with no suffix.
func parseVersion(text string) (version, bool) {
	withoutBuild, build, hasBuild := strings.Cut(text, "+")
	if hasBuild {
		if _, ok := identifiers(build); !ok {
			return version{}, false
		}
	}
	core, pre, hasPre := strings.Cut(withoutBuild, "-")
	var parsedPre []string
	if hasPre {
		var ok bool
		if parsedPre, ok = prereleaseIdentifiers(pre); !ok {
			return version{}, false
		}
	}
	nums, ok := numericParts(core, !hasBuild && !hasPre)
	if !ok {
		return version{}, false
	}
	v := version{major: nums[0], minor: "0", patch: "0", pre: parsedPre}
	if len(nums) > 1 {
		v.minor = nums[1]
	}
	if len(nums) > 2 {
		v.patch = nums[2]
	}
	return v, true
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

// compareNumeric compares digit strings with no leading zero: the longer is larger, else by byte.
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
	if len(a) == 0 || len(b) == 0 {
		return sign(len(b) - len(a))
	}
	for i := 0; i < min(len(a), len(b)); i++ {
		if c := compareIdentifier(a[i], b[i]); c != 0 {
			return c
		}
	}
	return sign(len(a) - len(b))
}

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
	return comparePrerelease(a.pre, b.pre)
}
