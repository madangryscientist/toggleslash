package jsonvalue

import (
	"errors"
	"math"
	"strconv"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// ErrSyntax is the one error Parse returns. It carries no text from the input, so a context a
// caller parsed never leaks through an error.
var ErrSyntax = errors.New("jsonvalue: not valid JSON")

// MaxDepth is how deeply arrays and objects may nest. The contract's floor is 512.
const MaxDepth = 10000

// Parse reads one JSON value from b. A leading byte order mark is skipped, a duplicate key keeps
// the last value, bytes that are not valid UTF-8 inside a string become U+FFFD as the WHATWG
// decoder does, and a lone surrogate written as an escape is kept.
func Parse(b []byte) (Value, error) {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		b = b[3:]
	}
	p := &parser{b: b}
	p.space()
	v, ok := p.value(0)
	if !ok {
		return Value{}, ErrSyntax
	}
	p.space()
	if p.i != len(p.b) {
		return Value{}, ErrSyntax
	}
	return v, nil
}

type parser struct {
	b []byte
	i int
}

func (p *parser) space() {
	for p.i < len(p.b) {
		switch p.b[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *parser) value(depth int) (Value, bool) {
	if p.i >= len(p.b) {
		return Value{}, false
	}
	switch c := p.b[p.i]; {
	case c == '{':
		return p.object(depth)
	case c == '[':
		return p.array(depth)
	case c == '"':
		s, ok := p.str()
		return NewString(s), ok
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	case c == 't':
		return NewBool(true), p.literal("true")
	case c == 'f':
		return NewBool(false), p.literal("false")
	case c == 'n':
		return Value{}, p.literal("null")
	}
	return Value{}, false
}

func (p *parser) literal(word string) bool {
	if len(p.b)-p.i >= len(word) && string(p.b[p.i:p.i+len(word)]) == word {
		p.i += len(word)
		return true
	}
	return false
}

func (p *parser) array(depth int) (Value, bool) {
	if depth >= MaxDepth {
		return Value{}, false
	}
	p.i++
	items := []Value{}
	p.space()
	if p.i < len(p.b) && p.b[p.i] == ']' {
		p.i++
		return NewArray(items), true
	}
	for {
		p.space()
		item, ok := p.value(depth + 1)
		if !ok {
			return Value{}, false
		}
		items = append(items, item)
		p.space()
		if p.i >= len(p.b) {
			return Value{}, false
		}
		switch p.b[p.i] {
		case ',':
			p.i++
		case ']':
			p.i++
			return NewArray(items), true
		default:
			return Value{}, false
		}
	}
}

func (p *parser) object(depth int) (Value, bool) {
	if depth >= MaxDepth {
		return Value{}, false
	}
	p.i++
	members := map[string]Value{}
	p.space()
	if p.i < len(p.b) && p.b[p.i] == '}' {
		p.i++
		return NewObject(members), true
	}
	for {
		p.space()
		if p.i >= len(p.b) || p.b[p.i] != '"' {
			return Value{}, false
		}
		key, ok := p.str()
		if !ok {
			return Value{}, false
		}
		p.space()
		if p.i >= len(p.b) || p.b[p.i] != ':' {
			return Value{}, false
		}
		p.i++
		p.space()
		item, ok := p.value(depth + 1)
		if !ok {
			return Value{}, false
		}
		members[key] = item
		p.space()
		if p.i >= len(p.b) {
			return Value{}, false
		}
		switch p.b[p.i] {
		case ',':
			p.i++
		case '}':
			p.i++
			return NewObject(members), true
		default:
			return Value{}, false
		}
	}
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func (p *parser) digits() bool {
	start := p.i
	for p.i < len(p.b) && isDigit(p.b[p.i]) {
		p.i++
	}
	return p.i > start
}

func (p *parser) number() (Value, bool) {
	start := p.i
	if p.b[p.i] == '-' {
		p.i++
	}
	if p.i >= len(p.b) {
		return Value{}, false
	}
	if p.b[p.i] == '0' {
		p.i++
	} else if !p.digits() {
		return Value{}, false
	}
	if p.i < len(p.b) && p.b[p.i] == '.' {
		p.i++
		if !p.digits() {
			return Value{}, false
		}
	}
	if p.i < len(p.b) && (p.b[p.i] == 'e' || p.b[p.i] == 'E') {
		p.i++
		if p.i < len(p.b) && (p.b[p.i] == '+' || p.b[p.i] == '-') {
			p.i++
		}
		if !p.digits() {
			return Value{}, false
		}
	}
	f, err := strconv.ParseFloat(string(p.b[start:p.i]), 64)
	if err != nil {
		var ne *strconv.NumError
		if !errors.As(err, &ne) || !errors.Is(ne.Err, strconv.ErrRange) {
			return Value{}, false
		}
		// Overflow is +Inf or -Inf, which the spec reads; underflow is a zero.
		if !math.IsInf(f, 0) && f != 0 {
			return Value{}, false
		}
	}
	return NewNumber(f), true
}

func hexValue(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

func (p *parser) hex4(at int) (int32, bool) {
	if at+4 > len(p.b) {
		return 0, false
	}
	var r int32
	for k := 0; k < 4; k++ {
		h := hexValue(p.b[at+k])
		if h < 0 {
			return 0, false
		}
		r = r<<4 | int32(h)
	}
	return r, true
}

// str reads a string starting at the opening quote and returns it as WTF-8.
func (p *parser) str() (string, bool) {
	p.i++
	var out []byte
	runStart := p.i
	flush := func(end int) {
		if end > runStart {
			out = append(out, wtf8.DecodeLossy(p.b[runStart:end])...)
		}
	}
	for p.i < len(p.b) {
		c := p.b[p.i]
		switch {
		case c == '"':
			flush(p.i)
			p.i++
			return string(out), true
		case c < 0x20:
			return "", false
		case c != '\\':
			p.i++
			continue
		}
		flush(p.i)
		p.i++
		if p.i >= len(p.b) {
			return "", false
		}
		e := p.b[p.i]
		p.i++
		switch e {
		case '"', '\\', '/':
			out = append(out, e)
		case 'b':
			out = append(out, '\b')
		case 'f':
			out = append(out, '\f')
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case 'u':
			r, ok := p.hex4(p.i)
			if !ok {
				return "", false
			}
			p.i += 4
			if r >= 0xD800 && r <= 0xDBFF && p.i+1 < len(p.b) && p.b[p.i] == '\\' && p.b[p.i+1] == 'u' {
				if low, ok := p.hex4(p.i + 2); ok && low >= 0xDC00 && low <= 0xDFFF {
					p.i += 6
					r = 0x10000 + (r-0xD800)<<10 + (low - 0xDC00)
				}
			}
			out = wtf8.AppendRune(out, r)
		default:
			return "", false
		}
		runStart = p.i
	}
	return "", false
}
