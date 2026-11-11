package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"

// The parser for `matches` patterns, to the "Patterns" section of docs/evaluation.md: code points in, a
// tree with each node's size out, or nothing for a pattern that is not valid.

const (
	maxLength = 1024
	maxSize   = 10000
	maxCount  = 100
	endOfText = -1
)

// item is code point ranges as pairs lo, hi (inclusive), or their complement when negate is set.
type item struct {
	ranges []rune
	negate bool
}

// charSet is a class: any of its items, or none of them when negate is set.
type charSet struct {
	items  []item
	negate bool
}

type nodeKind int

const (
	nodeSet nodeKind = iota
	nodeStart
	nodeEnd
	nodeConcat
	nodeAlt
	nodeRepeat
)

type node struct {
	kind  nodeKind
	size  int
	set   charSet
	parts []*node // concat parts, alt sides, or the one repeat body
	min   int
	max   int // -1 for no upper bound
}

var (
	digitItem = item{ranges: []rune{0x30, 0x39}}
	wordItem  = item{ranges: []rune{0x30, 0x39, 0x41, 0x5a, 0x5f, 0x5f, 0x61, 0x7a}}
	// U+0009 to U+000D are \t \n \v \f \r.
	spaceItem = item{ranges: []rune{0x09, 0x0d, 0x20, 0x20}}
)

func negated(i item) item { return item{ranges: i.ranges, negate: true} }

func single(c rune) item { return item{ranges: []rune{c, c}} }

func setOf(i item) *node {
	return &node{kind: nodeSet, set: charSet{items: []item{i}}, size: 1}
}

// invalid is the panic value a parse uses to give up; parsePattern recovers it.
type invalid struct{}

func fail() { panic(invalid{}) }

func saturate(size int) int {
	if size > maxSize+1 {
		return maxSize + 1
	}
	return size
}

func isDigitRune(c rune) bool { return c >= '0' && c <= '9' }

func isEscapable(c rune) bool {
	switch c {
	case '\\', '.', '[', ']', '(', ')', '|', '*', '+', '?', '{', '}', '^', '$', '-':
		return true
	}
	return false
}

type parser struct {
	cps []rune
	pos int
}

func (p *parser) peek(offset int) rune {
	if p.pos+offset >= len(p.cps) {
		return endOfText
	}
	return p.cps[p.pos+offset]
}

func (p *parser) take() rune {
	c := p.peek(0)
	p.pos++
	return c
}

func (p *parser) alternation() *node {
	sides := []*node{p.concat()}
	for p.peek(0) == '|' {
		p.pos++
		sides = append(sides, p.concat())
	}
	if len(sides) == 1 {
		return sides[0]
	}
	size := len(sides) - 1
	for _, s := range sides {
		size += s.size
	}
	return &node{kind: nodeAlt, parts: sides, size: saturate(size)}
}

func (p *parser) concat() *node {
	var parts []*node
	for c := p.peek(0); c != endOfText && c != '|' && c != ')'; c = p.peek(0) {
		atom := p.atom()
		anchor := atom.kind == nodeStart || atom.kind == nodeEnd
		next := p.peek(0)
		if !anchor && (next == '*' || next == '+' || next == '?' || next == '{') {
			atom = p.quantify(atom)
		}
		parts = append(parts, atom)
	}
	size := 0
	for _, part := range parts {
		size += part.size
	}
	return &node{kind: nodeConcat, parts: parts, size: saturate(size)}
}

func (p *parser) atom() *node {
	c := p.take()
	switch c {
	case '(':
		return p.group()
	case '[':
		return &node{kind: nodeSet, set: p.charClass(), size: 1}
	case '\\':
		return setOf(asItem(p.escape()))
	case '*', '+', '?', '{', '}', ']':
		fail()
	case '.':
		return setOf(item{ranges: []rune{0x0a, 0x0a}, negate: true})
	case '^':
		return &node{kind: nodeStart, size: 1}
	case '$':
		return &node{kind: nodeEnd, size: 1}
	case endOfText:
		fail()
	}
	return setOf(single(c))
}

func (p *parser) group() *node {
	inner := p.alternation()
	if p.take() != ')' {
		fail()
	}
	return inner
}

// atomValue is one code point or one of the shorthand classes.
type atomValue struct {
	c       rune
	class   item
	isClass bool
}

func asItem(a atomValue) item {
	if a.isClass {
		return a.class
	}
	return single(a.c)
}

// escape reads after a `\`: one code point, or one of the six shorthand classes.
func (p *parser) escape() atomValue {
	c := p.take()
	if isEscapable(c) {
		return atomValue{c: c}
	}
	switch c {
	case 'n':
		return atomValue{c: 0x0a}
	case 't':
		return atomValue{c: 0x09}
	case 'r':
		return atomValue{c: 0x0d}
	case 'f':
		return atomValue{c: 0x0c}
	case 'v':
		return atomValue{c: 0x0b}
	case 'd':
		return atomValue{class: digitItem, isClass: true}
	case 'w':
		return atomValue{class: wordItem, isClass: true}
	case 's':
		return atomValue{class: spaceItem, isClass: true}
	case 'D':
		return atomValue{class: negated(digitItem), isClass: true}
	case 'W':
		return atomValue{class: negated(wordItem), isClass: true}
	case 'S':
		return atomValue{class: negated(spaceItem), isClass: true}
	}
	fail()
	return atomValue{}
}

// charClass reads after `[`: the members up to `]`.
func (p *parser) charClass() charSet {
	neg := p.peek(0) == '^'
	if neg {
		p.pos++
	}
	items := []item{p.member(true)}
	for p.peek(0) != ']' {
		items = append(items, p.member(false))
	}
	p.pos++
	return charSet{items: items, negate: neg}
}

// member is one member: an atom, or a range of two single code points.
func (p *parser) member(first bool) item {
	from := p.classAtom(first)
	isRange := p.peek(0) == '-' && p.peek(1) != endOfText && p.peek(1) != ']'
	if !isRange {
		return asItem(from)
	}
	p.pos++
	to := p.classAtom(false)
	if from.isClass || to.isClass || from.c > to.c {
		fail()
	}
	return item{ranges: []rune{from.c, to.c}}
}

func (p *parser) classAtom(first bool) atomValue {
	c := p.take()
	// `]` here is a class with no members; the end and `[` are never members.
	if c == endOfText || c == '[' || c == ']' {
		fail()
	}
	if c == '\\' {
		return p.escape()
	}
	// An unescaped `-` is a literal only as the first member or directly before `]`.
	if c == '-' && !first && p.peek(0) != ']' {
		fail()
	}
	return atomValue{c: c}
}

func (p *parser) quantify(body *node) *node {
	c := p.take()
	if c != '{' {
		minimum := 0
		if c == '+' {
			minimum = 1
		}
		maximum := -1
		if c == '?' {
			maximum = 1
		}
		return &node{kind: nodeRepeat, parts: []*node{body}, min: minimum, max: maximum, size: saturate(body.size + 1)}
	}
	minimum := p.count()
	maximum := p.upper(minimum)
	if p.take() != '}' {
		fail()
	}
	if maximum < 0 {
		return repeat(body, minimum, -1, minimum+1)
	}
	if minimum > maximum {
		fail()
	}
	return repeat(body, minimum, maximum, maximum)
}

// upper reads after `{n`: n again for `{n}`, -1 for `{n,}`, m for `{n,m}`. It leaves the `}`.
func (p *parser) upper(minimum int) int {
	if p.peek(0) != ',' {
		return minimum
	}
	p.pos++
	if p.peek(0) == '}' {
		return -1
	}
	return p.count()
}

func repeat(body *node, minimum, maximum, times int) *node {
	size := saturate(times * body.size)
	if size < 1 {
		size = 1
	}
	return &node{kind: nodeRepeat, parts: []*node{body}, min: minimum, max: maximum, size: size}
}

// count reads ASCII digits with no leading zero, at most 100.
func (p *parser) count() int {
	c := p.take()
	if !isDigitRune(c) {
		fail()
	}
	if c == '0' {
		if isDigitRune(p.peek(0)) {
			fail()
		}
		return 0
	}
	value := int(c - '0')
	for isDigitRune(p.peek(0)) {
		value = value*10 + int(p.take()-'0')
		if value > maxCount {
			fail()
		}
	}
	return value
}

func (p *parser) parse() *node {
	n := p.alternation()
	if p.pos < len(p.cps) {
		fail() // an unmatched `)`
	}
	return n
}

// parsePattern is the tree for a pattern's code points, or nil when it is not a valid pattern: too long,
// malformed or too big.
func parsePattern(cps []rune) (tree *node) {
	if len(cps) > maxLength {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(invalid); !ok {
				panic(r)
			}
			tree = nil
		}
	}()
	n := (&parser{cps: cps}).parse()
	if n.size > maxSize {
		return nil
	}
	return n
}

// compilePattern is the compiled pattern, or nil when pattern is not a valid pattern.
func compilePattern(pattern string) *program {
	n := parsePattern(jsonv.CodePoints(pattern))
	if n == nil {
		return nil
	}
	p := &program{}
	p.emit(n)
	p.insts = append(p.insts, inst{op: opMatch})
	return p
}
