// Package wtf8 holds strings the way the SDK needs them: UTF-8 that may also carry a lone surrogate.
//
// A JSON string can spell a lone surrogate ("\ud800"), and the evaluation spec keeps it as it is, so it
// compares equal only to the same lone surrogate and never to U+FFFD. Go's own string conversions would
// replace it. A string here is WTF-8: valid UTF-8, except that a lone surrogate is stored as its 3-byte
// form (ED A0..BF 80..BF). A valid pair is always stored as one 4-byte code point, so two strings with the
// same UTF-16 code units have the same bytes.
package wtf8

import "unicode/utf8"

const (
	replacement = 0xFFFD
	surrogateLo = 0xD800
	surrogateHi = 0xDFFF
)

// IsSurrogate reports whether r is in U+D800 to U+DFFF.
func IsSurrogate(r rune) bool { return r >= surrogateLo && r <= surrogateHi }

// AppendRune appends r, writing a surrogate as its 3-byte form.
func AppendRune(dst []byte, r rune) []byte {
	if IsSurrogate(r) {
		return append(dst, 0xE0|byte(r>>12), 0x80|byte(r>>6)&0x3F, 0x80|byte(r)&0x3F)
	}
	return utf8.AppendRune(dst, r)
}

// DecodeRune reads the first code point of s, a lone surrogate included. It returns (utf8.RuneError, 1) for
// a byte that is not part of well-formed WTF-8, which a string of this package never holds.
func DecodeRune(s string) (rune, int) {
	if len(s) >= 3 && s[0] == 0xED && s[1] >= 0xA0 && s[1] <= 0xBF && s[2] >= 0x80 && s[2] <= 0xBF {
		return rune(s[0]&0x0F)<<12 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F), 3
	}
	return utf8.DecodeRuneInString(s)
}

// HasSurrogate reports whether s holds a lone surrogate.
func HasSurrogate(s string) bool {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == 0xED && s[i+1] >= 0xA0 {
			return true
		}
	}
	return false
}

// CodePoints returns the code points of s: a pair is one, a lone surrogate is itself.
func CodePoints(s string) []rune {
	out := make([]rune, 0, len(s))
	for i := 0; i < len(s); {
		r, n := DecodeRune(s[i:])
		out = append(out, r)
		i += n
	}
	return out
}

// FromCodePoints is the inverse of CodePoints.
func FromCodePoints(points []rune) string {
	out := make([]byte, 0, len(points))
	for _, r := range points {
		out = AppendRune(out, r)
	}
	return string(out)
}

// ToUTF8 returns s as valid UTF-8, with each lone surrogate replaced by U+FFFD. It is the form that is hashed.
func ToUTF8(s string) string {
	if !HasSurrogate(s) {
		return s
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		r, n := DecodeRune(s[i:])
		if IsSurrogate(r) {
			r = replacement
		}
		out = utf8.AppendRune(out, r)
		i += n
	}
	return string(out)
}

// Units returns the UTF-16 code units of s.
func Units(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	for i := 0; i < len(s); {
		r, n := DecodeRune(s[i:])
		i += n
		if r >= 0x10000 {
			r -= 0x10000
			out = append(out, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
			continue
		}
		out = append(out, uint16(r))
	}
	return out
}

// unitReader yields the UTF-16 code units of a string one at a time.
type unitReader struct {
	s       string
	pos     int
	pending uint16
	hasLow  bool
}

func (u *unitReader) next() (uint16, bool) {
	if u.hasLow {
		u.hasLow = false
		return u.pending, true
	}
	if u.pos >= len(u.s) {
		return 0, false
	}
	r, n := DecodeRune(u.s[u.pos:])
	u.pos += n
	if r >= 0x10000 {
		r -= 0x10000
		u.pending, u.hasLow = uint16(0xDC00+(r&0x3FF)), true
		return uint16(0xD800 + (r >> 10)), true
	}
	return uint16(r), true
}

// Compare orders a and b by UTF-16 code unit, which is how the spec orders strings: it is not byte order,
// which puts U+10000 and above before U+E000 to U+FFFF.
func Compare(a, b string) int {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	if i == len(a) && i == len(b) {
		return 0
	}
	// Back up to the start of the code point that holds the first difference.
	for i > 0 && (i < len(a) && a[i]&0xC0 == 0x80 || i < len(b) && b[i]&0xC0 == 0x80) {
		i--
	}
	x, y := unitReader{s: a[i:]}, unitReader{s: b[i:]}
	for {
		ux, okx := x.next()
		uy, oky := y.next()
		switch {
		case !okx && !oky:
			return 0
		case !okx:
			return -1
		case !oky:
			return 1
		case ux < uy:
			return -1
		case ux > uy:
			return 1
		}
	}
}

// Contains reports whether v occurs in a as a run of UTF-16 code units.
func Contains(a, v string) bool {
	if !HasSurrogate(v) {
		return indexBytes(a, v) >= 0
	}
	return indexUnits(Units(a), Units(v)) >= 0
}

// HasPrefix reports whether a begins with v, by UTF-16 code units.
func HasPrefix(a, v string) bool {
	if !HasSurrogate(v) {
		return len(a) >= len(v) && a[:len(v)] == v
	}
	ua, uv := Units(a), Units(v)
	return len(ua) >= len(uv) && equalUnits(ua[:len(uv)], uv)
}

// HasSuffix reports whether a ends with v, by UTF-16 code units.
func HasSuffix(a, v string) bool {
	if !HasSurrogate(v) {
		return len(a) >= len(v) && a[len(a)-len(v):] == v
	}
	ua, uv := Units(a), Units(v)
	return len(ua) >= len(uv) && equalUnits(ua[len(ua)-len(uv):], uv)
}

func indexBytes(a, v string) int {
	if v == "" {
		return 0
	}
	for i := 0; i+len(v) <= len(a); i++ {
		if a[i] == v[0] && a[i:i+len(v)] == v {
			return i
		}
	}
	return -1
}

func equalUnits(a, b []uint16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func indexUnits(a, v []uint16) int {
	for i := 0; i+len(v) <= len(a); i++ {
		if equalUnits(a[i:i+len(v)], v) {
			return i
		}
	}
	return -1
}

// FromUnits builds a string from UTF-16 code units: a valid pair becomes one code point and a lone
// surrogate stays one.
func FromUnits(units []uint16) string {
	out := make([]byte, 0, len(units))
	for i := 0; i < len(units); i++ {
		u := rune(units[i])
		if u >= 0xD800 && u <= 0xDBFF && i+1 < len(units) && units[i+1] >= 0xDC00 && units[i+1] <= 0xDFFF {
			u = 0x10000 + (u-0xD800)<<10 + rune(units[i+1]) - 0xDC00
			i++
		}
		out = AppendRune(out, u)
	}
	return string(out)
}

// Sanitize returns s as well-formed UTF-8: each ill-formed run is replaced the way the WHATWG decoder does.
// A string that is valid already is returned as it is. A Go string cannot carry a lone surrogate in a form
// this package would keep, because the bytes ED A0..BF are ill-formed UTF-8.
func Sanitize(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return DecodeWHATWG([]byte(s))
}

// DecodeWHATWG decodes bytes as UTF-8 the way the WHATWG Encoding Standard does: one U+FFFD for each maximal
// ill-formed subpart. It does not skip a byte order mark. The result is valid UTF-8.
func DecodeWHATWG(b []byte) string {
	out := make([]byte, 0, len(b))
	var (
		needed       int
		seen         int
		codePoint    rune
		lower, upper byte = 0x80, 0xBF
	)
	for i := 0; i < len(b); i++ {
		c := b[i]
		if needed == 0 {
			switch {
			case c <= 0x7F:
				out = append(out, c)
			case c >= 0xC2 && c <= 0xDF:
				needed, codePoint = 1, rune(c&0x1F)
			case c >= 0xE0 && c <= 0xEF:
				switch c {
				case 0xE0:
					lower = 0xA0
				case 0xED:
					upper = 0x9F
				}
				needed, codePoint = 2, rune(c&0x0F)
			case c >= 0xF0 && c <= 0xF4:
				switch c {
				case 0xF0:
					lower = 0x90
				case 0xF4:
					upper = 0x8F
				}
				needed, codePoint = 3, rune(c&0x07)
			default:
				out = utf8.AppendRune(out, replacement)
			}
			continue
		}
		if c < lower || c > upper {
			needed, seen, codePoint, lower, upper = 0, 0, 0, 0x80, 0xBF
			out = utf8.AppendRune(out, replacement)
			i--
			continue
		}
		lower, upper = 0x80, 0xBF
		codePoint = codePoint<<6 | rune(c&0x3F)
		seen++
		if seen == needed {
			out = utf8.AppendRune(out, codePoint)
			needed, seen, codePoint = 0, 0, 0
		}
	}
	if needed != 0 {
		out = utf8.AppendRune(out, replacement)
	}
	return string(out)
}
