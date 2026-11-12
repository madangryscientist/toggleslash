package toggleslash

// Patterns for the `matches` op, to the "Patterns" section of docs/evaluation.md.
//
// A hand-written parser reads a pattern, as code points, into a tree that knows each node's size, and the tree
// becomes a small program a Pike VM runs over code points. There is no backtracking and no regular expression
// package, so a match costs O(program x input) whatever the pattern, and a pattern a backtracking engine would
// hang on finishes at once.

// endOfPattern is what peek returns past the last code point.
const endOfPattern = rune(-1)

const (
	maxPatternLength = 1024
	maxPatternSize   = 10000
	maxPatternCount  = 100
)

type nodeKind uint8

const (
	nodeSet nodeKind = iota
	nodeStart
	nodeEnd
	nodeConcat
	nodeAlt
	nodeRepeat
)

// classItem is one member of a character class: code point ranges as a flat list lo, hi, lo, hi, ..., or
// their complement when negate is set.
type classItem struct {
	ranges []rune
	negate bool
}

func (item classItem) contains(c rune) bool {
	hit := false
	for i := 0; i+1 < len(item.ranges); i += 2 {
		if item.ranges[i] <= c && c <= item.ranges[i+1] {
			hit = true
			break
		}
	}
	return hit != item.negate
}

// charSet is a class: any of its items, or none of them when negate is set.
type charSet struct {
	items  []classItem
	negate bool
}

func (set *charSet) contains(c rune) bool {
	hit := false
	for _, item := range set.items {
		if item.contains(c) {
			hit = true
			break
		}
	}
	return hit != set.negate
}

type patternNode struct {
	kind   nodeKind
	size   int
	set    *charSet
	parts  []*patternNode
	body   *patternNode
	min    int
	max    int
	hasMax bool
}

// escaped is one escape: either a single code point (for a range endpoint) or a shorthand class.
type escaped struct {
	item   classItem
	single bool
	code   rune
}

func singleItem(c rune) classItem { return classItem{ranges: []rune{c, c}} }

func literalEscape(c rune) escaped {
	return escaped{item: singleItem(c), single: true, code: c}
}

func setOf(item classItem) *patternNode {
	return &patternNode{kind: nodeSet, size: 1, set: &charSet{items: []classItem{item}}}
}

var (
	digitItem = classItem{ranges: []rune{'0', '9'}}
	wordItem  = classItem{ranges: []rune{'0', '9', 'A', 'Z', '_', '_', 'a', 'z'}}
	spaceItem = classItem{ranges: []rune{'\t', '\r', ' ', ' '}}
)

func isPatternDigit(c rune) bool { return c >= '0' && c <= '9' }

func isQuantifier(c rune) bool {
	return c == '*' || c == '+' || c == '?' || c == '{'
}

func isEscapable(c rune) bool {
	switch c {
	case '\\', '.', '[', ']', '(', ')', '|', '*', '+', '?', '{', '}', '^', '$', '-':
		return true
	}
	return false
}

func saturateSize(size int) int {
	if size > maxPatternSize+1 {
		return maxPatternSize + 1
	}
	return size
}

type patternParser struct {
	cps []rune
	at  int
}

func (p *patternParser) peek(offset int) rune {
	at := p.at + offset
	if at < 0 || at >= len(p.cps) {
		return endOfPattern
	}
	return p.cps[at]
}

func (p *patternParser) take() rune {
	c := p.peek(0)
	p.at++
	return c
}

type patternFrame struct {
	sides []*patternNode
	parts []*patternNode
}

func (p *patternParser) parse() (*patternNode, bool) {
	var stack []patternFrame
	var sides, parts []*patternNode
	for {
		c := p.peek(0)
		if c == '|' {
			p.at++
			sides = append(sides, concatNodes(parts))
			parts = nil
			continue
		}
		if c == endOfPattern || c == ')' {
			sides = append(sides, concatNodes(parts))
			node := alternationNodes(sides)
			if c == endOfPattern {
				if len(stack) != 0 {
					return nil, false // a "(" with no ")"
				}
				return node, true
			}
			if len(stack) == 0 {
				return nil, false // an unmatched ")"
			}
			p.at++
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			sides, parts = top.sides, top.parts
			var ok bool
			parts, ok = p.appendAtom(parts, node)
			if !ok {
				return nil, false
			}
			continue
		}
		p.at++
		if c == '(' {
			stack = append(stack, patternFrame{sides: sides, parts: parts})
			sides, parts = nil, nil
			continue
		}
		atom, ok := p.atom(c)
		if !ok {
			return nil, false
		}
		parts, ok = p.appendAtom(parts, atom)
		if !ok {
			return nil, false
		}
	}
}

func (p *patternParser) appendAtom(parts []*patternNode, atom *patternNode) ([]*patternNode, bool) {
	anchor := atom.kind == nodeStart || atom.kind == nodeEnd
	if anchor || !isQuantifier(p.peek(0)) {
		return append(parts, atom), true
	}
	quantified, ok := p.quantify(atom)
	if !ok {
		return nil, false
	}
	return append(parts, quantified), true
}

func (p *patternParser) atom(c rune) (*patternNode, bool) {
	switch c {
	case '[':
		set, ok := p.charClass()
		if !ok {
			return nil, false
		}
		return &patternNode{kind: nodeSet, size: 1, set: set}, true
	case '\\':
		found, ok := p.escape()
		if !ok {
			return nil, false
		}
		return setOf(found.item), true
	case '*', '+', '?', '{', '}', ']':
		return nil, false
	case '.':
		return setOf(classItem{ranges: []rune{'\n', '\n'}, negate: true}), true
	case '^':
		return &patternNode{kind: nodeStart, size: 1}, true
	case '$':
		return &patternNode{kind: nodeEnd, size: 1}, true
	}
	return setOf(singleItem(c)), true
}

// escape reads what follows a backslash: one code point, or one of the six shorthand classes.
func (p *patternParser) escape() (escaped, bool) {
	c := p.take()
	if isEscapable(c) {
		return literalEscape(c), true
	}
	switch c {
	case 'n':
		return literalEscape('\n'), true
	case 't':
		return literalEscape('\t'), true
	case 'r':
		return literalEscape('\r'), true
	case 'f':
		return literalEscape('\f'), true
	case 'v':
		return literalEscape('\v'), true
	case 'd':
		return escaped{item: digitItem}, true
	case 'w':
		return escaped{item: wordItem}, true
	case 's':
		return escaped{item: spaceItem}, true
	case 'D':
		return escaped{item: classItem{ranges: digitItem.ranges, negate: true}}, true
	case 'W':
		return escaped{item: classItem{ranges: wordItem.ranges, negate: true}}, true
	case 'S':
		return escaped{item: classItem{ranges: spaceItem.ranges, negate: true}}, true
	}
	return escaped{}, false
}

func (p *patternParser) charClass() (*charSet, bool) {
	negate := p.peek(0) == '^'
	if negate {
		p.at++
	}
	first, ok := p.member(true)
	if !ok {
		return nil, false
	}
	items := []classItem{first}
	for p.peek(0) != ']' {
		item, ok := p.member(false)
		if !ok {
			return nil, false
		}
		items = append(items, item)
	}
	if p.peek(0) != ']' {
		return nil, false
	}
	p.at++
	return &charSet{items: items, negate: negate}, true
}

func (p *patternParser) member(first bool) (classItem, bool) {
	start, ok := p.classAtom(first)
	if !ok {
		return classItem{}, false
	}
	if p.peek(0) != '-' || p.peek(1) == endOfPattern || p.peek(1) == ']' {
		return start.item, true
	}
	p.at++ // the "-"
	end, ok := p.classAtom(false)
	if !ok {
		return classItem{}, false
	}
	if !start.single || !end.single || start.code > end.code {
		return classItem{}, false
	}
	return classItem{ranges: []rune{start.code, end.code}}, true
}

func (p *patternParser) classAtom(first bool) (escaped, bool) {
	c := p.take()
	switch c {
	case endOfPattern, '[', ']':
		return escaped{}, false
	case '\\':
		return p.escape()
	case '-':
		if !first && p.peek(0) != ']' {
			return escaped{}, false
		}
	}
	return literalEscape(c), true
}

func (p *patternParser) quantify(body *patternNode) (*patternNode, bool) {
	c := p.take()
	if c != '{' {
		low, high, hasMax := 0, 0, false
		switch c {
		case '+':
			low = 1
		case '?':
			low, high, hasMax = 0, 1, true
		}
		return &patternNode{
			kind:   nodeRepeat,
			size:   saturateSize(body.size + 1),
			body:   body,
			min:    low,
			max:    high,
			hasMax: hasMax,
		}, true
	}
	low, ok := p.count()
	if !ok {
		return nil, false
	}
	high, hasMax, ok := p.upper(low)
	if !ok {
		return nil, false
	}
	if p.take() != '}' {
		return nil, false
	}
	if !hasMax {
		return p.repeatNode(body, low, low, false, low+1), true
	}
	if low > high {
		return nil, false
	}
	return p.repeatNode(body, low, high, true, high), true
}

// upper reads what follows "{n": n again for {n}, no upper bound for {n,}, and m for {n,m}. The "}" is left.
func (p *patternParser) upper(low int) (int, bool, bool) {
	if p.peek(0) != ',' {
		return low, true, true
	}
	p.at++
	if p.peek(0) == '}' {
		return 0, false, true
	}
	high, ok := p.count()
	if !ok {
		return 0, false, false
	}
	return high, true, true
}

func (p *patternParser) repeatNode(body *patternNode, low, high int, hasMax bool, times int) *patternNode {
	size := times * body.size
	if size < 1 {
		size = 1
	}
	return &patternNode{
		kind:   nodeRepeat,
		size:   saturateSize(size),
		body:   body,
		min:    low,
		max:    high,
		hasMax: hasMax,
	}
}

// count reads ASCII digits with no leading zero, at most 100.
func (p *patternParser) count() (int, bool) {
	c := p.take()
	if !isPatternDigit(c) {
		return 0, false
	}
	if c == '0' {
		if isPatternDigit(p.peek(0)) {
			return 0, false
		}
		return 0, true
	}
	value := int(c - '0')
	for isPatternDigit(p.peek(0)) {
		value = value*10 + int(p.take()-'0')
		if value > maxPatternCount {
			return 0, false
		}
	}
	return value, true
}

func concatNodes(parts []*patternNode) *patternNode {
	size := 0
	for _, part := range parts {
		size += part.size
	}
	return &patternNode{kind: nodeConcat, size: saturateSize(size), parts: parts}
}

func alternationNodes(sides []*patternNode) *patternNode {
	if len(sides) == 1 {
		return sides[0]
	}
	size := len(sides) - 1
	for _, side := range sides {
		size += side.size
	}
	return &patternNode{kind: nodeAlt, size: saturateSize(size), parts: sides}
}

// The compiled program: one instruction per element, with jumps patched by index.
const (
	opChar uint8 = iota
	opSplit
	opStart
	opEnd
	opMatch
)

type instruction struct {
	op  uint8
	x   int
	y   int
	set *charSet
}

func emitNode(node *patternNode, program *[]instruction) {
	switch node.kind {
	case nodeSet:
		*program = append(*program, instruction{op: opChar, set: node.set})
	case nodeStart:
		*program = append(*program, instruction{op: opStart})
	case nodeEnd:
		*program = append(*program, instruction{op: opEnd})
	case nodeConcat:
		for _, part := range node.parts {
			emitNode(part, program)
		}
	case nodeAlt:
		emitAlternation(node.parts, program)
	case nodeRepeat:
		emitRepeat(node, program)
	}
}

func emitAlternation(sides []*patternNode, program *[]instruction) {
	var jumps []int
	last := len(sides) - 1
	for i, side := range sides {
		if i == last {
			emitNode(side, program)
			continue
		}
		choice := len(*program)
		*program = append(*program, instruction{op: opSplit, x: choice + 1, y: choice + 1})
		emitNode(side, program)
		jumps = append(jumps, len(*program))
		*program = append(*program, instruction{op: opSplit})
		(*program)[choice].y = len(*program)
	}
	for _, jump := range jumps {
		(*program)[jump].x = len(*program)
		(*program)[jump].y = len(*program)
	}
}

func emitRepeat(node *patternNode, program *[]instruction) {
	body := node.body
	if body == nil || body.size == 0 {
		// A body of size 0 is empty groups only and matches just the empty string; its copies would otherwise
		// cost instructions the size does not count.
		return
	}
	for i := 0; i < node.min; i++ {
		emitNode(body, program)
	}
	if !node.hasMax {
		top := len(*program)
		choice := len(*program)
		*program = append(*program, instruction{op: opSplit, x: choice + 1, y: choice + 1})
		emitNode(body, program)
		*program = append(*program, instruction{op: opSplit, x: top, y: top})
		(*program)[choice].y = len(*program)
		return
	}
	if node.max <= node.min {
		return
	}
	var optional []int
	for i := node.min; i < node.max; i++ {
		choice := len(*program)
		*program = append(*program, instruction{op: opSplit, x: choice + 1, y: choice + 1})
		optional = append(optional, choice)
		emitNode(body, program)
	}
	for _, choice := range optional {
		(*program)[choice].y = len(*program)
	}
}

// compilePattern parses and compiles a pattern, answering false when it is not a valid pattern (too long, too
// big or malformed).
func compilePattern(text string) ([]instruction, bool) {
	cps := codePointsOf(text)
	if len(cps) > maxPatternLength {
		return nil, false
	}
	parser := &patternParser{cps: cps}
	root, ok := parser.parse()
	if !ok || root.size > maxPatternSize {
		return nil, false
	}
	var program []instruction
	emitNode(root, &program)
	program = append(program, instruction{op: opMatch})
	return program, true
}

// patternMatches reports whether the pattern matches somewhere in text. Each instruction is visited at most
// once per input position.
func patternMatches(program []instruction, text string) bool {
	chars := codePointsOf(text)
	seen := make([]int, len(program))
	for i := range seen {
		seen[i] = -1
	}
	add := func(found []int, pc, pos int) ([]int, bool) {
		stack := []int{pc}
		for len(stack) > 0 {
			at := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[at] == pos {
				continue
			}
			seen[at] = pos
			step := program[at]
			switch step.op {
			case opMatch:
				return found, true
			case opChar:
				found = append(found, at)
			case opSplit:
				stack = append(stack, step.y, step.x)
			case opStart:
				if pos == 0 {
					stack = append(stack, at+1)
				}
			case opEnd:
				if pos == len(chars) {
					stack = append(stack, at+1)
				}
			}
		}
		return found, false
	}
	var current, following []int
	for pos := 0; ; pos++ {
		var matched bool
		current, matched = add(current, 0, pos)
		if matched {
			return true
		}
		if pos >= len(chars) {
			return false
		}
		following = following[:0]
		c := chars[pos]
		for _, pc := range current {
			step := program[pc]
			if step.op != opChar || !step.set.contains(c) {
				continue
			}
			var hit bool
			following, hit = add(following, pc+1, pos+1)
			if hit {
				return true
			}
		}
		current, following = following, current
	}
}
