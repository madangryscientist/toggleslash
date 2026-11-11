package eval

// Instants, to "Dates" in docs/evaluation.md: a hand-written walk over the bytes. No time package parsing
// is used, because each parser brings its own idea of what is valid. The result is Unix milliseconds as a
// float64 (every result is an integer below 2^53).

func digitsAt(s string, start, length int) (int, bool) {
	if start+length > len(s) {
		return 0, false
	}
	v := 0
	for i := start; i < start+length; i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		v = v*10 + int(c-'0')
	}
	return v, true
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
// every value before the last subtraction is non-negative, so integer division is floor division.
func daysFromCivil(year, month, day int) int {
	y := year
	if month <= 2 {
		y--
	}
	era := y / 400
	yearOfEra := y - era*400
	monthIndex := month - 3
	if month <= 2 {
		monthIndex = month + 9
	}
	dayOfYear := (153*monthIndex+2)/5 + day - 1
	dayOfEra := yearOfEra*365 + yearOfEra/4 - yearOfEra/100 + dayOfYear
	return era*146097 + dayOfEra - 719468
}

func parseDate(s string) (int, bool) {
	year, ok1 := digitsAt(s, 0, 4)
	month, ok2 := digitsAt(s, 5, 2)
	day, ok3 := digitsAt(s, 8, 2)
	if !ok1 || !ok2 || !ok3 || year < 1 || month < 1 || month > 12 || day < 1 || day > daysInMonth(year, month) {
		return 0, false
	}
	return daysFromCivil(year, month, day), true
}

func parseTime(s string) (int, bool) {
	hour, ok1 := digitsAt(s, 11, 2)
	minute, ok2 := digitsAt(s, 14, 2)
	second, ok3 := digitsAt(s, 17, 2)
	if !ok1 || !ok2 || !ok3 || hour > 23 || minute > 59 || second > 59 {
		return 0, false
	}
	return (hour*60+minute)*60 + second, true
}

// parseOffset reads Z, +HH:MM or -HH:MM that is all of s, in minutes east of UTC.
func parseOffset(s string) (int, bool) {
	if s == "Z" {
		return 0, true
	}
	if len(s) != 6 || s[3] != ':' || (s[0] != '+' && s[0] != '-') {
		return 0, false
	}
	hour, ok1 := digitsAt(s, 1, 2)
	minute, ok2 := digitsAt(s, 4, 2)
	if !ok1 || !ok2 || hour > 23 || minute > 59 {
		return 0, false
	}
	if s[0] == '-' {
		return -(hour*60 + minute), true
	}
	return hour*60 + minute, true
}

// parseInstant reads YYYY-MM-DDTHH:MM:SS[.f+](Z|+HH:MM|-HH:MM) as Unix milliseconds.
func parseInstant(s string) (float64, bool) {
	if len(s) < 20 || s[4] != '-' || s[7] != '-' || s[10] != 'T' || s[13] != ':' || s[16] != ':' {
		return 0, false
	}
	days, ok := parseDate(s)
	if !ok {
		return 0, false
	}
	seconds, ok := parseTime(s)
	if !ok {
		return 0, false
	}
	end, fractionMs := 19, 0
	if s[19] == '.' {
		end = 20
		for end < len(s) && s[end] >= '0' && s[end] <= '9' {
			end++
		}
		if end == 20 {
			return 0, false
		}
		kept := min(end-20, 3)
		v, _ := digitsAt(s, 20, kept)
		fractionMs = v * [4]int{1, 100, 10, 1}[kept]
	}
	offset, ok := parseOffset(s[end:])
	if !ok {
		return 0, false
	}
	ms := int64(days)*86400000 + int64(seconds)*1000 + int64(fractionMs) - int64(offset)*60000
	return float64(ms), true
}
