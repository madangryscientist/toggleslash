package toggleslash

// Instants, to the "Dates" section of docs/evaluation.md. A hand-written walk over the string: no regular
// expression, no time package and no date library, because each brings its own idea of what is valid.

func digitsAt(text string, start, length int) (int, bool) {
	value := 0
	for i := start; i < start+length; i++ {
		if i >= len(text) || !isDigitChar(text[i]) {
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
	switch month {
	case 4, 6, 9, 11:
		return 30
	}
	return 31
}

// daysFromCivil is the "days from civil" algorithm: days since 1970-01-01 in the proleptic Gregorian
// calendar, in integer arithmetic.
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

func parseDate(text string) (int, bool) {
	year, okYear := digitsAt(text, 0, 4)
	month, okMonth := digitsAt(text, 5, 2)
	day, okDay := digitsAt(text, 8, 2)
	if !okYear || !okMonth || !okDay {
		return 0, false
	}
	if year < 1 || month < 1 || month > 12 || day < 1 || day > daysInMonth(year, month) {
		return 0, false
	}
	return daysFromCivil(year, month, day), true
}

func parseTimeOfDay(text string) (int, bool) {
	hour, okHour := digitsAt(text, 0, 2)
	minute, okMinute := digitsAt(text, 3, 2)
	second, okSecond := digitsAt(text, 6, 2)
	if !okHour || !okMinute || !okSecond {
		return 0, false
	}
	if hour > 23 || minute > 59 || second > 59 {
		return 0, false
	}
	return (hour*60+minute)*60 + second, true
}

// parseFraction reads "." and one or more digits at start, keeps the first three, and answers the
// milliseconds and the index just past the fraction.
func parseFraction(text string, start int) (int, int, bool) {
	end := start + 1
	for end < len(text) && isDigitChar(text[end]) {
		end++
	}
	if end == start+1 {
		return 0, 0, false
	}
	kept := end - start - 1
	if kept > 3 {
		kept = 3
	}
	scale := [...]int{1, 100, 10, 1}[kept]
	value, ok := digitsAt(text, start+1, kept)
	if !ok {
		return 0, 0, false
	}
	return value * scale, end, true
}

func parseOffset(text string) (int, bool) {
	if text == "Z" {
		return 0, true
	}
	if len(text) != 6 || (text[0] != '+' && text[0] != '-') || text[3] != ':' {
		return 0, false
	}
	sign := 1
	if text[0] == '-' {
		sign = -1
	}
	hour, okHour := digitsAt(text, 1, 2)
	minute, okMinute := digitsAt(text, 4, 2)
	if !okHour || !okMinute || hour > 23 || minute > 59 {
		return 0, false
	}
	return sign * (hour*60 + minute), true
}

func hasInstantShape(text string) bool {
	return len(text) >= 20 && text[4] == '-' && text[7] == '-' && text[10] == 'T' &&
		text[13] == ':' && text[16] == ':'
}

func parseInstant(text string) (float64, bool) {
	if !isASCII(text) || !hasInstantShape(text) {
		return 0, false
	}
	days, okDays := parseDate(text)
	seconds, okSeconds := parseTimeOfDay(text[11:])
	if !okDays || !okSeconds {
		return 0, false
	}
	millis, after, okFraction := 0, 19, true
	if text[19] == '.' {
		millis, after, okFraction = parseFraction(text, 19)
	}
	if !okFraction {
		return 0, false
	}
	if after > len(text) {
		return 0, false
	}
	offset, okOffset := parseOffset(text[after:])
	if !okOffset {
		return 0, false
	}
	return float64(days*86400000 + seconds*1000 + millis - offset*60000), true
}

// instantOf reads a value as Unix milliseconds: a finite number as it is, a string in the strict form, and
// anything else (a bool, an object, a non-finite number) is not an instant.
func instantOf(value any) (float64, bool) {
	if _, ok := value.(bool); ok {
		return 0, false
	}
	if number, ok := numericValue(value); ok {
		if !isFiniteNumber(number) {
			return 0, false
		}
		return number, true
	}
	text, ok := value.(string)
	if !ok {
		return 0, false
	}
	return parseInstant(text)
}
