package wjson

import (
	"errors"
	"math"
	"strconv"
	"unicode/utf8"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// MaxDepth is how deeply arrays and objects may nest. The contract needs 512.
const MaxDepth = 1024

// ErrSyntax is returned for text that is not JSON.
var ErrSyntax = errors.New("wjson: invalid JSON")

// ErrDepth is returned for text nested deeper than MaxDepth.
var ErrDepth = errors.New("wjson: nested too deeply")

// Parse reads one JSON text. Bytes that are not UTF-8 are decoded as the WHATWG
// decoder does (U+FFFD), and a leading byte order mark is skipped.
func Parse(data []byte) (Value, error) {
	text := data
	if !utf8.Valid(text) {
		text = []byte(wtf8.DecodeUTF8(text))
	}
	if len(text) >= 3 && text[0] == 0xEF && text[1] == 0xBB && text[2] == 0xBF {
		text = text[3:]
	}
	p := &parser{b: text}
	p.ws()
	v, err := p.value(0)
	if err != nil {
		return Value{}, err
	}
	p.ws()
	if p.i != len(p.b) {
		return Value{}, ErrSyntax
	}
	return v, nil
}

type parser struct {
	b []byte
	i int
}

func (p *parser) ws() {
	for p.i < len(p.b) {
		switch p.b[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *parser) value(depth int) (Value, error) {
	if p.i >= len(p.b) {
		return Value{}, ErrSyntax
	}
	switch c := p.b[p.i]; {
	case c == '{':
		return p.object(depth)
	case c == '[':
		return p.array(depth)
	case c == '"':
		s, err := p.str()
		if err != nil {
			return Value{}, err
		}
		return String(s), nil
	case c == 't':
		return p.lit("true", Bool(true))
	case c == 'f':
		return p.lit("false", Bool(false))
	case c == 'n':
		return p.lit("null", Null())
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	}
	return Value{}, ErrSyntax
}

func (p *parser) lit(word string, v Value) (Value, error) {
	if len(p.b)-p.i < len(word) || string(p.b[p.i:p.i+len(word)]) != word {
		return Value{}, ErrSyntax
	}
	p.i += len(word)
	return v, nil
}

func (p *parser) number() (Value, error) {
	start := p.i
	if p.b[p.i] == '-' {
		p.i++
	}
	if p.i >= len(p.b) {
		return Value{}, ErrSyntax
	}
	switch {
	case p.b[p.i] == '0':
		p.i++
	case p.b[p.i] >= '1' && p.b[p.i] <= '9':
		p.digits()
	default:
		return Value{}, ErrSyntax
	}
	if p.i < len(p.b) && p.b[p.i] == '.' {
		p.i++
		if !p.digits() {
			return Value{}, ErrSyntax
		}
	}
	if p.i < len(p.b) && (p.b[p.i] == 'e' || p.b[p.i] == 'E') {
		p.i++
		if p.i < len(p.b) && (p.b[p.i] == '+' || p.b[p.i] == '-') {
			p.i++
		}
		if !p.digits() {
			return Value{}, ErrSyntax
		}
	}
	f, err := strconv.ParseFloat(string(p.b[start:p.i]), 64)
	if err != nil {
		// A range error still returns the nearest value: +-Inf for overflow.
		var ne *strconv.NumError
		if !errors.As(err, &ne) || !errors.Is(ne.Err, strconv.ErrRange) {
			return Value{}, ErrSyntax
		}
		if math.IsInf(f, 0) || f == 0 {
			return Number(f), nil
		}
	}
	return Number(f), nil
}

func (p *parser) digits() bool {
	start := p.i
	for p.i < len(p.b) && p.b[p.i] >= '0' && p.b[p.i] <= '9' {
		p.i++
	}
	return p.i > start
}

func hex4(b []byte) (rune, bool) {
	if len(b) < 4 {
		return 0, false
	}
	var r rune
	for _, c := range b[:4] {
		r <<= 4
		switch {
		case c >= '0' && c <= '9':
			r |= rune(c - '0')
		case c >= 'a' && c <= 'f':
			r |= rune(c-'a') + 10
		case c >= 'A' && c <= 'F':
			r |= rune(c-'A') + 10
		default:
			return 0, false
		}
	}
	return r, true
}

func (p *parser) str() (string, error) {
	p.i++ // opening quote
	start := p.i
	// Fast path: no escape and no control character.
	for p.i < len(p.b) {
		c := p.b[p.i]
		if c == '"' {
			s := string(p.b[start:p.i])
			p.i++
			return s, nil
		}
		if c == '\\' || c < 0x20 {
			break
		}
		p.i++
	}
	buf := append([]byte(nil), p.b[start:p.i]...)
	for p.i < len(p.b) {
		c := p.b[p.i]
		switch {
		case c == '"':
			p.i++
			return string(buf), nil
		case c < 0x20:
			return "", ErrSyntax
		case c != '\\':
			buf = append(buf, c)
			p.i++
			continue
		}
		p.i++
		if p.i >= len(p.b) {
			return "", ErrSyntax
		}
		e := p.b[p.i]
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
			r, ok := hex4(p.b[p.i:])
			if !ok {
				return "", ErrSyntax
			}
			p.i += 4
			if r >= 0xD800 && r <= 0xDBFF && p.i+6 <= len(p.b) && p.b[p.i] == '\\' && p.b[p.i+1] == 'u' {
				if lo, ok := hex4(p.b[p.i+2:]); ok && lo >= 0xDC00 && lo <= 0xDFFF {
					r = 0x10000 + (r-0xD800)<<10 + (lo - 0xDC00)
					p.i += 6
				}
			}
			buf = wtf8.AppendRune(buf, r)
		default:
			return "", ErrSyntax
		}
	}
	return "", ErrSyntax
}

func (p *parser) array(depth int) (Value, error) {
	if depth >= MaxDepth {
		return Value{}, ErrDepth
	}
	p.i++
	p.ws()
	items := []Value{}
	if p.i < len(p.b) && p.b[p.i] == ']' {
		p.i++
		return Array(items), nil
	}
	for {
		p.ws()
		v, err := p.value(depth + 1)
		if err != nil {
			return Value{}, err
		}
		items = append(items, v)
		p.ws()
		if p.i >= len(p.b) {
			return Value{}, ErrSyntax
		}
		switch p.b[p.i] {
		case ',':
			p.i++
		case ']':
			p.i++
			return Array(items), nil
		default:
			return Value{}, ErrSyntax
		}
	}
}

func (p *parser) object(depth int) (Value, error) {
	if depth >= MaxDepth {
		return Value{}, ErrDepth
	}
	p.i++
	p.ws()
	o := NewObject()
	if p.i < len(p.b) && p.b[p.i] == '}' {
		p.i++
		return ObjectValue(o), nil
	}
	for {
		p.ws()
		if p.i >= len(p.b) || p.b[p.i] != '"' {
			return Value{}, ErrSyntax
		}
		k, err := p.str()
		if err != nil {
			return Value{}, err
		}
		p.ws()
		if p.i >= len(p.b) || p.b[p.i] != ':' {
			return Value{}, ErrSyntax
		}
		p.i++
		p.ws()
		v, err := p.value(depth + 1)
		if err != nil {
			return Value{}, err
		}
		o.Set(k, v)
		p.ws()
		if p.i >= len(p.b) {
			return Value{}, ErrSyntax
		}
		switch p.b[p.i] {
		case ',':
			p.i++
		case '}':
			p.i++
			return ObjectValue(o), nil
		default:
			return Value{}, ErrSyntax
		}
	}
}
