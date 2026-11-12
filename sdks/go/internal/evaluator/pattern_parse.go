package evaluator

// The parser for `matches` patterns, to the "Patterns" section of docs/evaluation.md: code points in, a tree with
// each node's size out, or false for a pattern that is not valid. pattern.go compiles the tree and runs it.

const (
	maxPatternLength = 1024
	maxPatternSize   = 10000
	maxCount         = 100
	endOfPattern     = -1
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
	parts []*node // the parts of a concat, the sides of an alt, or the one body of a repeat
	min   int
	max   int // -1 for no upper bound
	size  int
}

func single(c rune) item { return item{ranges: []rune{c, c}} }

func setOf(it item) *node {
	return &node{kind: nodeSet, set: &charSet{items: []item{it}}, size: 1}
}

var (
	digitItem = item{ranges: []rune{'0', '9'}}
	wordItem  = item{ranges: []rune{'0', '9', 'A', 'Z', '_', '_', 'a', 'z'}}
	// U+0009 to U+000D are \t \n \v \f \r.
	spaceItem = item{ranges: []rune{0x09, 0x0d, 0x20, 0x20}}
)

func complement(it item) item { return item{ranges: it.ranges, negate: true} }

// escaped is what a backslash before a code point stands for: one code point, or one of the shorthand classes.
type escaped struct {
	point rune
	class *item
}

func shorthand(c rune) (escaped, bool) {
	switch c {
	case 'n':
		return escaped{point: 0x0a}, true
	case 't':
		return escaped{point: 0x09}, true
	case 'r':
		return escaped{point: 0x0d}, true
	case 'f':
		return escaped{point: 0x0c}, true
	case 'v':
		return escaped{point: 0x0b}, true
	case 'd':
		return escaped{class: &digitItem}, true
	case 'w':
		return escaped{class: &wordItem}, true
	case 's':
		return escaped{class: &spaceItem}, true
	case 'D':
		it := complement(digitItem)
		return escaped{class: &it}, true
	case 'W':
		it := complement(wordItem)
		return escaped{class: &it}, true
	case 'S':
		it := complement(spaceItem)
		return escaped{class: &it}, true
	}
	return escaped{}, false
}

func isEscapable(c rune) bool {
	switch c {
	case '\\', '.', '[', ']', '(', ')', '|', '*', '+', '?', '{', '}', '^', '$', '-':
		return true
	}
	return false
}

func (e escaped) item() item {
	if e.class != nil {
		return *e.class
	}
	return single(e.point)
}

func saturate(size int) int { return min(size, maxPatternSize+1) }

type patternParser struct {
	cps []rune
	pos int
	bad bool
}

func (p *patternParser) fail() { p.bad = true }

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
	if p.bad || p.pos < len(p.cps) { // the second is an unmatched ')'
		return nil
	}
	return n
}

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
	for c := p.peek(0); !p.bad && c != endOfPattern && c != '|' && c != ')'; c = p.peek(0) {
		atom := p.atom()
		if p.bad {
			break
		}
		anchor := atom.kind == nodeStart || atom.kind == nodeEnd
		if next := p.peek(0); !anchor && (next == '*' || next == '+' || next == '?' || next == '{') {
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
		return setOf(p.escape().item())
	case '*', '+', '?', '{', '}', ']':
		p.fail()
		return &node{}
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

// escape reads after a backslash: one code point, or one of the six shorthand classes.
func (p *patternParser) escape() escaped {
	c := p.take()
	if isEscapable(c) {
		return escaped{point: c}
	}
	e, ok := shorthand(c)
	if !ok {
		p.fail()
	}
	return e
}

// charClass reads after '[': the members up to ']'.
func (p *patternParser) charClass() *charSet {
	negate := p.peek(0) == '^'
	if negate {
		p.pos++
	}
	items := []item{p.member(true)}
	for !p.bad && p.peek(0) != ']' {
		items = append(items, p.member(false))
	}
	p.pos++
	return &charSet{items: items, negate: negate}
}

// member reads one member: an atom, or a range of two single code points.
func (p *patternParser) member(first bool) item {
	from := p.classAtom(first)
	if p.bad {
		return item{}
	}
	isRange := p.peek(0) == '-' && p.peek(1) != endOfPattern && p.peek(1) != ']'
	if !isRange {
		return from.item()
	}
	p.pos++
	to := p.classAtom(false)
	if p.bad || from.class != nil || to.class != nil || from.point > to.point {
		p.fail()
		return item{}
	}
	return item{ranges: []rune{from.point, to.point}}
}

func (p *patternParser) classAtom(first bool) escaped {
	c := p.take()
	// ']' here is a class with no members; the end and '[' are never members.
	if c == endOfPattern || c == '[' || c == ']' {
		p.fail()
		return escaped{}
	}
	if c == '\\' {
		return p.escape()
	}
	// An unescaped '-' is a literal only as the first member or directly before ']'.
	if c == '-' && !first && p.peek(0) != ']' {
		p.fail()
	}
	return escaped{point: c}
}

func (p *patternParser) quantify(body *node) *node {
	c := p.take()
	if c != '{' {
		minCount := 0
		if c == '+' {
			minCount = 1
		}
		maxCount := -1
		if c == '?' {
			maxCount = 1
		}
		return &node{kind: nodeRepeat, parts: []*node{body}, min: minCount, max: maxCount, size: saturate(body.size + 1)}
	}
	lo := p.count()
	hi := p.upper(lo)
	if p.take() != '}' {
		p.fail()
	}
	if p.bad {
		return body
	}
	if hi < 0 {
		return p.repeat(body, lo, -1, lo+1)
	}
	if lo > hi {
		p.fail()
		return body
	}
	return p.repeat(body, lo, hi, hi)
}

// upper reads after {n: n again for {n}, -1 for {n,}, m for {n,m}. It leaves the '}'.
func (p *patternParser) upper(lo int) int {
	if p.peek(0) != ',' {
		return lo
	}
	p.pos++
	if p.peek(0) == '}' {
		return -1
	}
	return p.count()
}

func (p *patternParser) repeat(body *node, lo, hi, times int) *node {
	return &node{kind: nodeRepeat, parts: []*node{body}, min: lo, max: hi, size: max(1, saturate(times*body.size))}
}

// count reads ASCII digits with no leading zero, at most 100.
func (p *patternParser) count() int {
	c := p.take()
	if c < '0' || c > '9' {
		p.fail()
		return 0
	}
	if c == '0' {
		if next := p.peek(0); next >= '0' && next <= '9' {
			p.fail()
		}
		return 0
	}
	value := int(c - '0')
	for next := p.peek(0); next >= '0' && next <= '9'; next = p.peek(0) {
		value = value*10 + int(p.take()-'0')
		if value > maxCount {
			p.fail()
			return 0
		}
	}
	return value
}

// parsePattern returns the tree for a pattern's code points, or false when it is not valid: too long, malformed
// or too big.
func parsePattern(cps []rune) (*node, bool) {
	if len(cps) > maxPatternLength {
		return nil, false
	}
	p := &patternParser{cps: cps}
	n := p.parse()
	if n == nil || n.size > maxPatternSize {
		return nil, false
	}
	return n, true
}
