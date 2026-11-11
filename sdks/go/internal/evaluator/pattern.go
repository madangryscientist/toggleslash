package evaluator

// The `matches` dialect of docs/evaluation.md ("Patterns"): a parser to a small tree, a size check, a
// compiler to a program, and a Pike VM that runs the program over code points. There is no backtracking,
// no platform regular expression and no cgo.

const (
	maxPatternLength = 1024
	maxPatternSize   = 10000
	sizeCap          = maxPatternSize + 1
	maxCount         = 100
)

type rng struct{ lo, hi rune }

type set struct {
	ranges []rng
	negate bool
}

func (s *set) has(r rune) bool {
	in := false
	for _, g := range s.ranges {
		if r >= g.lo && r <= g.hi {
			in = true
			break
		}
	}
	return in != s.negate
}

type nodeKind uint8

const (
	nSet nodeKind = iota
	nStart
	nEnd
	nConcat
	nAlt
	nRepeat
)

type node struct {
	kind     nodeKind
	set      *set
	children []*node
	min, max int // nRepeat; max == -1 means unbounded
	size     int
}

func sat(n int) int {
	if n > sizeCap {
		return sizeCap
	}
	return n
}

type patternParser struct {
	p []rune
	i int
}

func (pp *patternParser) more() bool { return pp.i < len(pp.p) }
func (pp *patternParser) peek() rune { return pp.p[pp.i] }

func isMeta(r rune) bool {
	switch r {
	case '\\', '.', '[', ']', '(', ')', '|', '*', '+', '?', '{', '}', '^', '$':
		return true
	}
	return false
}

var (
	digitRanges = []rng{{'0', '9'}}
	wordRanges  = []rng{{'0', '9'}, {'A', 'Z'}, {'_', '_'}, {'a', 'z'}}
	spaceRanges = []rng{{'\t', '\r'}, {' ', ' '}}
)

func complement(rs []rng) []rng {
	// rs is sorted and disjoint for the three base sets.
	var out []rng
	next := rune(0)
	for _, g := range rs {
		if g.lo > next {
			out = append(out, rng{next, g.lo - 1})
		}
		next = g.hi + 1
	}
	if next <= 0x10FFFF {
		out = append(out, rng{next, 0x10FFFF})
	}
	return out
}

// escape reads the escape that follows a backslash (pp.i is on the character after it). It returns either
// a single code point (single true) or a set of ranges (a class escape such as \d).
func (pp *patternParser) escape() (r rune, rs []rng, single bool, ok bool) {
	if !pp.more() {
		return 0, nil, false, false
	}
	c := pp.peek()
	pp.i++
	switch {
	case isMeta(c) || c == '-':
		return c, nil, true, true
	case c == 'n':
		return '\n', nil, true, true
	case c == 't':
		return '\t', nil, true, true
	case c == 'r':
		return '\r', nil, true, true
	case c == 'f':
		return '\f', nil, true, true
	case c == 'v':
		return '\v', nil, true, true
	case c == 'd':
		return 0, digitRanges, false, true
	case c == 'w':
		return 0, wordRanges, false, true
	case c == 's':
		return 0, spaceRanges, false, true
	case c == 'D':
		return 0, complement(digitRanges), false, true
	case c == 'W':
		return 0, complement(wordRanges), false, true
	case c == 'S':
		return 0, complement(spaceRanges), false, true
	}
	return 0, nil, false, false
}

type classAtom struct {
	r      rune
	rs     []rng
	single bool
}

// classAtom reads one atom inside a class. first is true for the first member of the class.
func (pp *patternParser) classAtom(first bool) (classAtom, bool) {
	if !pp.more() {
		return classAtom{}, false
	}
	c := pp.peek()
	switch c {
	case '[':
		return classAtom{}, false
	case '\\':
		pp.i++
		r, rs, single, ok := pp.escape()
		if !ok {
			return classAtom{}, false
		}
		return classAtom{r: r, rs: rs, single: single}, true
	case '-':
		// A bare '-' is a literal only as the first member or directly before ']'.
		if !first && (pp.i+1 >= len(pp.p) || pp.p[pp.i+1] != ']') {
			return classAtom{}, false
		}
	}
	pp.i++
	return classAtom{r: c, single: true}, true
}

func (pp *patternParser) class() (*node, bool) {
	// pp.i is just after '['.
	s := &set{}
	if pp.more() && pp.peek() == '^' {
		s.negate = true
		pp.i++
	}
	members := 0
	for {
		if !pp.more() {
			return nil, false
		}
		if pp.peek() == ']' {
			if members == 0 {
				return nil, false
			}
			pp.i++
			return &node{kind: nSet, set: s, size: 1}, true
		}
		a, ok := pp.classAtom(members == 0)
		if !ok {
			return nil, false
		}
		members++
		if pp.i+1 < len(pp.p) && pp.peek() == '-' && pp.p[pp.i+1] != ']' {
			pp.i++
			b, ok := pp.classAtom(false)
			if !ok || !a.single || !b.single || a.r > b.r {
				return nil, false
			}
			s.ranges = append(s.ranges, rng{a.r, b.r})
			continue
		}
		if a.single {
			s.ranges = append(s.ranges, rng{a.r, a.r})
		} else {
			s.ranges = append(s.ranges, a.rs...)
		}
	}
}

func (pp *patternParser) count() (int, bool) {
	start := pp.i
	for pp.more() && pp.peek() >= '0' && pp.peek() <= '9' {
		pp.i++
	}
	n := pp.i - start
	if n == 0 || (n > 1 && pp.p[start] == '0') || n > 3 {
		return 0, false
	}
	v := 0
	for _, d := range pp.p[start:pp.i] {
		v = v*10 + int(d-'0')
	}
	if v > maxCount {
		return 0, false
	}
	return v, true
}

// quant is a parsed quantifier. sym is true for the symbols * + ? and false for the braces, which differ in size.
type quant struct {
	min, max int
	sym      bool
}

// quantifier reads a quantifier if one starts at pp.i. found is false when none does; ok is false when a
// '{' does not start a valid quantifier.
func (pp *patternParser) quantifier() (q quant, found, ok bool) {
	if !pp.more() {
		return quant{}, false, true
	}
	switch pp.peek() {
	case '*':
		pp.i++
		return quant{0, -1, true}, true, true
	case '+':
		pp.i++
		return quant{1, -1, true}, true, true
	case '?':
		pp.i++
		return quant{0, 1, true}, true, true
	case '{':
		pp.i++
		n, ok := pp.count()
		if !ok || !pp.more() {
			return quant{}, true, false
		}
		switch pp.peek() {
		case '}':
			pp.i++
			return quant{n, n, false}, true, true
		case ',':
			pp.i++
			if pp.more() && pp.peek() == '}' {
				pp.i++
				return quant{n, -1, false}, true, true
			}
			m, ok := pp.count()
			if !ok || !pp.more() || pp.peek() != '}' || m < n {
				return quant{}, true, false
			}
			pp.i++
			return quant{n, m, false}, true, true
		}
		return quant{}, true, false
	}
	return quant{}, false, true
}

func isQuantifierStart(r rune) bool { return r == '*' || r == '+' || r == '?' || r == '{' }

func (pp *patternParser) atom() (*node, bool) {
	c := pp.peek()
	switch {
	case c == '(':
		pp.i++
		inner, ok := pp.alt()
		if !ok || !pp.more() || pp.peek() != ')' {
			return nil, false
		}
		pp.i++
		return inner, true
	case c == '[':
		pp.i++
		return pp.class()
	case c == '\\':
		pp.i++
		r, rs, single, ok := pp.escape()
		if !ok {
			return nil, false
		}
		if single {
			rs = []rng{{r, r}}
		}
		return &node{kind: nSet, set: &set{ranges: rs}, size: 1}, true
	case c == '.':
		pp.i++
		return &node{kind: nSet, set: &set{ranges: []rng{{'\n', '\n'}}, negate: true}, size: 1}, true
	case c == '^':
		pp.i++
		return &node{kind: nStart, size: 1}, true
	case c == '$':
		pp.i++
		return &node{kind: nEnd, size: 1}, true
	case c == ')' || c == ']' || c == '}' || isQuantifierStart(c):
		return nil, false
	}
	pp.i++
	return &node{kind: nSet, set: &set{ranges: []rng{{c, c}}}, size: 1}, true
}

func repeatSize(q quant, inner int) int {
	var s int
	switch {
	case q.sym:
		s = inner + 1
	case q.max == -1:
		s = (q.min + 1) * inner
	default:
		s = q.max * inner
	}
	if s < 1 {
		s = 1
	}
	return sat(s)
}

func (pp *patternParser) concat() (*node, bool) {
	var parts []*node
	size := 0
	for pp.more() && pp.peek() != '|' && pp.peek() != ')' {
		a, ok := pp.atom()
		if !ok {
			return nil, false
		}
		q, found, ok := pp.quantifier()
		if !ok {
			return nil, false
		}
		if found {
			if a.kind == nStart || a.kind == nEnd {
				return nil, false
			}
			a = &node{kind: nRepeat, children: []*node{a}, min: q.min, max: q.max, size: repeatSize(q, a.size)}
			if pp.more() && isQuantifierStart(pp.peek()) {
				return nil, false
			}
		}
		parts = append(parts, a)
		size = sat(size + a.size)
	}
	return &node{kind: nConcat, children: parts, size: size}, true
}

func (pp *patternParser) alt() (*node, bool) {
	first, ok := pp.concat()
	if !ok {
		return nil, false
	}
	if !pp.more() || pp.peek() != '|' {
		return first, true
	}
	sides := []*node{first}
	size := first.size
	for pp.more() && pp.peek() == '|' {
		pp.i++
		n, ok := pp.concat()
		if !ok {
			return nil, false
		}
		sides = append(sides, n)
		size = sat(size + n.size + 1)
	}
	return &node{kind: nAlt, children: sides, size: size}, true
}

// ---- program ----

type opcode uint8

const (
	opChar opcode = iota
	opSplit
	opJmp
	opStart
	opEnd
	opMatch
)

type inst struct {
	op   opcode
	set  *set
	x, y int
}

// Pattern is a compiled `matches` pattern.
type Pattern struct {
	insts []inst
}

type compiler struct{ insts []inst }

func (c *compiler) emit(i inst) int {
	c.insts = append(c.insts, i)
	return len(c.insts) - 1
}

func (c *compiler) node(n *node) {
	switch n.kind {
	case nSet:
		c.emit(inst{op: opChar, set: n.set})
	case nStart:
		c.emit(inst{op: opStart})
	case nEnd:
		c.emit(inst{op: opEnd})
	case nConcat:
		for _, ch := range n.children {
			c.node(ch)
		}
	case nAlt:
		var jumps []int
		for i, ch := range n.children {
			if i < len(n.children)-1 {
				split := c.emit(inst{op: opSplit})
				c.insts[split].x = len(c.insts)
				c.node(ch)
				jumps = append(jumps, c.emit(inst{op: opJmp}))
				c.insts[split].y = len(c.insts)
			} else {
				c.node(ch)
			}
		}
		for _, j := range jumps {
			c.insts[j].x = len(c.insts)
		}
	case nRepeat:
		c.repeat(n)
	}
}

func (c *compiler) star(body *node) {
	split := c.emit(inst{op: opSplit})
	c.insts[split].x = len(c.insts)
	c.node(body)
	c.emit(inst{op: opJmp, x: split})
	c.insts[split].y = len(c.insts)
}

func (c *compiler) repeat(n *node) {
	body := n.children[0]
	if body.size == 0 {
		return
	}
	for range n.min {
		c.node(body)
	}
	if n.max == -1 {
		c.star(body)
		return
	}
	var splits []int
	for i := n.min; i < n.max; i++ {
		split := c.emit(inst{op: opSplit})
		c.insts[split].x = len(c.insts)
		splits = append(splits, split)
		c.node(body)
	}
	for _, s := range splits {
		c.insts[s].y = len(c.insts)
	}
}

// CompilePattern parses a pattern. It returns nil when the pattern is not valid.
func CompilePattern(text string) *Pattern {
	pp := &patternParser{p: codePoints(text)}
	if len(pp.p) > maxPatternLength {
		return nil
	}
	root, ok := pp.alt()
	if !ok || pp.more() || root.size > maxPatternSize {
		return nil
	}
	c := &compiler{}
	c.node(root)
	c.emit(inst{op: opMatch})
	return &Pattern{insts: c.insts}
}

// Match reports whether the pattern matches anywhere in the code points of s.
func (p *Pattern) Match(in []rune) bool {
	n := len(p.insts)
	mark := make([]int32, n)
	gen := int32(1)
	stack := make([]int, 0, 64)
	clist := make([]int, 0, 16)
	nlist := make([]int, 0, 16)
	matched := false

	closure := func(pc, pos int, list []int) []int {
		stack = append(stack[:0], pc)
		for len(stack) > 0 {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if mark[top] == gen {
				continue
			}
			mark[top] = gen
			ins := &p.insts[top]
			switch ins.op {
			case opJmp:
				stack = append(stack, ins.x)
			case opSplit:
				stack = append(stack, ins.y, ins.x)
			case opStart:
				if pos == 0 {
					stack = append(stack, top+1)
				}
			case opEnd:
				if pos == len(in) {
					stack = append(stack, top+1)
				}
			case opMatch:
				matched = true
			case opChar:
				list = append(list, top)
			}
		}
		return list
	}

	clist = closure(0, 0, clist)
	for pos := 0; ; pos++ {
		if matched {
			return true
		}
		if pos >= len(in) {
			return false
		}
		r := in[pos]
		gen++
		nlist = nlist[:0]
		for _, pc := range clist {
			if p.insts[pc].set.has(r) {
				nlist = closure(pc+1, pos+1, nlist)
			}
		}
		nlist = closure(0, pos+1, nlist)
		clist, nlist = nlist, clist
	}
}
