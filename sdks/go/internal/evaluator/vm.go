package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"

// Patterns for the `matches` op run on a Pike VM over code points: no backtracking and no regexp
// package, so a match costs O(program x input) whatever the pattern.

type opcode int

const (
	opChar opcode = iota
	opSplit
	opStart
	opEnd
	opMatch
)

// inst is one instruction. A jump is a split whose two targets are the same.
type inst struct {
	op   opcode
	set  charSet
	x, y int
}

type program struct {
	insts []inst
}

func (p *program) split(x int) int {
	p.insts = append(p.insts, inst{op: opSplit, x: x, y: x})
	return len(p.insts) - 1
}

func (p *program) emitAlt(sides []*node) {
	var jumps []int
	last := len(sides) - 1
	for i, side := range sides {
		choice := -1
		if i < last {
			choice = p.split(len(p.insts) + 1)
		}
		p.emit(side)
		if choice < 0 {
			continue
		}
		jumps = append(jumps, p.split(0))
		p.insts[choice].y = len(p.insts)
	}
	for _, j := range jumps {
		p.insts[j].x = len(p.insts)
		p.insts[j].y = len(p.insts)
	}
}

func (p *program) emitRepeat(body *node, minimum, maximum int) {
	// A body of size 0 is empty groups only and matches just the empty string, so repeating it emits
	// nothing. Its copies would otherwise cost instructions that the size does not count.
	if body.size == 0 {
		return
	}
	for i := 0; i < minimum; i++ {
		p.emit(body)
	}
	if maximum < 0 {
		top := len(p.insts)
		loop := p.split(top + 1)
		p.emit(body)
		p.split(top)
		p.insts[loop].y = len(p.insts)
		return
	}
	var optional []int
	for i := minimum; i < maximum; i++ {
		optional = append(optional, p.split(len(p.insts)+1))
		p.emit(body)
	}
	for _, c := range optional {
		p.insts[c].y = len(p.insts)
	}
}

func (p *program) emit(n *node) {
	switch n.kind {
	case nodeSet:
		p.insts = append(p.insts, inst{op: opChar, set: n.set})
	case nodeStart:
		p.insts = append(p.insts, inst{op: opStart})
	case nodeEnd:
		p.insts = append(p.insts, inst{op: opEnd})
	case nodeConcat:
		for _, part := range n.parts {
			p.emit(part)
		}
	case nodeAlt:
		p.emitAlt(n.parts)
	case nodeRepeat:
		p.emitRepeat(n.parts[0], n.min, n.max)
	}
}

func inItem(it item, c rune) bool {
	hit := false
	for i := 0; i+1 < len(it.ranges) && !hit; i += 2 {
		hit = it.ranges[i] <= c && c <= it.ranges[i+1]
	}
	return hit != it.negate
}

func inSet(s charSet, c rune) bool {
	any := false
	for _, it := range s.items {
		if inItem(it, c) {
			any = true
			break
		}
	}
	return any != s.negate
}

// matches reports whether the pattern matches somewhere in text. Each instruction is visited once per
// position.
func (p *program) matches(text string) bool {
	input := jsonv.CodePoints(text)
	length := len(input)
	seen := make([]int, len(p.insts))
	for i := range seen {
		seen[i] = -1
	}
	var stack []int
	current := make([]int, 0, len(p.insts))
	next := make([]int, 0, len(p.insts))

	// add puts pc and everything it reaches without reading on list; true when that reaches a match.
	add := func(list *[]int, pc, pos int) bool {
		stack = append(stack[:0], pc)
		for len(stack) > 0 {
			at := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if at >= len(p.insts) || seen[at] == pos {
				continue
			}
			seen[at] = pos
			in := &p.insts[at]
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
			in := &p.insts[pc]
			if in.op == opChar && inSet(in.set, c) && add(&next, pc+1, pos+1) {
				return true
			}
		}
		current, next = next, current
	}
}
