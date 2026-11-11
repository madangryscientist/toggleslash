package jsonvalue

import (
	"errors"
	"strconv"
)

// ErrSyntax is returned by Parse for text that is not JSON. It carries no part of the input.
var ErrSyntax = errors.New("jsonvalue: invalid JSON")

// ErrDepth is returned by Parse for a document nested deeper than MaxDepth.
var ErrDepth = errors.New("jsonvalue: JSON nested too deeply")

// MaxDepth is the deepest nesting Parse accepts. The contract asks for at least 512.
const MaxDepth = 1000

// Parse reads one JSON document. Input that is not valid UTF-8 is decoded as LossyUTF8 first, and a
// leading byte order mark is skipped. Numbers are binary64 (1e999 is +Inf, 9007199254740993 is
// 9007199254740992, -0 is -0). A lone surrogate escape ("\ud800") is kept. A repeated object key keeps
// the last value.
func Parse(data []byte) (Value, error) {
	src := LossyUTF8(data)
	if len(src) >= 3 && src[0] == 0xEF && src[1] == 0xBB && src[2] == 0xBF {
		src = src[3:]
	}
	p := parser{src: src}
	p.skipSpace()
	v, err := p.value(0)
	if err != nil {
		return Value{}, err
	}
	p.skipSpace()
	if p.i != len(p.src) {
		return Value{}, ErrSyntax
	}
	return v, nil
}

type parser struct {
	src string
	i   int
}

func (p *parser) skipSpace() {
	for p.i < len(p.src) {
		switch p.src[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *parser) value(depth int) (Value, error) {
	if p.i >= len(p.src) {
		return Value{}, ErrSyntax
	}
	switch c := p.src[p.i]; {
	case c == '{':
		return p.object(depth + 1)
	case c == '[':
		return p.array(depth + 1)
	case c == '"':
		s, err := p.str()
		if err != nil {
			return Value{}, err
		}
		return Value{kind: String, s: s}, nil
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	case p.lit("true"):
		return Value{kind: Bool, b: true}, nil
	case p.lit("false"):
		return Value{kind: Bool}, nil
	case p.lit("null"):
		return Value{}, nil
	}
	return Value{}, ErrSyntax
}

func (p *parser) lit(word string) bool {
	if len(p.src)-p.i >= len(word) && p.src[p.i:p.i+len(word)] == word {
		p.i += len(word)
		return true
	}
	return false
}

func (p *parser) array(depth int) (Value, error) {
	if depth > MaxDepth {
		return Value{}, ErrDepth
	}
	p.i++
	out := []Value{}
	p.skipSpace()
	if p.i < len(p.src) && p.src[p.i] == ']' {
		p.i++
		return Value{kind: Array, arr: out}, nil
	}
	for {
		p.skipSpace()
		v, err := p.value(depth)
		if err != nil {
			return Value{}, err
		}
		out = append(out, v)
		p.skipSpace()
		if p.i >= len(p.src) {
			return Value{}, ErrSyntax
		}
		switch p.src[p.i] {
		case ',':
			p.i++
		case ']':
			p.i++
			return Value{kind: Array, arr: out}, nil
		default:
			return Value{}, ErrSyntax
		}
	}
}

func (p *parser) object(depth int) (Value, error) {
	if depth > MaxDepth {
		return Value{}, ErrDepth
	}
	p.i++
	o := &object{index: map[string]int{}}
	p.skipSpace()
	if p.i < len(p.src) && p.src[p.i] == '}' {
		p.i++
		return Value{kind: Object, obj: o}, nil
	}
	for {
		p.skipSpace()
		if p.i >= len(p.src) || p.src[p.i] != '"' {
			return Value{}, ErrSyntax
		}
		key, err := p.str()
		if err != nil {
			return Value{}, err
		}
		p.skipSpace()
		if p.i >= len(p.src) || p.src[p.i] != ':' {
			return Value{}, ErrSyntax
		}
		p.i++
		p.skipSpace()
		v, err := p.value(depth)
		if err != nil {
			return Value{}, err
		}
		o.add(key, v)
		p.skipSpace()
		if p.i >= len(p.src) {
			return Value{}, ErrSyntax
		}
		switch p.src[p.i] {
		case ',':
			p.i++
		case '}':
			p.i++
			return Value{kind: Object, obj: o}, nil
		default:
			return Value{}, ErrSyntax
		}
	}
}

func (p *parser) number() (Value, error) {
	start := p.i
	if p.src[p.i] == '-' {
		p.i++
	}
	if p.i >= len(p.src) {
		return Value{}, ErrSyntax
	}
	switch c := p.src[p.i]; {
	case c == '0':
		p.i++
	case c >= '1' && c <= '9':
		p.digits()
	default:
		return Value{}, ErrSyntax
	}
	if p.i < len(p.src) && p.src[p.i] == '.' {
		p.i++
		if !p.digits() {
			return Value{}, ErrSyntax
		}
	}
	if p.i < len(p.src) && (p.src[p.i] == 'e' || p.src[p.i] == 'E') {
		p.i++
		if p.i < len(p.src) && (p.src[p.i] == '+' || p.src[p.i] == '-') {
			p.i++
		}
		if !p.digits() {
			return Value{}, ErrSyntax
		}
	}
	f, err := strconv.ParseFloat(p.src[start:p.i], 64)
	if err != nil {
		// ParseFloat returns +-Inf for an overflow and 0 for an underflow with ErrRange, as IEEE-754 rounding does;
		// any other error is a syntax error.
		if !errors.Is(err, strconv.ErrRange) {
			return Value{}, ErrSyntax
		}
	}
	return Value{kind: Number, n: f}, nil
}

func (p *parser) digits() bool {
	start := p.i
	for p.i < len(p.src) && p.src[p.i] >= '0' && p.src[p.i] <= '9' {
		p.i++
	}
	return p.i > start
}

func hexVal(c byte) int {
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

func (p *parser) hex4() (rune, bool) {
	if len(p.src)-p.i < 4 {
		return 0, false
	}
	var r rune
	for k := 0; k < 4; k++ {
		h := hexVal(p.src[p.i+k])
		if h < 0 {
			return 0, false
		}
		r = r<<4 | rune(h)
	}
	p.i += 4
	return r, true
}

// str reads a string at p.i (the opening quote) and returns it as WTF-8.
func (p *parser) str() (string, error) {
	p.i++
	start := p.i
	// Fast path: no escape before the closing quote.
	for p.i < len(p.src) {
		c := p.src[p.i]
		if c == '"' {
			s := p.src[start:p.i]
			p.i++
			return s, nil
		}
		if c == '\\' {
			break
		}
		if c < 0x20 {
			return "", ErrSyntax
		}
		p.i++
	}
	buf := append(make([]byte, 0, p.i-start+16), p.src[start:p.i]...)
	for p.i < len(p.src) {
		c := p.src[p.i]
		switch {
		case c == '"':
			p.i++
			return string(buf), nil
		case c < 0x20:
			return "", ErrSyntax
		case c != '\\':
			buf = append(buf, c)
			p.i++
		default:
			p.i++
			if p.i >= len(p.src) {
				return "", ErrSyntax
			}
			e := p.src[p.i]
			p.i++
			switch e {
			case '"', '\\', '/':
				buf = append(buf, e)
			case 'b':
				buf = append(buf, '\b')
			case 'f':
				buf = append(buf, '\f')
			case 'n':
				buf = append(buf, '\n')
			case 'r':
				buf = append(buf, '\r')
			case 't':
				buf = append(buf, '\t')
			case 'u':
				r, ok := p.hex4()
				if !ok {
					return "", ErrSyntax
				}
				if r >= 0xD800 && r <= 0xDBFF && len(p.src)-p.i >= 6 && p.src[p.i] == '\\' && p.src[p.i+1] == 'u' {
					save := p.i
					p.i += 2
					lo, ok := p.hex4()
					if ok && lo >= 0xDC00 && lo <= 0xDFFF {
						r = 0x10000 + (r-0xD800)<<10 + (lo - 0xDC00)
					} else {
						p.i = save
					}
				}
				buf = AppendCodePoint(buf, r)
			default:
				return "", ErrSyntax
			}
		}
	}
	return "", ErrSyntax
}
