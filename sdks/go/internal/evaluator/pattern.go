package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"

// The parser for `matches` patterns, to "Patterns" in docs/evaluation.md: code points in, a tree with each
// node's size out, or nothing for a pattern that is not valid. program.go compiles the tree and runs it.
// There is no regexp import anywhere in this package, and a test holds that.

const (
	maxPatternLength = 1024
	maxPatternSize   = 10000
	maxCount         = 100
	endOfPattern     = rune(-1)
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
	parts []*node // concat parts or alt sides
	body  *node   // repeat body
	min   int
	max   int // -1 is unbounded
	size  int
}

// codePoints reads a WTF-8 string as code points: a valid pair is one, a lone surrogate is itself.
func codePoints(s string) []rune {
	out := make([]rune, 0, len(s))
	for i := 0; i < len(s); {
		r, n := jsonvalue.DecodeRune(s[i:])
		out = append(out, r)
		i += n
	}
	return out
}

var (
	digitItem = item{ranges: []rune{'0', '9'}}
	wordItem  = item{ranges: []rune{'0', '9', 'A', 'Z', '_', '_', 'a', 'z'}}
	// U+0009 to U+000D are \t \n \v \f \r.
	spaceItem = item{ranges: []rune{0x09, 0x0D, 0x20, 0x20}}
)

func negated(i item) item { return item{ranges: i.ranges, negate: true} }

func saturate(size int) int {
	if size > maxPatternSize+1 {
		return maxPatternSize + 1
	}
	return size
}

func isDigitRune(c rune) bool { return c >= '0' && c <= '9' }

func single(c rune) item { return item{ranges: []rune{c, c}} }

func setOf(i item) *node {
	return &node{kind: nodeSet, set: &charSet{items: []item{i}}, size: 1}
}

type patternParser struct {
	cps []rune
	pos int
	bad bool
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

func (p *patternParser) fail() *node {
	p.bad = true
	return nil
}

// parsePattern returns the tree for a pattern, or nil when it is too long, malformed or too big.
func parsePattern(cps []rune) *node {
	if len(cps) > maxPatternLength {
		return nil
	}
	p := &patternParser{cps: cps}
	n := p.alternation()
	if p.bad || n == nil || p.pos < len(cps) { // a left over `)` is unmatched
		return nil
	}
	if n.size > maxPatternSize {
		return nil
	}
	return n
}

func (p *patternParser) alternation() *node {
	first := p.concat()
	if p.bad {
		return nil
	}
	if p.peek(0) != '|' {
		return first
	}
	sides := []*node{first}
	for p.peek(0) == '|' {
		p.pos++
		side := p.concat()
		if p.bad {
			return nil
		}
		sides = append(sides, side)
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
	for c := p.peek(0); c != endOfPattern && c != '|' && c != ')'; c = p.peek(0) {
		atom := p.atom()
		if p.bad {
			return nil
		}
		anchor := atom.kind == nodeStart || atom.kind == nodeEnd
		if q := p.peek(0); !anchor && (q == '*' || q == '+' || q == '?' || q == '{') {
			atom = p.quantify(atom)
			if p.bad {
				return nil
			}
		}
		parts = append(parts, atom)
		size = saturate(size + atom.size)
	}
	return &node{kind: nodeConcat, parts: parts, size: size}
}

func (p *patternParser) atom() *node {
	c := p.take()
	switch c {
	case '(':
		inner := p.alternation()
		if p.bad || p.take() != ')' {
			return p.fail()
		}
		return inner
	case '[':
		set := p.charClass()
		if p.bad {
			return nil
		}
		return &node{kind: nodeSet, set: set, size: 1}
	case '\\':
		it, ok := p.escape()
		if !ok {
			return p.fail()
		}
		return setOf(it)
	case '*', '+', '?', '{', '}', ']', endOfPattern:
		return p.fail()
	case '.':
		return setOf(negated(single('\n')))
	case '^':
		return &node{kind: nodeStart, size: 1}
	case '$':
		return &node{kind: nodeEnd, size: 1}
	}
	return setOf(single(c))
}

// escape is after a `\`: one code point, or one of the six shorthand classes.
func (p *patternParser) escape() (item, bool) {
	c := p.take()
	switch c {
	case '\\', '.', '[', ']', '(', ')', '|', '*', '+', '?', '{', '}', '^', '$', '-':
		return single(c), true
	case 'n':
		return single('\n'), true
	case 't':
		return single('\t'), true
	case 'r':
		return single('\r'), true
	case 'f':
		return single('\f'), true
	case 'v':
		return single('\v'), true
	case 'd':
		return digitItem, true
	case 'w':
		return wordItem, true
	case 's':
		return spaceItem, true
	case 'D':
		return negated(digitItem), true
	case 'W':
		return negated(wordItem), true
	case 'S':
		return negated(spaceItem), true
	}
	return item{}, false
}

// charClass is after `[`: the members up to `]`.
func (p *patternParser) charClass() *charSet {
	neg := p.peek(0) == '^'
	if neg {
		p.pos++
	}
	first, ok := p.member(true)
	if !ok {
		p.bad = true
		return nil
	}
	items := []item{first}
	for p.peek(0) != ']' {
		m, ok := p.member(false)
		if !ok {
			p.bad = true
			return nil
		}
		items = append(items, m)
	}
	p.pos++
	return &charSet{items: items, negate: neg}
}

// member is one member: an atom, or a range of two single code points.
func (p *patternParser) member(first bool) (item, bool) {
	from, ok := p.classAtom(first)
	if !ok {
		return item{}, false
	}
	if p.peek(0) != '-' || p.peek(1) == endOfPattern || p.peek(1) == ']' {
		return from, true
	}
	p.pos++
	to, ok := p.classAtom(false)
	if !ok || !isSingle(from) || !isSingle(to) || from.ranges[0] > to.ranges[0] {
		return item{}, false
	}
	return item{ranges: []rune{from.ranges[0], to.ranges[0]}}, true
}

func isSingle(i item) bool {
	return !i.negate && len(i.ranges) == 2 && i.ranges[0] == i.ranges[1]
}

func (p *patternParser) classAtom(first bool) (item, bool) {
	c := p.take()
	// `]` here is a class with no members; the end and `[` are never members.
	if c == endOfPattern || c == '[' || c == ']' {
		return item{}, false
	}
	if c == '\\' {
		return p.escape()
	}
	// An unescaped `-` is a literal only as the first member or directly before `]`.
	if c == '-' && !first && p.peek(0) != ']' {
		return item{}, false
	}
	return single(c), true
}

func (p *patternParser) quantify(body *node) *node {
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
		return &node{kind: nodeRepeat, body: body, min: minimum, max: maximum, size: saturate(body.size + 1)}
	}
	minimum, ok := p.count()
	if !ok {
		return p.fail()
	}
	maximum := minimum
	if p.peek(0) == ',' {
		p.pos++
		if p.peek(0) == '}' {
			maximum = -1
		} else if maximum, ok = p.count(); !ok {
			return p.fail()
		}
	}
	if p.take() != '}' {
		return p.fail()
	}
	if maximum == -1 {
		return repeatNode(body, minimum, -1, minimum+1)
	}
	if minimum > maximum {
		return p.fail()
	}
	return repeatNode(body, minimum, maximum, maximum)
}

func repeatNode(body *node, minimum, maximum, times int) *node {
	size := saturate(times * body.size)
	if size < 1 {
		size = 1
	}
	return &node{kind: nodeRepeat, body: body, min: minimum, max: maximum, size: size}
}

// count is ASCII digits with no leading zero, at most 100.
func (p *patternParser) count() (int, bool) {
	c := p.take()
	if !isDigitRune(c) {
		return 0, false
	}
	if c == '0' {
		if isDigitRune(p.peek(0)) {
			return 0, false
		}
		return 0, true
	}
	value := int(c - '0')
	for isDigitRune(p.peek(0)) {
		value = value*10 + int(p.take()-'0')
		if value > maxCount {
			return 0, false
		}
	}
	return value, true
}
