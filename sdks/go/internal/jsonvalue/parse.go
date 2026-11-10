package jsonvalue

import (
	"errors"
	"math"
	"strconv"
	"unicode/utf8"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// MaxDepth is the deepest nesting of arrays and objects Parse reads.
const MaxDepth = 1000

// ErrSyntax is returned for text that is not one JSON value.
var ErrSyntax = errors.New("jsonvalue: invalid JSON")

type parser struct {
	data []byte
	i    int
}

// Parse reads one JSON value (RFC 8259) from data. A leading UTF-8 byte order mark is skipped. A number
// out of range is the infinity it rounds to, a string keeps a lone surrogate, and bytes that are not UTF-8
// become U+FFFD. Any other fault returns ErrSyntax.
func Parse(data []byte) (Value, error) {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}
	p := &parser{data: data}
	p.space()
	v, err := p.value(0)
	if err != nil {
		return Value{}, err
	}
	p.space()
	if p.i != len(p.data) {
		return Value{}, ErrSyntax
	}
	return v, nil
}

func (p *parser) space() {
	for p.i < len(p.data) {
		switch p.data[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *parser) literal(word string, v Value) (Value, error) {
	if len(p.data)-p.i < len(word) || string(p.data[p.i:p.i+len(word)]) != word {
		return Value{}, ErrSyntax
	}
	p.i += len(word)
	return v, nil
}

func (p *parser) value(depth int) (Value, error) {
	if p.i >= len(p.data) {
		return Value{}, ErrSyntax
	}
	switch c := p.data[p.i]; {
	case c == '{':
		return p.object(depth + 1)
	case c == '[':
		return p.array(depth + 1)
	case c == '"':
		s, err := p.str()
		return Value{kind: KindString, s: s}, err
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

func (p *parser) array(depth int) (Value, error) {
	if depth > MaxDepth {
		return Value{}, ErrSyntax
	}
	p.i++
	items := []Value{}
	p.space()
	if p.i < len(p.data) && p.data[p.i] == ']' {
		p.i++
		return Value{kind: KindArray, arr: items}, nil
	}
	for {
		p.space()
		item, err := p.value(depth)
		if err != nil {
			return Value{}, err
		}
		items = append(items, item)
		p.space()
		if p.i >= len(p.data) {
			return Value{}, ErrSyntax
		}
		c := p.data[p.i]
		p.i++
		if c == ']' {
			return Value{kind: KindArray, arr: items}, nil
		}
		if c != ',' {
			return Value{}, ErrSyntax
		}
	}
}

func (p *parser) object(depth int) (Value, error) {
	if depth > MaxDepth {
		return Value{}, ErrSyntax
	}
	p.i++
	o := &object{vals: map[string]Value{}}
	p.space()
	if p.i < len(p.data) && p.data[p.i] == '}' {
		p.i++
		return Value{kind: KindObject, obj: o}, nil
	}
	for {
		p.space()
		if p.i >= len(p.data) || p.data[p.i] != '"' {
			return Value{}, ErrSyntax
		}
		key, err := p.str()
		if err != nil {
			return Value{}, err
		}
		p.space()
		if p.i >= len(p.data) || p.data[p.i] != ':' {
			return Value{}, ErrSyntax
		}
		p.i++
		p.space()
		item, err := p.value(depth)
		if err != nil {
			return Value{}, err
		}
		if _, seen := o.vals[key]; !seen {
			o.keys = append(o.keys, key)
		}
		o.vals[key] = item
		p.space()
		if p.i >= len(p.data) {
			return Value{}, ErrSyntax
		}
		c := p.data[p.i]
		p.i++
		if c == '}' {
			return Value{kind: KindObject, obj: o}, nil
		}
		if c != ',' {
			return Value{}, ErrSyntax
		}
	}
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// number checks the RFC 8259 grammar, then converts. strconv reports a result beyond the float64 range as
// +Inf or -Inf with ErrRange, which is the value the spec wants.
func (p *parser) number() (Value, error) {
	start := p.i
	if p.data[p.i] == '-' {
		p.i++
	}
	if p.i >= len(p.data) || !isDigit(p.data[p.i]) {
		return Value{}, ErrSyntax
	}
	if p.data[p.i] == '0' {
		p.i++
	} else {
		for p.i < len(p.data) && isDigit(p.data[p.i]) {
			p.i++
		}
	}
	if p.i < len(p.data) && p.data[p.i] == '.' {
		p.i++
		if p.i >= len(p.data) || !isDigit(p.data[p.i]) {
			return Value{}, ErrSyntax
		}
		for p.i < len(p.data) && isDigit(p.data[p.i]) {
			p.i++
		}
	}
	if p.i < len(p.data) && (p.data[p.i] == 'e' || p.data[p.i] == 'E') {
		p.i++
		if p.i < len(p.data) && (p.data[p.i] == '+' || p.data[p.i] == '-') {
			p.i++
		}
		if p.i >= len(p.data) || !isDigit(p.data[p.i]) {
			return Value{}, ErrSyntax
		}
		for p.i < len(p.data) && isDigit(p.data[p.i]) {
			p.i++
		}
	}
	f, err := strconv.ParseFloat(string(p.data[start:p.i]), 64)
	if err != nil && !(errors.Is(err, strconv.ErrRange) && math.IsInf(f, 0)) {
		return Value{}, ErrSyntax
	}
	return Number(f), nil
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

// hex4 reads four hex digits at offset at, or -1.
func (p *parser) hex4(at int) int {
	if at+4 > len(p.data) {
		return -1
	}
	v := 0
	for _, c := range p.data[at : at+4] {
		h := hexValue(c)
		if h < 0 {
			return -1
		}
		v = v<<4 | h
	}
	return v
}

// str reads the string whose opening quote is at p.i.
func (p *parser) str() (string, error) {
	p.i++
	start := p.i
	for p.i < len(p.data) {
		c := p.data[p.i]
		if c == '"' {
			s := string(p.data[start:p.i])
			p.i++
			return s, nil
		}
		if c == '\\' || c < 0x20 || c >= utf8.RuneSelf {
			break
		}
		p.i++
	}
	buf := append(make([]byte, 0, p.i-start+16), p.data[start:p.i]...)
	for p.i < len(p.data) {
		c := p.data[p.i]
		switch {
		case c == '"':
			p.i++
			return string(buf), nil
		case c < 0x20:
			return "", ErrSyntax
		case c == '\\':
			var err error
			if buf, err = p.escape(buf); err != nil {
				return "", err
			}
		case c < utf8.RuneSelf:
			buf = append(buf, c)
			p.i++
		default:
			r, size := utf8.DecodeRune(p.data[p.i:])
			if r == utf8.RuneError && size == 1 {
				buf = append(buf, "�"...)
			} else {
				buf = append(buf, p.data[p.i:p.i+size]...)
			}
			p.i += size
		}
	}
	return "", ErrSyntax
}

// escape reads the escape sequence that starts at p.i (a backslash) and appends what it stands for.
func (p *parser) escape(buf []byte) ([]byte, error) {
	if p.i+1 >= len(p.data) {
		return nil, ErrSyntax
	}
	c := p.data[p.i+1]
	p.i += 2
	switch c {
	case '"', '\\', '/':
		return append(buf, c), nil
	case 'b':
		return append(buf, '\b'), nil
	case 'f':
		return append(buf, '\f'), nil
	case 'n':
		return append(buf, '\n'), nil
	case 'r':
		return append(buf, '\r'), nil
	case 't':
		return append(buf, '\t'), nil
	case 'u':
		u := p.hex4(p.i)
		if u < 0 {
			return nil, ErrSyntax
		}
		p.i += 4
		if u >= 0xD800 && u <= 0xDBFF && p.i+1 < len(p.data) && p.data[p.i] == '\\' && p.data[p.i+1] == 'u' {
			if low := p.hex4(p.i + 2); low >= 0xDC00 && low <= 0xDFFF {
				p.i += 6
				u = 0x10000 + (u-0xD800)<<10 + (low - 0xDC00)
			}
		}
		return wtf8.AppendRune(buf, rune(u)), nil
	}
	return nil, ErrSyntax
}
