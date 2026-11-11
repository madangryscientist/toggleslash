package jsonvalue

import (
	"errors"
	"strconv"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// MaxDepth is how deeply arrays and objects may nest. The contract asks for at least 512.
const MaxDepth = 1000

// ErrSyntax is returned for any input that is not one JSON value.
var ErrSyntax = errors.New("invalid JSON")

type parser struct {
	s     string
	i     int
	depth int
}

// Parse reads one JSON value from text, which must be well-formed UTF-8 (see wtf8.Repair). A leading byte
// order mark is skipped. Numbers are binary64: 1e999 is +Inf and 9007199254740993 is 9007199254740992.
func Parse(text string) (Value, error) {
	p := &parser{s: text}
	if len(p.s) >= 3 && p.s[:3] == "\xEF\xBB\xBF" {
		p.i = 3
	}
	p.space()
	v, err := p.value()
	if err != nil {
		return Value{}, err
	}
	p.space()
	if p.i != len(p.s) {
		return Value{}, ErrSyntax
	}
	return v, nil
}

func (p *parser) space() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *parser) value() (Value, error) {
	if p.i >= len(p.s) {
		return Value{}, ErrSyntax
	}
	switch c := p.s[p.i]; {
	case c == '{':
		return p.object()
	case c == '[':
		return p.array()
	case c == '"':
		s, err := p.str()
		return Value{Kind: String, S: s}, err
	case c == 't':
		return p.literal("true", Value{Kind: Bool, B: true})
	case c == 'f':
		return p.literal("false", Value{Kind: Bool})
	case c == 'n':
		return p.literal("null", Value{})
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	}
	return Value{}, ErrSyntax
}

func (p *parser) literal(word string, v Value) (Value, error) {
	if len(p.s)-p.i < len(word) || p.s[p.i:p.i+len(word)] != word {
		return Value{}, ErrSyntax
	}
	p.i += len(word)
	return v, nil
}

func (p *parser) number() (Value, error) {
	start := p.i
	if p.s[p.i] == '-' {
		p.i++
	}
	if p.i >= len(p.s) {
		return Value{}, ErrSyntax
	}
	if p.s[p.i] == '0' {
		p.i++
	} else if p.s[p.i] >= '1' && p.s[p.i] <= '9' {
		p.digits()
	} else {
		return Value{}, ErrSyntax
	}
	if p.i < len(p.s) && p.s[p.i] == '.' {
		p.i++
		if !p.digits() {
			return Value{}, ErrSyntax
		}
	}
	if p.i < len(p.s) && (p.s[p.i] == 'e' || p.s[p.i] == 'E') {
		p.i++
		if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
			p.i++
		}
		if !p.digits() {
			return Value{}, ErrSyntax
		}
	}
	// ParseFloat returns +Inf or -Inf with a range error for an out of range literal, and 0 for an underflow;
	// binary64 reads both that way, so the error is dropped.
	n, err := strconv.ParseFloat(p.s[start:p.i], 64)
	if err != nil {
		var ne *strconv.NumError
		if !errors.As(err, &ne) || !errors.Is(ne.Err, strconv.ErrRange) {
			return Value{}, ErrSyntax
		}
	}
	return Value{Kind: Number, N: n}, nil
}

func (p *parser) digits() bool {
	start := p.i
	for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
		p.i++
	}
	return p.i > start
}

func hex4(s string) (rune, bool) {
	if len(s) < 4 {
		return 0, false
	}
	var r rune
	for i := 0; i < 4; i++ {
		c := s[i]
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

func (p *parser) str() (string, error) {
	p.i++ // the opening quote
	start := p.i
	// Fast path: no escape before the closing quote.
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == '"' {
			s := p.s[start:p.i]
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
	out := append([]byte(nil), p.s[start:p.i]...)
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch {
		case c == '"':
			p.i++
			return string(out), nil
		case c < 0x20:
			return "", ErrSyntax
		case c != '\\':
			out = append(out, c)
			p.i++
			continue
		}
		p.i++
		if p.i >= len(p.s) {
			return "", ErrSyntax
		}
		e := p.s[p.i]
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
			r, ok := hex4(p.s[p.i:])
			if !ok {
				return "", ErrSyntax
			}
			p.i += 4
			if r >= 0xD800 && r <= 0xDBFF && len(p.s)-p.i >= 6 && p.s[p.i] == '\\' && p.s[p.i+1] == 'u' {
				if low, ok := hex4(p.s[p.i+2:]); ok && low >= 0xDC00 && low <= 0xDFFF {
					r = 0x10000 + (r-0xD800)<<10 + (low - 0xDC00)
					p.i += 6
				}
			}
			out = wtf8.Append(out, r)
		default:
			return "", ErrSyntax
		}
	}
	return "", ErrSyntax
}

func (p *parser) array() (Value, error) {
	p.depth++
	if p.depth > MaxDepth {
		return Value{}, ErrSyntax
	}
	p.i++
	items := []Value{}
	p.space()
	if p.i < len(p.s) && p.s[p.i] == ']' {
		p.i++
		p.depth--
		return Value{Kind: Array, A: items}, nil
	}
	for {
		p.space()
		v, err := p.value()
		if err != nil {
			return Value{}, err
		}
		items = append(items, v)
		p.space()
		if p.i >= len(p.s) {
			return Value{}, ErrSyntax
		}
		c := p.s[p.i]
		p.i++
		if c == ']' {
			p.depth--
			return Value{Kind: Array, A: items}, nil
		}
		if c != ',' {
			return Value{}, ErrSyntax
		}
	}
}

func (p *parser) object() (Value, error) {
	p.depth++
	if p.depth > MaxDepth {
		return Value{}, ErrSyntax
	}
	p.i++
	members := &Members{M: map[string]Value{}}
	p.space()
	if p.i < len(p.s) && p.s[p.i] == '}' {
		p.i++
		p.depth--
		return Value{Kind: Object, O: members}, nil
	}
	for {
		p.space()
		if p.i >= len(p.s) || p.s[p.i] != '"' {
			return Value{}, ErrSyntax
		}
		name, err := p.str()
		if err != nil {
			return Value{}, err
		}
		p.space()
		if p.i >= len(p.s) || p.s[p.i] != ':' {
			return Value{}, ErrSyntax
		}
		p.i++
		p.space()
		v, err := p.value()
		if err != nil {
			return Value{}, err
		}
		if _, seen := members.M[name]; !seen {
			members.Keys = append(members.Keys, name)
		}
		members.M[name] = v
		p.space()
		if p.i >= len(p.s) {
			return Value{}, ErrSyntax
		}
		c := p.s[p.i]
		p.i++
		if c == '}' {
			p.depth--
			return Value{Kind: Object, O: members}, nil
		}
		if c != ',' {
			return Value{}, ErrSyntax
		}
	}
}
