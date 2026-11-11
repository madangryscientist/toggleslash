package evaluator

// The parser for `matches` patterns, to the "Patterns" section of docs/evaluation.md: code points
// in, a tree with each node's size out, or no tree for a pattern that is not valid.

const (
	maxPatternLength = 1024
	maxPatternSize   = 10000
	maxCount         = 100
	endOfPattern     = -1
)

// item is a set of code point ranges (pairs lo, hi, inclusive), or their complement.
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
	kind     nodeKind
	set      charSet
	parts    []*node // concat parts, alt sides, or the one repeat body
	min, max int     // repeat; max -1 is unbounded
	size     int
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
	return &node{kind: nodeSet, set: charSet{items: []item{i}}, size: 1}
}

type invalidPattern struct{}

func fail() { panic(invalidPattern{}) }

func saturate(size int) int { return min(size, maxPatternSize+1) }

func isPatternDigit(c int) bool { return c >= '0' && c <= '9' }

// atomValue is the result of an escape or a class atom: one code point, or a shorthand class.
type atomValue struct {
	r  rune
	it *item
}

func (a atomValue) item() item {
	if a.it != nil {
		return *a.it
	}
	return single(a.r)
}

type patternParser struct {
	cps []rune
	pos int
}

func (p *patternParser) peek(offset int) int {
	if p.pos+offset >= len(p.cps) {
		return endOfPattern
	}
	return int(p.cps[p.pos+offset])
}

func (p *patternParser) take() int {
	c := p.peek(0)
	p.pos++
	return c
}

func (p *patternParser) parse() *node {
	n := p.alternation()
	if p.pos < len(p.cps) {
		fail() // an unmatched ")"
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
	for _, s := range sides {
		size += s.size
	}
	return &node{kind: nodeAlt, parts: sides, size: saturate(size)}
}

func isQuantifier(c int) bool { return c == '*' || c == '+' || c == '?' || c == '{' }

func (p *patternParser) concat() *node {
	var parts []*node
	size := 0
	for c := p.peek(0); c != endOfPattern && c != '|' && c != ')'; c = p.peek(0) {
		atom := p.atom()
		anchor := atom.kind == nodeStart || atom.kind == nodeEnd
		if !anchor && isQuantifier(p.peek(0)) {
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
		fail()
	case '.':
		return setOf(item{ranges: []rune{'\n', '\n'}, negate: true})
	case '^':
		return &node{kind: nodeStart, size: 1}
	case '$':
		return &node{kind: nodeEnd, size: 1}
	}
	return setOf(single(rune(c)))
}

func (p *patternParser) group() *node {
	inner := p.alternation()
	if p.take() != ')' {
		fail()
	}
	return inner
}

// escape reads what follows a "\": one code point, or one of the six shorthand classes.
func (p *patternParser) escape() atomValue {
	c := p.take()
	switch c {
	case '\\', '.', '[', ']', '(', ')', '|', '*', '+', '?', '{', '}', '^', '$', '-':
		return atomValue{r: rune(c)}
	case 'n':
		return atomValue{r: '\n'}
	case 't':
		return atomValue{r: '\t'}
	case 'r':
		return atomValue{r: '\r'}
	case 'f':
		return atomValue{r: '\f'}
	case 'v':
		return atomValue{r: '\v'}
	case 'd':
		return atomValue{it: &digitItem}
	case 'w':
		return atomValue{it: &wordItem}
	case 's':
		return atomValue{it: &spaceItem}
	case 'D':
		i := negated(digitItem)
		return atomValue{it: &i}
	case 'W':
		i := negated(wordItem)
		return atomValue{it: &i}
	case 'S':
		i := negated(spaceItem)
		return atomValue{it: &i}
	}
	fail()
	return atomValue{}
}

// charClass reads after "[": the members up to "]".
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

func (p *patternParser) member(first bool) item {
	from := p.classAtom(first)
	isRange := p.peek(0) == '-' && p.peek(1) != endOfPattern && p.peek(1) != ']'
	if !isRange {
		return from.item()
	}
	p.pos++
	to := p.classAtom(false)
	if from.it != nil || to.it != nil || from.r > to.r {
		fail()
	}
	return item{ranges: []rune{from.r, to.r}}
}

func (p *patternParser) classAtom(first bool) atomValue {
	c := p.take()
	if c == endOfPattern || c == '[' || c == ']' {
		fail()
	}
	if c == '\\' {
		return p.escape()
	}
	if c == '-' && !first && p.peek(0) != ']' {
		fail()
	}
	return atomValue{r: rune(c)}
}

func (p *patternParser) quantify(body *node) *node {
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
	hi := p.upper(lo)
	if p.take() != '}' {
		fail()
	}
	if hi == -1 {
		return repeatNode(body, lo, -1, lo+1)
	}
	if lo > hi {
		fail()
	}
	return repeatNode(body, lo, hi, hi)
}

// upper reads after "{n": n again for "{n}", -1 for "{n,}", m for "{n,m}". It leaves the "}".
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

func repeatNode(body *node, lo, hi, times int) *node {
	return &node{kind: nodeRepeat, parts: []*node{body}, min: lo, max: hi, size: max(1, saturate(times*body.size))}
}

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
	v := c - '0'
	for isPatternDigit(p.peek(0)) {
		v = v*10 + (p.take() - '0')
		if v > maxCount {
			fail()
		}
	}
	return v
}

// parsePattern returns the tree for a pattern's code points, or nil when the pattern is too long,
// malformed or too big.
func parsePattern(cps []rune) (n *node) {
	if len(cps) > maxPatternLength {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(invalidPattern); !ok {
				panic(r)
			}
			n = nil
		}
	}()
	n = (&patternParser{cps: cps}).parse()
	if n.size > maxPatternSize {
		return nil
	}
	return n
}
