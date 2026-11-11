package evaluator

import "strings"

// semver is a parsed version. The numeric parts stay digit strings: nothing is converted to a number.
type semver struct {
	major, minor, patch string
	pre                 []string
}

func isDigitByte(c byte) bool { return c >= '0' && c <= '9' }

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

func isIdentByte(c byte) bool {
	return isDigitByte(c) || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '-'
}

// identifiers splits on "." and checks that none is empty and each is ASCII letters, digits and "-".
func identifiers(s string) ([]string, bool) {
	parts := strings.Split(s, ".")
	for _, p := range parts {
		if p == "" {
			return nil, false
		}
		for i := 0; i < len(p); i++ {
			if !isIdentByte(p[i]) {
				return nil, false
			}
		}
	}
	return parts, true
}

func splitAt(s string, sep byte) (string, string, bool) {
	i := strings.IndexByte(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+1:], true
}

func parseSemver(s string) (semver, bool) {
	withoutBuild, build, hasBuild := splitAt(s, '+')
	if hasBuild {
		if _, ok := identifiers(build); !ok {
			return semver{}, false
		}
	}
	core, prerelease, hasPre := splitAt(withoutBuild, '-')
	var pre []string
	if hasPre {
		parts, ok := identifiers(prerelease)
		if !ok {
			return semver{}, false
		}
		for _, p := range parts {
			if allDigits(p) && !isNumericPart(p) {
				return semver{}, false
			}
		}
		pre = parts
	}
	nums := strings.Split(core, ".")
	short := !hasBuild && !hasPre
	if len(nums) > 3 || (len(nums) < 3 && !short) {
		return semver{}, false
	}
	for _, n := range nums {
		if !isNumericPart(n) {
			return semver{}, false
		}
	}
	v := semver{major: nums[0], minor: "0", patch: "0", pre: pre}
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

// compareNumeric orders digit strings with no leading zero: the longer is larger, else by code unit.
func compareNumeric(a, b string) int {
	if len(a) != len(b) {
		return sign(len(a) - len(b))
	}
	return strings.Compare(a, b)
}

func compareIdent(a, b string) int {
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

func comparePre(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return sign(len(b) - len(a))
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		if c := compareIdent(a[i], b[i]); c != 0 {
			return c
		}
	}
	return sign(len(a) - len(b))
}

// compareSemver orders two versions by SemVer 2.0.0 section 11; build metadata is already gone.
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
	return comparePre(a.pre, b.pre)
}
