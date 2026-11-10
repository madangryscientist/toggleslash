package evaluator

// The parser for `matches` patterns, to the "Patterns" section of docs/evaluation.md: code points in,
// a tree with each node's size out, or "not valid" for anything else. pattern.go compiles the tree and
// runs it.

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
	kind     nodeKind
	set      charSet
	parts    []*node // concat parts, alt sides, or the one repeat body
	min, max int     // repeat counts; max is -1 for "no upper bound"
	size     int
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

type invalid struct{}

type patternParser struct {
	cps []rune
	pos int
}

func saturate(size int) int {
	if size > patternMaxSize+1 {
		return patternMaxSize + 1
	}
	return size
}

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

// fail aborts the parse; parsePattern recovers it.
func fail() { panic(invalid{}) }

func isQuantifier(c rune) bool { return c == '*' || c == '+' || c == '?' || c == '{' }

func notAnAtom(c rune) bool {
	switch c {
	case '*', '+', '?', '{', '}', ']':
		return true
	}
	return false
}

func (p *patternParser) parse() *node {
	n := p.alternation()
	if p.pos < len(p.cps) {
		fail() // an unmatched )
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

func (p *patternParser) concat() *node {
	var parts []*node
	size := 0
	for c := p.peek(0); c != patternEnd && c != '|' && c != ')'; c = p.peek(0) {
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
	switch {
	case c == '(':
		inner := p.alternation()
		if p.take() != ')' {
			fail()
		}
		return inner
	case c == '[':
		return &node{kind: nodeSet, set: p.charClass(), size: 1}
	case c == '\\':
		return setOf(p.escape())
	case notAnAtom(c):
		fail()
	case c == '.':
		return setOf(item{ranges: []rune{'\n', '\n'}, negate: true})
	case c == '^':
		return &node{kind: nodeStart, size: 1}
	case c == '$':
		return &node{kind: nodeEnd, size: 1}
	}
	return setOf(single(c))
}

// escape reads after a backslash: one code point, or one of the six shorthand classes.
func (p *patternParser) escape() item {
	c := p.take()
	switch c {
	case '\\', '.', '[', ']', '(', ')', '|', '*', '+', '?', '{', '}', '^', '$', '-':
		return single(c)
	case 'n':
		return single('\n')
	case 't':
		return single('\t')
	case 'r':
		return single('\r')
	case 'f':
		return single('\f')
	case 'v':
		return single('\v')
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
	fail()
	return item{}
}

// isSingle reports whether it is exactly one code point (so it can end a range).
func isSingle(it item) bool {
	return !it.negate && len(it.ranges) == 2 && it.ranges[0] == it.ranges[1]
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

// member reads one member: an atom, or a range of two single code points.
func (p *patternParser) member(first bool) item {
	from, fromSingle := p.classAtom(first)
	isRange := p.peek(0) == '-' && p.peek(1) != patternEnd && p.peek(1) != ']'
	if !isRange {
		return from
	}
	p.pos++
	to, toSingle := p.classAtom(false)
	if !fromSingle || !toSingle || from.ranges[0] > to.ranges[0] {
		fail()
	}
	return item{ranges: []rune{from.ranges[0], to.ranges[0]}}
}

// classAtom reads one class atom; the bool is true for a single code point (written plain or escaped
// to one), false for a shorthand class such as \d.
func (p *patternParser) classAtom(first bool) (item, bool) {
	c := p.take()
	// "]" here is a class with no members; END and "[" are never members.
	if c == patternEnd || c == '[' || c == ']' {
		fail()
	}
	if c == '\\' {
		it := p.escape()
		return it, isSingle(it)
	}
	// An unescaped "-" is a literal only as the first member or directly before "]".
	if c == '-' && !first && p.peek(0) != ']' {
		fail()
	}
	return single(c), true
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
		return &node{kind: nodeRepeat, parts: []*node{body}, min: min, max: max, size: saturate(body.size + 1)}
	}
	min := p.count()
	max := p.upper(min)
	if p.take() != '}' {
		fail()
	}
	if max < 0 {
		return repeatNode(body, min, -1, min+1)
	}
	if min > max {
		fail()
	}
	return repeatNode(body, min, max, max)
}

// upper reads after "{n": n again for {n}, -1 for {n,}, m for {n,m}. It leaves the "}".
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
	return &node{kind: nodeRepeat, parts: []*node{body}, min: min, max: max, size: size}
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
		if value > patternMaxCount {
			fail()
		}
	}
	return value
}

// parsePattern returns the tree for a pattern's code points, or false when it is not a valid pattern:
// too long, malformed or too big.
func parsePattern(cps []rune) (n *node, ok bool) {
	if len(cps) > patternMaxLength {
		return nil, false
	}
	defer func() {
		if r := recover(); r != nil {
			if _, isInvalid := r.(invalid); !isInvalid {
				panic(r)
			}
			n, ok = nil, false
		}
	}()
	root := (&patternParser{cps: cps}).parse()
	if root.size > patternMaxSize {
		return nil, false
	}
	return root, true
}
