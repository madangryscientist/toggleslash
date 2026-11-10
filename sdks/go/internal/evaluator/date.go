package evaluator

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// digitsAt reads length ASCII digits at start. The text is bytes; every position the format fixes is
// ASCII, so a multi-byte character anywhere fails a digit or separator check.
func digitsAt(s string, start, length int) (int, bool) {
	if start+length > len(s) {
		return 0, false
	}
	v := 0
	for i := start; i < start+length; i++ {
		if !isDigit(s[i]) {
			return 0, false
		}
		v = v*10 + int(s[i]-'0')
	}
	return v, true
}

func isLeapYear(y int) bool { return y%4 == 0 && (y%100 != 0 || y%400 == 0) }

func daysInMonth(y, m int) int {
	switch m {
	case 2:
		if isLeapYear(y) {
			return 29
		}
		return 28
	case 4, 6, 9, 11:
		return 30
	}
	return 31
}

// daysFromCivil is days since 1970-01-01 in the proleptic Gregorian calendar. For years 1 to 9999 every
// value before the last subtraction is non-negative, so floor and truncating division agree.
func daysFromCivil(year, month, day int) int {
	y := year
	if month <= 2 {
		y--
	}
	era := y / 400
	yoe := y - era*400
	mi := month - 3
	if month <= 2 {
		mi = month + 9
	}
	doy := (153*mi+2)/5 + day - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return era*146097 + doe - 719468
}

func parseDate(s string) (int, bool) {
	year, ok1 := digitsAt(s, 0, 4)
	month, ok2 := digitsAt(s, 5, 2)
	day, ok3 := digitsAt(s, 8, 2)
	if !ok1 || !ok2 || !ok3 || year < 1 || month < 1 || month > 12 {
		return 0, false
	}
	if day < 1 || day > daysInMonth(year, month) {
		return 0, false
	}
	return daysFromCivil(year, month, day), true
}

func parseTime(s string) (int, bool) {
	h, ok1 := digitsAt(s, 0, 2)
	m, ok2 := digitsAt(s, 3, 2)
	sec, ok3 := digitsAt(s, 6, 2)
	if !ok1 || !ok2 || !ok3 || h > 23 || m > 59 || sec > 59 {
		return 0, false
	}
	return (h*60+m)*60 + sec, true
}

// parseFraction reads the fraction that starts at start (a "."), as milliseconds, and where it ends.
func parseFraction(s string, start int) (ms, end int, ok bool) {
	end = start + 1
	for end < len(s) && isDigit(s[end]) {
		end++
	}
	if end == start+1 {
		return 0, 0, false
	}
	kept := end - start - 1
	if kept > 3 {
		kept = 3
	}
	v, _ := digitsAt(s, start+1, kept)
	scale := [4]int{1, 100, 10, 1}[kept]
	return v * scale, end, true
}

// parseOffset reads Z, +HH:MM or -HH:MM as all of s, in minutes east of UTC.
func parseOffset(s string) (int, bool) {
	if s == "Z" {
		return 0, true
	}
	if len(s) != 6 || (s[0] != '+' && s[0] != '-') || s[3] != ':' {
		return 0, false
	}
	h, ok1 := digitsAt(s, 1, 2)
	m, ok2 := digitsAt(s, 4, 2)
	if !ok1 || !ok2 || h > 23 || m > 59 {
		return 0, false
	}
	if s[0] == '-' {
		return -(h*60 + m), true
	}
	return h*60 + m, true
}

func hasShape(s string) bool {
	return len(s) >= 20 && s[4] == '-' && s[7] == '-' && s[10] == 'T' && s[13] == ':' && s[16] == ':'
}

// parseInstant reads YYYY-MM-DDTHH:MM:SS[.f+](Z|+HH:MM|-HH:MM) as Unix milliseconds.
func parseInstant(s string) (float64, bool) {
	if !hasShape(s) {
		return 0, false
	}
	days, ok := parseDate(s)
	if !ok {
		return 0, false
	}
	seconds, ok := parseTime(s[11:])
	if !ok {
		return 0, false
	}
	ms, end := 0, 19
	if s[19] == '.' {
		var fok bool
		if ms, end, fok = parseFraction(s, 19); !fok {
			return 0, false
		}
	}
	offset, ok := parseOffset(s[end:])
	if !ok {
		return 0, false
	}
	return float64(days)*86400000 + float64(seconds)*1000 + float64(ms) - float64(offset)*60000, true
}

// toInstant: a finite number is Unix milliseconds, a string is parsed, anything else is not an instant.
func toInstant(v jsonv.Value) (float64, bool) {
	if n, ok := v.AsNumber(); ok {
		if math.IsInf(n, 0) || math.IsNaN(n) {
			return 0, false
		}
		return n, true
	}
	if s, ok := v.AsString(); ok {
		return parseInstant(s)
	}
	return 0, false
}
