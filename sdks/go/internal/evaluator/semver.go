package evaluator

// Semantic versions, to docs/evaluation.md ("Semantic versions"): a hand-written walk over the bytes, no
// regular expression. A non-ASCII byte is never part of a version, so a WTF-8 string needs no decoding.

type semver struct {
	major, minor, patch string
	pre                 []string
}

func isDigitByte(c byte) bool { return c >= '0' && c <= '9' }

func isIdentByte(c byte) bool {
	return isDigitByte(c) || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '-'
}

func allDigits(s string) bool {
	for i := range len(s) {
		if !isDigitByte(s[i]) {
			return false
		}
	}
	return len(s) > 0
}

func validIdentifiers(s string, noLeadingZero bool) ([]string, bool) {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i < len(s) && s[i] != '.' {
			if !isIdentByte(s[i]) {
				return nil, false
			}
			continue
		}
		id := s[start:i]
		if id == "" {
			return nil, false
		}
		if noLeadingZero && allDigits(id) && len(id) > 1 && id[0] == '0' {
			return nil, false
		}
		out = append(out, id)
		start = i + 1
	}
	return out, true
}

func numericPart(s string) bool {
	return allDigits(s) && (s == "0" || s[0] != '0')
}

func parseSemver(s string) (semver, bool) {
	var v semver
	rest := s
	hasSuffix := false
	for i := 0; i < len(rest); i++ {
		if rest[i] == '+' {
			if _, ok := validIdentifiers(rest[i+1:], false); !ok {
				return v, false
			}
			rest = rest[:i]
			hasSuffix = true
			break
		}
	}
	for i := 0; i < len(rest); i++ {
		if rest[i] == '-' {
			pre, ok := validIdentifiers(rest[i+1:], true)
			if !ok {
				return v, false
			}
			v.pre = pre
			rest = rest[:i]
			hasSuffix = true
			break
		}
	}
	var parts [3]string
	n := 0
	start := 0
	for i := 0; i <= len(rest); i++ {
		if i < len(rest) && rest[i] != '.' {
			continue
		}
		if n >= 3 {
			return v, false
		}
		parts[n] = rest[start:i]
		n++
		start = i + 1
	}
	if hasSuffix && n != 3 {
		return v, false
	}
	for i := range n {
		if !numericPart(parts[i]) {
			return v, false
		}
	}
	for i := n; i < 3; i++ {
		parts[i] = "0"
	}
	v.major, v.minor, v.patch = parts[0], parts[1], parts[2]
	return v, true
}

func compareNumeric(a, b string) int {
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
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
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		x, y := a.pre[i], b.pre[i]
		xn, yn := allDigits(x), allDigits(y)
		var c int
		switch {
		case xn && yn:
			c = compareNumeric(x, y)
		case xn:
			c = -1
		case yn:
			c = 1
		case x < y:
			c = -1
		case x > y:
			c = 1
		}
		if c != 0 {
			return c
		}
	}
	switch {
	case len(a.pre) < len(b.pre):
		return -1
	case len(a.pre) > len(b.pre):
		return 1
	}
	return 0
}
