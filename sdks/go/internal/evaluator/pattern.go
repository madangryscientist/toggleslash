package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"

// The `matches` op: a hand-written parser to a tree, then a Pike VM over code points. No
// backtracking and no regexp package, so a match costs O(program x input) whatever the pattern.

const (
	maxPatternLength = 1024
	maxPatternSize   = 10000
	maxPatternCount  = 100
	patternEnd       = -1
)

// item is code point ranges as pairs lo, hi (inclusive), or their complement when negate is set.
type item struct {
	ranges []int32
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
	size  int
	set   *charSet
	parts []*node // concat parts, or alt sides
	body  *node
	min   int
	max   int // -1 is unbounded
}

type invalidPattern struct{}

func failPattern() { panic(invalidPattern{}) }

func saturate(size int) int {
	if size > maxPatternSize+1 {
		return maxPatternSize + 1
	}
	return size
}

func isDigitCP(c int32) bool { return c >= '0' && c <= '9' }

var (
	digitItem = item{ranges: []int32{0x30, 0x39}}
	wordItem  = item{ranges: []int32{0x30, 0x39, 0x41, 0x5a, 0x5f, 0x5f, 0x61, 0x7a}}
	// U+0009 to U+000D are \t \n \v \f \r.
	spaceItem = item{ranges: []int32{0x09, 0x0d, 0x20, 0x20}}
)

func negated(i item) item { return item{ranges: i.ranges, negate: true} }

func single(c int32) item { return item{ranges: []int32{c, c}} }

func setOf(i item) *node {
	return &node{kind: nodeSet, size: 1, set: &charSet{items: []item{i}}}
}

// atomValue is one code point or one shorthand class.
type atomValue struct {
	cp    int32
	class item
	isCP  bool
}

func (a atomValue) asItem() item {
	if a.isCP {
		return single(a.cp)
	}
	return a.class
}

type patternParser struct {
	cps []int32
	pos int
}

func (p *patternParser) peek(offset int) int32 {
	if p.pos+offset < len(p.cps) {
		return p.cps[p.pos+offset]
	}
	return patternEnd
}

func (p *patternParser) take() int32 {
	c := p.peek(0)
	p.pos++
	return c
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

func isQuantifier(c int32) bool { return c == '*' || c == '+' || c == '?' || c == '{' }

func (p *patternParser) concat() *node {
	var parts []*node
	for c := p.peek(0); c != patternEnd && c != '|' && c != ')'; c = p.peek(0) {
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
	switch c {
	case '(':
		inner := p.alternation()
		if p.take() != ')' {
			failPattern()
		}
		return inner
	case '[':
		return &node{kind: nodeSet, set: p.charClass(), size: 1}
	case '\\':
		return setOf(p.escape().asItem())
	case '*', '+', '?', '{', '}', ']':
		failPattern()
	case '.':
		return setOf(item{ranges: []int32{'\n', '\n'}, negate: true})
	case '^':
		return &node{kind: nodeStart, size: 1}
	case '$':
		return &node{kind: nodeEnd, size: 1}
	}
	if c == patternEnd {
		failPattern()
	}
	return setOf(single(c))
}

// escape reads after a backslash: one code point, or one of the six shorthand classes.
func (p *patternParser) escape() atomValue {
	c := p.take()
	switch c {
	case '\\', '.', '[', ']', '(', ')', '|', '*', '+', '?', '{', '}', '^', '$', '-':
		return atomValue{cp: c, isCP: true}
	case 'n':
		return atomValue{cp: 0x0a, isCP: true}
	case 't':
		return atomValue{cp: 0x09, isCP: true}
	case 'r':
		return atomValue{cp: 0x0d, isCP: true}
	case 'f':
		return atomValue{cp: 0x0c, isCP: true}
	case 'v':
		return atomValue{cp: 0x0b, isCP: true}
	case 'd':
		return atomValue{class: digitItem}
	case 'w':
		return atomValue{class: wordItem}
	case 's':
		return atomValue{class: spaceItem}
	case 'D':
		return atomValue{class: negated(digitItem)}
	case 'W':
		return atomValue{class: negated(wordItem)}
	case 'S':
		return atomValue{class: negated(spaceItem)}
	}
	failPattern()
	return atomValue{}
}

// charClass reads after '[': the members up to ']'.
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

// member is one member: an atom, or a range of two single code points.
func (p *patternParser) member(first bool) item {
	from := p.classAtom(first)
	isRange := p.peek(0) == '-' && p.peek(1) != patternEnd && p.peek(1) != ']'
	if !isRange {
		return from.asItem()
	}
	p.pos++
	to := p.classAtom(false)
	if !from.isCP || !to.isCP || from.cp > to.cp {
		failPattern()
	}
	return item{ranges: []int32{from.cp, to.cp}}
}

func (p *patternParser) classAtom(first bool) atomValue {
	c := p.take()
	// ']' here is a class with no members; END and '[' are never members.
	if c == patternEnd || c == '[' || c == ']' {
		failPattern()
	}
	if c == '\\' {
		return p.escape()
	}
	// An unescaped '-' is a literal only as the first member or directly before ']'.
	if c == '-' && !first && p.peek(0) != ']' {
		failPattern()
	}
	return atomValue{cp: c, isCP: true}
}

func (p *patternParser) quantify(body *node) *node {
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
		failPattern()
	}
	if max == -1 {
		return p.repeat(body, min, -1, min+1)
	}
	if min > max {
		failPattern()
	}
	return p.repeat(body, min, max, max)
}

// upper reads after '{n': n again for {n}, -1 for {n,}, m for {n,m}. It leaves the '}'.
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

func (p *patternParser) repeat(body *node, min, max, times int) *node {
	size := saturate(times * body.size)
	if size < 1 {
		size = 1
	}
	return &node{kind: nodeRepeat, body: body, min: min, max: max, size: size}
}

// count reads ASCII digits with no leading zero, at most 100.
func (p *patternParser) count() int {
	c := p.take()
	if !isDigitCP(c) {
		failPattern()
	}
	if c == '0' {
		if isDigitCP(p.peek(0)) {
			failPattern()
		}
		return 0
	}
	value := int(c - '0')
	for isDigitCP(p.peek(0)) {
		value = value*10 + int(p.take()-'0')
		if value > maxPatternCount {
			failPattern()
		}
	}
	return value
}

func parsePattern(cps []int32) (root *node) {
	if len(cps) > maxPatternLength {
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
	p := &patternParser{cps: cps}
	root = p.alternation()
	if p.pos < len(p.cps) {
		return nil // an unmatched ')'
	}
	if root.size > maxPatternSize {
		return nil
	}
	return root
}

// The program.

type instOp uint8

const (
	opChar instOp = iota
	opSplit
	opStart
	opEnd
	opMatch
)

type inst struct {
	op   instOp
	set  *charSet
	x, y int
}

// pattern is a compiled pattern.
type pattern struct{ program []inst }

func (g *pattern) split(x int) int {
	g.program = append(g.program, inst{op: opSplit, x: x, y: x})
	return len(g.program) - 1
}

func (g *pattern) emitAlt(sides []*node) {
	var jumps []int
	last := len(sides) - 1
	for i, side := range sides {
		choice := -1
		if i < last {
			choice = g.split(len(g.program) + 1)
		}
		g.emit(side)
		if choice < 0 {
			continue
		}
		jumps = append(jumps, g.split(0))
		g.program[choice].y = len(g.program)
	}
	for _, jump := range jumps {
		g.program[jump].x = len(g.program)
		g.program[jump].y = len(g.program)
	}
}

func (g *pattern) emitRepeat(body *node, min, max int) {
	// A body of size 0 is empty groups only and matches just the empty string, so repeating it
	// emits nothing.
	if body.size == 0 {
		return
	}
	for i := 0; i < min; i++ {
		g.emit(body)
	}
	if max == -1 {
		top := len(g.program)
		loop := g.split(top + 1)
		g.emit(body)
		g.split(top)
		g.program[loop].y = len(g.program)
		return
	}
	var optional []int
	for i := min; i < max; i++ {
		optional = append(optional, g.split(len(g.program)+1))
		g.emit(body)
	}
	for _, choice := range optional {
		g.program[choice].y = len(g.program)
	}
}

func (g *pattern) emit(n *node) {
	switch n.kind {
	case nodeSet:
		g.program = append(g.program, inst{op: opChar, set: n.set})
	case nodeStart:
		g.program = append(g.program, inst{op: opStart})
	case nodeEnd:
		g.program = append(g.program, inst{op: opEnd})
	case nodeConcat:
		for _, part := range n.parts {
			g.emit(part)
		}
	case nodeAlt:
		g.emitAlt(n.parts)
	case nodeRepeat:
		g.emitRepeat(n.body, n.min, n.max)
	}
}

// compilePattern returns the compiled pattern, or nil when text is not a valid pattern.
func compilePattern(text string) *pattern {
	root := parsePattern(wtf8.CodePoints(text))
	if root == nil {
		return nil
	}
	g := &pattern{}
	g.emit(root)
	g.program = append(g.program, inst{op: opMatch})
	return g
}

func inItem(it item, c int32) bool {
	hit := false
	for i := 0; i+1 < len(it.ranges) && !hit; i += 2 {
		hit = it.ranges[i] <= c && c <= it.ranges[i+1]
	}
	return hit != it.negate
}

func inSet(set *charSet, c int32) bool {
	found := false
	for _, it := range set.items {
		if inItem(it, c) {
			found = true
			break
		}
	}
	return found != set.negate
}

// matches reports whether the pattern matches somewhere in text. Each instruction is visited once
// per position.
func (g *pattern) matches(text string) bool {
	input := wtf8.CodePoints(text)
	length := len(input)
	program := g.program
	seen := make([]int, len(program))
	for i := range seen {
		seen[i] = -1
	}
	var stack []int
	current := []int{}
	next := []int{}

	add := func(list *[]int, pc, pos int) bool {
		stack = append(stack[:0], pc)
		for len(stack) > 0 {
			at := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if at >= len(program) || seen[at] == pos {
				continue
			}
			seen[at] = pos
			in := program[at]
			switch in.op {
			case opMatch:
				return true
			case opChar:
				*list = append(*list, at)
			case opSplit:
				stack = append(stack, in.y, in.x)
			case opStart:
				if pos == 0 {
					stack = append(stack, at+1)
				}
			case opEnd:
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
			in := program[pc]
			if in.op == opChar && inSet(in.set, c) && add(&next, pc+1, pos+1) {
				return true
			}
		}
		current, next = next, current
	}
}
