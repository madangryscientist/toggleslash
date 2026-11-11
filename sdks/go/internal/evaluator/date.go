package evaluator

// Instants, to the "Dates" section of docs/evaluation.md. A hand-written walk over the bytes: no
// regular expression and no time.Parse. The result is Unix milliseconds as a float64.

func digits(s string, start, length int) (int, bool) {
	value := 0
	for i := start; i < start+length; i++ {
		if i >= len(s) || !isDigitByte(s[i]) {
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

// daysFromCivil is the days since 1970-01-01 in the proleptic Gregorian calendar. For years 1 to
// 9999 every value before the last subtraction is non-negative, so integer division is floor.
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
	if !ok1 || !ok2 || !ok3 {
		return 0, false
	}
	if year < 1 || month < 1 || month > 12 {
		return 0, false
	}
	if day < 1 || day > daysInMonth(year, month) {
		return 0, false
	}
	return daysFromCivil(year, month, day), true
}

func parseTime(s string) (int, bool) {
	hour, ok1 := digits(s, 11, 2)
	minute, ok2 := digits(s, 14, 2)
	second, ok3 := digits(s, 17, 2)
	if !ok1 || !ok2 || !ok3 {
		return 0, false
	}
	if hour > 23 || minute > 59 || second > 59 {
		return 0, false
	}
	return (hour*60+minute)*60 + second, true
}

// parseFraction reads the fraction that starts at start (a '.') and returns its milliseconds and
// where it ends; it fails without a digit.
func parseFraction(s string, start int) (ms, end int, ok bool) {
	end = start + 1
	for end < len(s) && isDigitByte(s[end]) {
		end++
	}
	if end == start+1 {
		return 0, 0, false
	}
	// The first three digits are the milliseconds; a shorter fraction is padded on the right.
	kept := min(end-start-1, 3)
	scale := [...]int{1, 100, 10, 1}[kept]
	value, _ := digits(s, start+1, kept)
	return value * scale, end, true
}

// parseOffset reads the offset Z, +HH:MM or -HH:MM that is all of s, in minutes east of UTC.
func parseOffset(s string) (int, bool) {
	if s == "Z" {
		return 0, true
	}
	if len(s) != 6 || s[3] != ':' || (s[0] != '+' && s[0] != '-') {
		return 0, false
	}
	hour, ok1 := digits(s, 1, 2)
	minute, ok2 := digits(s, 4, 2)
	if !ok1 || !ok2 || hour > 23 || minute > 59 {
		return 0, false
	}
	sign := 1
	if s[0] == '-' {
		sign = -1
	}
	return sign * (hour*60 + minute), true
}

func hasShape(s string) bool {
	return len(s) >= 20 && s[4] == '-' && s[7] == '-' && s[10] == 'T' && s[13] == ':' && s[16] == ':'
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// parseInstant reads YYYY-MM-DDTHH:MM:SS[.f+](Z|+HH:MM|-HH:MM) as Unix milliseconds.
func parseInstant(s string) (float64, bool) {
	// Only ASCII spells an instant; this also keeps byte length equal to the spec's length.
	if !isASCII(s) || !hasShape(s) {
		return 0, false
	}
	days, ok1 := parseDate(s)
	seconds, ok2 := parseTime(s)
	if !ok1 || !ok2 {
		return 0, false
	}
	ms, end := 0, 19
	if s[19] == '.' {
		var ok bool
		ms, end, ok = parseFraction(s, 19)
		if !ok {
			return 0, false
		}
	}
	offset, ok := parseOffset(s[end:])
	if !ok {
		return 0, false
	}
	total := int64(days)*86_400_000 + int64(seconds)*1000 + int64(ms) - int64(offset)*60_000
	return float64(total), true
}
