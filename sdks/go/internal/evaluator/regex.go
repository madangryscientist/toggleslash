package evaluator

// Patterns for the matches op: pattern.go reads a pattern into a tree; this compiles the tree to a small
// program and runs it on a Pike VM over code points. There is no backtracking and no platform regular
// expression, so a match costs O(program x input) whatever the pattern.

type opcode int

const (
	opChar opcode = iota
	opSplit
	opStart
	opEnd
	opMatch
)

type inst struct {
	op   opcode
	set  charSet // opChar
	x, y int     // opSplit; a jump is a split whose two targets are the same
}

// program is a compiled pattern.
type program []inst

func (pg *program) split(x int) int {
	*pg = append(*pg, inst{op: opSplit, x: x, y: x})
	return len(*pg) - 1
}

func (pg *program) emitAlt(sides []*node) {
	var jumps []int
	last := len(sides) - 1
	for i, side := range sides {
		choice := -1
		if i < last {
			choice = pg.split(len(*pg) + 1)
		}
		pg.emit(side)
		if choice < 0 {
			continue
		}
		jumps = append(jumps, pg.split(0))
		(*pg)[choice].y = len(*pg)
	}
	for _, j := range jumps {
		(*pg)[j].x = len(*pg)
		(*pg)[j].y = len(*pg)
	}
}

func (pg *program) emitRepeat(body *node, min, max int) {
	// A body of size 0 is empty groups only and matches just the empty string, so repeating it emits nothing.
	if body.size == 0 {
		return
	}
	for range min {
		pg.emit(body)
	}
	if max < 0 {
		top := len(*pg)
		loop := pg.split(top + 1)
		pg.emit(body)
		pg.split(top)
		(*pg)[loop].y = len(*pg)
		return
	}
	var optional []int
	for i := min; i < max; i++ {
		optional = append(optional, pg.split(len(*pg)+1))
		pg.emit(body)
	}
	for _, c := range optional {
		(*pg)[c].y = len(*pg)
	}
}

func (pg *program) emit(n *node) {
	switch n.kind {
	case nodeSet:
		*pg = append(*pg, inst{op: opChar, set: n.set})
	case nodeStart:
		*pg = append(*pg, inst{op: opStart})
	case nodeEnd:
		*pg = append(*pg, inst{op: opEnd})
	case nodeConcat:
		for _, part := range n.parts {
			pg.emit(part)
		}
	case nodeAlt:
		pg.emitAlt(n.parts)
	case nodeRepeat:
		pg.emitRepeat(n.body, n.min, n.max)
	}
}

// compilePattern is the compiled pattern, or nil when the text is not a valid pattern.
func compilePattern(pattern string) *program {
	n := parsePattern(codePoints(pattern))
	if n == nil {
		return nil
	}
	var pg program
	pg.emit(n)
	pg = append(pg, inst{op: opMatch})
	return &pg
}

func inItem(it item, c rune) bool {
	hit := false
	for i := 0; i+1 < len(it.ranges) && !hit; i += 2 {
		hit = it.ranges[i] <= c && c <= it.ranges[i+1]
	}
	return hit != it.negate
}

func inSet(s charSet, c rune) bool {
	hit := false
	for _, it := range s.items {
		if inItem(it, c) {
			hit = true
			break
		}
	}
	return hit != s.negate
}

// matches reports whether the pattern matches somewhere in text, read as code points. A Pike VM: each
// instruction is visited once per position.
func (pg *program) matches(text string) bool {
	input := codePoints(text)
	length := len(input)
	prog := *pg
	seen := make([]int, len(prog))
	for i := range seen {
		seen[i] = -1
	}
	var stack []int
	current := make([]int, 0, len(prog))
	next := make([]int, 0, len(prog))

	// add puts pc and everything it reaches without reading into list; true when that reaches a match.
	add := func(list *[]int, pc, pos int) bool {
		stack = append(stack, pc)
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
				stack = stack[:0]
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
			if prog[pc].op == opChar && inSet(prog[pc].set, c) && add(&next, pc+1, pos+1) {
				return true
			}
		}
		current, next = next, current
	}
}
