package eval

// The parser for `matches` patterns, to the "Patterns" section of docs/evaluation.md: code points in, a
// tree with each node's size out, or nil for a pattern that is not valid.

const (
	maxPatternLength = 1024
	maxPatternSize   = 10000
	maxPatternCount  = 100
	endOfPattern     = -1
)

// item is code point ranges as pairs lo, hi (inclusive), or their complement when negate is set.
type item struct {
	ranges []rune
	negate bool
}

// charSet is any of its items, or none of them when negate is set.
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
	parts []*node // concat parts, alt sides, or the one body of a repeat
	min   int
	max   int // -1 is no upper bound
}

type parser struct {
	cps []rune
	pos int
	bad bool
}

func single(c rune) item { return item{ranges: []rune{c, c}} }

func setOf(it item) *node {
	return &node{kind: nodeSet, set: charSet{items: []item{it}}, size: 1}
}

var (
	digitItem = item{ranges: []rune{'0', '9'}}
	wordItem  = item{ranges: []rune{'0', '9', 'A', 'Z', '_', '_', 'a', 'z'}}
	// U+0009 to U+000D are \t \n \v \f \r.
	spaceItem = item{ranges: []rune{0x09, 0x0d, ' ', ' '}}
)

func negated(it item) item { return item{ranges: it.ranges, negate: true} }

func isPatternDigit(c rune) bool { return c >= '0' && c <= '9' }

func saturate(size int) int {
	if size > maxPatternSize+1 {
		return maxPatternSize + 1
	}
	return size
}

func isEscapable(c rune) bool {
	switch c {
	case '\\', '.', '[', ']', '(', ')', '|', '*', '+', '?', '{', '}', '^', '$', '-':
		return true
	}
	return false
}

func isQuantifier(c rune) bool { return c == '*' || c == '+' || c == '?' || c == '{' }

func notAnAtom(c rune) bool {
	return c == '*' || c == '+' || c == '?' || c == '{' || c == '}' || c == ']'
}

func (p *parser) fail() {
	p.bad = true
}

func (p *parser) peek(offset int) rune {
	if i := p.pos + offset; i < len(p.cps) {
		return p.cps[i]
	}
	return endOfPattern
}

func (p *parser) take() rune {
	c := p.peek(0)
	p.pos++
	return c
}

func (p *parser) parse() *node {
	n := p.alternation()
	if p.bad || p.pos < len(p.cps) { // an unmatched `)` leaves input behind
		return nil
	}
	return n
}

func (p *parser) alternation() *node {
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

func (p *parser) concat() *node {
	var parts []*node
	size := 0
	for c := p.peek(0); !p.bad && c != endOfPattern && c != '|' && c != ')'; c = p.peek(0) {
		atom := p.atom()
		if p.bad {
			break
		}
		anchor := atom.kind == nodeStart || atom.kind == nodeEnd
		if !anchor && isQuantifier(p.peek(0)) {
			atom = p.quantify(atom)
		}
		parts = append(parts, atom)
		size += atom.size
	}
	return &node{kind: nodeConcat, parts: parts, size: saturate(size)}
}

func (p *parser) atom() *node {
	c := p.take()
	switch {
	case c == '(':
		return p.group()
	case c == '[':
		return &node{kind: nodeSet, set: p.charClass(), size: 1}
	case c == '\\':
		return setOf(p.escape())
	case notAnAtom(c):
		p.fail()
		return nil
	case c == '.':
		return setOf(item{ranges: []rune{'\n', '\n'}, negate: true})
	case c == '^':
		return &node{kind: nodeStart, size: 1}
	case c == '$':
		return &node{kind: nodeEnd, size: 1}
	}
	return setOf(single(c))
}

func (p *parser) group() *node {
	inner := p.alternation()
	if p.bad || p.take() != ')' {
		p.fail()
	}
	return inner
}

// escape reads after a `\`: one code point, or one of the six shorthand classes.
func (p *parser) escape() item {
	c := p.take()
	if isEscapable(c) {
		return single(c)
	}
	switch c {
	case 'n':
		return single(0x0a)
	case 't':
		return single(0x09)
	case 'r':
		return single(0x0d)
	case 'f':
		return single(0x0c)
	case 'v':
		return single(0x0b)
	case 'd':
		return digitItem
	case 'w':
		return wordItem
	case 's':
		return spaceItem
	case 'D':
		return negated(digitItem)
	case 'W':
		return negated(wordItem)
	case 'S':
		return negated(spaceItem)
	}
	p.fail()
	return item{}
}

// charClass reads after `[`: the members up to `]`.
func (p *parser) charClass() charSet {
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

// classAtom is one member atom: a code point (single is true) or a shorthand class.
func (p *parser) classAtom(first bool) (it item, single bool) {
	c := p.take()
	// `]` here is a class with no members; the end and `[` are never members.
	if c == endOfPattern || c == '[' || c == ']' {
		p.fail()
		return item{}, false
	}
	if c == '\\' {
		it = p.escape()
		return it, len(it.ranges) == 2 && it.ranges[0] == it.ranges[1] && !it.negate
	}
	// An unescaped `-` is a literal only as the first member or directly before `]`.
	if c == '-' && !first && p.peek(0) != ']' {
		p.fail()
		return item{}, false
	}
	return item{ranges: []rune{c, c}}, true
}

// member is one member: an atom, or a range of two single code points.
func (p *parser) member(first bool) item {
	from, fromSingle := p.classAtom(first)
	if p.bad {
		return item{}
	}
	isRange := p.peek(0) == '-' && p.peek(1) != endOfPattern && p.peek(1) != ']'
	if !isRange {
		return from
	}
	p.pos++
	to, toSingle := p.classAtom(false)
	if p.bad || !fromSingle || !toSingle || from.ranges[0] > to.ranges[0] {
		p.fail()
		return item{}
	}
	return item{ranges: []rune{from.ranges[0], to.ranges[0]}}
}

func (p *parser) quantify(body *node) *node {
	c := p.take()
	if c != '{' {
		lo, hi := 0, -1
		if c == '+' {
			lo = 1
		}
		if c == '?' {
			hi = 1
		}
		return &node{kind: nodeRepeat, parts: []*node{body}, min: lo, max: hi, size: saturate(body.size + 1)}
	}
	lo := p.count()
	hi, bounded := p.upper(lo)
	if p.bad || p.take() != '}' {
		p.fail()
		return body
	}
	if !bounded {
		return repeatNode(body, lo, -1, lo+1)
	}
	if lo > hi {
		p.fail()
		return body
	}
	return repeatNode(body, lo, hi, hi)
}

// upper reads after `{n`: n again for `{n}`, unbounded for `{n,}`, m for `{n,m}`. It leaves the `}`.
func (p *parser) upper(lo int) (int, bool) {
	if p.peek(0) != ',' {
		return lo, true
	}
	p.pos++
	if p.peek(0) == '}' {
		return 0, false
	}
	return p.count(), true
}

func repeatNode(body *node, lo, hi, times int) *node {
	size := saturate(times * body.size)
	if size < 1 {
		size = 1
	}
	return &node{kind: nodeRepeat, parts: []*node{body}, min: lo, max: hi, size: size}
}

// count reads ASCII digits with no leading zero, at most 100.
func (p *parser) count() int {
	c := p.take()
	if !isPatternDigit(c) {
		p.fail()
		return 0
	}
	if c == '0' {
		if isPatternDigit(p.peek(0)) {
			p.fail()
		}
		return 0
	}
	value := int(c - '0')
	for isPatternDigit(p.peek(0)) {
		value = value*10 + int(p.take()-'0')
		if value > maxPatternCount {
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
	p := &parser{cps: cps}
	n := p.parse()
	if n == nil || n.size > maxPatternSize {
		return nil
	}
	return n
}
