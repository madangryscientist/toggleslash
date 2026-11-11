package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"

// The parser for `matches` patterns, to the "Patterns" section of docs/evaluation.md: code points in, a
// tree with each node's size out, or "not valid". pattern.go compiles the tree and runs it.

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
	kind     nodeKind
	size     int
	set      charSet // nodeSet
	parts    []*node // nodeConcat, nodeAlt
	body     *node   // nodeRepeat
	min, max int     // nodeRepeat; max is -1 for no limit
}

type invalidPattern struct{}

func fail() { panic(invalidPattern{}) }

func saturate(size int) int { return min(size, maxPatternSize+1) }

func single(c rune) item { return item{ranges: []rune{c, c}} }

func setOf(it item) *node {
	return &node{kind: nodeSet, set: charSet{items: []item{it}}, size: 1}
}

var (
	digitItem = item{ranges: []rune{'0', '9'}}
	wordItem  = item{ranges: []rune{'0', '9', 'A', 'Z', '_', '_', 'a', 'z'}}
	// U+0009 to U+000D are \t \n \v \f \r.
	spaceItem = item{ranges: []rune{0x09, 0x0D, 0x20, 0x20}}
)

func negated(it item) item { return item{ranges: it.ranges, negate: true} }

// isEscapable reports whether `\` may precede c to mean c itself.
func isEscapable(c rune) bool {
	switch c {
	case '\\', '.', '[', ']', '(', ')', '|', '*', '+', '?', '{', '}', '^', '$', '-':
		return true
	}
	return false
}

// isNotAnAtom: code points that cannot start an atom outside a class.
func isNotAnAtom(c rune) bool {
	switch c {
	case '*', '+', '?', '{', '}', ']':
		return true
	}
	return false
}

func isQuantifier(c rune) bool {
	switch c {
	case '*', '+', '?', '{':
		return true
	}
	return false
}

func isDigitRune(c rune) bool { return c >= '0' && c <= '9' }

type patternParser struct {
	cps []rune
	pos int
}

func (p *patternParser) peek(offset int) rune {
	if p.pos+offset < len(p.cps) {
		return p.cps[p.pos+offset]
	}
	return endOfPattern
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
	for _, side := range sides {
		size += side.size
	}
	return &node{kind: nodeAlt, parts: sides, size: saturate(size)}
}

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
		size = saturate(size + atom.size)
	}
	return &node{kind: nodeConcat, parts: parts, size: size}
}

func (p *patternParser) atom() *node {
	c := p.take()
	switch {
	case c == '(':
		return p.group()
	case c == '[':
		return &node{kind: nodeSet, set: p.charClass(), size: 1}
	case c == '\\':
		return setOf(asItem(p.escape()))
	case isNotAnAtom(c):
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

func (p *patternParser) group() *node {
	inner := p.alternation()
	if p.take() != ')' {
		fail()
	}
	return inner
}

// escape reads what follows a `\`: one code point, or one of the six shorthand classes.
func (p *patternParser) escape() any {
	c := p.take()
	if isEscapable(c) {
		return c
	}
	switch c {
	case 'n':
		return rune('\n')
	case 't':
		return rune('\t')
	case 'r':
		return rune('\r')
	case 'f':
		return rune('\f')
	case 'v':
		return rune('\v')
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
	return nil
}

func asItem(atom any) item {
	if it, ok := atom.(item); ok {
		return it
	}
	return single(atom.(rune))
}

// charClass reads the members up to `]`, after `[`.
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
	from := p.classAtom(first)
	isRange := p.peek(0) == '-' && p.peek(1) != endOfPattern && p.peek(1) != ']'
	if !isRange {
		return asItem(from)
	}
	p.pos++
	to := p.classAtom(false)
	lo, okLo := from.(rune)
	hi, okHi := to.(rune)
	if !okLo || !okHi || lo > hi {
		fail()
	}
	return item{ranges: []rune{lo, hi}}
}

func (p *patternParser) classAtom(first bool) any {
	c := p.take()
	// `]` here is a class with no members; the end and `[` are never members.
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
	return c
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
		return &node{kind: nodeRepeat, body: body, min: lo, max: hi, size: saturate(body.size + 1)}
	}
	lo := p.count()
	hi := p.upper(lo)
	if p.take() != '}' {
		fail()
	}
	if hi < 0 {
		return repeat(body, lo, -1, lo+1)
	}
	if lo > hi {
		fail()
	}
	return repeat(body, lo, hi, hi)
}

// upper reads what follows `{n`: n again for `{n}`, -1 for `{n,}`, m for `{n,m}`. It leaves the `}`.
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

func repeat(body *node, lo, hi, times int) *node {
	return &node{kind: nodeRepeat, body: body, min: lo, max: hi, size: max(1, saturate(times*body.size))}
}

// count reads ASCII digits with no leading zero, at most 100.
func (p *patternParser) count() int {
	c := p.take()
	if !isDigitRune(c) {
		fail()
	}
	if c == '0' {
		if isDigitRune(p.peek(0)) {
			fail()
		}
		return 0
	}
	value := int(c - '0')
	for isDigitRune(p.peek(0)) {
		value = value*10 + int(p.take()-'0')
		if value > maxCount {
			fail()
		}
	}
	return value
}

// parsePattern returns the tree for a pattern, or nil when it is not a valid pattern: too long,
// malformed or too big.
func parsePattern(pattern string) (tree *node) {
	cps := wtf8.Runes(pattern)
	if len(cps) > maxPatternLength {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(invalidPattern); !ok {
				panic(r)
			}
			tree = nil
		}
	}()
	p := &patternParser{cps: cps}
	tree = p.alternation()
	if p.pos < len(cps) { // an unmatched `)`
		return nil
	}
	if tree.size > maxPatternSize {
		return nil
	}
	return tree
}
