package eval

import "strings"

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

// isNumericPart is "0" or digits with no leading zero.
func isNumericPart(s string) bool {
	return s != "" && allDigits(s) && (s == "0" || s[0] != '0')
}

// identifiers splits on "." and checks that none is empty and each is ASCII letters, digits and "-".
func identifiers(s string) ([]string, bool) {
	parts := strings.Split(s, ".")
	for _, p := range parts {
		if p == "" {
			return nil, false
		}
		for i := 0; i < len(p); i++ {
			if !isIdentifierByte(p[i]) {
				return nil, false
			}
		}
	}
	return parts, true
}

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
	nums := strings.Split(s, ".")
	if len(nums) > 3 || len(nums) < 3 && !short {
		return nil, false
	}
	for _, n := range nums {
		if !isNumericPart(n) {
			return nil, false
		}
	}
	return nums, true
}

func parseSemver(text string) (semver, bool) {
	withoutBuild, build, hasBuild := splitAt(text, '+')
	if hasBuild {
		if _, ok := identifiers(build); !ok {
			return semver{}, false
		}
	}
	core, pre, hasPre := splitAt(withoutBuild, '-')
	var preParts []string
	if hasPre {
		var ok bool
		if preParts, ok = prereleaseIdentifiers(pre); !ok {
			return semver{}, false
		}
	}
	nums, ok := numericParts(core, !hasBuild && !hasPre)
	if !ok {
		return semver{}, false
	}
	v := semver{major: nums[0], minor: "0", patch: "0", prerelease: preParts}
	if len(nums) > 1 {
		v.minor = nums[1]
	}
	if len(nums) > 2 {
		v.patch = nums[2]
	}
	return v, true
}

func sign(c int) int {
	switch {
	case c < 0:
		return -1
	case c > 0:
		return 1
	}
	return 0
}

// compareNumeric orders digit strings with no leading zero: the longer is larger, else by code unit.
func compareNumeric(a, b string) int {
	if len(a) != len(b) {
		return sign(len(a) - len(b))
	}
	return sign(strings.Compare(a, b))
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
	return sign(strings.Compare(a, b))
}

func comparePrerelease(a, b []string) int {
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
