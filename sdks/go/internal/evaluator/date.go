package evaluator

import "math"

// digits reads length ASCII digits at start. It reports false when any is not a digit.
func digits(text string, start, length int) (int, bool) {
	if start+length > len(text) {
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

// daysFromCivil is the number of days since 1970-01-01 in the proleptic Gregorian calendar. For years 1 to 9999
// every value before the last subtraction is non-negative, so truncating and floor division agree.
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
	year, ok1 := digits(text, 0, 4)
	month, ok2 := digits(text, 5, 2)
	day, ok3 := digits(text, 8, 2)
	if !ok1 || !ok2 || !ok3 {
		return 0, false
	}
	if year < 1 || month < 1 || month > 12 || day < 1 || day > daysInMonth(year, month) {
		return 0, false
	}
	return daysFromCivil(year, month, day), true
}

func parseTime(text string, at int) (int, bool) {
	hour, ok1 := digits(text, at, 2)
	minute, ok2 := digits(text, at+3, 2)
	second, ok3 := digits(text, at+6, 2)
	if !ok1 || !ok2 || !ok3 || hour > 23 || minute > 59 || second > 59 {
		return 0, false
	}
	return (hour*60+minute)*60 + second, true
}

// parseFraction reads the fraction that starts at start (a '.'). The first three digits are the milliseconds, and
// a shorter fraction is padded on the right. It reports where the fraction ends.
func parseFraction(text string, start int) (ms, end int, ok bool) {
	end = start + 1
	for end < len(text) && isDigit(text[end]) {
		end++
	}
	if end == start+1 {
		return 0, 0, false
	}
	kept := min(end-start-1, 3)
	value, _ := digits(text, start+1, kept)
	scale := [...]int{1, 100, 10, 1}[kept]
	return value * scale, end, true
}

// parseOffset reads the Z, +HH:MM or -HH:MM that is all of text, in minutes east of UTC.
func parseOffset(text string) (int, bool) {
	if text == "Z" {
		return 0, true
	}
	if len(text) != 6 || text[3] != ':' || (text[0] != '+' && text[0] != '-') {
		return 0, false
	}
	hour, ok1 := digits(text, 1, 2)
	minute, ok2 := digits(text, 4, 2)
	if !ok1 || !ok2 || hour > 23 || minute > 59 {
		return 0, false
	}
	if text[0] == '-' {
		return -(hour*60 + minute), true
	}
	return hour*60 + minute, true
}

func hasShape(text string) bool {
	return len(text) >= 20 && text[4] == '-' && text[7] == '-' && text[10] == 'T' && text[13] == ':' && text[16] == ':'
}

// parseInstant reads YYYY-MM-DDTHH:MM:SS[.f+](Z|+HH:MM|-HH:MM) as Unix milliseconds.
func parseInstant(text string) (float64, bool) {
	if !hasShape(text) {
		return 0, false
	}
	days, ok1 := parseDate(text)
	seconds, ok2 := parseTime(text, 11)
	if !ok1 || !ok2 {
		return 0, false
	}
	fractionMs, end := 0, 19
	if text[19] == '.' {
		var ok bool
		if fractionMs, end, ok = parseFraction(text, 19); !ok {
			return 0, false
		}
	}
	offset, ok := parseOffset(text[end:])
	if !ok {
		return 0, false
	}
	ms := int64(days)*86_400_000 + int64(seconds)*1000 + int64(fractionMs) - int64(offset)*60_000
	return float64(ms), true
}

// toInstant reads a finite number as Unix milliseconds and a string as a date; anything else is not an instant.
func toInstant(number float64, isNumber bool, text string, isString bool) (float64, bool) {
	if isNumber {
		return number, !math.IsInf(number, 0) && !math.IsNaN(number)
	}
	if isString {
		return parseInstant(text)
	}
	return 0, false
}
