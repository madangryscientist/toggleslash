package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"

// Patterns for the `matches` op, to the "Patterns" section of docs/evaluation.md. pattern_parse.go
// reads a pattern into a tree; this compiles the tree to a small program and runs it on a Pike VM over
// code points: no backtracking and no regexp package, so a match costs O(program x input) whatever the
// pattern.

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

// pattern is a compiled `matches` pattern.
type pattern struct{ program []inst }

func emitSplit(program *[]inst, x int) int {
	*program = append(*program, inst{op: opSplit, x: x, y: x})
	return len(*program) - 1
}

func emitAlt(sides []*node, program *[]inst) {
	var jumps []int
	last := len(sides) - 1
	for i, side := range sides {
		choice := -1
		if i < last {
			choice = emitSplit(program, len(*program)+1)
		}
		emit(side, program)
		if choice < 0 {
			continue
		}
		jumps = append(jumps, emitSplit(program, 0))
		(*program)[choice].y = len(*program)
	}
	for _, j := range jumps {
		(*program)[j].x = len(*program)
		(*program)[j].y = len(*program)
	}
}

func emitRepeat(body *node, min, max int, program *[]inst) {
	// A body of size 0 is empty groups only and matches just the empty string, so repeating it emits
	// nothing. Its copies would otherwise cost instructions that the size does not count.
	if body.size == 0 {
		return
	}
	for i := 0; i < min; i++ {
		emit(body, program)
	}
	if max < 0 {
		top := len(*program)
		loop := emitSplit(program, top+1)
		emit(body, program)
		emitSplit(program, top)
		(*program)[loop].y = len(*program)
		return
	}
	var optional []int
	for i := min; i < max; i++ {
		optional = append(optional, emitSplit(program, len(*program)+1))
		emit(body, program)
	}
	for _, c := range optional {
		(*program)[c].y = len(*program)
	}
}

func emit(n *node, program *[]inst) {
	switch n.kind {
	case nodeSet:
		set := n.set
		*program = append(*program, inst{op: opChar, set: &set})
	case nodeStart:
		*program = append(*program, inst{op: opStart})
	case nodeEnd:
		*program = append(*program, inst{op: opEnd})
	case nodeConcat:
		for _, part := range n.parts {
			emit(part, program)
		}
	case nodeAlt:
		emitAlt(n.parts, program)
	case nodeRepeat:
		emitRepeat(n.parts[0], n.min, n.max, program)
	}
}

// compilePattern compiles text, or reports false when it is not a valid pattern.
func compilePattern(text string) (*pattern, bool) {
	root, ok := parsePattern(wtf8.CodePoints(text))
	if !ok {
		return nil, false
	}
	var program []inst
	emit(root, &program)
	program = append(program, inst{op: opMatch})
	return &pattern{program: program}, true
}

func inItem(it *item, c rune) bool {
	hit := false
	for i := 0; i+1 < len(it.ranges) && !hit; i += 2 {
		hit = it.ranges[i] <= c && c <= it.ranges[i+1]
	}
	return hit != it.negate
}

func inSet(set *charSet, c rune) bool {
	hit := false
	for i := range set.items {
		if inItem(&set.items[i], c) {
			hit = true
			break
		}
	}
	return hit != set.negate
}

// matches reports whether the pattern matches somewhere in text. A Pike VM: each instruction is
// visited once per position.
func (p *pattern) matches(text string) bool {
	program := p.program
	input := wtf8.CodePoints(text)
	length := len(input)
	seen := make([]int32, len(program))
	for i := range seen {
		seen[i] = -1
	}
	var stack []int
	current := make([]int, 0, len(program))
	next := make([]int, 0, len(program))

	// add puts pc and everything it reaches without reading on list; true when that reaches a match.
	add := func(list *[]int, pc, pos int) bool {
		stack = append(stack[:0], pc)
		for len(stack) > 0 {
			at := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if at >= len(program) || int(seen[at]) == pos {
				continue
			}
			seen[at] = int32(pos)
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
			if inSet(in.set, c) && add(&next, pc+1, pos+1) {
				return true
			}
		}
		current, next = next, current
		next = next[:0]
	}
}
