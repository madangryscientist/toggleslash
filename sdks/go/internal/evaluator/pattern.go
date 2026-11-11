package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"

// Patterns for the `matches` op. pattern_parse.go reads a pattern into a tree; this compiles the
// tree to a small program and runs it on a Pike VM over code points: no backtracking and no platform regex
// package, so a match costs O(program x input) whatever the pattern.

type opCode uint8

const (
	instChar opCode = iota
	instSplit
	instStart
	instEnd
	instMatch
)

type inst struct {
	op   opCode
	set  *charSet
	x, y int
}

type pattern struct{ prog []inst }

func emitSplit(prog *[]inst, x int) int {
	*prog = append(*prog, inst{op: instSplit, x: x, y: x})
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
		(*prog)[j].x, (*prog)[j].y = len(*prog), len(*prog)
	}
}

func emitRepeat(body *node, lo, hi int, prog *[]inst) {
	// A body of size 0 is empty groups only and matches just the empty string, so repeating it
	// emits nothing.
	if body.size == 0 {
		return
	}
	for i := 0; i < lo; i++ {
		emit(body, prog)
	}
	if hi == -1 {
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
		set := n.set
		*prog = append(*prog, inst{op: instChar, set: &set})
	case nodeStart:
		*prog = append(*prog, inst{op: instStart})
	case nodeEnd:
		*prog = append(*prog, inst{op: instEnd})
	case nodeConcat:
		for _, p := range n.parts {
			emit(p, prog)
		}
	case nodeAlt:
		emitAlt(n.parts, prog)
	case nodeRepeat:
		emitRepeat(n.parts[0], n.min, n.max, prog)
	}
}

// compilePattern returns the compiled pattern, or nil when text is not a valid pattern.
func compilePattern(text string) *pattern {
	n := parsePattern(wtf8.CodePoints(text))
	if n == nil {
		return nil
	}
	var prog []inst
	emit(n, &prog)
	prog = append(prog, inst{op: instMatch})
	return &pattern{prog: prog}
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

// matches reports whether the pattern matches somewhere in text. Each instruction is visited once
// per position.
func (p *pattern) matches(text string) bool {
	input := wtf8.CodePoints(text)
	n := len(input)
	prog := p.prog
	seen := make([]int32, len(prog))
	for i := range seen {
		seen[i] = -1
	}
	var stack []int
	current := make([]int, 0, len(prog))
	next := make([]int, 0, len(prog))

	add := func(list *[]int, pc, pos int) bool {
		stack = append(stack[:0], pc)
		for len(stack) > 0 {
			at := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if at >= len(prog) || seen[at] == int32(pos) {
				continue
			}
			seen[at] = int32(pos)
			in := prog[at]
			switch in.op {
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
				if pos == n {
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
		if pos >= n {
			return false
		}
		c := input[pos]
		next = next[:0]
		for _, pc := range current {
			in := prog[pc]
			if in.op == instChar && inSet(in.set, c) && add(&next, pc+1, pos+1) {
				return true
			}
		}
		current, next = next, current[:0]
	}
}
