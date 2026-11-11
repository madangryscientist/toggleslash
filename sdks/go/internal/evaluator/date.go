package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"

// digits reads count ASCII digits at start; ok is false when any is not one or the text is too short.
func digits(s string, start, count int) (int, bool) {
	if start+count > len(s) {
		return 0, false
	}
	v := 0
	for i := start; i < start+count; i++ {
		if !isDigit(s[i]) {
			return 0, false
		}
		v = v*10 + int(s[i]-'0')
	}
	return v, true
}

func isLeapYear(year int) bool { return year%4 == 0 && (year%100 != 0 || year%400 == 0) }

func daysInMonth(year, month int) int {
	switch month {
	case 2:
		if isLeapYear(year) {
			return 29
		}
		return 28
	case 4, 6, 9, 11:
		return 30
	}
	return 31
}

// daysFromCivil is the days since 1970-01-01 in the proleptic Gregorian calendar. For years 1 to 9999 every
// value before the last subtraction is non-negative, so truncating division is floor division.
func daysFromCivil(year, month, day int) int {
	y := year
	if month <= 2 {
		y--
	}
	era := y / 400
	yearOfEra := y - era*400
	monthIndex := month + 9
	if month > 2 {
		monthIndex = month - 3
	}
	dayOfYear := (153*monthIndex+2)/5 + day - 1
	dayOfEra := yearOfEra*365 + yearOfEra/4 - yearOfEra/100 + dayOfYear
	return era*146097 + dayOfEra - 719468
}

func parseDate(s string) (int, bool) {
	year, ok1 := digits(s, 0, 4)
	month, ok2 := digits(s, 5, 2)
	day, ok3 := digits(s, 8, 2)
	if !ok1 || !ok2 || !ok3 || year < 1 || month < 1 || month > 12 || day < 1 || day > daysInMonth(year, month) {
		return 0, false
	}
	return daysFromCivil(year, month, day), true
}

func parseTime(s string) (int, bool) {
	hour, ok1 := digits(s, 0, 2)
	minute, ok2 := digits(s, 3, 2)
	second, ok3 := digits(s, 6, 2)
	if !ok1 || !ok2 || !ok3 || hour > 23 || minute > 59 || second > 59 {
		return 0, false
	}
	return (hour*60+minute)*60 + second, true
}

// parseFraction reads the fraction that starts at start (a "."): its milliseconds and where it ends.
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
	v, _ := digits(s, start+1, kept)
	scale := [...]int{1, 100, 10, 1}[kept]
	return v * scale, end, true
}

// parseOffset reads an offset that is all of s: Z, +HH:MM or -HH:MM, as minutes east of UTC.
func parseOffset(s string) (int, bool) {
	if s == "Z" {
		return 0, true
	}
	if len(s) != 6 || (s[0] != '+' && s[0] != '-') || s[3] != ':' {
		return 0, false
	}
	hour, ok1 := digits(s, 1, 2)
	minute, ok2 := digits(s, 4, 2)
	if !ok1 || !ok2 || hour > 23 || minute > 59 {
		return 0, false
	}
	if s[0] == '-' {
		return -(hour*60 + minute), true
	}
	return hour*60 + minute, true
}

func hasShape(s string) bool {
	return len(s) >= 20 && s[4] == '-' && s[7] == '-' && s[10] == 'T' && s[13] == ':' && s[16] == ':'
}

// parseInstant reads YYYY-MM-DDTHH:MM:SS[.f+](Z|+HH:MM|-HH:MM) as Unix milliseconds.
func parseInstant(s string) (float64, bool) {
	if !hasShape(s) {
		return 0, false
	}
	days, ok1 := parseDate(s)
	seconds, ok2 := parseTime(s[11:])
	if !ok1 || !ok2 {
		return 0, false
	}
	fractionMs, end := 0, 19
	if s[19] == '.' {
		var ok bool
		if fractionMs, end, ok = parseFraction(s, 19); !ok {
			return 0, false
		}
	}
	offset, ok := parseOffset(s[end:])
	if !ok {
		return 0, false
	}
	ms := int64(days)*86400000 + int64(seconds)*1000 + int64(fractionMs) - int64(offset)*60000
	return float64(ms), true
}

// toInstant reads a finite number as Unix milliseconds and a string in the strict form as the instant it spells.
func toInstant(v jsonv.Value) (float64, bool) {
	if n, ok := v.AsNumber(); ok {
		return n, jsonv.IsFinite(n)
	}
	if s, ok := v.AsString(); ok {
		return parseInstant(s)
	}
	return 0, false
}
