package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"

// Patterns for the `matches` op, to the "Patterns" section of docs/evaluation.md. pattern_parse.go reads a
// pattern into a tree; this compiles the tree to a small program and runs it on a Pike VM over code
// points: no backtracking and no regexp package, so a match costs O(program x input) whatever the pattern.

type instOp uint8

const (
	opChar instOp = iota
	opSplit
	opStart
	opEnd
	opMatch
)

// inst is one instruction. A jump is a split whose two targets are the same.
type inst struct {
	op   instOp
	set  *charSet // opChar
	x, y int      // opSplit
}

// Pattern is a compiled `matches` pattern.
type Pattern struct {
	program []inst
}

type compiler struct {
	program []inst
}

func (c *compiler) split(x int) int {
	c.program = append(c.program, inst{op: opSplit, x: x, y: x})
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
		c.program[j].x = len(c.program)
		c.program[j].y = len(c.program)
	}
}

func (c *compiler) emitRepeat(body *node, lo, hi int) {
	// A body of size 0 is empty groups only and matches just the empty string, so repeating it emits
	// nothing. Its copies would otherwise cost instructions that the size does not count.
	if body.size == 0 {
		return
	}
	for i := 0; i < lo; i++ {
		c.emit(body)
	}
	if hi < 0 {
		top := len(c.program)
		loop := c.split(top + 1)
		c.emit(body)
		c.split(top)
		c.program[loop].y = len(c.program)
		return
	}
	var optional []int
	for i := lo; i < hi; i++ {
		optional = append(optional, c.split(len(c.program)+1))
		c.emit(body)
	}
	for _, choice := range optional {
		c.program[choice].y = len(c.program)
	}
}

func (c *compiler) emit(n *node) {
	switch n.kind {
	case nodeSet:
		set := n.set
		c.program = append(c.program, inst{op: opChar, set: &set})
	case nodeStart:
		c.program = append(c.program, inst{op: opStart})
	case nodeEnd:
		c.program = append(c.program, inst{op: opEnd})
	case nodeConcat:
		for _, part := range n.parts {
			c.emit(part)
		}
	case nodeAlt:
		c.emitAlt(n.parts)
	case nodeRepeat:
		c.emitRepeat(n.body, n.min, n.max)
	}
}

// CompilePattern returns the compiled pattern, or nil when pattern is not a valid pattern.
func CompilePattern(pattern string) *Pattern {
	tree := parsePattern(pattern)
	if tree == nil {
		return nil
	}
	c := &compiler{}
	c.emit(tree)
	c.program = append(c.program, inst{op: opMatch})
	return &Pattern{program: c.program}
}

func inItem(it item, c rune) bool {
	hit := false
	for i := 0; i+1 < len(it.ranges) && !hit; i += 2 {
		hit = it.ranges[i] <= c && c <= it.ranges[i+1]
	}
	return hit != it.negate
}

func inSet(set *charSet, c rune) bool {
	hit := false
	for _, it := range set.items {
		if inItem(it, c) {
			hit = true
			break
		}
	}
	return hit != set.negate
}

// Matches reports whether the pattern matches somewhere in text. It is a Pike VM: each instruction is
// visited at most once per input position.
func (p *Pattern) Matches(text string) bool {
	input := wtf8.Runes(text)
	length := len(input)
	program := p.program
	seen := make([]int, len(program))
	for i := range seen {
		seen[i] = -1
	}
	var stack []int
	current := make([]int, 0, len(program))
	next := make([]int, 0, len(program))

	// add puts pc and everything it reaches without reading into list; it is true when that reaches a match.
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
		// `current` now holds the threads for pos+1; add(0, pos+1) joins them with a fresh start.
	}
}
