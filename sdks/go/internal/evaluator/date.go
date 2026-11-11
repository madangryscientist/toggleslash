package evaluator

// Instants, to docs/evaluation.md ("Dates"): a hand-written walk over the bytes, no Date.parse or time
// package. The value is Unix milliseconds as a float64.

func twoDigits(s string, i int) (int, bool) {
	if !isDigitByte(s[i]) || !isDigitByte(s[i+1]) {
		return 0, false
	}
	return int(s[i]-'0')*10 + int(s[i+1]-'0'), true
}

func daysInMonth(year, month int) int {
	switch month {
	case 1, 3, 5, 7, 8, 10, 12:
		return 31
	case 4, 6, 9, 11:
		return 30
	}
	if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
		return 29
	}
	return 28
}

// daysFromCivil is days since 1970-01-01 in the proleptic Gregorian calendar, for years 1 to 9999.
func daysFromCivil(year, month, day int) int64 {
	y := int64(year)
	if month <= 2 {
		y--
	}
	era := y / 400
	yoe := y - era*400
	mp := int64((month + 9) % 12)
	doy := (153*mp+2)/5 + int64(day) - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return era*146097 + doe - 719468
}

func parseDate(s string) (float64, bool) {
	n := len(s)
	if n < 20 || s[4] != '-' || s[7] != '-' || s[10] != 'T' || s[13] != ':' || s[16] != ':' {
		return 0, false
	}
	for i := range 4 {
		if !isDigitByte(s[i]) {
			return 0, false
		}
	}
	year := int(s[0]-'0')*1000 + int(s[1]-'0')*100 + int(s[2]-'0')*10 + int(s[3]-'0')
	month, ok1 := twoDigits(s, 5)
	day, ok2 := twoDigits(s, 8)
	hour, ok3 := twoDigits(s, 11)
	minute, ok4 := twoDigits(s, 14)
	second, ok5 := twoDigits(s, 17)
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 {
		return 0, false
	}
	if year < 1 || month < 1 || month > 12 || day < 1 || day > daysInMonth(year, month) ||
		hour > 23 || minute > 59 || second > 59 {
		return 0, false
	}
	i := 19
	fraction := 0
	if s[i] == '.' {
		i++
		start := i
		for i < n && isDigitByte(s[i]) {
			i++
		}
		if i == start {
			return 0, false
		}
		scale := 100
		for j := start; j < i && j < start+3; j++ {
			fraction += int(s[j]-'0') * scale
			scale /= 10
		}
	}
	offsetMinutes := 0
	rest := s[i:]
	switch {
	case rest == "Z":
	case len(rest) == 6 && (rest[0] == '+' || rest[0] == '-') && rest[3] == ':':
		oh, ok1 := twoDigits(rest, 1)
		om, ok2 := twoDigits(rest, 4)
		if !ok1 || !ok2 || oh > 23 || om > 59 {
			return 0, false
		}
		offsetMinutes = oh*60 + om
		if rest[0] == '-' {
			offsetMinutes = -offsetMinutes
		}
	default:
		return 0, false
	}
	ms := daysFromCivil(year, month, day)*86400000 +
		((int64(hour)*60+int64(minute))*60+int64(second))*1000 + int64(fraction) -
		int64(offsetMinutes)*60000
	return float64(ms), true
}
