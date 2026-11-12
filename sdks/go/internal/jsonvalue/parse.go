package jsonvalue

import (
	"errors"
	"strconv"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// MaxDepth is how deep a document may nest. The contract lets an SDK set a limit of at least 512, and Go cannot
// recover from stack exhaustion, so a hostile body needs one.
const MaxDepth = 10000

// ErrSyntax is returned for text that is not JSON. It carries no part of the text: it can come from a body.
var ErrSyntax = errors.New("invalid JSON")

// ErrDepth is returned for a document nested deeper than MaxDepth.
var ErrDepth = errors.New("JSON nested too deeply")

// ParseBytes reads a document from raw bytes, as a response body arrives: the bytes are decoded as UTF-8 the
// WHATWG way (one U+FFFD for each ill-formed run), a leading byte order mark is skipped, and the rest is parsed.
func ParseBytes(data []byte) (Value, error) {
	text := wtf8.DecodeWHATWG(data)
	if len(text) >= 3 && text[:3] == "\xef\xbb\xbf" {
		text = text[3:]
	}
	return Parse(text)
}

// Parse reads a document from text that is already UTF-8. Duplicate keys keep the last value.
func Parse(text string) (Value, error) {
	p := &parser{s: text}
	p.skipSpace()
	v, err := p.value(0)
	if err != nil {
		return Value{}, err
	}
	p.skipSpace()
	if p.pos != len(p.s) {
		return Value{}, ErrSyntax
	}
	return v, nil
}

type parser struct {
	s   string
	pos int
}

func (p *parser) skipSpace() {
	for p.pos < len(p.s) {
		switch p.s[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *parser) value(depth int) (Value, error) {
	if p.pos >= len(p.s) {
		return Value{}, ErrSyntax
	}
	switch c := p.s[p.pos]; {
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
	if len(p.s)-p.pos < len(word) || p.s[p.pos:p.pos+len(word)] != word {
		return Value{}, ErrSyntax
	}
	p.pos += len(word)
	return v, nil
}

func (p *parser) object(depth int) (Value, error) {
	if depth > MaxDepth {
		return Value{}, ErrDepth
	}
	p.pos++
	o := &object{m: map[string]Value{}}
	p.skipSpace()
	if p.pos < len(p.s) && p.s[p.pos] == '}' {
		p.pos++
		return Value{kind: KindObject, obj: o}, nil
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.s) || p.s[p.pos] != '"' {
			return Value{}, ErrSyntax
		}
		key, err := p.str()
		if err != nil {
			return Value{}, err
		}
		p.skipSpace()
		if p.pos >= len(p.s) || p.s[p.pos] != ':' {
			return Value{}, ErrSyntax
		}
		p.pos++
		p.skipSpace()
		v, err := p.value(depth)
		if err != nil {
			return Value{}, err
		}
		o.set(key, v)
		p.skipSpace()
		if p.pos >= len(p.s) {
			return Value{}, ErrSyntax
		}
		c := p.s[p.pos]
		p.pos++
		if c == '}' {
			return Value{kind: KindObject, obj: o}, nil
		}
		if c != ',' {
			return Value{}, ErrSyntax
		}
	}
}

func (p *parser) array(depth int) (Value, error) {
	if depth > MaxDepth {
		return Value{}, ErrDepth
	}
	p.pos++
	var items []Value
	p.skipSpace()
	if p.pos < len(p.s) && p.s[p.pos] == ']' {
		p.pos++
		return Value{kind: KindArray, arr: []Value{}}, nil
	}
	for {
		p.skipSpace()
		v, err := p.value(depth)
		if err != nil {
			return Value{}, err
		}
		items = append(items, v)
		p.skipSpace()
		if p.pos >= len(p.s) {
			return Value{}, ErrSyntax
		}
		c := p.s[p.pos]
		p.pos++
		if c == ']' {
			return Value{kind: KindArray, arr: items}, nil
		}
		if c != ',' {
			return Value{}, ErrSyntax
		}
	}
}

// number checks the RFC 8259 grammar by hand and then converts with strconv, which rounds correctly. An
// overflow is infinity, not an error.
func (p *parser) number() (Value, error) {
	start := p.pos
	if p.s[p.pos] == '-' {
		p.pos++
	}
	if p.pos >= len(p.s) {
		return Value{}, ErrSyntax
	}
	switch {
	case p.s[p.pos] == '0':
		p.pos++
	case p.s[p.pos] >= '1' && p.s[p.pos] <= '9':
		p.digits()
	default:
		return Value{}, ErrSyntax
	}
	if p.pos < len(p.s) && p.s[p.pos] == '.' {
		p.pos++
		if !p.digits() {
			return Value{}, ErrSyntax
		}
	}
	if p.pos < len(p.s) && (p.s[p.pos] == 'e' || p.s[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.s) && (p.s[p.pos] == '+' || p.s[p.pos] == '-') {
			p.pos++
		}
		if !p.digits() {
			return Value{}, ErrSyntax
		}
	}
	f, err := strconv.ParseFloat(p.s[start:p.pos], 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return Value{}, ErrSyntax
	}
	return Number(f), nil
}

// digits skips ASCII digits and reports whether there was at least one.
func (p *parser) digits() bool {
	start := p.pos
	for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
		p.pos++
	}
	return p.pos > start
}

func (p *parser) str() (string, error) {
	p.pos++ // the opening quote
	start := p.pos
	// Fast path: no escape, no control character.
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		if c == '"' {
			out := p.s[start:p.pos]
			p.pos++
			return out, nil
		}
		if c == '\\' {
			break
		}
		if c < 0x20 {
			return "", ErrSyntax
		}
		p.pos++
	}
	buf := append([]byte(nil), p.s[start:p.pos]...)
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		switch {
		case c == '"':
			p.pos++
			return string(buf), nil
		case c < 0x20:
			return "", ErrSyntax
		case c != '\\':
			buf = append(buf, c)
			p.pos++
			continue
		}
		p.pos++
		if p.pos >= len(p.s) {
			return "", ErrSyntax
		}
		esc := p.s[p.pos]
		p.pos++
		switch esc {
		case '"', '\\', '/':
			buf = append(buf, esc)
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
			r, err := p.unicodeEscape()
			if err != nil {
				return "", err
			}
			buf = wtf8.AppendRune(buf, r)
		default:
			return "", ErrSyntax
		}
	}
	return "", ErrSyntax
}

func (p *parser) hex4() (rune, bool) {
	if len(p.s)-p.pos < 4 {
		return 0, false
	}
	var r rune
	for i := 0; i < 4; i++ {
		c := p.s[p.pos+i]
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
	p.pos += 4
	return r, true
}

// unicodeEscape reads the XXXX after \u. A high surrogate followed by an escaped low one is joined into one
// code point; any other surrogate stays alone.
func (p *parser) unicodeEscape() (rune, error) {
	r, ok := p.hex4()
	if !ok {
		return 0, ErrSyntax
	}
	if r >= 0xD800 && r <= 0xDBFF && len(p.s)-p.pos >= 6 && p.s[p.pos] == '\\' && p.s[p.pos+1] == 'u' {
		save := p.pos
		p.pos += 2
		low, ok := p.hex4()
		if ok && low >= 0xDC00 && low <= 0xDFFF {
			return 0x10000 + (r-0xD800)<<10 + (low - 0xDC00), nil
		}
		p.pos = save
	}
	return r, nil
}
