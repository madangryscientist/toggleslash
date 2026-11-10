package jsonv

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"unicode/utf8"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// MaxDepth is the deepest nesting Parse accepts. The spec sets no limit; this one keeps the recursion
// off the stack's end.
const MaxDepth = 512

// ErrSyntax is wrapped by every parse error. Its text never carries the input.
var ErrSyntax = errors.New("invalid JSON")

type parser struct {
	in  []byte
	pos int
}

// Parse reads one JSON value from data. A leading byte order mark is skipped. Trailing input other
// than whitespace is an error.
func Parse(data []byte) (Value, error) {
	p := &parser{in: data}
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		p.pos = 3
	}
	p.skipSpace()
	v, err := p.value(0)
	if err != nil {
		return Null(), err
	}
	p.skipSpace()
	if p.pos != len(p.in) {
		return Null(), p.fail("trailing input")
	}
	return v, nil
}

func (p *parser) fail(what string) error {
	return fmt.Errorf("%w: %s at byte %d", ErrSyntax, what, p.pos)
}

func (p *parser) skipSpace() {
	for p.pos < len(p.in) {
		switch p.in[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *parser) value(depth int) (Value, error) {
	if p.pos >= len(p.in) {
		return Null(), p.fail("unexpected end")
	}
	switch c := p.in[p.pos]; {
	case c == '{':
		return p.object(depth)
	case c == '[':
		return p.array(depth)
	case c == '"':
		s, err := p.str()
		if err != nil {
			return Null(), err
		}
		return rawString(s), nil
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	case c == 't':
		return p.literal("true", Bool(true))
	case c == 'f':
		return p.literal("false", Bool(false))
	case c == 'n':
		return p.literal("null", Null())
	}
	return Null(), p.fail("unexpected character")
}

func (p *parser) literal(word string, v Value) (Value, error) {
	if len(p.in)-p.pos >= len(word) && string(p.in[p.pos:p.pos+len(word)]) == word {
		p.pos += len(word)
		return v, nil
	}
	return Null(), p.fail("unexpected character")
}

func (p *parser) array(depth int) (Value, error) {
	if depth >= MaxDepth {
		return Null(), p.fail("nesting too deep")
	}
	p.pos++
	items := []Value{}
	p.skipSpace()
	if p.pos < len(p.in) && p.in[p.pos] == ']' {
		p.pos++
		return Array(items), nil
	}
	for {
		p.skipSpace()
		v, err := p.value(depth + 1)
		if err != nil {
			return Null(), err
		}
		items = append(items, v)
		p.skipSpace()
		if p.pos >= len(p.in) {
			return Null(), p.fail("unexpected end")
		}
		switch p.in[p.pos] {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return Array(items), nil
		default:
			return Null(), p.fail("expected , or ]")
		}
	}
}

func (p *parser) object(depth int) (Value, error) {
	if depth >= MaxDepth {
		return Null(), p.fail("nesting too deep")
	}
	p.pos++
	o := &object{m: map[string]Value{}}
	p.skipSpace()
	if p.pos < len(p.in) && p.in[p.pos] == '}' {
		p.pos++
		return Value{kind: KindObject, obj: o}, nil
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.in) || p.in[p.pos] != '"' {
			return Null(), p.fail("expected a member name")
		}
		k, err := p.str()
		if err != nil {
			return Null(), err
		}
		p.skipSpace()
		if p.pos >= len(p.in) || p.in[p.pos] != ':' {
			return Null(), p.fail("expected :")
		}
		p.pos++
		p.skipSpace()
		v, err := p.value(depth + 1)
		if err != nil {
			return Null(), err
		}
		if _, dup := o.m[k]; !dup {
			o.keys = append(o.keys, k)
		}
		o.m[k] = v // a duplicate name keeps the last value
		p.skipSpace()
		if p.pos >= len(p.in) {
			return Null(), p.fail("unexpected end")
		}
		switch p.in[p.pos] {
		case ',':
			p.pos++
		case '}':
			p.pos++
			return Value{kind: KindObject, obj: o}, nil
		default:
			return Null(), p.fail("expected , or }")
		}
	}
}

func (p *parser) number() (Value, error) {
	start := p.pos
	if p.in[p.pos] == '-' {
		p.pos++
	}
	if p.pos >= len(p.in) {
		return Null(), p.fail("bad number")
	}
	switch c := p.in[p.pos]; {
	case c == '0':
		p.pos++
	case c >= '1' && c <= '9':
		p.digits()
	default:
		return Null(), p.fail("bad number")
	}
	if p.pos < len(p.in) && p.in[p.pos] == '.' {
		p.pos++
		if !p.digits() {
			return Null(), p.fail("bad number")
		}
	}
	if p.pos < len(p.in) && (p.in[p.pos] == 'e' || p.in[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.in) && (p.in[p.pos] == '+' || p.in[p.pos] == '-') {
			p.pos++
		}
		if !p.digits() {
			return Null(), p.fail("bad number")
		}
	}
	f, err := strconv.ParseFloat(string(p.in[start:p.pos]), 64)
	if err != nil {
		var ne *strconv.NumError
		if !errors.As(err, &ne) || !errors.Is(ne.Err, strconv.ErrRange) {
			return Null(), p.fail("bad number")
		}
		// ParseFloat already returns +Inf or -Inf for an overflow: the spec reads 1e999 as Infinity.
	}
	if math.IsNaN(f) {
		return Null(), p.fail("bad number")
	}
	return Number(f), nil
}

func (p *parser) digits() bool {
	start := p.pos
	for p.pos < len(p.in) && p.in[p.pos] >= '0' && p.in[p.pos] <= '9' {
		p.pos++
	}
	return p.pos > start
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

// hex4 reads four hex digits at in[at:].
func (p *parser) hex4(at int) (rune, bool) {
	if at+4 > len(p.in) {
		return 0, false
	}
	var r rune
	for i := 0; i < 4; i++ {
		h := hexVal(p.in[at+i])
		if h < 0 {
			return 0, false
		}
		r = r<<4 | rune(h)
	}
	return r, true
}

func (p *parser) str() (string, error) {
	p.pos++ // opening quote
	// Fast path: no escape, no control, no non-ASCII byte.
	start := p.pos
	for p.pos < len(p.in) {
		c := p.in[p.pos]
		if c == '"' {
			s := string(p.in[start:p.pos])
			p.pos++
			return s, nil
		}
		if c == '\\' || c < 0x20 || c >= 0x80 {
			break
		}
		p.pos++
	}
	buf := append([]byte(nil), p.in[start:p.pos]...)
	for p.pos < len(p.in) {
		c := p.in[p.pos]
		switch {
		case c == '"':
			p.pos++
			return string(buf), nil
		case c < 0x20:
			return "", p.fail("control character in string")
		case c == '\\':
			p.pos++
			if p.pos >= len(p.in) {
				return "", p.fail("unexpected end")
			}
			e := p.in[p.pos]
			p.pos++
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
				r, ok := p.hex4(p.pos)
				if !ok {
					return "", p.fail("bad \\u escape")
				}
				p.pos += 4
				if r >= 0xD800 && r <= 0xDBFF && p.pos+1 < len(p.in) && p.in[p.pos] == '\\' && p.in[p.pos+1] == 'u' {
					if lo, ok := p.hex4(p.pos + 2); ok && lo >= 0xDC00 && lo <= 0xDFFF {
						r = 0x10000 + (r-0xD800)<<10 + (lo - 0xDC00)
						p.pos += 6
					}
				}
				buf = wtf8.AppendRune(buf, r)
			default:
				return "", p.fail("bad escape")
			}
		case c < 0x80:
			buf = append(buf, c)
			p.pos++
		default:
			r, n := utf8.DecodeRune(p.in[p.pos:])
			buf = wtf8.AppendRune(buf, r) // an invalid byte is RuneError (U+FFFD) of width 1
			p.pos += n
		}
	}
	return "", p.fail("unexpected end")
}
