package eval

// Instants, to the "Dates" section of docs/evaluation.md. A hand-written walk over the string: no regular
// expression, no time package and no library. The result is Unix milliseconds as a binary64 number.
//
// The walk reads bytes. Every position it checks must hold an ASCII digit or separator, so a string with a
// non-ASCII character in the first 19 code units fails either way, and the fraction and the offset are
// ASCII too.

// digitsAt reads length ASCII digits at start; ok is false when any is missing.
func digitsAt(text string, start, length int) (int, bool) {
	if start < 0 || start+length > len(text) {
		return 0, false
	}
	value := 0
	for i := start; i < start+length; i++ {
		if !isDigit(text[i]) {
			return 0, false
		}
		value = value*10 + int(text[i]-'0')
	}
	return value, true
}

func isLeapYear(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

func daysInMonth(year, month int) int {
	if month == 2 {
		if isLeapYear(year) {
			return 29
		}
		return 28
	}
	if month == 4 || month == 6 || month == 9 || month == 11 {
		return 30
	}
	return 31
}

// daysFromCivil is the days since 1970-01-01 in the proleptic Gregorian calendar. For years 1 to 9999
// every value before the last subtraction is non-negative, so floor and truncating division agree.
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

// parseDate reads YYYY-MM-DD at the start of text as days since 1970-01-01.
func parseDate(text string) (int, bool) {
	year, ok1 := digitsAt(text, 0, 4)
	month, ok2 := digitsAt(text, 5, 2)
	day, ok3 := digitsAt(text, 8, 2)
	if !ok1 || !ok2 || !ok3 || year < 1 || month < 1 || month > 12 {
		return 0, false
	}
	if day < 1 || day > daysInMonth(year, month) {
		return 0, false
	}
	return daysFromCivil(year, month, day), true
}

// parseTime reads HH:MM:SS at the start of text as seconds since midnight.
func parseTime(text string) (int, bool) {
	hour, ok1 := digitsAt(text, 0, 2)
	minute, ok2 := digitsAt(text, 3, 2)
	second, ok3 := digitsAt(text, 6, 2)
	if !ok1 || !ok2 || !ok3 || hour > 23 || minute > 59 || second > 59 {
		return 0, false
	}
	return (hour*60+minute)*60 + second, true
}

// parseFraction reads the fraction that starts at start (a `.`) as milliseconds, and where it ends.
func parseFraction(text string, start int) (ms, end int, ok bool) {
	end = start + 1
	for end < len(text) && isDigit(text[end]) {
		end++
	}
	if end == start+1 {
		return 0, 0, false
	}
	// The first three digits are the milliseconds; a shorter fraction is padded on the right.
	kept := min(end-start-1, 3)
	scale := [...]int{1, 100, 10, 1}[kept]
	v, _ := digitsAt(text, start+1, kept)
	return v * scale, end, true
}

// parseOffset reads the offset `Z`, `+HH:MM` or `-HH:MM` that is all of text, in minutes east of UTC.
func parseOffset(text string) (int, bool) {
	if text == "Z" {
		return 0, true
	}
	if len(text) != 6 || (text[0] != '+' && text[0] != '-') || text[3] != ':' {
		return 0, false
	}
	hour, ok1 := digitsAt(text, 1, 2)
	minute, ok2 := digitsAt(text, 4, 2)
	if !ok1 || !ok2 || hour > 23 || minute > 59 {
		return 0, false
	}
	if text[0] == '-' {
		return -(hour*60 + minute), true
	}
	return hour*60 + minute, true
}

// hasShape checks the separators in place; the shortest form is "0001-01-01T00:00:00Z".
func hasShape(text string) bool {
	return len(text) >= 20 && text[4] == '-' && text[7] == '-' && text[10] == 'T' && text[13] == ':' && text[16] == ':'
}

// parseInstant reads YYYY-MM-DDTHH:MM:SS[.f+](Z|+HH:MM|-HH:MM) as Unix milliseconds.
func parseInstant(text string) (float64, bool) {
	if !hasShape(text) {
		return 0, false
	}
	days, okDate := parseDate(text)
	seconds, okTime := parseTime(text[11:])
	if !okDate || !okTime {
		return 0, false
	}
	ms, end := 0, 19
	if text[19] == '.' {
		var ok bool
		if ms, end, ok = parseFraction(text, 19); !ok {
			return 0, false
		}
	}
	offset, ok := parseOffset(text[end:])
	if !ok {
		return 0, false
	}
	total := int64(days)*86_400_000 + int64(seconds)*1000 + int64(ms) - int64(offset)*60_000
	return float64(total), true
}
