package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/wjson"

// Instants, to the "Dates" section of docs/evaluation.md: a hand-written walk
// over the bytes. Any byte of a non-ASCII character fails a digit, a separator
// or the offset, so the byte walk and the spec's UTF-16 walk agree.

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// digits reads n ASCII digits at start.
func digits(text string, start, n int) (int, bool) {
	value := 0
	for i := start; i < start+n; i++ {
		if i >= len(text) || !isDigit(text[i]) {
			return 0, false
		}
		value = value*10 + int(text[i]-'0')
	}
	return value, true
}

func isLeapYear(y int) bool { return y%4 == 0 && (y%100 != 0 || y%400 == 0) }

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

// daysFromCivil is the days since 1970-01-01 in the proleptic Gregorian
// calendar. For years 1 to 9999 every value before the last subtraction is
// non-negative.
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

// parseTime reads HH:MM:SS at offset 11, as seconds since midnight.
func parseTime(text string) (int, bool) {
	hour, ok1 := digits(text, 11, 2)
	minute, ok2 := digits(text, 14, 2)
	second, ok3 := digits(text, 17, 2)
	if !ok1 || !ok2 || !ok3 || hour > 23 || minute > 59 || second > 59 {
		return 0, false
	}
	return (hour*60+minute)*60 + second, true
}

// parseFraction reads the fraction that starts at start (a "."): its
// milliseconds and where it ends. The first three digits are the milliseconds;
// a shorter fraction is padded on the right.
func parseFraction(text string, start int) (ms, end int, ok bool) {
	end = start + 1
	for end < len(text) && isDigit(text[end]) {
		end++
	}
	if end == start+1 {
		return 0, 0, false
	}
	kept := min(end-start-1, 3)
	scale := [...]int{1, 100, 10, 1}[kept]
	value, _ := digits(text, start+1, kept)
	return value * scale, end, true
}

// parseOffset reads the rest of the text as Z, +HH:MM or -HH:MM, in minutes
// east of UTC.
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

func hasShape(t string) bool {
	return len(t) >= 20 && t[4] == '-' && t[7] == '-' && t[10] == 'T' && t[13] == ':' && t[16] == ':'
}

// parseInstant reads YYYY-MM-DDTHH:MM:SS[.f+](Z|+HH:MM|-HH:MM) as Unix
// milliseconds.
func parseInstant(text string) (float64, bool) {
	if !hasShape(text) {
		return 0, false
	}
	days, ok := parseDate(text)
	if !ok {
		return 0, false
	}
	seconds, ok := parseTime(text)
	if !ok {
		return 0, false
	}
	ms, end := 0, 19
	if text[19] == '.' {
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

// toInstant reads a finite number as Unix milliseconds and a string as an
// instant; anything else is not one.
func toInstant(v wjson.Value) (float64, bool) {
	switch v.Kind() {
	case wjson.KindNumber:
		return v.Number(), v.IsFinite()
	case wjson.KindString:
		return parseInstant(v.String())
	}
	return 0, false
}
