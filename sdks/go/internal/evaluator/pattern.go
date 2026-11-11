package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"

// The parser for `matches` patterns, to the "Patterns" section of docs/evaluation.md: code points in, a tree
// with each node's size out, or nil for a pattern that is not valid. regex.go compiles the tree and runs it.

const (
	maxPatternLength = 1024
	maxPatternSize   = 10000
	maxRepeatCount   = 100
	patternEnd       = rune(-1)
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
	kind     nodeKind
	size     int
	set      charSet // nodeSet
	parts    []*node // nodeConcat, and the sides of nodeAlt
	body     *node   // nodeRepeat
	min, max int     // nodeRepeat; max is -1 for no upper bound
}

var (
	digitItem = item{ranges: []rune{'0', '9'}}
	wordItem  = item{ranges: []rune{'0', '9', 'A', 'Z', '_', '_', 'a', 'z'}}
	// U+0009 to U+000D are \t \n \v \f \r.
	spaceItem = item{ranges: []rune{0x09, 0x0d, ' ', ' '}}
)

func negated(i item) item { return item{ranges: i.ranges, negate: true} }

func single(c rune) item { return item{ranges: []rune{c, c}} }

func setOf(i item) *node {
	return &node{kind: nodeSet, size: 1, set: charSet{items: []item{i}}}
}

// atomValue is what an escape or a class atom reads: one code point, or one of the six shorthand classes.
type atomValue struct {
	isItem bool
	c      rune
	it     item
}

func (a atomValue) asItem() item {
	if a.isItem {
		return a.it
	}
	return single(a.c)
}

type patternParser struct {
	cps []rune
	pos int
	bad bool
}

func saturate(size int) int {
	if size > maxPatternSize+1 {
		return maxPatternSize + 1
	}
	return size
}

func isRuneDigit(c rune) bool { return c >= '0' && c <= '9' }

func (p *patternParser) peek(offset int) rune {
	if p.pos+offset < len(p.cps) {
		return p.cps[p.pos+offset]
	}
	return patternEnd
}

func (p *patternParser) take() rune {
	c := p.peek(0)
	p.pos++
	return c
}

func (p *patternParser) fail() { p.bad = true }

func (p *patternParser) alternation() *node {
	sides := []*node{p.concat()}
	for !p.bad && p.peek(0) == '|' {
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

func (p *patternParser) concat() *node {
	var parts []*node
	size := 0
	for c := p.peek(0); !p.bad && c != patternEnd && c != '|' && c != ')'; c = p.peek(0) {
		atom := p.atom()
		if p.bad {
			break
		}
		anchor := atom.kind == nodeStart || atom.kind == nodeEnd
		if c := p.peek(0); !anchor && (c == '*' || c == '+' || c == '?' || c == '{') {
			atom = p.quantify(atom)
		}
		parts = append(parts, atom)
		size += atom.size
	}
	return &node{kind: nodeConcat, parts: parts, size: saturate(size)}
}

func (p *patternParser) atom() *node {
	c := p.take()
	switch c {
	case '(':
		return p.group()
	case '[':
		return &node{kind: nodeSet, set: p.charClass(), size: 1}
	case '\\':
		return setOf(p.escape().asItem())
	case '*', '+', '?', '{', '}', ']', patternEnd:
		p.fail()
		return nil
	case '.':
		return setOf(item{ranges: []rune{'\n', '\n'}, negate: true})
	case '^':
		return &node{kind: nodeStart, size: 1}
	case '$':
		return &node{kind: nodeEnd, size: 1}
	}
	return setOf(single(c))
}

func (p *patternParser) group() *node {
	inner := p.alternation()
	if p.take() != ')' {
		p.fail()
	}
	return inner
}

// escape reads what follows a backslash: one code point, or one of the six shorthand classes.
func (p *patternParser) escape() atomValue {
	c := p.take()
	switch c {
	case '\\', '.', '[', ']', '(', ')', '|', '*', '+', '?', '{', '}', '^', '$', '-':
		return atomValue{c: c}
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
		return atomValue{isItem: true, it: digitItem}
	case 'w':
		return atomValue{isItem: true, it: wordItem}
	case 's':
		return atomValue{isItem: true, it: spaceItem}
	case 'D':
		return atomValue{isItem: true, it: negated(digitItem)}
	case 'W':
		return atomValue{isItem: true, it: negated(wordItem)}
	case 'S':
		return atomValue{isItem: true, it: negated(spaceItem)}
	}
	p.fail()
	return atomValue{}
}

// charClass reads a class after its "[": the members up to "]".
func (p *patternParser) charClass() charSet {
	negate := p.peek(0) == '^'
	if negate {
		p.pos++
	}
	items := []item{p.member(true)}
	for !p.bad && p.peek(0) != ']' {
		items = append(items, p.member(false))
	}
	p.pos++
	return charSet{items: items, negate: negate}
}

// member reads one member: an atom, or a range of two single code points.
func (p *patternParser) member(first bool) item {
	from := p.classAtom(first)
	if p.bad {
		return item{}
	}
	isRange := p.peek(0) == '-' && p.peek(1) != patternEnd && p.peek(1) != ']'
	if !isRange {
		return from.asItem()
	}
	p.pos++
	to := p.classAtom(false)
	if p.bad || from.isItem || to.isItem || from.c > to.c {
		p.fail()
		return item{}
	}
	return item{ranges: []rune{from.c, to.c}}
}

func (p *patternParser) classAtom(first bool) atomValue {
	c := p.take()
	// "]" here is a class with no members; the end and "[" are never members.
	if c == patternEnd || c == '[' || c == ']' {
		p.fail()
		return atomValue{}
	}
	if c == '\\' {
		return p.escape()
	}
	// An unescaped "-" is a literal only as the first member or directly before "]".
	if c == '-' && !first && p.peek(0) != ']' {
		p.fail()
		return atomValue{}
	}
	return atomValue{c: c}
}

func (p *patternParser) quantify(body *node) *node {
	c := p.take()
	if c != '{' {
		min, max := 0, -1
		if c == '+' {
			min = 1
		}
		if c == '?' {
			max = 1
		}
		return &node{kind: nodeRepeat, body: body, min: min, max: max, size: saturate(body.size + 1)}
	}
	min := p.count()
	max := p.upper(min)
	if p.take() != '}' {
		p.fail()
	}
	if p.bad {
		return body
	}
	if max < 0 {
		return repeatNode(body, min, -1, min+1)
	}
	if min > max {
		p.fail()
		return body
	}
	return repeatNode(body, min, max, max)
}

// upper reads what follows "{n": n again for "{n}", -1 for "{n,}", m for "{n,m}". It leaves the "}".
func (p *patternParser) upper(min int) int {
	if p.peek(0) != ',' {
		return min
	}
	p.pos++
	if p.peek(0) == '}' {
		return -1
	}
	return p.count()
}

func repeatNode(body *node, min, max, times int) *node {
	size := saturate(times * body.size)
	if size < 1 {
		size = 1
	}
	return &node{kind: nodeRepeat, body: body, min: min, max: max, size: size}
}

// count reads ASCII digits with no leading zero, at most 100.
func (p *patternParser) count() int {
	c := p.take()
	if !isRuneDigit(c) {
		p.fail()
		return 0
	}
	if c == '0' {
		if isRuneDigit(p.peek(0)) {
			p.fail()
		}
		return 0
	}
	value := int(c - '0')
	for isRuneDigit(p.peek(0)) {
		value = value*10 + int(p.take()-'0')
		if value > maxRepeatCount {
			p.fail()
			return 0
		}
	}
	return value
}

// parsePattern is the tree for a pattern's code points, or nil when it is not a valid pattern: too long,
// malformed or too big.
func parsePattern(cps []rune) *node {
	if len(cps) > maxPatternLength {
		return nil
	}
	p := &patternParser{cps: cps}
	n := p.alternation()
	if p.bad || p.pos < len(cps) { // the second is an unmatched ")"
		return nil
	}
	if n.size > maxPatternSize {
		return nil
	}
	return n
}

// codePoints reads a WTF-8 string as code points: a valid pair is one, a lone surrogate is itself.
func codePoints(s string) []rune { return jsonv.Runes(s) }
