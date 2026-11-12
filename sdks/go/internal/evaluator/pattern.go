package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"

// Patterns for the `matches` op. pattern_parse.go reads a pattern into a tree; this compiles the tree to a small
// program and runs it on a Pike VM over code points: no backtracking and no platform regexp, so a match costs
// O(program x input) whatever the pattern.

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

// pattern is a compiled pattern.
type pattern []inst

func emitSplit(prog *[]inst, x int) int {
	*prog = append(*prog, inst{op: opSplit, x: x, y: x})
	return len(*prog) - 1
}

func emitAlt(sides []*node, prog *[]inst) {
	var jumps []int
	last := len(sides) - 1
	for i, side := range sides {
		choice := -1
		if i < last {
			choice = emitSplit(prog, len(*prog)+1)
		}
		emit(side, prog)
		if choice < 0 {
			continue
		}
		jumps = append(jumps, emitSplit(prog, 0))
		(*prog)[choice].y = len(*prog)
	}
	for _, j := range jumps {
		(*prog)[j].x = len(*prog)
		(*prog)[j].y = len(*prog)
	}
}

func emitRepeat(body *node, lo, hi int, prog *[]inst) {
	// A body of size 0 is empty groups only and matches just the empty string, so repeating it emits
	// nothing. Its copies would otherwise cost instructions that the size does not count.
	if body.size == 0 {
		return
	}
	for i := 0; i < lo; i++ {
		emit(body, prog)
	}
	if hi < 0 {
		top := len(*prog)
		loop := emitSplit(prog, top+1)
		emit(body, prog)
		emitSplit(prog, top)
		(*prog)[loop].y = len(*prog)
		return
	}
	var optional []int
	for i := lo; i < hi; i++ {
		optional = append(optional, emitSplit(prog, len(*prog)+1))
		emit(body, prog)
	}
	for _, c := range optional {
		(*prog)[c].y = len(*prog)
	}
}

func emit(n *node, prog *[]inst) {
	switch n.kind {
	case nodeSet:
		*prog = append(*prog, inst{op: opChar, set: n.set})
	case nodeStart:
		*prog = append(*prog, inst{op: opStart})
	case nodeEnd:
		*prog = append(*prog, inst{op: opEnd})
	case nodeConcat:
		for _, part := range n.parts {
			emit(part, prog)
		}
	case nodeAlt:
		emitAlt(n.parts, prog)
	case nodeRepeat:
		emitRepeat(n.parts[0], n.min, n.max, prog)
	}
}

// compilePattern returns the compiled pattern, or false when text is not a valid pattern.
func compilePattern(text string) (pattern, bool) {
	tree, ok := parsePattern(wtf8.CodePoints(text))
	if !ok {
		return nil, false
	}
	var prog []inst
	emit(tree, &prog)
	prog = append(prog, inst{op: opMatch})
	return prog, true
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

// matches reports whether the pattern matches somewhere in text. It is a Pike VM: each instruction is visited once
// per position.
func (prog pattern) matches(text string) bool {
	input := wtf8.CodePoints(text)
	length := len(input)
	seen := make([]int, len(prog))
	for i := range seen {
		seen[i] = -1
	}
	var stack []int
	var current, next []int

	// add puts pc and everything it reaches without reading on list, and reports whether that reaches a match.
	add := func(list *[]int, pc, pos int) bool {
		stack = append(stack[:0], pc)
		for len(stack) > 0 {
			at := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if at >= len(prog) || seen[at] == pos {
				continue
			}
			seen[at] = pos
			in := prog[at]
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
			in := prog[pc]
			if in.op == opChar && inSet(in.set, c) && add(&next, pc+1, pos+1) {
				return true
			}
		}
		current, next = next, current
	}
}
