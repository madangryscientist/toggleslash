package eval

import (
	"sort"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonval"
)

// Patterns, to docs/evaluation.md "Patterns": a small dialect parsed to a
// program and run as a Pike VM that visits each instruction at most once per
// input position, so there is no backtracking. No platform regular expression
// is used, not even as a fast path.

const (
	maxPatternPoints = 1024
	maxPatternSize   = 10000
	maxRepeatCount   = 100
)

// instruction opcodes.
const (
	opRune = iota
	opAny
	opClass
	opSplit
	opJump
	opBOL
	opEOL
	opMatch
)

type instruction struct {
	op    int
	ch    rune
	class *runeClass
	x, y  int
}

type runeRange struct {
	lo, hi rune
}

// runeClass is one character class: code point ranges, optionally negated.
type runeClass struct {
	negated bool
	ranges  []runeRange
}

func (c *runeClass) matches(r rune) bool {
	found := false
	for _, class := range c.ranges {
		if r >= class.lo && r <= class.hi {
			found = true
			break
		}
	}
	return found != c.negated
}

var (
	digitRanges = []runeRange{{'0', '9'}}
	wordRanges  = []runeRange{{'0', '9'}, {'A', 'Z'}, {'a', 'z'}, {'_', '_'}}
	spaceRanges = []runeRange{{' ', ' '}, {'\t', '\r'}}
)

// complementRanges is every code point the ranges do not hold. An escape such
// as `\D` is a set of its own, so a class that holds one can still be negated as
// a whole.
func complementRanges(ranges []runeRange) []runeRange {
	sorted := make([]runeRange, len(ranges))
	copy(sorted, ranges)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].lo < sorted[j].lo })
	out := make([]runeRange, 0, len(sorted)+1)
	next := rune(0)
	for _, class := range sorted {
		if class.lo > next {
			out = append(out, runeRange{lo: next, hi: class.lo - 1})
		}
		if class.hi+1 > next {
			next = class.hi + 1
		}
	}
	if next <= 0x10FFFF {
		out = append(out, runeRange{lo: next, hi: 0x10FFFF})
	}
	return out
}

// patternNodes are the parse tree of one pattern.
type (
	literalNode     struct{ ch rune }
	anyNode         struct{}
	classNode       struct{ class *runeClass }
	beginNode       struct{}
	endNode         struct{}
	concatNode      struct{ kids []patternNode }
	alternationNode struct{ kids []patternNode }
	groupNode       struct{ kid patternNode }
	repeatNode      struct {
		kid      patternNode
		min, max int // max is -1 when unbounded
		bare     bool
	}
)

type patternNode interface{}

type compiledPattern struct {
	prog []instruction
}

// patternMatches reports whether patternText is a valid pattern that matches
// somewhere in candidate. An invalid pattern matches nothing.
func patternMatches(candidate, patternText string) bool {
	parsed, ok := parsePattern(patternText)
	if !ok {
		return false
	}
	return runPattern(parsed.prog, jsonval.CodePoints(candidate))
}

func parsePattern(text string) (*compiledPattern, bool) {
	points := jsonval.CodePoints(text)
	if len(points) > maxPatternPoints {
		return nil, false
	}
	p := &patternParser{points: points}
	node, ok := p.alternation()
	if !ok || p.pos != len(p.points) {
		return nil, false
	}
	if nodeSize(node) > maxPatternSize {
		return nil, false
	}
	c := &compiler{}
	c.compile(node)
	c.emit(instruction{op: opMatch})
	return &compiledPattern{prog: c.insts}, true
}

type patternParser struct {
	points []rune
	pos    int
}

func (p *patternParser) alternation() (patternNode, bool) {
	first, ok := p.concatenation()
	if !ok {
		return nil, false
	}
	kids := []patternNode{first}
	for p.pos < len(p.points) && p.points[p.pos] == '|' {
		p.pos++
		next, ok := p.concatenation()
		if !ok {
			return nil, false
		}
		kids = append(kids, next)
	}
	if len(kids) == 1 {
		return kids[0], true
	}
	return alternationNode{kids: kids}, true
}

func (p *patternParser) concatenation() (patternNode, bool) {
	var kids []patternNode
	for p.pos < len(p.points) {
		c := p.points[p.pos]
		if c == '|' || c == ')' {
			break
		}
		item, ok := p.item()
		if !ok {
			return nil, false
		}
		kids = append(kids, item)
	}
	return concatNode{kids: kids}, true
}

func (p *patternParser) item() (patternNode, bool) {
	atom, ok := p.atom()
	if !ok {
		return nil, false
	}
	if p.pos >= len(p.points) {
		return atom, true
	}
	var min, max int
	switch p.points[p.pos] {
	case '*':
		p.pos++
		min, max = 0, -1
	case '+':
		p.pos++
		min, max = 1, -1
	case '?':
		p.pos++
		min, max = 0, 1
	case '{':
		return p.counted(atom)
	default:
		return atom, true
	}
	switch atom.(type) {
	case beginNode, endNode:
		return nil, false
	}
	return repeatNode{kid: atom, min: min, max: max, bare: true}, true
}

func (p *patternParser) counted(atom patternNode) (patternNode, bool) {
	switch atom.(type) {
	case beginNode, endNode:
		return nil, false
	}
	p.pos++ // '{'
	min, ok := p.count()
	if !ok {
		return nil, false
	}
	if p.pos < len(p.points) && p.points[p.pos] == '}' {
		p.pos++
		return repeatNode{kid: atom, min: min, max: min}, true
	}
	if p.pos >= len(p.points) || p.points[p.pos] != ',' {
		return nil, false
	}
	p.pos++
	if p.pos < len(p.points) && p.points[p.pos] == '}' {
		p.pos++
		return repeatNode{kid: atom, min: min, max: -1}, true
	}
	max, ok := p.count()
	if !ok {
		return nil, false
	}
	if p.pos >= len(p.points) || p.points[p.pos] != '}' {
		return nil, false
	}
	p.pos++
	if max < min {
		return nil, false
	}
	return repeatNode{kid: atom, min: min, max: max}, true
}

// count reads a repetition count: ASCII digits, no leading zero, at most 100.
func (p *patternParser) count() (int, bool) {
	start := p.pos
	for p.pos < len(p.points) && p.points[p.pos] >= '0' && p.points[p.pos] <= '9' {
		p.pos++
	}
	if p.pos == start {
		return 0, false
	}
	if p.pos-start > 1 && p.points[start] == '0' {
		return 0, false
	}
	value := 0
	for _, digit := range p.points[start:p.pos] {
		value = value*10 + int(digit-'0')
	}
	if value > maxRepeatCount {
		return 0, false
	}
	return value, true
}

func (p *patternParser) atom() (patternNode, bool) {
	if p.pos >= len(p.points) {
		return nil, false
	}
	c := p.points[p.pos]
	switch c {
	case '(':
		p.pos++
		if p.pos < len(p.points) && p.points[p.pos] == '?' {
			return nil, false
		}
		inner, ok := p.alternation()
		if !ok {
			return nil, false
		}
		if p.pos >= len(p.points) || p.points[p.pos] != ')' {
			return nil, false
		}
		p.pos++
		return groupNode{kid: inner}, true
	case '[':
		class, ok := p.class()
		if !ok {
			return nil, false
		}
		return classNode{class: class}, true
	case '.':
		p.pos++
		return anyNode{}, true
	case '^':
		p.pos++
		return beginNode{}, true
	case '$':
		p.pos++
		return endNode{}, true
	case '\\':
		return p.escape()
	case ')', '|', '*', '+', '?', '{', '}', ']':
		return nil, false
	default:
		p.pos++
		return literalNode{ch: c}, true
	}
}

// escape reads one `\` escape outside a class.
func (p *patternParser) escape() (patternNode, bool) {
	p.pos++ // '\'
	if p.pos >= len(p.points) {
		return nil, false
	}
	c := p.points[p.pos]
	p.pos++
	switch c {
	case '\\', '.', '[', ']', '(', ')', '|', '*', '+', '?', '{', '}', '^', '$', '-':
		return literalNode{ch: c}, true
	case 'n':
		return literalNode{ch: '\n'}, true
	case 't':
		return literalNode{ch: '\t'}, true
	case 'r':
		return literalNode{ch: '\r'}, true
	case 'f':
		return literalNode{ch: '\f'}, true
	case 'v':
		return literalNode{ch: '\v'}, true
	case 'd':
		return classNode{class: &runeClass{ranges: digitRanges}}, true
	case 'w':
		return classNode{class: &runeClass{ranges: wordRanges}}, true
	case 's':
		return classNode{class: &runeClass{ranges: spaceRanges}}, true
	case 'D':
		return classNode{class: &runeClass{ranges: complementRanges(digitRanges)}}, true
	case 'W':
		return classNode{class: &runeClass{ranges: complementRanges(wordRanges)}}, true
	case 'S':
		return classNode{class: &runeClass{ranges: complementRanges(spaceRanges)}}, true
	default:
		return nil, false
	}
}

// class reads a `[` class. Members are read left to right; `-` is a literal only
// as the first member or directly before `]`, and a range endpoint is one code
// point, never a set.
func (p *patternParser) class() (*runeClass, bool) {
	p.pos++ // '['
	class := &runeClass{}
	if p.pos < len(p.points) && p.points[p.pos] == '^' {
		class.negated = true
		p.pos++
	}
	members := 0
	for {
		if p.pos >= len(p.points) {
			return nil, false
		}
		if p.points[p.pos] == ']' {
			break
		}
		low, set, single, ok := p.classAtom(members == 0)
		if !ok {
			return nil, false
		}
		if !single {
			class.ranges = append(class.ranges, set...)
			members++
			continue
		}
		if p.pos+1 < len(p.points) && p.points[p.pos] == '-' && p.points[p.pos+1] != ']' {
			p.pos++ // '-'
			high, _, highSingle, ok := p.classAtom(false)
			if !ok || !highSingle {
				return nil, false
			}
			if high < low {
				return nil, false
			}
			class.ranges = append(class.ranges, runeRange{lo: low, hi: high})
			members++
			continue
		}
		class.ranges = append(class.ranges, runeRange{lo: low, hi: low})
		members++
	}
	if members == 0 {
		return nil, false
	}
	p.pos++ // ']'
	return class, true
}

// classAtom reads one class member: a single code point, or the set an escape
// such as `\d` stands for.
func (p *patternParser) classAtom(first bool) (rune, []runeRange, bool, bool) {
	if p.pos >= len(p.points) {
		return 0, nil, false, false
	}
	c := p.points[p.pos]
	switch c {
	case ']', '[':
		return 0, nil, false, false
	case '\\':
		node, ok := p.escape()
		if !ok {
			return 0, nil, false, false
		}
		switch parsed := node.(type) {
		case literalNode:
			return parsed.ch, nil, true, true
		case classNode:
			return 0, parsed.class.ranges, false, true
		default:
			return 0, nil, false, false
		}
	case '-':
		beforeClose := p.pos+1 < len(p.points) && p.points[p.pos+1] == ']'
		if !first && !beforeClose {
			return 0, nil, false, false
		}
		p.pos++
		return '-', nil, true, true
	default:
		p.pos++
		return c, nil, true, true
	}
}

// nodeSize is the parse size of docs/evaluation.md "Patterns", which bounds the
// work a pattern can cause. Every step saturates at one past the limit instead
// of wrapping: a size that overflowed would pass the check and then compile a
// pattern whose repetition multiplies out to a hang.
func nodeSize(n patternNode) int {
	switch node := n.(type) {
	case literalNode, anyNode, classNode, beginNode, endNode:
		return 1
	case concatNode:
		total := 0
		for _, kid := range node.kids {
			total = sizeSum(total, nodeSize(kid))
		}
		return total
	case alternationNode:
		total := 0
		for _, kid := range node.kids {
			total = sizeSum(total, nodeSize(kid))
		}
		return sizeSum(total, len(node.kids)-1)
	case groupNode:
		return nodeSize(node.kid)
	case repeatNode:
		var total int
		switch {
		case node.bare:
			total = sizeSum(nodeSize(node.kid), 1)
		case node.max < 0:
			total = sizeProduct(node.min+1, nodeSize(node.kid))
		default:
			total = sizeProduct(node.max, nodeSize(node.kid))
		}
		if total < 1 {
			total = 1
		}
		return total
	default:
		return 1
	}
}

// sizeLimit is one past the largest size a pattern may have, so a saturated
// total always fails the check.
const sizeLimit = maxPatternSize + 1

func sizeSum(total, add int) int {
	if total >= sizeLimit || add >= sizeLimit || add > sizeLimit-total {
		return sizeLimit
	}
	return total + add
}

func sizeProduct(a, b int) int {
	if a <= 0 || b <= 0 {
		return 0
	}
	if a >= sizeLimit || b >= sizeLimit || a > sizeLimit/b {
		return sizeLimit
	}
	return a * b
}

type compiler struct {
	insts []instruction
}

func (c *compiler) emit(inst instruction) int {
	c.insts = append(c.insts, inst)
	return len(c.insts) - 1
}

func (c *compiler) compile(n patternNode) {
	switch node := n.(type) {
	case literalNode:
		c.emit(instruction{op: opRune, ch: node.ch})
	case anyNode:
		c.emit(instruction{op: opAny})
	case classNode:
		c.emit(instruction{op: opClass, class: node.class})
	case beginNode:
		c.emit(instruction{op: opBOL})
	case endNode:
		c.emit(instruction{op: opEOL})
	case groupNode:
		c.compile(node.kid)
	case concatNode:
		for _, kid := range node.kids {
			c.compile(kid)
		}
	case alternationNode:
		var jumps []int
		for i, kid := range node.kids {
			if i == len(node.kids)-1 {
				c.compile(kid)
				break
			}
			split := c.emit(instruction{op: opSplit})
			c.insts[split].x = split + 1
			c.compile(kid)
			jumps = append(jumps, c.emit(instruction{op: opJump}))
			c.insts[split].y = len(c.insts)
		}
		end := len(c.insts)
		for _, jump := range jumps {
			c.insts[jump].x = end
		}
	case repeatNode:
		c.compileRepeat(node)
	}
}

func (c *compiler) compileRepeat(node repeatNode) {
	for i := 0; i < node.min; i++ {
		c.compile(node.kid)
	}
	if node.max < 0 {
		split := c.emit(instruction{op: opSplit})
		c.insts[split].x = split + 1
		c.compile(node.kid)
		c.emit(instruction{op: opJump, x: split})
		c.insts[split].y = len(c.insts)
		return
	}
	var splits []int
	for i := node.min; i < node.max; i++ {
		split := c.emit(instruction{op: opSplit})
		c.insts[split].x = split + 1
		splits = append(splits, split)
		c.compile(node.kid)
	}
	end := len(c.insts)
	for _, split := range splits {
		c.insts[split].y = end
	}
}

// threadList is the list of instructions the VM is at for one position. seen
// holds the epoch each instruction was last added in, so a position visits each
// instruction once even through an empty loop.
type threadList struct {
	pcs   []int
	seen  []int64
	epoch int64
}

// runPattern runs the program over input and reports whether it matches
// anywhere. A thread starts at every position, which is the search.
func runPattern(prog []instruction, input []rune) bool {
	list := threadList{pcs: make([]int, 0, len(prog)), seen: make([]int64, len(prog))}
	next := threadList{pcs: make([]int, 0, len(prog)), seen: make([]int64, len(prog))}
	var epoch int64
	n := len(input)
	for pos := 0; pos <= n; pos++ {
		epoch++
		list.epoch = epoch
		var matched bool
		list, matched = addThread(prog, list, 0, pos, n)
		if matched {
			return true
		}
		if pos == n {
			return false
		}
		epoch++
		next.epoch = epoch
		next.pcs = next.pcs[:0]
		for _, pc := range list.pcs {
			if matchesInstruction(prog[pc], input[pos]) {
				next, matched = addThread(prog, next, pc+1, pos+1, n)
				if matched {
					return true
				}
			}
		}
		list, next = next, list
	}
	return false
}

// addThread follows the empty instructions from pc until it queues an
// instruction that reads input, and reports a match.
func addThread(prog []instruction, list threadList, pc, pos, n int) (threadList, bool) {
	if list.seen[pc] == list.epoch {
		return list, false
	}
	list.seen[pc] = list.epoch
	switch prog[pc].op {
	case opJump:
		return addThread(prog, list, prog[pc].x, pos, n)
	case opSplit:
		list, matched := addThread(prog, list, prog[pc].x, pos, n)
		if matched {
			return list, true
		}
		return addThread(prog, list, prog[pc].y, pos, n)
	case opBOL:
		if pos != 0 {
			return list, false
		}
		return addThread(prog, list, pc+1, pos, n)
	case opEOL:
		if pos != n {
			return list, false
		}
		return addThread(prog, list, pc+1, pos, n)
	case opMatch:
		return list, true
	default:
		list.pcs = append(list.pcs, pc)
		return list, false
	}
}

func matchesInstruction(inst instruction, r rune) bool {
	switch inst.op {
	case opRune:
		return inst.ch == r
	case opAny:
		return r != '\n'
	case opClass:
		return inst.class.matches(r)
	default:
		return false
	}
}
