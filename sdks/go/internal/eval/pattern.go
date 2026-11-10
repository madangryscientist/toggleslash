package eval

// Patterns for the "matches" op, to the "Patterns" section of docs/evaluation.md. pattern_parse.go reads a
// pattern into a tree; this compiles the tree to a small program and runs it on a Pike VM over code points:
// no backtracking and no regexp package, so a match costs O(program x input) whatever the pattern.

type instOp uint8

const (
	opChar instOp = iota
	opSplit
	opStartAnchor
	opEndAnchor
	opMatch
)

// inst is one instruction. A jump is a split whose two targets are the same.
type inst struct {
	op   instOp
	set  *charSet
	x, y int
}

// pattern is a compiled pattern.
type pattern []inst

func addSplit(program []inst, x int) []inst { return append(program, inst{op: opSplit, x: x, y: x}) }

func emitAlt(sides []*node, program []inst) []inst {
	var jumps []int
	last := len(sides) - 1
	for i, side := range sides {
		choice := -1
		if i < last {
			choice = len(program)
			program = addSplit(program, len(program)+1)
		}
		program = emit(side, program)
		if choice < 0 {
			continue
		}
		jumps = append(jumps, len(program))
		program = addSplit(program, 0)
		program[choice].y = len(program)
	}
	for _, j := range jumps {
		program[j].x, program[j].y = len(program), len(program)
	}
	return program
}

func emitRepeat(body *node, minimum, maximum int, program []inst) []inst {
	// A body of size 0 is empty groups only and matches just the empty string, so repeating it emits
	// nothing. Its copies would otherwise cost instructions that the size does not count.
	if body.size == 0 {
		return program
	}
	for i := 0; i < minimum; i++ {
		program = emit(body, program)
	}
	if maximum < 0 {
		top := len(program)
		program = addSplit(program, top+1)
		program = emit(body, program)
		program = addSplit(program, top)
		program[top].y = len(program)
		return program
	}
	var optional []int
	for i := minimum; i < maximum; i++ {
		optional = append(optional, len(program))
		program = addSplit(program, len(program)+1)
		program = emit(body, program)
	}
	for _, choice := range optional {
		program[choice].y = len(program)
	}
	return program
}

func emit(n *node, program []inst) []inst {
	switch n.kind {
	case nodeSet:
		return append(program, inst{op: opChar, set: n.set})
	case nodeStart:
		return append(program, inst{op: opStartAnchor})
	case nodeEnd:
		return append(program, inst{op: opEndAnchor})
	case nodeConcat:
		for _, part := range n.parts {
			program = emit(part, program)
		}
		return program
	case nodeAlt:
		return emitAlt(n.parts, program)
	default:
		return emitRepeat(n.body, n.min, n.max, program)
	}
}

// compilePattern is the compiled pattern, or false when text is not a valid pattern.
func compilePattern(text string) (pattern, bool) {
	root, ok := parsePattern(codePoints(text))
	if !ok {
		return nil, false
	}
	program := emit(root, nil)
	return append(program, inst{op: opMatch}), true
}

func inItem(i item, c rune) bool {
	hit := false
	for k := 0; k+1 < len(i.ranges) && !hit; k += 2 {
		hit = i.ranges[k] <= c && c <= i.ranges[k+1]
	}
	return hit != i.negate
}

func inSet(set *charSet, c rune) bool {
	hit := false
	for _, i := range set.items {
		if inItem(i, c) {
			hit = true
			break
		}
	}
	return hit != set.negate
}

// matcher is the state of one run of a program over one input.
type matcher struct {
	program pattern
	length  int
	seen    []int32
	stack   []int
}

// add puts pc and everything it reaches without reading onto list; it reports whether that reaches a match.
func (m *matcher) add(list []int, pc, pos int) ([]int, bool) {
	m.stack = append(m.stack[:0], pc)
	for len(m.stack) > 0 {
		at := m.stack[len(m.stack)-1]
		m.stack = m.stack[:len(m.stack)-1]
		if m.seen[at] == int32(pos) {
			continue
		}
		m.seen[at] = int32(pos)
		in := &m.program[at]
		switch in.op {
		case opMatch:
			return list, true
		case opChar:
			list = append(list, at)
		case opSplit:
			m.stack = append(m.stack, in.y, in.x)
		case opStartAnchor:
			if pos == 0 {
				m.stack = append(m.stack, at+1)
			}
		case opEndAnchor:
			if pos == m.length {
				m.stack = append(m.stack, at+1)
			}
		}
	}
	return list, false
}

// patternMatches reports whether the pattern matches somewhere in text. A Pike VM: each instruction is
// visited once per position.
func patternMatches(program pattern, text string) bool {
	input := codePoints(text)
	m := &matcher{program: program, length: len(input), seen: make([]int32, len(program))}
	for i := range m.seen {
		m.seen[i] = -1
	}
	var current, next []int
	for pos := 0; ; pos++ {
		var matched bool
		if current, matched = m.add(current, 0, pos); matched {
			return true
		}
		if pos >= len(input) {
			return false
		}
		c := input[pos]
		next = next[:0]
		for _, pc := range current {
			in := &m.program[pc]
			if in.op != opChar || !inSet(in.set, c) {
				continue
			}
			if next, matched = m.add(next, pc+1, pos+1); matched {
				return true
			}
		}
		current, next = next, current
	}
}
