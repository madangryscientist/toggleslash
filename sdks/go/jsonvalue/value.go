// Package jsonvalue is a JSON value, reader and writer that keep what the ToggleSlash specs need and
// encoding/json does not: a number such as 1e999 reads as +Inf instead of failing, and a lone surrogate
// escape survives (strings are WTF-8, see internal/wtf8).
package jsonvalue

import (
	"errors"
	"math"
	"strconv"
	"unicode/utf8"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// Kind is the JSON type of a Value.
type Kind uint8

// The kinds of Value.
const (
	KindNull Kind = iota
	KindBool
	KindNumber
	KindString
	KindArray
	KindObject
)

// Member is one property of an object.
type Member struct {
	Key   string
	Value Value
}

type object struct {
	members []Member
	index   map[string]int
}

const indexThreshold = 8

func (o *object) find(key string) int {
	if o.index != nil {
		if i, ok := o.index[key]; ok {
			return i
		}
		return -1
	}
	for i := range o.members {
		if o.members[i].Key == key {
			return i
		}
	}
	return -1
}

// set keeps the last value of a duplicate key, in the position of the first.
func (o *object) set(key string, v Value) {
	if i := o.find(key); i >= 0 {
		o.members[i].Value = v
		return
	}
	o.members = append(o.members, Member{key, v})
	if o.index != nil {
		o.index[key] = len(o.members) - 1
	} else if len(o.members) > indexThreshold {
		o.index = make(map[string]int, len(o.members)*2)
		for i := range o.members {
			o.index[o.members[i].Key] = i
		}
	}
}

// Value is a JSON value. The zero Value is null. A Value is immutable: nothing here changes one.
type Value struct {
	kind Kind
	b    bool
	n    float64
	s    string
	arr  []Value
	obj  *object
}

// Null is the JSON null.
var Null = Value{}

// FromBool returns a boolean value.
func FromBool(b bool) Value { return Value{kind: KindBool, b: b} }

// FromNumber returns a number value.
func FromNumber(f float64) Value { return Value{kind: KindNumber, n: f} }

// FromString returns a string value. Bytes that are not valid UTF-8 are replaced as the WHATWG decoder does.
func FromString(s string) Value { return Value{kind: KindString, s: wtf8.Sanitize(s)} }

// FromArray returns an array value holding the given elements (not copied).
func FromArray(vs ...Value) Value { return Value{kind: KindArray, arr: vs} }

// FromObject returns an object value. A duplicate key keeps the last value.
func FromObject(members ...Member) Value {
	o := &object{members: make([]Member, 0, len(members))}
	for _, m := range members {
		o.set(wtf8.Sanitize(m.Key), m.Value)
	}
	return Value{kind: KindObject, obj: o}
}

// From converts a Go value: nil, bool, the integer and float types, string, []string, []any, []Value,
// map[string]any, map[string]Value and Value. The second result is false for anything else.
func From(x any) (Value, bool) {
	switch t := x.(type) {
	case nil:
		return Null, true
	case Value:
		return t, true
	case bool:
		return FromBool(t), true
	case string:
		return FromString(t), true
	case int:
		return FromNumber(float64(t)), true
	case int8:
		return FromNumber(float64(t)), true
	case int16:
		return FromNumber(float64(t)), true
	case int32:
		return FromNumber(float64(t)), true
	case int64:
		return FromNumber(float64(t)), true
	case uint:
		return FromNumber(float64(t)), true
	case uint8:
		return FromNumber(float64(t)), true
	case uint16:
		return FromNumber(float64(t)), true
	case uint32:
		return FromNumber(float64(t)), true
	case uint64:
		return FromNumber(float64(t)), true
	case float32:
		return FromNumber(float64(t)), true
	case float64:
		return FromNumber(t), true
	case []string:
		out := make([]Value, len(t))
		for i, s := range t {
			out[i] = FromString(s)
		}
		return FromArray(out...), true
	case []Value:
		return FromArray(append([]Value(nil), t...)...), true
	case []any:
		out := make([]Value, len(t))
		for i, e := range t {
			v, ok := From(e)
			if !ok {
				return Null, false
			}
			out[i] = v
		}
		return FromArray(out...), true
	case map[string]Value:
		ms := make([]Member, 0, len(t))
		for k, e := range t {
			ms = append(ms, Member{k, e})
		}
		return FromObject(ms...), true
	case map[string]any:
		ms := make([]Member, 0, len(t))
		for k, e := range t {
			v, ok := From(e)
			if !ok {
				return Null, false
			}
			ms = append(ms, Member{k, v})
		}
		return FromObject(ms...), true
	}
	return Null, false
}

// Kind returns the JSON type.
func (v Value) Kind() Kind { return v.kind }

// IsNull reports whether v is null.
func (v Value) IsNull() bool { return v.kind == KindNull }

// AsBool returns the boolean and true when v is one.
func (v Value) AsBool() (bool, bool) { return v.b, v.kind == KindBool }

// AsNumber returns the number and true when v is one. A number may be infinite.
func (v Value) AsNumber() (float64, bool) { return v.n, v.kind == KindNumber }

// AsString returns the string (WTF-8) and true when v is one.
func (v Value) AsString() (string, bool) { return v.s, v.kind == KindString }

// Elements returns the elements of an array (nil for any other kind). The slice must not be changed.
func (v Value) Elements() []Value {
	if v.kind != KindArray {
		return nil
	}
	return v.arr
}

// Members returns the properties of an object in order (nil for any other kind). The slice must not be changed.
func (v Value) Members() []Member {
	if v.kind != KindObject {
		return nil
	}
	return v.obj.members
}

// Get returns the own property of an object named key.
func (v Value) Get(key string) (Value, bool) {
	if v.kind != KindObject {
		return Null, false
	}
	if i := v.obj.find(key); i >= 0 {
		return v.obj.members[i].Value, true
	}
	return Null, false
}

// IsFinite reports whether v is a number that is neither infinite nor NaN.
func (v Value) IsFinite() bool {
	return v.kind == KindNumber && !math.IsInf(v.n, 0) && !math.IsNaN(v.n)
}

// Equal is deep equality. Numbers compare as IEEE-754 values (1 equals 1.0 and -0 equals 0), strings by
// bytes, objects without regard to order.
func (v Value) Equal(w Value) bool {
	if v.kind != w.kind {
		return false
	}
	switch v.kind {
	case KindNull:
		return true
	case KindBool:
		return v.b == w.b
	case KindNumber:
		return v.n == w.n
	case KindString:
		return v.s == w.s
	case KindArray:
		if len(v.arr) != len(w.arr) {
			return false
		}
		for i := range v.arr {
			if !v.arr[i].Equal(w.arr[i]) {
				return false
			}
		}
		return true
	default:
		if len(v.obj.members) != len(w.obj.members) {
			return false
		}
		for _, m := range v.obj.members {
			o, ok := w.Get(m.Key)
			if !ok || !m.Value.Equal(o) {
				return false
			}
		}
		return true
	}
}

// ---- writing ----

// MarshalJSON writes v as JSON. A number that is not finite is written as null, as JSON.stringify does.
func (v Value) MarshalJSON() ([]byte, error) { return v.AppendJSON(nil), nil }

// String is the JSON text of v.
func (v Value) String() string { return string(v.AppendJSON(nil)) }

// AppendJSON appends the JSON text of v to dst.
func (v Value) AppendJSON(dst []byte) []byte {
	switch v.kind {
	case KindBool:
		return strconv.AppendBool(dst, v.b)
	case KindNumber:
		return appendNumber(dst, v.n)
	case KindString:
		return AppendQuoted(dst, v.s)
	case KindArray:
		dst = append(dst, '[')
		for i, e := range v.arr {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = e.AppendJSON(dst)
		}
		return append(dst, ']')
	case KindObject:
		dst = append(dst, '{')
		for i, m := range v.obj.members {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = AppendQuoted(dst, m.Key)
			dst = append(dst, ':')
			dst = m.Value.AppendJSON(dst)
		}
		return append(dst, '}')
	}
	return append(dst, "null"...)
}

func appendNumber(dst []byte, f float64) []byte {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return append(dst, "null"...)
	}
	if f == 0 {
		return append(dst, '0')
	}
	a := math.Abs(f)
	if a >= 1e21 || a < 1e-6 {
		return strconv.AppendFloat(dst, f, 'e', -1, 64)
	}
	return strconv.AppendFloat(dst, f, 'f', -1, 64)
}

const hexDigits = "0123456789abcdef"

// AppendQuoted appends s (WTF-8) as a JSON string. A lone surrogate is written as a \u escape.
func AppendQuoted(dst []byte, s string) []byte {
	dst = append(dst, '"')
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '"':
			dst = append(dst, '\\', '"')
		case c == '\\':
			dst = append(dst, '\\', '\\')
		case c == '\n':
			dst = append(dst, '\\', 'n')
		case c == '\r':
			dst = append(dst, '\\', 'r')
		case c == '\t':
			dst = append(dst, '\\', 't')
		case c == '\b':
			dst = append(dst, '\\', 'b')
		case c == '\f':
			dst = append(dst, '\\', 'f')
		case c < 0x20:
			dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xF])
		case c < 0x80:
			dst = append(dst, c)
		case wtf8.IsSurrogateAt(s, i):
			r, _ := wtf8.Decode(s, i)
			dst = append(dst, '\\', 'u', hexDigits[r>>12], hexDigits[(r>>8)&0xF], hexDigits[(r>>4)&0xF], hexDigits[r&0xF])
			i += 3
			continue
		default:
			r, n := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && n == 1 {
				dst = append(dst, "\xef\xbf\xbd"...)
			} else {
				dst = append(dst, s[i:i+n]...)
			}
			i += n
			continue
		}
		i++
	}
	return append(dst, '"')
}

// ---- reading ----

// MaxDepth is how deeply an array or object may nest before Parse refuses the text.
const MaxDepth = 4096

// ErrSyntax is returned for text that is not JSON; ErrDepth for text nested deeper than MaxDepth.
var (
	ErrSyntax = errors.New("jsonvalue: invalid JSON")
	ErrDepth  = errors.New("jsonvalue: nested too deeply")
)

type parser struct {
	b []byte
	i int
}

// Parse reads one JSON value. A leading byte order mark is skipped; a duplicate object key keeps the last
// value; a string may carry raw bytes that are not valid UTF-8 (each maximal invalid subpart becomes one
// U+FFFD) and a lone surrogate escape (kept). A number out of binary64's range is an infinity. Parse never
// panics.
func Parse(data []byte) (Value, error) {
	p := &parser{b: data}
	if len(p.b) >= 3 && p.b[0] == 0xEF && p.b[1] == 0xBB && p.b[2] == 0xBF {
		p.i = 3
	}
	p.space()
	v, err := p.value(0)
	if err != nil {
		return Null, err
	}
	p.space()
	if p.i != len(p.b) {
		return Null, ErrSyntax
	}
	return v, nil
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

func (p *parser) value(depth int) (Value, error) {
	if p.i >= len(p.b) {
		return Null, ErrSyntax
	}
	switch c := p.b[p.i]; {
	case c == '{':
		return p.object(depth)
	case c == '[':
		return p.array(depth)
	case c == '"':
		s, err := p.str()
		if err != nil {
			return Null, err
		}
		return Value{kind: KindString, s: s}, nil
	case c == 't':
		return p.word("true", FromBool(true))
	case c == 'f':
		return p.word("false", FromBool(false))
	case c == 'n':
		return p.word("null", Null)
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	}
	return Null, ErrSyntax
}

func (p *parser) word(w string, v Value) (Value, error) {
	if len(p.b)-p.i < len(w) || string(p.b[p.i:p.i+len(w)]) != w {
		return Null, ErrSyntax
	}
	p.i += len(w)
	return v, nil
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func (p *parser) number() (Value, error) {
	start := p.i
	if p.b[p.i] == '-' {
		p.i++
	}
	if p.i >= len(p.b) {
		return Null, ErrSyntax
	}
	switch {
	case p.b[p.i] == '0':
		p.i++
	case p.b[p.i] >= '1' && p.b[p.i] <= '9':
		for p.i < len(p.b) && isDigit(p.b[p.i]) {
			p.i++
		}
	default:
		return Null, ErrSyntax
	}
	if p.i < len(p.b) && p.b[p.i] == '.' {
		p.i++
		d := p.i
		for p.i < len(p.b) && isDigit(p.b[p.i]) {
			p.i++
		}
		if p.i == d {
			return Null, ErrSyntax
		}
	}
	if p.i < len(p.b) && (p.b[p.i] == 'e' || p.b[p.i] == 'E') {
		p.i++
		if p.i < len(p.b) && (p.b[p.i] == '+' || p.b[p.i] == '-') {
			p.i++
		}
		d := p.i
		for p.i < len(p.b) && isDigit(p.b[p.i]) {
			p.i++
		}
		if p.i == d {
			return Null, ErrSyntax
		}
	}
	// The text is checked, so the only error ParseFloat can return is a range error, whose value is the
	// infinity (or zero) we want.
	f, _ := strconv.ParseFloat(string(p.b[start:p.i]), 64)
	return Value{kind: KindNumber, n: f}, nil
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

func (p *parser) str() (string, error) {
	p.i++ // the opening quote
	start := p.i
	// Fast path: no escape and only ASCII.
	for p.i < len(p.b) {
		c := p.b[p.i]
		if c == '"' {
			s := string(p.b[start:p.i])
			p.i++
			return s, nil
		}
		if c == '\\' || c >= 0x80 || c < 0x20 {
			break
		}
		p.i++
	}
	out := append(make([]byte, 0, p.i-start+16), p.b[start:p.i]...)
	for p.i < len(p.b) {
		c := p.b[p.i]
		switch {
		case c == '"':
			p.i++
			return string(out), nil
		case c < 0x20:
			return "", ErrSyntax
		case c == '\\':
			if p.i+1 >= len(p.b) {
				return "", ErrSyntax
			}
			e := p.b[p.i+1]
			p.i += 2
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
				r, ok := hex4(p.b[p.i:])
				if !ok {
					return "", ErrSyntax
				}
				p.i += 4
				if r >= 0xD800 && r <= 0xDBFF && p.i+1 < len(p.b) && p.b[p.i] == '\\' && p.b[p.i+1] == 'u' {
					if lo, ok := hex4(p.b[p.i+2:]); ok && lo >= 0xDC00 && lo <= 0xDFFF {
						r = 0x10000 + (r-0xD800)<<10 + (lo - 0xDC00)
						p.i += 6
					}
				}
				out = wtf8.AppendCodePoint(out, r)
			default:
				return "", ErrSyntax
			}
		case c < 0x80:
			out = append(out, c)
			p.i++
		default:
			r, n := wtf8.DecodeWHATWG(p.b[p.i:])
			out = utf8.AppendRune(out, r)
			p.i += n
		}
	}
	return "", ErrSyntax
}

func (p *parser) array(depth int) (Value, error) {
	if depth >= MaxDepth {
		return Null, ErrDepth
	}
	p.i++
	var elems []Value
	p.space()
	if p.i < len(p.b) && p.b[p.i] == ']' {
		p.i++
		return Value{kind: KindArray, arr: []Value{}}, nil
	}
	for {
		p.space()
		v, err := p.value(depth + 1)
		if err != nil {
			return Null, err
		}
		elems = append(elems, v)
		p.space()
		if p.i >= len(p.b) {
			return Null, ErrSyntax
		}
		switch p.b[p.i] {
		case ',':
			p.i++
		case ']':
			p.i++
			return Value{kind: KindArray, arr: elems}, nil
		default:
			return Null, ErrSyntax
		}
	}
}

func (p *parser) object(depth int) (Value, error) {
	if depth >= MaxDepth {
		return Null, ErrDepth
	}
	p.i++
	o := &object{}
	p.space()
	if p.i < len(p.b) && p.b[p.i] == '}' {
		p.i++
		return Value{kind: KindObject, obj: o}, nil
	}
	for {
		p.space()
		if p.i >= len(p.b) || p.b[p.i] != '"' {
			return Null, ErrSyntax
		}
		k, err := p.str()
		if err != nil {
			return Null, err
		}
		p.space()
		if p.i >= len(p.b) || p.b[p.i] != ':' {
			return Null, ErrSyntax
		}
		p.i++
		p.space()
		v, err := p.value(depth + 1)
		if err != nil {
			return Null, err
		}
		o.set(k, v)
		p.space()
		if p.i >= len(p.b) {
			return Null, ErrSyntax
		}
		switch p.b[p.i] {
		case ',':
			p.i++
		case '}':
			p.i++
			return Value{kind: KindObject, obj: o}, nil
		default:
			return Null, ErrSyntax
		}
	}
}
