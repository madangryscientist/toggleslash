package eval

// The `matches` op: a parser for the pattern dialect in docs/evaluation.md ("Patterns"), a compiler to a
// small program, and a Pike VM over code points. There is no backtracking and no platform regexp.

const (
	maxPatternLength = 1024
	maxPatternSize   = 10000
	maxPatternCount  = 100
	endOfPattern     = -1
)

// item is a set of inclusive code point ranges (lo, hi pairs), or their complement when negate is set.
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
	set   charSet
	parts []*node // concat parts, alt sides, or the one repeated body
	min   int
	max   int // -1 is unbounded
	size  int
}

type parseError struct{}

func fail() { panic(parseError{}) }

func saturate(size int) int { return min(size, maxPatternSize+1) }

func single(c rune) item { return item{ranges: []rune{c, c}} }

func setOf(it item) *node {
	return &node{kind: nodeSet, set: charSet{items: []item{it}}, size: 1}
}

var (
	digitItem = item{ranges: []rune{'0', '9'}}
	wordItem  = item{ranges: []rune{'0', '9', 'A', 'Z', '_', '_', 'a', 'z'}}
	// U+0009 to U+000D are \t \n \v \f \r.
	spaceItem = item{ranges: []rune{0x09, 0x0D, ' ', ' '}}
)

func negated(it item) item { return item{ranges: it.ranges, negate: true} }

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
	for c := p.peek(0); c != endOfPattern && c != '|' && c != ')'; c = p.peek(0) {
		atom := p.atom()
		anchor := atom.kind == nodeStart || atom.kind == nodeEnd
		if q := p.peek(0); !anchor && (q == '*' || q == '+' || q == '?' || q == '{') {
			atom = p.quantify(atom)
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
		if p.take() != ')' {
			fail()
		}
		return inner
	case '[':
		return &node{kind: nodeSet, set: p.charClass(), size: 1}
	case '\\':
		return setOf(p.escape())
	case '*', '+', '?', '{', '}', ']', endOfPattern:
		fail()
	case '.':
		return setOf(negated(single('\n')))
	case '^':
		return &node{kind: nodeStart, size: 1}
	case '$':
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

// isSingle reports whether an item is exactly one code point (so it may be a range endpoint).
func isSingle(it item) bool {
	return !it.negate && len(it.ranges) == 2 && it.ranges[0] == it.ranges[1]
}

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
	if p.peek(0) != '-' || p.peek(1) == endOfPattern || p.peek(1) == ']' {
		return from
	}
	p.pos++
	to := p.classAtom(false)
	// A shorthand class (\d, \D) is not a single code point. A single-character escape (\n, \]) is.
	if !isSingle(from) || !isSingle(to) || from.ranges[0] > to.ranges[0] {
		fail()
	}
	return item{ranges: []rune{from.ranges[0], to.ranges[0]}}
}

func (p *patternParser) classAtom(first bool) item {
	c := p.take()
	if c == endOfPattern || c == '[' || c == ']' {
		fail()
	}
	if c == '\\' {
		return p.escape()
	}
	// An unescaped `-` is a literal only as the first member or directly before `]`.
	if c == '-' && !first && p.peek(0) != ']' {
		fail()
	}
	return single(c)
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
	hi := lo
	if p.peek(0) == ',' {
		p.pos++
		if p.peek(0) == '}' {
			hi = -1
		} else {
			hi = p.count()
		}
	}
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

func repeatNode(body *node, lo, hi, times int) *node {
	return &node{kind: nodeRepeat, parts: []*node{body}, min: lo, max: hi, size: max(1, saturate(times*body.size))}
}

func isDigit(c rune) bool { return c >= '0' && c <= '9' }

// count reads ASCII digits with no leading zero, at most 100.
func (p *patternParser) count() int {
	c := p.take()
	if !isDigit(c) {
		fail()
	}
	if c == '0' {
		if isDigit(p.peek(0)) {
			fail()
		}
		return 0
	}
	value := int(c - '0')
	for isDigit(p.peek(0)) {
		value = value*10 + int(p.take()-'0')
		if value > maxPatternCount {
			fail()
		}
	}
	return value
}

// parsePattern returns the tree for a pattern's code points, or nil when it is not a valid pattern.
func parsePattern(cps []rune) (n *node) {
	if len(cps) > maxPatternLength {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(parseError); !ok {
				panic(r)
			}
			n = nil
		}
	}()
	p := &patternParser{cps: cps}
	n = p.alternation()
	if p.pos < len(p.cps) { // an unmatched `)`
		return nil
	}
	if n.size > maxPatternSize {
		return nil
	}
	return n
}

type opcode uint8

const (
	opChar opcode = iota
	opSplit
	opStart
	opEnd
	opMatch
)

type inst struct {
	op   opcode
	set  *charSet
	x, y int
}

// Pattern is a compiled pattern.
type Pattern struct {
	program []inst
}

type emitter struct{ program []inst }

func (e *emitter) split(x int) int {
	e.program = append(e.program, inst{op: opSplit, x: x, y: x})
	return len(e.program) - 1
}

func (e *emitter) emit(n *node) {
	switch n.kind {
	case nodeSet:
		set := n.set
		e.program = append(e.program, inst{op: opChar, set: &set})
	case nodeStart:
		e.program = append(e.program, inst{op: opStart})
	case nodeEnd:
		e.program = append(e.program, inst{op: opEnd})
	case nodeConcat:
		for _, part := range n.parts {
			e.emit(part)
		}
	case nodeAlt:
		e.emitAlt(n.parts)
	case nodeRepeat:
		e.emitRepeat(n.parts[0], n.min, n.max)
	}
}

func (e *emitter) emitAlt(sides []*node) {
	var jumps []int
	last := len(sides) - 1
	for i, side := range sides {
		choice := -1
		if i < last {
			choice = e.split(len(e.program) + 1)
		}
		e.emit(side)
		if choice < 0 {
			continue
		}
		jumps = append(jumps, e.split(0))
		e.program[choice].y = len(e.program)
	}
	for _, j := range jumps {
		e.program[j].x = len(e.program)
		e.program[j].y = len(e.program)
	}
}

func (e *emitter) emitRepeat(body *node, lo, hi int) {
	// A body of size 0 is empty groups only and matches just the empty string; repeating it emits nothing.
	if body.size == 0 {
		return
	}
	for range lo {
		e.emit(body)
	}
	if hi < 0 {
		top := len(e.program)
		loop := e.split(top + 1)
		e.emit(body)
		e.split(top)
		e.program[loop].y = len(e.program)
		return
	}
	var optional []int
	for i := lo; i < hi; i++ {
		optional = append(optional, e.split(len(e.program)+1))
		e.emit(body)
	}
	for _, c := range optional {
		e.program[c].y = len(e.program)
	}
}

// CompilePattern returns the compiled pattern, or nil when text is not a valid pattern.
func CompilePattern(cps []rune) *Pattern {
	n := parsePattern(cps)
	if n == nil {
		return nil
	}
	e := &emitter{}
	e.emit(n)
	e.program = append(e.program, inst{op: opMatch})
	return &Pattern{program: e.program}
}

func inItem(it item, c rune) bool {
	hit := false
	for i := 0; i+1 < len(it.ranges) && !hit; i += 2 {
		hit = it.ranges[i] <= c && c <= it.ranges[i+1]
	}
	return hit != it.negate
}

func inSet(s *charSet, c rune) bool {
	hit := false
	for _, it := range s.items {
		if inItem(it, c) {
			hit = true
			break
		}
	}
	return hit != s.negate
}

// Matches reports whether the pattern matches somewhere in the code points. A Pike VM: each instruction
// is visited at most once per input position.
func (p *Pattern) Matches(input []rune) bool {
	program := p.program
	length := len(input)
	seen := make([]int, len(program))
	for i := range seen {
		seen[i] = -1
	}
	var stack []int
	current := make([]int, 0, len(program))
	next := make([]int, 0, len(program))

	add := func(list *[]int, pc, pos int) bool {
		stack = append(stack[:0], pc)
		for len(stack) > 0 {
			at := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if at >= len(program) || seen[at] == pos {
				continue
			}
			seen[at] = pos
			in := &program[at]
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
			in := &program[pc]
			if in.op == opChar && inSet(in.set, c) && add(&next, pc+1, pos+1) {
				return true
			}
		}
		current, next = next, current
	}
}
