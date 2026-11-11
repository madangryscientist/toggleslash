package eval

import "github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"

// Patterns for the `matches` op, compiled to a small program and run on a Pike VM over code points: no
// backtracking and no platform regular expression engine, so a match costs O(program x input).

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
	x, y int // a jump is a split whose two targets are the same
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
		(*prog)[j].x, (*prog)[j].y = len(*prog), len(*prog)
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
		set := n.set
		*prog = append(*prog, inst{op: opChar, set: &set})
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

// compilePattern is the compiled pattern, or nil when text is not a valid pattern.
func compilePattern(text string) pattern {
	root := parsePattern(jsonvalue.CodePoints(text))
	if root == nil {
		return nil
	}
	var prog []inst
	emit(root, &prog)
	prog = append(prog, inst{op: opMatch})
	return prog
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

type vm struct {
	prog   pattern
	length int
	seen   []int32
	stack  []int
}

// add puts pc and everything it reaches without reading on list, and reports whether that reaches a match.
func (m *vm) add(list *[]int, pc, pos int) bool {
	m.stack = append(m.stack[:0], pc)
	for len(m.stack) > 0 {
		at := m.stack[len(m.stack)-1]
		m.stack = m.stack[:len(m.stack)-1]
		if at >= len(m.prog) || m.seen[at] == int32(pos) {
			continue
		}
		m.seen[at] = int32(pos)
		in := &m.prog[at]
		switch in.op {
		case opMatch:
			return true
		case opChar:
			*list = append(*list, at)
		case opSplit:
			m.stack = append(m.stack, in.y, in.x)
		case opStart:
			if pos == 0 {
				m.stack = append(m.stack, at+1)
			}
		case opEnd:
			if pos == m.length {
				m.stack = append(m.stack, at+1)
			}
		}
	}
	return false
}

// matches reports whether the pattern matches somewhere in text. Each instruction is visited once per position.
func (p pattern) matches(text string) bool {
	input := jsonvalue.CodePoints(text)
	m := &vm{prog: p, length: len(input), seen: make([]int32, len(p))}
	for i := range m.seen {
		m.seen[i] = -1
	}
	var current, next []int
	for pos := 0; ; pos++ {
		if m.add(&current, 0, pos) {
			return true
		}
		if pos >= len(input) {
			return false
		}
		c := input[pos]
		next = next[:0]
		for _, pc := range current {
			in := &p[pc]
			if in.op == opChar && inSet(in.set, c) && m.add(&next, pc+1, pos+1) {
				return true
			}
		}
		current, next = next, current
	}
}
