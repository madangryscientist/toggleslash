package eval

import "github.com/madangryscientist/toggleslash/sdks/go/jsonval"

// Semantic versions, to docs/evaluation.md "Semantic versions". A hand written
// walk over the bytes: no regular expression and no library, and no numeric
// part is ever converted to a number.

type semanticVersion struct {
	major      string
	minor      string
	patch      string
	prerelease []string
}

func semverDigit(c byte) bool { return c >= '0' && c <= '9' }

func semverIdentifierChar(c byte) bool {
	return semverDigit(c) || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '-'
}

func allDigits(text string) bool {
	for i := 0; i < len(text); i++ {
		if !semverDigit(text[i]) {
			return false
		}
	}
	return true
}

// numericPart is `0` or ASCII digits with no leading zero.
func numericPart(text string) bool {
	return text != "" && allDigits(text) && (text == "0" || text[0] != '0')
}

// identifiers splits on `.`; every part must be non-empty and hold only
// identifiers' characters.
func identifiers(text string) ([]string, bool) {
	parts := splitString(text, '.')
	for _, part := range parts {
		if part == "" {
			return nil, false
		}
		for i := 0; i < len(part); i++ {
			if !semverIdentifierChar(part[i]) {
				return nil, false
			}
		}
	}
	return parts, true
}

// splitAt splits text at its first occurrence of sep. The second result is
// false when sep is not in text.
func splitAt(text string, sep byte) (string, string, bool) {
	for i := 0; i < len(text); i++ {
		if text[i] == sep {
			return text[:i], text[i+1:], true
		}
	}
	return text, "", false
}

func splitString(text string, sep byte) []string {
	out := make([]string, 0, 4)
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == sep {
			out = append(out, text[start:i])
			start = i + 1
		}
	}
	return append(out, text[start:])
}

func prereleaseIdentifiers(text string) ([]string, bool) {
	parts, ok := identifiers(text)
	if !ok {
		return nil, false
	}
	for _, part := range parts {
		if allDigits(part) && !numericPart(part) {
			return nil, false
		}
	}
	return parts, true
}

// numericParts reads `MAJOR.MINOR.PATCH`, or a short form (`MAJOR`,
// `MAJOR.MINOR`) when short is allowed.
func numericParts(text string, short bool) ([]string, bool) {
	numbers := splitString(text, '.')
	if len(numbers) > 3 || (len(numbers) < 3 && !short) {
		return nil, false
	}
	for _, number := range numbers {
		if !numericPart(number) {
			return nil, false
		}
	}
	return numbers, true
}

// parseSemanticVersion reads the version text spells, or reports that it is not
// one.
func parseSemanticVersion(text string) (semanticVersion, bool) {
	withoutBuild, build, hasBuild := splitAt(text, '+')
	if hasBuild {
		if _, ok := identifiers(build); !ok {
			return semanticVersion{}, false
		}
	}
	core, prerelease, hasPrerelease := splitAt(withoutBuild, '-')
	var parsedPrerelease []string
	if hasPrerelease {
		parsed, ok := prereleaseIdentifiers(prerelease)
		if !ok {
			return semanticVersion{}, false
		}
		parsedPrerelease = parsed
	}
	numbers, ok := numericParts(core, !hasBuild && !hasPrerelease)
	if !ok {
		return semanticVersion{}, false
	}
	version := semanticVersion{prerelease: parsedPrerelease}
	version.major = numbers[0]
	if len(numbers) > 1 {
		version.minor = numbers[1]
	} else {
		version.minor = "0"
	}
	if len(numbers) > 2 {
		version.patch = numbers[2]
	} else {
		version.patch = "0"
	}
	return version, true
}

// compareNumeric orders two digit strings with no leading zero: the longer is
// larger, then by code unit.
func compareNumeric(a, b string) int {
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return compareASCII(a, b)
}

func compareASCII(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func compareIdentifier(a, b string) int {
	aNumeric := allDigits(a)
	bNumeric := allDigits(b)
	switch {
	case aNumeric && bNumeric:
		return compareNumeric(a, b)
	case aNumeric:
		return -1
	case bNumeric:
		return 1
	default:
		return compareASCII(a, b)
	}
}

func comparePrerelease(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		// No prerelease is above any prerelease of the same core.
		switch {
		case len(b) > len(a):
			return 1
		case len(b) < len(a):
			return -1
		default:
			return 0
		}
	}
	shared := len(a)
	if len(b) < shared {
		shared = len(b)
	}
	for i := 0; i < shared; i++ {
		if order := compareIdentifier(a[i], b[i]); order != 0 {
			return order
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	default:
		return 0
	}
}

// compareSemanticVersions orders two versions by SemVer 2.0.0 section 11; build
// metadata is already gone.
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

func matchesSemver(op string, attribute, value jsonval.Value) bool {
	candidateText, ok := asString(attribute)
	if !ok {
		return false
	}
	wantedText, ok := asString(value)
	if !ok {
		return false
	}
	candidate, ok := parseSemanticVersion(candidateText)
	if !ok {
		return false
	}
	wanted, ok := parseSemanticVersion(wantedText)
	if !ok {
		return false
	}
	order := compareSemanticVersions(candidate, wanted)
	switch op {
	case "semverEq":
		return order == 0
	case "semverLt":
		return order < 0
	case "semverLte":
		return order <= 0
	case "semverGt":
		return order > 0
	default:
		return order >= 0
	}
}
