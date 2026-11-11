package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"

// Instants, to the "Dates" section of docs/evaluation.md. A hand-written walk over the string: no regular
// expression, no time.Parse and no library. The result is Unix milliseconds as a binary64 number.

// digits reads length ASCII digits at start as a number; false when any is not a digit or the text is short.
func digits(s string, start, length int) (int, bool) {
	if start < 0 || start+length > len(s) {
		return 0, false
	}
	value := 0
	for i := start; i < start+length; i++ {
		if !isDigitByte(s[i]) {
			return 0, false
		}
		value = value*10 + int(s[i]-'0')
	}
	return value, true
}

func isLeapYear(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

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

// daysFromCivil is the days since 1970-01-01 in the proleptic Gregorian calendar. For years 1 to 9999
// every value before the last subtraction is non-negative, so truncating and floor division agree.
func daysFromCivil(year, month, day int) int64 {
	y := int64(year)
	if month <= 2 {
		y--
	}
	era := y / 400
	yearOfEra := y - era*400
	monthIndex := int64(month - 3)
	if month <= 2 {
		monthIndex = int64(month + 9)
	}
	dayOfYear := (153*monthIndex+2)/5 + int64(day) - 1
	dayOfEra := yearOfEra*365 + yearOfEra/4 - yearOfEra/100 + dayOfYear
	return era*146097 + dayOfEra - 719468
}

// parseDate reads YYYY-MM-DD at the start of s as days since 1970-01-01.
func parseDate(s string) (int64, bool) {
	year, ok1 := digits(s, 0, 4)
	month, ok2 := digits(s, 5, 2)
	day, ok3 := digits(s, 8, 2)
	if !ok1 || !ok2 || !ok3 || year < 1 || month < 1 || month > 12 {
		return 0, false
	}
	if day < 1 || day > daysInMonth(year, month) {
		return 0, false
	}
	return daysFromCivil(year, month, day), true
}

// parseTime reads HH:MM:SS at the start of s as seconds since midnight.
func parseTime(s string) (int64, bool) {
	hour, ok1 := digits(s, 0, 2)
	minute, ok2 := digits(s, 3, 2)
	second, ok3 := digits(s, 6, 2)
	if !ok1 || !ok2 || !ok3 || hour > 23 || minute > 59 || second > 59 {
		return 0, false
	}
	return int64((hour*60+minute)*60 + second), true
}

// parseFraction reads the fraction that starts at start (a `.`): its milliseconds and where it ends.
func parseFraction(s string, start int) (int64, int, bool) {
	end := start + 1
	for end < len(s) && isDigitByte(s[end]) {
		end++
	}
	if end == start+1 {
		return 0, 0, false
	}
	// The first three digits are the milliseconds; a shorter fraction is padded on the right.
	kept := min(end-start-1, 3)
	value, _ := digits(s, start+1, kept)
	scale := [4]int{1, 100, 10, 1}[kept]
	return int64(value * scale), end, true
}

// parseOffset reads the offset `Z`, `+HH:MM` or `-HH:MM` that is all of s, in minutes east of UTC.
func parseOffset(s string) (int64, bool) {
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
	minutes := int64(hour*60 + minute)
	if s[0] == '-' {
		minutes = -minutes
	}
	return minutes, true
}

// hasShape checks the separators; the shortest form is "0001-01-01T00:00:00Z".
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
	var fractionMs int64
	end := 19
	if s[19] == '.' {
		var fraction int
		if fractionMs, fraction, ok = parseFraction(s, 19); !ok {
			return 0, false
		}
		end = fraction
	}
	offsetMinutes, ok := parseOffset(s[end:])
	if !ok {
		return 0, false
	}
	return float64(days*86_400_000 + seconds*1000 + fractionMs - offsetMinutes*60_000), true
}

// toInstant reads a finite number as Unix milliseconds and a string by parseInstant; anything else is
// not an instant.
func toInstant(v jsonvalue.Value) (float64, bool) {
	if n, ok := v.AsNumber(); ok {
		return n, v.IsFiniteNumber()
	}
	if s, ok := v.AsString(); ok {
		return parseInstant(s)
	}
	return 0, false
}
