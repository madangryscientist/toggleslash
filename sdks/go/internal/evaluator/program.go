package evaluator

// A pattern's program and the Pike VM that runs it over code points: no backtracking, so a match costs
// O(program x input) whatever the pattern.

type instOp uint8

const (
	opChar instOp = iota
	opSplit
	opStart
	opEnd
	opMatch
)

type inst struct {
	op   instOp
	set  *charSet
	x, y int
}

type program []inst

// compilePattern is the program for a pattern, or nil when the pattern is not valid.
func compilePattern(pattern string) program {
	root := parsePattern(codePoints(pattern))
	if root == nil {
		return nil
	}
	var prog program
	prog.emit(root)
	prog = append(prog, inst{op: opMatch})
	return prog
}

func (p *program) split(x int) int {
	*p = append(*p, inst{op: opSplit, x: x, y: x})
	return len(*p) - 1
}

func (p *program) emit(n *node) {
	switch n.kind {
	case nodeSet:
		*p = append(*p, inst{op: opChar, set: n.set})
	case nodeStart:
		*p = append(*p, inst{op: opStart})
	case nodeEnd:
		*p = append(*p, inst{op: opEnd})
	case nodeConcat:
		for _, part := range n.parts {
			p.emit(part)
		}
	case nodeAlt:
		p.emitAlt(n.parts)
	case nodeRepeat:
		p.emitRepeat(n.body, n.min, n.max)
	}
}

func (p *program) emitAlt(sides []*node) {
	var jumps []int
	last := len(sides) - 1
	for i, side := range sides {
		choice := -1
		if i < last {
			choice = p.split(len(*p) + 1)
		}
		p.emit(side)
		if choice < 0 {
			continue
		}
		jumps = append(jumps, p.split(0))
		(*p)[choice].y = len(*p)
	}
	for _, j := range jumps {
		(*p)[j].x = len(*p)
		(*p)[j].y = len(*p)
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
		top := len(*p)
		loop := p.split(top + 1)
		p.emit(body)
		p.split(top)
		(*p)[loop].y = len(*p)
		return
	}
	var optional []int
	for i := minimum; i < maximum; i++ {
		optional = append(optional, p.split(len(*p)+1))
		p.emit(body)
	}
	for _, c := range optional {
		(*p)[c].y = len(*p)
	}
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

// matches reports whether the program matches somewhere in text, read as code points.
func (prog program) matches(text string) bool {
	input := codePoints(text)
	seen := make([]int, len(prog))
	for i := range seen {
		seen[i] = -1
	}
	var stack []int
	var current, next []int

	// add puts pc and everything it reaches without reading on the list; it reports whether that reaches
	// a match.
	add := func(list *[]int, pc, pos int) bool {
		stack = append(stack[:0], pc)
		for len(stack) > 0 {
			at := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[at] == pos {
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
				if pos == len(input) {
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
		if pos >= len(input) {
			return false
		}
		c := input[pos]
		next = next[:0]
		for _, pc := range current {
			if inSet(prog[pc].set, c) && add(&next, pc+1, pos+1) {
				return true
			}
		}
		current, next = next, current
	}
}
