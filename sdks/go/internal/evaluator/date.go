package evaluator

// Instants, to the "Dates" section of docs/evaluation.md: a hand-written walk over the string, no
// time.Parse, so the calendar and the rules are the spec's. The result is Unix milliseconds as a
// float64.

func digits(s string, start, length int) (int, bool) {
	if start < 0 || start+length > len(s) {
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

// daysFromCivil is the days since 1970-01-01 in the proleptic Gregorian calendar.
func daysFromCivil(year, month, day int) int {
	y := year
	if month <= 2 {
		y--
	}
	era := y / 400 // y >= 0 for years 1 to 9999
	yoe := y - era*400
	mp := month + 9
	if month > 2 {
		mp = month - 3
	}
	doy := (153*mp+2)/5 + day - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return era*146097 + doe - 719468
}

func parseDate(s string) (int, bool) {
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

func parseTime(s string) (int, bool) {
	h, ok1 := digits(s, 0, 2)
	m, ok2 := digits(s, 3, 2)
	sec, ok3 := digits(s, 6, 2)
	if !ok1 || !ok2 || !ok3 || h > 23 || m > 59 || sec > 59 {
		return 0, false
	}
	return (h*60+m)*60 + sec, true
}

// parseFraction reads the fraction that starts at start (a "."): the milliseconds and where it ends.
func parseFraction(s string, start int) (ms, end int, ok bool) {
	end = start + 1
	for end < len(s) && isDigit(s[end]) {
		end++
	}
	if end == start+1 {
		return 0, 0, false
	}
	kept := min(end-start-1, 3)
	v, _ := digits(s, start+1, kept)
	return v * [...]int{1, 100, 10, 1}[kept], end, true
}

// parseOffset reads Z, +HH:MM or -HH:MM as all of s, in minutes east of UTC.
func parseOffset(s string) (int, bool) {
	if s == "Z" {
		return 0, true
	}
	if len(s) != 6 || (s[0] != '+' && s[0] != '-') || s[3] != ':' {
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

func hasShape(s string) bool {
	return len(s) >= 20 && s[4] == '-' && s[7] == '-' && s[10] == 'T' && s[13] == ':' && s[16] == ':'
}

// parseInstant reads YYYY-MM-DDTHH:MM:SS[.f+](Z|+HH:MM|-HH:MM) as Unix milliseconds.
func parseInstant(s string) (float64, bool) {
	if !hasShape(s) {
		return 0, false
	}
	days, ok1 := parseDate(s)
	secs, ok2 := parseTime(s[11:])
	if !ok1 || !ok2 {
		return 0, false
	}
	ms, end := 0, 19
	if s[19] == '.' {
		var ok bool
		if ms, end, ok = parseFraction(s, 19); !ok {
			return 0, false
		}
	}
	off, ok := parseOffset(s[end:])
	if !ok {
		return 0, false
	}
	return float64(days)*86400000 + float64(secs)*1000 + float64(ms) - float64(off)*60000, true
}
