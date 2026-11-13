package jsonval

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// Read parses one JSON document from r and returns its value. It refuses a
// document with more than maxDepth nested containers, skips one leading byte
// order mark, and reads the numbers and strings docs/evaluation.md requires.
func Read(r io.Reader, maxDepth int) (Value, error) {
	br := bufio.NewReader(r)
	if bom, err := br.Peek(3); err == nil && bom[0] == 0xEF && bom[1] == 0xBB && bom[2] == 0xBF {
		_, _ = br.Discard(3)
	}
	d := &decoder{r: br, max: maxDepth}
	if err := d.skipSpace(); err != nil {
		return nil, err
	}
	v, err := d.value()
	if err != nil {
		return nil, err
	}
	if err := d.skipSpace(); err != nil {
		return nil, err
	}
	if _, err := d.r.Peek(1); err == nil {
		return nil, d.errorf("trailing data")
	}
	return v, nil
}

type decoder struct {
	r     *bufio.Reader
	off   int64
	depth int
	max   int
}

func (d *decoder) errorf(format string, args ...any) error {
	return &SyntaxError{Offset: d.off, Msg: fmt.Sprintf(format, args...)}
}

func (d *decoder) next() (byte, error) {
	b, err := d.r.ReadByte()
	if err != nil {
		return 0, d.errorf("unexpected end of input")
	}
	d.off++
	return b, nil
}

func (d *decoder) peek() (byte, bool) {
	p, err := d.r.Peek(1)
	if err != nil || len(p) == 0 {
		return 0, false
	}
	return p[0], true
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func (d *decoder) skipSpace() error {
	for {
		b, ok := d.peek()
		if !ok {
			return nil
		}
		if !isSpace(b) {
			return nil
		}
		_, err := d.next()
		if err != nil {
			return err
		}
	}
}

func (d *decoder) value() (Value, error) {
	b, ok := d.peek()
	if !ok {
		return nil, d.errorf("unexpected end of input")
	}
	switch b {
	case '{':
		return d.object()
	case '[':
		return d.array()
	case '"':
		s, err := d.string()
		if err != nil {
			return nil, err
		}
		return String(s), nil
	case 't':
		if err := d.literal("true"); err != nil {
			return nil, err
		}
		return Bool(true), nil
	case 'f':
		if err := d.literal("false"); err != nil {
			return nil, err
		}
		return Bool(false), nil
	case 'n':
		if err := d.literal("null"); err != nil {
			return nil, err
		}
		return Null{}, nil
	default:
		if b == '-' || (b >= '0' && b <= '9') {
			return d.number()
		}
		return nil, d.errorf("unexpected character %q", b)
	}
}

func (d *decoder) literal(want string) error {
	for i := 0; i < len(want); i++ {
		b, err := d.next()
		if err != nil {
			return err
		}
		if b != want[i] {
			return d.errorf("expected %q", want)
		}
	}
	return nil
}

func (d *decoder) object() (Value, error) {
	d.depth++
	if d.depth > d.max {
		return nil, d.errorf("more than %d nested containers", d.max)
	}
	defer func() { d.depth-- }()
	if _, err := d.next(); err != nil {
		return nil, err
	}
	out := Object{}
	if err := d.skipSpace(); err != nil {
		return nil, err
	}
	if b, ok := d.peek(); ok && b == '}' {
		_, err := d.next()
		if err != nil {
			return nil, err
		}
		return out, nil
	}
	for {
		if err := d.skipSpace(); err != nil {
			return nil, err
		}
		b, ok := d.peek()
		if !ok {
			return nil, d.errorf("unexpected end of input in object")
		}
		if b != '"' {
			return nil, d.errorf("expected an object key")
		}
		key, err := d.string()
		if err != nil {
			return nil, err
		}
		if err := d.skipSpace(); err != nil {
			return nil, err
		}
		if b, ok := d.peek(); !ok || b != ':' {
			return nil, d.errorf("expected ':'")
		}
		if _, err := d.next(); err != nil {
			return nil, err
		}
		if err := d.skipSpace(); err != nil {
			return nil, err
		}
		v, err := d.value()
		if err != nil {
			return nil, err
		}
		out[key] = v
		if err := d.skipSpace(); err != nil {
			return nil, err
		}
		b, ok = d.peek()
		if !ok {
			return nil, d.errorf("unexpected end of input in object")
		}
		if b == ',' {
			if _, err := d.next(); err != nil {
				return nil, err
			}
			continue
		}
		if b == '}' {
			if _, err := d.next(); err != nil {
				return nil, err
			}
			return out, nil
		}
		return nil, d.errorf("expected ',' or '}'")
	}
}

func (d *decoder) array() (Value, error) {
	d.depth++
	if d.depth > d.max {
		return nil, d.errorf("more than %d nested containers", d.max)
	}
	defer func() { d.depth-- }()
	if _, err := d.next(); err != nil {
		return nil, err
	}
	out := Array{}
	if err := d.skipSpace(); err != nil {
		return nil, err
	}
	if b, ok := d.peek(); ok && b == ']' {
		_, err := d.next()
		if err != nil {
			return nil, err
		}
		return out, nil
	}
	for {
		if err := d.skipSpace(); err != nil {
			return nil, err
		}
		v, err := d.value()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		if err := d.skipSpace(); err != nil {
			return nil, err
		}
		b, ok := d.peek()
		if !ok {
			return nil, d.errorf("unexpected end of input in array")
		}
		if b == ',' {
			if _, err := d.next(); err != nil {
				return nil, err
			}
			continue
		}
		if b == ']' {
			if _, err := d.next(); err != nil {
				return nil, err
			}
			return out, nil
		}
		return nil, d.errorf("expected ',' or ']'")
	}
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func (d *decoder) number() (Value, error) {
	tok := make([]byte, 0, 24)
	for {
		b, ok := d.peek()
		if !ok {
			break
		}
		if b == '-' || b == '+' || b == '.' || b == 'e' || b == 'E' || isDigit(b) {
			if _, err := d.next(); err != nil {
				return nil, err
			}
			tok = append(tok, b)
			continue
		}
		break
	}
	if !isNumberToken(tok) {
		return nil, d.errorf("malformed number")
	}
	f, err := strconv.ParseFloat(string(tok), 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return nil, d.errorf("malformed number")
	}
	return Number(f), nil
}

// isNumberToken reports whether tok is the JSON number grammar. strconv accepts
// more than JSON does, so the token is checked before it is parsed.
func isNumberToken(tok []byte) bool {
	i := 0
	if i < len(tok) && tok[i] == '-' {
		i++
	}
	if i >= len(tok) {
		return false
	}
	switch {
	case tok[i] == '0':
		i++
	case tok[i] >= '1' && tok[i] <= '9':
		for i < len(tok) && isDigit(tok[i]) {
			i++
		}
	default:
		return false
	}
	if i < len(tok) && tok[i] == '.' {
		i++
		if i >= len(tok) || !isDigit(tok[i]) {
			return false
		}
		for i < len(tok) && isDigit(tok[i]) {
			i++
		}
	}
	if i < len(tok) && (tok[i] == 'e' || tok[i] == 'E') {
		i++
		if i < len(tok) && (tok[i] == '+' || tok[i] == '-') {
			i++
		}
		if i >= len(tok) || !isDigit(tok[i]) {
			return false
		}
		for i < len(tok) && isDigit(tok[i]) {
			i++
		}
	}
	return i == len(tok)
}

func (d *decoder) string() (string, error) {
	if _, err := d.next(); err != nil {
		return "", err
	}
	out := make([]byte, 0, 16)
	for {
		b, err := d.next()
		if err != nil {
			return "", err
		}
		switch {
		case b == '"':
			return string(out), nil
		case b < 0x20:
			return "", d.errorf("control character in string")
		case b == '\\':
			escaped, err := d.escape(out)
			if err != nil {
				return "", err
			}
			out = escaped
		case b < 0x80:
			out = append(out, b)
		default:
			next, err := d.rawRune(b, out)
			if err != nil {
				return "", err
			}
			out = next
		}
	}
}

func (d *decoder) escape(out []byte) ([]byte, error) {
	e, err := d.next()
	if err != nil {
		return nil, err
	}
	switch e {
	case '"', '\\', '/':
		return append(out, e), nil
	case 'b':
		return append(out, '\b'), nil
	case 'f':
		return append(out, '\f'), nil
	case 'n':
		return append(out, '\n'), nil
	case 'r':
		return append(out, '\r'), nil
	case 't':
		return append(out, '\t'), nil
	case 'u':
		cp, err := d.hex4()
		if err != nil {
			return nil, err
		}
		if cp >= 0xD800 && cp <= 0xDBFF {
			if lo, ok := d.lowSurrogate(); ok {
				cp = 0x10000 + (cp-0xD800)<<10 + (lo - 0xDC00)
			}
			return appendCodePoint(out, cp), nil
		}
		return appendCodePoint(out, cp), nil
	default:
		return nil, d.errorf("invalid escape")
	}
}

func (d *decoder) hex4() (rune, error) {
	var cp rune
	for i := 0; i < 4; i++ {
		b, err := d.next()
		if err != nil {
			return 0, err
		}
		var v rune
		switch {
		case b >= '0' && b <= '9':
			v = rune(b - '0')
		case b >= 'a' && b <= 'f':
			v = rune(b-'a') + 10
		case b >= 'A' && b <= 'F':
			v = rune(b-'A') + 10
		default:
			return 0, d.errorf("invalid \\u escape")
		}
		cp = cp<<4 | v
	}
	return cp, nil
}

// lowSurrogate consumes the "\uXXXX" that follows a high surrogate escape when
// it holds a low surrogate, and reports whether it did.
func (d *decoder) lowSurrogate() (rune, bool) {
	p, err := d.r.Peek(6)
	if err != nil && len(p) < 6 {
		return 0, false
	}
	if p[0] != '\\' || p[1] != 'u' {
		return 0, false
	}
	var cp rune
	for i := 2; i < 6; i++ {
		var v rune
		switch {
		case p[i] >= '0' && p[i] <= '9':
			v = rune(p[i] - '0')
		case p[i] >= 'a' && p[i] <= 'f':
			v = rune(p[i]-'a') + 10
		case p[i] >= 'A' && p[i] <= 'F':
			v = rune(p[i]-'A') + 10
		default:
			return 0, false
		}
		cp = cp<<4 | v
	}
	if cp < 0xDC00 || cp > 0xDFFF {
		return 0, false
	}
	if _, err := d.r.Discard(6); err != nil {
		return 0, false
	}
	d.off += 6
	return cp, true
}

// rawRune appends the code point that the lead byte b starts, decoding its
// continuation bytes. An ill formed sequence becomes one U+FFFD for its
// maximal subpart, as a UTF-8 decoder does.
func (d *decoder) rawRune(lead byte, out []byte) ([]byte, error) {
	need, lo, hi, ok := sequenceShape(lead)
	if !ok {
		return append(out, 0xEF, 0xBF, 0xBD), nil
	}
	p, err := d.r.Peek(need)
	if err != nil && len(p) < need {
		// Truncated at the end of the input: the maximal subpart is the
		// continuation bytes present and valid, and nothing more. A byte that
		// is not a continuation (the string's closing quote, say) stays for the
		// caller; discarding it too would swallow the quote and fail a body a
		// WHATWG decoder reads.
		keep := 0
		for i := 0; i < len(p); i++ {
			c := p[i]
			if i == 0 {
				if c < lo || c > hi {
					break
				}
			} else if c < 0x80 || c > 0xBF {
				break
			}
			keep++
		}
		if keep > 0 {
			if _, derr := d.r.Discard(keep); derr != nil {
				return nil, d.errorf("unexpected end of input")
			}
			d.off += int64(keep)
		}
		return append(out, 0xEF, 0xBF, 0xBD), nil
	}
	consumed := 0
	for i := 0; i < need; i++ {
		c := p[i]
		if i == 0 {
			if c < lo || c > hi {
				break
			}
		} else if c < 0x80 || c > 0xBF {
			break
		}
		consumed++
	}
	if consumed < need {
		if consumed > 0 {
			if _, derr := d.r.Discard(consumed); derr != nil {
				return nil, d.errorf("unexpected end of input")
			}
			d.off += int64(consumed)
		}
		return append(out, 0xEF, 0xBF, 0xBD), nil
	}
	if _, derr := d.r.Discard(need); derr != nil {
		return nil, d.errorf("unexpected end of input")
	}
	d.off += int64(need)
	out = append(out, lead)
	return append(out, p[:need]...), nil
}

// sequenceShape gives the length of the sequence a lead byte starts, and the
// range its first continuation byte falls in.
func sequenceShape(lead byte) (need int, lo, hi byte, ok bool) {
	switch {
	case lead >= 0xC2 && lead <= 0xDF:
		return 1, 0x80, 0xBF, true
	case lead == 0xE0:
		return 2, 0xA0, 0xBF, true
	case lead >= 0xE1 && lead <= 0xEC:
		return 2, 0x80, 0xBF, true
	case lead == 0xED:
		return 2, 0x80, 0x9F, true
	case lead >= 0xEE && lead <= 0xEF:
		return 2, 0x80, 0xBF, true
	case lead == 0xF0:
		return 3, 0x90, 0xBF, true
	case lead >= 0xF1 && lead <= 0xF3:
		return 3, 0x80, 0xBF, true
	case lead == 0xF4:
		return 3, 0x80, 0x8F, true
	default:
		return 0, 0, 0, false
	}
}
