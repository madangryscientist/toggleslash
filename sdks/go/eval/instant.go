package eval

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonval"
)

// Instants, to docs/evaluation.md "Dates". A hand written walk over the bytes:
// no regular expression, no date parsing from the standard library and no
// library, so it reads the same way as the other implementations.

func instantDigit(c byte) bool { return c >= '0' && c <= '9' }

// instantDigits reads length ASCII digits at start, and reports whether every
// one of them was a digit.
func instantDigits(text string, start, length int) (int, bool) {
	if start+length > len(text) {
		return 0, false
	}
	value := 0
	for i := start; i < start+length; i++ {
		if !instantDigit(text[i]) {
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
	default:
		return 31
	}
}

// daysFromCivil is the number of days since 1970-01-01 in the proleptic
// Gregorian calendar.
func daysFromCivil(year, month, day int) int {
	y := year
	if month <= 2 {
		y = year - 1
	}
	era := y / 400
	if y < 0 && y%400 != 0 {
		era--
	}
	yearOfEra := y - era*400
	monthIndex := month + 9
	if month > 2 {
		monthIndex = month - 3
	}
	dayOfYear := (153*monthIndex+2)/5 + day - 1
	dayOfEra := yearOfEra*365 + yearOfEra/4 - yearOfEra/100 + dayOfYear
	return era*146097 + dayOfEra - 719468
}

func parseInstantDate(text string) (int, bool) {
	year, ok := instantDigits(text, 0, 4)
	if !ok {
		return 0, false
	}
	month, ok := instantDigits(text, 5, 2)
	if !ok {
		return 0, false
	}
	day, ok := instantDigits(text, 8, 2)
	if !ok {
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

func parseInstantTime(text string) (int, bool) {
	hour, ok := instantDigits(text, 0, 2)
	if !ok {
		return 0, false
	}
	minute, ok := instantDigits(text, 3, 2)
	if !ok {
		return 0, false
	}
	second, ok := instantDigits(text, 6, 2)
	if !ok {
		return 0, false
	}
	if hour > 23 || minute > 59 || second > 59 {
		return 0, false
	}
	return (hour*60+minute)*60 + second, true
}

// parseFraction reads the milliseconds of the fraction that starts at start,
// and where the fraction ends. A shorter fraction is padded on the right.
func parseFraction(text string, start int) (int, int, bool) {
	end := start + 1
	for end < len(text) && instantDigit(text[end]) {
		end++
	}
	if end == start+1 {
		return 0, 0, false
	}
	kept := end - start - 1
	if kept > 3 {
		kept = 3
	}
	scale := 1
	switch kept {
	case 1:
		scale = 100
	case 2:
		scale = 10
	}
	digits, ok := instantDigits(text, start+1, kept)
	if !ok {
		return 0, 0, false
	}
	return digits * scale, end, true
}

// parseOffset reads `Z`, `+HH:MM` or `-HH:MM`, in minutes east of UTC.
func parseOffset(text string) (int, bool) {
	if text == "Z" {
		return 0, true
	}
	if len(text) != 6 || text[3] != ':' {
		return 0, false
	}
	var sign int
	switch text[0] {
	case '+':
		sign = 1
	case '-':
		sign = -1
	default:
		return 0, false
	}
	hour, ok := instantDigits(text, 1, 2)
	if !ok {
		return 0, false
	}
	minute, ok := instantDigits(text, 4, 2)
	if !ok {
		return 0, false
	}
	if hour > 23 || minute > 59 {
		return 0, false
	}
	return sign * (hour*60 + minute), true
}

// isInstantShape checks the separators in place; the shortest form is
// "0001-01-01T00:00:00Z".
func isInstantShape(text string) bool {
	return len(text) >= 20 &&
		text[4] == '-' &&
		text[7] == '-' &&
		text[10] == 'T' &&
		text[13] == ':' &&
		text[16] == ':'
}

// parseInstantText reads `YYYY-MM-DDTHH:MM:SS[.f+](Z|+HH:MM|-HH:MM)` as Unix
// milliseconds.
func parseInstantText(text string) (float64, bool) {
	if !isInstantShape(text) {
		return 0, false
	}
	days, ok := parseInstantDate(text)
	if !ok {
		return 0, false
	}
	seconds, ok := parseInstantTime(text[11:])
	if !ok {
		return 0, false
	}
	millis := 0
	end := 19
	if text[19] == '.' {
		millis, end, ok = parseFraction(text, 19)
		if !ok {
			return 0, false
		}
	}
	offsetMinutes, ok := parseOffset(text[end:])
	if !ok {
		return 0, false
	}
	// The sum is int64: an int is 32 bits on a 386 or arm build, where a date
	// after 1970-01-25 would overflow the day-to-millisecond multiply.
	millisSinceEpoch := int64(days)*86400000 + int64(seconds)*1000 + int64(millis) - int64(offsetMinutes)*60000
	return float64(millisSinceEpoch), true
}

// toInstant reads a finite number as Unix milliseconds, or a string as an
// instant.
func toInstant(value jsonval.Value) (float64, bool) {
	if n, ok := asNumber(value); ok {
		if math.IsInf(n, 0) || math.IsNaN(n) {
			return 0, false
		}
		return n, true
	}
	if text, ok := asString(value); ok {
		return parseInstantText(text)
	}
	return 0, false
}

func matchesInstant(op string, attribute, value jsonval.Value) bool {
	candidate, ok := toInstant(attribute)
	if !ok {
		return false
	}
	wanted, ok := toInstant(value)
	if !ok {
		return false
	}
	if op == "before" {
		return candidate < wanted
	}
	return candidate > wanted
}
