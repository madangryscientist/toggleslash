package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"

// digits reads length ASCII digits of s at start.
func digits(s string, start, length int) (int, bool) {
	if start+length > len(s) {
		return 0, false
	}
	v := 0
	for i := start; i < start+length; i++ {
		if !isDigitByte(s[i]) {
			return 0, false
		}
		v = v*10 + int(s[i]-'0')
	}
	return v, true
}

func isLeap(y int) bool { return y%4 == 0 && (y%100 != 0 || y%400 == 0) }

func daysInMonth(y, m int) int {
	switch m {
	case 2:
		if isLeap(y) {
			return 29
		}
		return 28
	case 4, 6, 9, 11:
		return 30
	}
	return 31
}

// daysFromCivil is the days since 1970-01-01 in the proleptic Gregorian calendar. For years 1 to 9999
// every value before the last subtraction is non-negative, so integer division agrees everywhere.
func daysFromCivil(year, month, day int) int {
	y := year
	if month <= 2 {
		y--
	}
	era := y / 400
	yoe := y - era*400
	mi := month + 9
	if month > 2 {
		mi = month - 3
	}
	doy := (153*mi+2)/5 + day - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return era*146097 + doe - 719468
}

func parseOffset(s string) (int, bool) {
	if s == "Z" {
		return 0, true
	}
	if len(s) != 6 || s[3] != ':' || (s[0] != '+' && s[0] != '-') {
		return 0, false
	}
	h, ok1 := digits(s, 1, 2)
	m, ok2 := digits(s, 4, 2)
	if !ok1 || !ok2 || h > 23 || m > 59 {
		return 0, false
	}
	if s[0] == '-' {
		return -(h*60 + m), true
	}
	return h*60 + m, true
}

// parseInstant reads YYYY-MM-DDTHH:MM:SS[.f+](Z|+HH:MM|-HH:MM) as Unix milliseconds.
func parseInstant(s string) (float64, bool) {
	if len(s) < 20 || s[4] != '-' || s[7] != '-' || s[10] != 'T' || s[13] != ':' || s[16] != ':' {
		return 0, false
	}
	year, ok1 := digits(s, 0, 4)
	month, ok2 := digits(s, 5, 2)
	day, ok3 := digits(s, 8, 2)
	hour, ok4 := digits(s, 11, 2)
	minute, ok5 := digits(s, 14, 2)
	second, ok6 := digits(s, 17, 2)
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 {
		return 0, false
	}
	if year < 1 || month < 1 || month > 12 || day < 1 || day > daysInMonth(year, month) {
		return 0, false
	}
	if hour > 23 || minute > 59 || second > 59 {
		return 0, false
	}
	end, ms := 19, 0
	if s[19] == '.' {
		end = 20
		for end < len(s) && isDigitByte(s[end]) {
			end++
		}
		if end == 20 {
			return 0, false
		}
		kept := end - 20
		if kept > 3 {
			kept = 3
		}
		ms, _ = digits(s, 20, kept)
		for i := kept; i < 3; i++ {
			ms *= 10
		}
	}
	offset, ok := parseOffset(s[end:])
	if !ok {
		return 0, false
	}
	days := daysFromCivil(year, month, day)
	total := int64(days)*86400000 + int64((hour*60+minute)*60+second)*1000 + int64(ms) - int64(offset)*60000
	return float64(total), true
}

// toInstant reads a finite number as Unix milliseconds and a string as an instant.
func toInstant(v jsonv.Value) (float64, bool) {
	if n, ok := v.Number(); ok {
		return n, v.IsFinite()
	}
	if s, ok := v.Str(); ok {
		return parseInstant(s)
	}
	return 0, false
}
