package eval

import "github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"

// The parser for "matches" patterns, to the "Patterns" section of docs/evaluation.md: code points in, a
// tree with each node's size out, or false for a pattern that is not valid. pattern.go compiles the tree
// and runs it. Nothing here uses the regexp package.

const (
	maxPatternLength = 1024
	maxPatternSize   = 10000
	maxRepeatCount   = 100
	endOfPattern     = -1
)

// item is code point ranges as pairs lo, hi, inclusive, or their complement when negate is set.
type item struct {
	ranges []rune
	negate bool
}

// charSet is a class: any of its items, or none of them when negate is set.
type charSet struct {
	items  []item
	negate bool
}

type nodeKind uint8

const (
	nodeSet nodeKind = iota
	nodeStart
	nodeEnd
	nodeConcat
	nodeAlt
	nodeRepeat
)

// node is one node of the tree. A repeat with max -1 has no upper bound.
type node struct {
	kind     nodeKind
	set      *charSet
	parts    []*node // the parts of a concat or the sides of an alt
	body     *node
	min, max int
	size     int
}

func single(c rune) item { return item{ranges: []rune{c, c}} }

func setOf(i item) *node {
	return &node{kind: nodeSet, set: &charSet{items: []item{i}}, size: 1}
}

var (
	digitItem = item{ranges: []rune{'0', '9'}}
	wordItem  = item{ranges: []rune{'0', '9', 'A', 'Z', '_', '_', 'a', 'z'}}
	// U+0009 to U+000D are \t \n \v \f \r.
	spaceItem = item{ranges: []rune{0x09, 0x0d, ' ', ' '}}
)

func negated(i item) item { return item{ranges: i.ranges, negate: true} }

// escapable: a backslash before one of these is that character.
func escapable(c rune) bool {
	switch c {
	case '\\', '.', '[', ']', '(', ')', '|', '*', '+', '?', '{', '}', '^', '$', '-':
		return true
	}
	return false
}

// escapeClass maps the escapes that are not one character: a code point, or one of the shorthand classes.
func escapeClass(c rune) (cp rune, shorthand item, isItem, ok bool) {
	switch c {
	case 'n':
		return 0x0a, item{}, false, true
	case 't':
		return 0x09, item{}, false, true
	case 'r':
		return 0x0d, item{}, false, true
	case 'f':
		return 0x0c, item{}, false, true
	case 'v':
		return 0x0b, item{}, false, true
	case 'd':
		return 0, digitItem, true, true
	case 'w':
		return 0, wordItem, true, true
	case 's':
		return 0, spaceItem, true, true
	case 'D':
		return 0, negated(digitItem), true, true
	case 'W':
		return 0, negated(wordItem), true, true
	case 'S':
		return 0, negated(spaceItem), true, true
	}
	return 0, item{}, false, false
}

// notAnAtom: code points that cannot start an atom outside a class.
func notAnAtom(c rune) bool {
	switch c {
	case '*', '+', '?', '{', '}', ']':
		return true
	}
	return false
}

func isQuantifier(c rune) bool { return c == '*' || c == '+' || c == '?' || c == '{' }

type invalidPattern struct{}

func fail() { panic(invalidPattern{}) }

func saturate(size int) int { return min(size, maxPatternSize+1) }

// atomValue is a class atom or an escape: one code point, or a shorthand class.
type atomValue struct {
	cp      rune
	shorthd item
	isItem  bool
}

func (a atomValue) asItem() item {
	if a.isItem {
		return a.shorthd
	}
	return single(a.cp)
}

type patternParser struct {
	cps []rune
	pos int
}

func (p *patternParser) peek(offset int) rune {
	if p.pos+offset >= len(p.cps) {
		return endOfPattern
	}
	return p.cps[p.pos+offset]
}

func (p *patternParser) take() rune {
	c := p.peek(0)
	p.pos++
	return c
}

func (p *patternParser) parse() *node {
	n := p.alternation()
	if p.pos < len(p.cps) { // an unmatched ")"
		fail()
	}
	return n
}

func (p *patternParser) alternation() *node {
	sides := []*node{p.concat()}
	for p.peek(0) == '|' {
		p.pos++
		sides = append(sides, p.concat())
	}
	if len(sides) == 1 {
		return sides[0]
	}
	size := len(sides) - 1
	for _, side := range sides {
		size += side.size
	}
	return &node{kind: nodeAlt, parts: sides, size: saturate(size)}
}

func (p *patternParser) concat() *node {
	var parts []*node
	for c := p.peek(0); c != endOfPattern && c != '|' && c != ')'; c = p.peek(0) {
		atom := p.atom()
		anchor := atom.kind == nodeStart || atom.kind == nodeEnd
		if !anchor && isQuantifier(p.peek(0)) {
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

func (p *patternParser) atom() *node {
	c := p.take()
	switch {
	case c == '(':
		return p.group()
	case c == '[':
		set := p.charClass()
		return &node{kind: nodeSet, set: &set, size: 1}
	case c == '\\':
		return setOf(p.escape().asItem())
	case notAnAtom(c):
		fail()
	case c == '.':
		return setOf(item{ranges: []rune{0x0a, 0x0a}, negate: true})
	case c == '^':
		return &node{kind: nodeStart, size: 1}
	case c == '$':
		return &node{kind: nodeEnd, size: 1}
	}
	return setOf(single(c))
}

func (p *patternParser) group() *node {
	inner := p.alternation()
	if p.take() != ')' {
		fail()
	}
	return inner
}

// escape reads what follows a backslash: one code point, or one of the six shorthand classes.
func (p *patternParser) escape() atomValue {
	c := p.take()
	if escapable(c) {
		return atomValue{cp: c}
	}
	cp, shorthand, isItem, ok := escapeClass(c)
	if !ok {
		fail()
	}
	return atomValue{cp: cp, shorthd: shorthand, isItem: isItem}
}

// charClass reads the members after "[" up to "]".
func (p *patternParser) charClass() charSet {
	negate := p.peek(0) == '^'
	if negate {
		p.pos++
	}
	items := []item{p.member(true)}
	for p.peek(0) != ']' {
		items = append(items, p.member(false))
	}
	p.pos++
	return charSet{items: items, negate: negate}
}

// member is one member: an atom, or a range of two single code points.
func (p *patternParser) member(first bool) item {
	from := p.classAtom(first)
	isRange := p.peek(0) == '-' && p.peek(1) != endOfPattern && p.peek(1) != ']'
	if !isRange {
		return from.asItem()
	}
	p.pos++
	to := p.classAtom(false)
	if from.isItem || to.isItem || from.cp > to.cp {
		fail()
	}
	return item{ranges: []rune{from.cp, to.cp}}
}

func (p *patternParser) classAtom(first bool) atomValue {
	c := p.take()
	// "]" here is a class with no members; the end and "[" are never members.
	if c == endOfPattern || c == '[' || c == ']' {
		fail()
	}
	if c == '\\' {
		return p.escape()
	}
	// An unescaped "-" is a literal only as the first member or directly before "]".
	if c == '-' && !first && p.peek(0) != ']' {
		fail()
	}
	return atomValue{cp: c}
}

func (p *patternParser) quantify(body *node) *node {
	c := p.take()
	if c != '{' {
		minimum, maximum := 0, -1
		if c == '+' {
			minimum = 1
		}
		if c == '?' {
			maximum = 1
		}
		return &node{kind: nodeRepeat, body: body, min: minimum, max: maximum, size: saturate(body.size + 1)}
	}
	minimum := p.count()
	maximum := p.upper(minimum)
	if p.take() != '}' {
		fail()
	}
	if maximum < 0 {
		return repeatNode(body, minimum, -1, minimum+1)
	}
	if minimum > maximum {
		fail()
	}
	return repeatNode(body, minimum, maximum, maximum)
}

// upper reads what follows {n: n again for {n}, -1 for {n,}, m for {n,m}. It leaves the "}".
func (p *patternParser) upper(minimum int) int {
	if p.peek(0) != ',' {
		return minimum
	}
	p.pos++
	if p.peek(0) == '}' {
		return -1
	}
	return p.count()
}

func repeatNode(body *node, minimum, maximum, times int) *node {
	return &node{kind: nodeRepeat, body: body, min: minimum, max: maximum, size: max(1, saturate(times*body.size))}
}

func isPatternDigit(c rune) bool { return c >= '0' && c <= '9' }

// count reads ASCII digits with no leading zero, at most 100.
func (p *patternParser) count() int {
	c := p.take()
	if !isPatternDigit(c) {
		fail()
	}
	if c == '0' {
		if isPatternDigit(p.peek(0)) {
			fail()
		}
		return 0
	}
	value := int(c - '0')
	for isPatternDigit(p.peek(0)) {
		value = value*10 + int(p.take()-'0')
		if value > maxRepeatCount {
			fail()
		}
	}
	return value
}

// parsePattern is the tree for a pattern's code points, or false when it is not a valid pattern: too long,
// malformed or too big.
func parsePattern(cps []rune) (n *node, ok bool) {
	if len(cps) > maxPatternLength {
		return nil, false
	}
	defer func() {
		if r := recover(); r != nil {
			if _, isInvalid := r.(invalidPattern); !isInvalid {
				panic(r)
			}
			n, ok = nil, false
		}
	}()
	n = (&patternParser{cps: cps}).parse()
	return n, n.size <= maxPatternSize
}

// codePoints of a WTF-8 string; a lone surrogate is a code point of its own.
func codePoints(s string) []rune { return wtf8.CodePoints(s) }
