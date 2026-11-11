package jsonvalue

import (
	"errors"
	"math"
	"strconv"
	"unicode/utf8"
)

// MaxDepth is how deep arrays and objects may nest. The contract allows a limit of at least 512.
const MaxDepth = 4096

// ErrSyntax is returned for text that is not one JSON value (RFC 8259). The error carries no part of the
// text, so it is safe to log.
var ErrSyntax = errors.New("invalid JSON")

// ErrDepth is returned for text nested deeper than MaxDepth.
var ErrDepth = errors.New("JSON nested too deeply")

// Parse reads one JSON value from data, which must be valid UTF-8 (see DecodeLossy). It is strict
// RFC 8259: no comments, no trailing commas, no leading zeros. Numbers are binary64 (1e999 is an infinity,
// 9007199254740993 reads as 9007199254740992), a duplicate object key keeps its last value, and a string
// escape that spells a lone surrogate is kept as one.
func Parse(data []byte) (Value, error) {
	p := parser{data: data}
	p.skipSpace()
	v, err := p.value(0)
	if err != nil {
		return Value{}, err
	}
	p.skipSpace()
	if p.pos != len(p.data) {
		return Value{}, ErrSyntax
	}
	return v, nil
}

// ParseBody reads a response body: a leading byte order mark is skipped and bytes that are not UTF-8 are
// decoded the WHATWG way before the text is parsed.
func ParseBody(data []byte) (Value, error) {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}
	return Parse(DecodeLossy(data))
}

type parser struct {
	data []byte
	pos  int
}

func (p *parser) skipSpace() {
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *parser) value(depth int) (Value, error) {
	if p.pos >= len(p.data) {
		return Value{}, ErrSyntax
	}
	switch c := p.data[p.pos]; {
	case c == '{':
		return p.object(depth + 1)
	case c == '[':
		return p.array(depth + 1)
	case c == '"':
		s, err := p.str()
		return String(s), err
	case c == 't':
		return p.literal("true", Bool(true))
	case c == 'f':
		return p.literal("false", Bool(false))
	case c == 'n':
		return p.literal("null", Null())
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	}
	return Value{}, ErrSyntax
}

func (p *parser) literal(word string, v Value) (Value, error) {
	if len(p.data)-p.pos < len(word) || string(p.data[p.pos:p.pos+len(word)]) != word {
		return Value{}, ErrSyntax
	}
	p.pos += len(word)
	return v, nil
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func (p *parser) digits() bool {
	start := p.pos
	for p.pos < len(p.data) && isDigit(p.data[p.pos]) {
		p.pos++
	}
	return p.pos > start
}

func (p *parser) number() (Value, error) {
	start := p.pos
	if p.data[p.pos] == '-' {
		p.pos++
	}
	if p.pos >= len(p.data) {
		return Value{}, ErrSyntax
	}
	switch {
	case p.data[p.pos] == '0':
		p.pos++
	case isDigit(p.data[p.pos]):
		p.digits()
	default:
		return Value{}, ErrSyntax
	}
	if p.pos < len(p.data) && p.data[p.pos] == '.' {
		p.pos++
		if !p.digits() {
			return Value{}, ErrSyntax
		}
	}
	if p.pos < len(p.data) && (p.data[p.pos] == 'e' || p.data[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.data) && (p.data[p.pos] == '+' || p.data[p.pos] == '-') {
			p.pos++
		}
		if !p.digits() {
			return Value{}, ErrSyntax
		}
	}
	f, err := strconv.ParseFloat(string(p.data[start:p.pos]), 64)
	if err != nil {
		var ne *strconv.NumError
		if !errors.As(err, &ne) || !errors.Is(ne.Err, strconv.ErrRange) {
			return Value{}, ErrSyntax
		}
		// An overflow comes back as an infinity of the right sign, which is what the spec wants.
		if !math.IsInf(f, 0) {
			return Value{}, ErrSyntax
		}
	}
	return Number(f), nil
}

func hex4(b []byte) (rune, bool) {
	if len(b) < 4 {
		return 0, false
	}
	var r rune
	for _, c := range b[:4] {
		switch {
		case c >= '0' && c <= '9':
			r = r<<4 | rune(c-'0')
		case c >= 'a' && c <= 'f':
			r = r<<4 | rune(c-'a'+10)
		case c >= 'A' && c <= 'F':
			r = r<<4 | rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	return r, true
}

// str reads a string whose opening quote is at p.pos and returns it as WTF-8.
func (p *parser) str() (string, error) {
	p.pos++
	start := p.pos
	// The common case has no escape: the string is a slice of the input.
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		switch {
		case c == '"':
			s := string(p.data[start:p.pos])
			p.pos++
			return s, nil
		case c == '\\':
			return p.strEscaped(start)
		case c < 0x20:
			return "", ErrSyntax
		case c >= 0x80:
			r, n := utf8.DecodeRune(p.data[p.pos:])
			if r == utf8.RuneError && n <= 1 {
				return "", ErrSyntax
			}
			p.pos += n
		default:
			p.pos++
		}
	}
	return "", ErrSyntax
}

func (p *parser) strEscaped(start int) (string, error) {
	out := append(make([]byte, 0, p.pos-start+16), p.data[start:p.pos]...)
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		switch {
		case c == '"':
			p.pos++
			return string(out), nil
		case c == '\\':
			p.pos++
			if p.pos >= len(p.data) {
				return "", ErrSyntax
			}
			e := p.data[p.pos]
			p.pos++
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
				r, ok := hex4(p.data[p.pos:])
				if !ok {
					return "", ErrSyntax
				}
				p.pos += 4
				if r >= 0xD800 && r <= 0xDBFF && p.pos+1 < len(p.data) && p.data[p.pos] == '\\' && p.data[p.pos+1] == 'u' {
					if low, ok := hex4(p.data[p.pos+2:]); ok && low >= 0xDC00 && low <= 0xDFFF {
						r = (r-0xD800)<<10 + (low - 0xDC00) + 0x10000
						p.pos += 6
					}
				}
				out = AppendCodePoint(out, r)
			default:
				return "", ErrSyntax
			}
		case c < 0x20:
			return "", ErrSyntax
		case c >= 0x80:
			r, n := utf8.DecodeRune(p.data[p.pos:])
			if r == utf8.RuneError && n <= 1 {
				return "", ErrSyntax
			}
			out = append(out, p.data[p.pos:p.pos+n]...)
			p.pos += n
		default:
			out = append(out, c)
			p.pos++
		}
	}
	return "", ErrSyntax
}

func (p *parser) array(depth int) (Value, error) {
	if depth > MaxDepth {
		return Value{}, ErrDepth
	}
	p.pos++
	items := []Value{}
	p.skipSpace()
	if p.pos < len(p.data) && p.data[p.pos] == ']' {
		p.pos++
		return Array(items), nil
	}
	for {
		p.skipSpace()
		v, err := p.value(depth)
		if err != nil {
			return Value{}, err
		}
		items = append(items, v)
		p.skipSpace()
		if p.pos >= len(p.data) {
			return Value{}, ErrSyntax
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return Array(items), nil
		default:
			return Value{}, ErrSyntax
		}
	}
}

func (p *parser) object(depth int) (Value, error) {
	if depth > MaxDepth {
		return Value{}, ErrDepth
	}
	p.pos++
	o := NewObject()
	p.skipSpace()
	if p.pos < len(p.data) && p.data[p.pos] == '}' {
		p.pos++
		return ObjectValue(o), nil
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.data) || p.data[p.pos] != '"' {
			return Value{}, ErrSyntax
		}
		k, err := p.str()
		if err != nil {
			return Value{}, err
		}
		p.skipSpace()
		if p.pos >= len(p.data) || p.data[p.pos] != ':' {
			return Value{}, ErrSyntax
		}
		p.pos++
		p.skipSpace()
		v, err := p.value(depth)
		if err != nil {
			return Value{}, err
		}
		o.Set(k, v)
		p.skipSpace()
		if p.pos >= len(p.data) {
			return Value{}, ErrSyntax
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
		case '}':
			p.pos++
			return ObjectValue(o), nil
		default:
			return Value{}, ErrSyntax
		}
	}
}
