package eval

import "github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"

// The parser for `matches` patterns, to the "Patterns" section of docs/evaluation.md: code points in, a
// tree with each node's size out, or nil for a pattern that is not valid.

const (
	patternMaxLength = 1024
	patternMaxSize   = 10000
	patternMaxCount  = 100
	patternEnd       = -1
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

type nodeKind uint8

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
	set   *charSet
	parts []*node // concat parts, or alt sides
	body  *node   // repeat
	min   int
	max   int // -1 is unbounded
	size  int
}

type invalidPattern struct{}

func fail() { panic(invalidPattern{}) }

func saturate(size int) int {
	if size > patternMaxSize+1 {
		return patternMaxSize + 1
	}
	return size
}

func isDigitRune(c rune) bool { return c >= '0' && c <= '9' }

func single(c rune) item { return item{ranges: []rune{c, c}} }

func setOf(it item) *node {
	return &node{kind: nodeSet, set: &charSet{items: []item{it}}, size: 1}
}

var (
	digitItem = item{ranges: []rune{'0', '9'}}
	wordItem  = item{ranges: []rune{'0', '9', 'A', 'Z', '_', '_', 'a', 'z'}}
	// U+0009 to U+000D are \t \n \v \f \r.
	spaceItem = item{ranges: []rune{0x09, 0x0d, ' ', ' '}}
)

func negated(it item) item { return item{ranges: it.ranges, negate: true} }

// escapable are the characters a backslash makes literal.
const escapable = "\\.[]()|*+?{}^$-"

type parser struct {
	cps []rune
	pos int
}

func (p *parser) peek(offset int) rune {
	if p.pos+offset < len(p.cps) {
		return p.cps[p.pos+offset]
	}
	return patternEnd
}

func (p *parser) take() rune {
	c := p.peek(0)
	p.pos++
	return c
}

func (p *parser) parse() *node {
	n := p.alternation()
	if p.pos < len(p.cps) {
		fail() // an unmatched `)`
	}
	return n
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
	for c := p.peek(0); c != patternEnd && c != '|' && c != ')'; c = p.peek(0) {
		atom := p.atom()
		anchor := atom.kind == nodeStart || atom.kind == nodeEnd
		next := p.peek(0)
		quantifier := next == '*' || next == '+' || next == '?' || next == '{'
		if !anchor && quantifier {
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
		return setOf(p.escapeItem())
	case '*', '+', '?', '{', '}', ']':
		fail()
	case '.':
		return setOf(item{ranges: []rune{'\n', '\n'}, negate: true})
	case '^':
		return &node{kind: nodeStart, size: 1}
	case '$':
		return &node{kind: nodeEnd, size: 1}
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

// escape reads after a `\`: one code point (lit true) or one of the six shorthand classes.
func (p *parser) escape() (rune, item, bool) {
	c := p.take()
	if c != patternEnd {
		for _, e := range escapable {
			if e == c {
				return c, item{}, true
			}
		}
	}
	switch c {
	case 'n':
		return '\n', item{}, true
	case 't':
		return '\t', item{}, true
	case 'r':
		return '\r', item{}, true
	case 'f':
		return '\f', item{}, true
	case 'v':
		return '\v', item{}, true
	case 'd':
		return 0, digitItem, false
	case 'w':
		return 0, wordItem, false
	case 's':
		return 0, spaceItem, false
	case 'D':
		return 0, negated(digitItem), false
	case 'W':
		return 0, negated(wordItem), false
	case 'S':
		return 0, negated(spaceItem), false
	}
	fail()
	return 0, item{}, false
}

func (p *parser) escapeItem() item {
	c, it, lit := p.escape()
	if lit {
		return single(c)
	}
	return it
}

// charClass reads after `[`: the members up to `]`.
func (p *parser) charClass() *charSet {
	negate := p.peek(0) == '^'
	if negate {
		p.pos++
	}
	items := []item{p.member(true)}
	for p.peek(0) != ']' {
		items = append(items, p.member(false))
	}
	p.pos++
	return &charSet{items: items, negate: negate}
}

// member reads one member: an atom, or a range of two single code points.
func (p *parser) member(first bool) item {
	fromC, fromItem, fromLit := p.classAtom(first)
	isRange := p.peek(0) == '-' && p.peek(1) != patternEnd && p.peek(1) != ']'
	if !isRange {
		if fromLit {
			return single(fromC)
		}
		return fromItem
	}
	p.pos++
	toC, _, toLit := p.classAtom(false)
	if !fromLit || !toLit || fromC > toC {
		fail()
	}
	return item{ranges: []rune{fromC, toC}}
}

func (p *parser) classAtom(first bool) (rune, item, bool) {
	c := p.take()
	// `]` here is a class with no members; the end and `[` are never members.
	if c == patternEnd || c == '[' || c == ']' {
		fail()
	}
	if c == '\\' {
		return p.escape()
	}
	// An unescaped `-` is a literal only as the first member or directly before `]`.
	if c == '-' && !first && p.peek(0) != ']' {
		fail()
	}
	return c, item{}, true
}

func (p *parser) quantify(body *node) *node {
	c := p.take()
	if c != '{' {
		min := 0
		if c == '+' {
			min = 1
		}
		max := -1
		if c == '?' {
			max = 1
		}
		return &node{kind: nodeRepeat, body: body, min: min, max: max, size: saturate(body.size + 1)}
	}
	min := p.count()
	max := p.upper(min)
	if p.take() != '}' {
		fail()
	}
	if max == -1 {
		return p.repeat(body, min, -1, min+1)
	}
	if min > max {
		fail()
	}
	return p.repeat(body, min, max, max)
}

// upper reads after `{n`: n again for `{n}`, -1 for `{n,}`, m for `{n,m}`. It leaves the `}`.
func (p *parser) upper(min int) int {
	if p.peek(0) != ',' {
		return min
	}
	p.pos++
	if p.peek(0) == '}' {
		return -1
	}
	return p.count()
}

func (p *parser) repeat(body *node, min, max, times int) *node {
	size := saturate(times * body.size)
	if size < 1 {
		size = 1
	}
	return &node{kind: nodeRepeat, body: body, min: min, max: max, size: size}
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
		if value > patternMaxCount {
			fail()
		}
	}
	return value
}

// parsePattern returns the tree for a pattern, or nil when it is not a valid pattern: too long,
// malformed or too big.
func parsePattern(text string) (tree *node) {
	cps := wtf8.CodePoints(text)
	if len(cps) > patternMaxLength {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(invalidPattern); !ok {
				panic(r)
			}
			tree = nil
		}
	}()
	n := (&parser{cps: cps}).parse()
	if n.size > patternMaxSize {
		return nil
	}
	return n
}
