package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"

// Patterns for the matches op, to the "Patterns" section of docs/evaluation.md.
// A pattern is read into a tree (with each node's size), compiled to a small
// program and run on a Pike VM over code points: no backtracking and no regexp
// package, so a match costs O(program x input) whatever the pattern.

const (
	patternMaxLength = 1024
	patternMaxSize   = 10000
	patternMaxCount  = 100
	patternEnd       = rune(-1)
)

// item is code point ranges as pairs lo, hi (inclusive), or their complement
// when negate is set.
type item struct {
	ranges []rune
	negate bool
}

// charSet is any of its items, or none of them when negate is set.
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
	kind   nodeKind
	size   int
	set    *charSet // nodeSet
	parts  []*node  // nodeConcat, nodeAlt
	body   *node    // nodeRepeat
	lo, hi int      // nodeRepeat; hi is -1 for no upper bound
}

type instKind uint8

const (
	instChar instKind = iota
	instSplit
	instStart
	instEnd
	instMatch
)

// inst is one instruction. A jump is a split whose two targets are the same.
type inst struct {
	kind instKind
	set  *charSet
	x, y int
}

// pattern is a compiled pattern.
type pattern struct{ program []inst }

func saturate(size int) int { return min(size, patternMaxSize+1) }

func single(c rune) item { return item{ranges: []rune{c, c}} }

func setOf(i item) *node {
	return &node{kind: nodeSet, size: 1, set: &charSet{items: []item{i}}}
}

var (
	digitItem = item{ranges: []rune{'0', '9'}}
	wordItem  = item{ranges: []rune{'0', '9', 'A', 'Z', '_', '_', 'a', 'z'}}
	// U+0009 to U+000D are \t \n \v \f \r.
	spaceItem = item{ranges: []rune{0x09, 0x0d, 0x20, 0x20}}
)

func negated(i item) item { return item{ranges: i.ranges, negate: true} }

// escapable is the set of code points a backslash takes literally.
func escapable(c rune) bool {
	switch c {
	case '\\', '.', '[', ']', '(', ')', '|', '*', '+', '?', '{', '}', '^', '$', '-':
		return true
	}
	return false
}

// atomValue is a single code point, or an item when isItem is set.
type atomValue struct {
	cp     rune
	it     item
	isItem bool
}

func (a atomValue) asItem() item {
	if a.isItem {
		return a.it
	}
	return single(a.cp)
}

func escapeValue(c rune) (atomValue, bool) {
	switch c {
	case 'n':
		return atomValue{cp: 0x0a}, true
	case 't':
		return atomValue{cp: 0x09}, true
	case 'r':
		return atomValue{cp: 0x0d}, true
	case 'f':
		return atomValue{cp: 0x0c}, true
	case 'v':
		return atomValue{cp: 0x0b}, true
	case 'd':
		return atomValue{it: digitItem, isItem: true}, true
	case 'w':
		return atomValue{it: wordItem, isItem: true}, true
	case 's':
		return atomValue{it: spaceItem, isItem: true}, true
	case 'D':
		return atomValue{it: negated(digitItem), isItem: true}, true
	case 'W':
		return atomValue{it: negated(wordItem), isItem: true}, true
	case 'S':
		return atomValue{it: negated(spaceItem), isItem: true}, true
	}
	return atomValue{}, false
}

// patternParser reads code points. Every failure is a panic of invalidPattern,
// recovered by parsePattern.
type patternParser struct {
	cps []rune
	pos int
}

type invalidPattern struct{}

func fail() { panic(invalidPattern{}) }

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

func isQuantifier(c rune) bool { return c == '*' || c == '+' || c == '?' || c == '{' }

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
	switch c {
	case '(':
		return p.group()
	case '[':
		return &node{kind: nodeSet, size: 1, set: p.charClass()}
	case '\\':
		return setOf(p.escape().asItem())
	case '*', '+', '?', '{', '}', ']':
		fail()
	case '.':
		return setOf(item{ranges: []rune{0x0a, 0x0a}, negate: true})
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
		fail()
	}
	return inner
}

// escape is after a backslash: one code point, or one of the six shorthand
// classes.
func (p *patternParser) escape() atomValue {
	c := p.take()
	if escapable(c) {
		return atomValue{cp: c}
	}
	v, ok := escapeValue(c)
	if !ok {
		fail()
	}
	return v
}

// charClass is after "[": the members up to "]".
func (p *patternParser) charClass() *charSet {
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

// member is an atom, or a range of two single code points.
func (p *patternParser) member(first bool) item {
	from := p.classAtom(first)
	isRange := p.peek(0) == '-' && p.peek(1) != patternEnd && p.peek(1) != ']'
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
	if c == patternEnd || c == '[' || c == ']' {
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
		lo, hi := 0, -1
		if c == '+' {
			lo = 1
		}
		if c == '?' {
			hi = 1
		}
		return &node{kind: nodeRepeat, body: body, lo: lo, hi: hi, size: saturate(body.size + 1)}
	}
	lo := p.count()
	hi := p.upper(lo)
	if p.take() != '}' {
		fail()
	}
	if hi < 0 {
		return repeatNode(body, lo, -1, lo+1)
	}
	if lo > hi {
		fail()
	}
	return repeatNode(body, lo, hi, hi)
}

// upper is after "{n": n again for {n}, -1 for {n,}, m for {n,m}. It leaves the "}".
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
	return &node{kind: nodeRepeat, body: body, lo: lo, hi: hi, size: max(1, saturate(times*body.size))}
}

// count is ASCII digits with no leading zero, at most 100.
func (p *patternParser) count() int {
	c := p.take()
	if c < '0' || c > '9' {
		fail()
	}
	if c == '0' {
		if d := p.peek(0); d >= '0' && d <= '9' {
			fail()
		}
		return 0
	}
	value := int(c - '0')
	for d := p.peek(0); d >= '0' && d <= '9'; d = p.peek(0) {
		value = value*10 + int(p.take()-'0')
		if value > patternMaxCount {
			fail()
		}
	}
	return value
}

// parsePattern is the tree for a pattern's code points, or nil when it is not a
// valid pattern: too long, malformed or too big.
func parsePattern(cps []rune) (root *node) {
	if len(cps) > patternMaxLength {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(invalidPattern); !ok {
				panic(r)
			}
			root = nil
		}
	}()
	n := (&patternParser{cps: cps}).parse()
	if n.size > patternMaxSize {
		return nil
	}
	return n
}

type compiler struct{ program []inst }

// split appends a split and returns its index.
func (c *compiler) split(x int) int {
	c.program = append(c.program, inst{kind: instSplit, x: x, y: x})
	return len(c.program) - 1
}

func (c *compiler) emitAlt(sides []*node) {
	var jumps []int
	last := len(sides) - 1
	for i, side := range sides {
		choice := -1
		if i < last {
			choice = c.split(len(c.program) + 1)
		}
		c.emit(side)
		if choice < 0 {
			continue
		}
		jumps = append(jumps, c.split(0))
		c.program[choice].y = len(c.program)
	}
	for _, j := range jumps {
		c.program[j].x, c.program[j].y = len(c.program), len(c.program)
	}
}

func (c *compiler) emitRepeat(n *node) {
	// A body of size 0 is empty groups only and matches just the empty string,
	// so repeating it emits nothing.
	if n.body.size == 0 {
		return
	}
	for i := 0; i < n.lo; i++ {
		c.emit(n.body)
	}
	if n.hi < 0 {
		top := len(c.program)
		loop := c.split(top + 1)
		c.emit(n.body)
		c.split(top)
		c.program[loop].y = len(c.program)
		return
	}
	var optional []int
	for i := n.lo; i < n.hi; i++ {
		optional = append(optional, c.split(len(c.program)+1))
		c.emit(n.body)
	}
	for _, o := range optional {
		c.program[o].y = len(c.program)
	}
}

func (c *compiler) emit(n *node) {
	switch n.kind {
	case nodeSet:
		c.program = append(c.program, inst{kind: instChar, set: n.set})
	case nodeStart:
		c.program = append(c.program, inst{kind: instStart})
	case nodeEnd:
		c.program = append(c.program, inst{kind: instEnd})
	case nodeConcat:
		for _, part := range n.parts {
			c.emit(part)
		}
	case nodeAlt:
		c.emitAlt(n.parts)
	case nodeRepeat:
		c.emitRepeat(n)
	}
}

// compilePattern is the compiled pattern, or nil when text is not a valid one.
func compilePattern(text string) *pattern {
	root := parsePattern(wtf8.CodePoints(text))
	if root == nil {
		return nil
	}
	c := &compiler{}
	c.emit(root)
	c.program = append(c.program, inst{kind: instMatch})
	return &pattern{program: c.program}
}

func (i item) has(c rune) bool {
	hit := false
	for k := 0; k+1 < len(i.ranges) && !hit; k += 2 {
		hit = i.ranges[k] <= c && c <= i.ranges[k+1]
	}
	return hit != i.negate
}

func (s *charSet) has(c rune) bool {
	hit := false
	for _, it := range s.items {
		if it.has(c) {
			hit = true
			break
		}
	}
	return hit != s.negate
}

// matches reports whether the pattern matches somewhere in text. Each
// instruction is visited once per position.
func (p *pattern) matches(text string) bool {
	input := wtf8.CodePoints(text)
	length := len(input)
	seen := make([]int, len(p.program))
	for i := range seen {
		seen[i] = -1
	}
	var stack, current, next []int

	// add puts pc and everything it reaches without reading on list, and
	// reports whether that reaches a match.
	add := func(list *[]int, pc, pos int) bool {
		stack = append(stack[:0], pc)
		for len(stack) > 0 {
			at := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if at >= len(p.program) || seen[at] == pos {
				continue
			}
			seen[at] = pos
			in := p.program[at]
			switch in.kind {
			case instMatch:
				return true
			case instChar:
				*list = append(*list, at)
			case instSplit:
				stack = append(stack, in.y, in.x)
			case instStart:
				if pos == 0 {
					stack = append(stack, at+1)
				}
			case instEnd:
				if pos == length {
					stack = append(stack, at+1)
				}
			}
		}
		return false
	}

	for pos := 0; ; pos++ {
		if add(&current, 0, pos) {
			return true
		}
		if pos >= length {
			return false
		}
		c := input[pos]
		next = next[:0]
		for _, pc := range current {
			in := p.program[pc]
			if in.kind == instChar && in.set.has(c) && add(&next, pc+1, pos+1) {
				return true
			}
		}
		current, next = next, current
	}
}
