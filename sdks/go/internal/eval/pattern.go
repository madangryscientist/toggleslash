package eval

import "github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"

// Patterns for the `matches` op: the tree from pattern_parse.go compiles to a small program that runs on
// a Pike VM over code points. There is no backtracking and no regexp package, so a match costs
// O(program x input) whatever the pattern.

type instOp uint8

const (
	instChar instOp = iota
	instSplit
	instStart
	instEnd
	instMatch
)

type inst struct {
	op   instOp
	set  *charSet
	x, y int
}

// program is a compiled pattern.
type program []inst

func (pr *program) splitAt(x int) int {
	*pr = append(*pr, inst{op: instSplit, x: x, y: x})
	return len(*pr) - 1
}

func (pr *program) emitAlt(sides []*node) {
	var jumps []int
	last := len(sides) - 1
	for i, side := range sides {
		choice := -1
		if i < last {
			choice = pr.splitAt(len(*pr) + 1)
		}
		pr.emit(side)
		if choice < 0 {
			continue
		}
		jumps = append(jumps, pr.splitAt(0))
		(*pr)[choice].y = len(*pr)
	}
	for _, j := range jumps {
		(*pr)[j].x = len(*pr)
		(*pr)[j].y = len(*pr)
	}
}

func (pr *program) emitRepeat(body *node, min, max int) {
	// A body of size 0 is empty groups only and matches just the empty string, so repeating it emits
	// nothing. Its copies would otherwise cost instructions that the size does not count.
	if body.size == 0 {
		return
	}
	for i := 0; i < min; i++ {
		pr.emit(body)
	}
	if max == -1 {
		top := len(*pr)
		loop := pr.splitAt(top + 1)
		pr.emit(body)
		pr.splitAt(top)
		(*pr)[loop].y = len(*pr)
		return
	}
	var optional []int
	for i := min; i < max; i++ {
		optional = append(optional, pr.splitAt(len(*pr)+1))
		pr.emit(body)
	}
	for _, c := range optional {
		(*pr)[c].y = len(*pr)
	}
}

func (pr *program) emit(n *node) {
	switch n.kind {
	case nodeSet:
		*pr = append(*pr, inst{op: instChar, set: n.set})
	case nodeStart:
		*pr = append(*pr, inst{op: instStart})
	case nodeEnd:
		*pr = append(*pr, inst{op: instEnd})
	case nodeConcat:
		for _, part := range n.parts {
			pr.emit(part)
		}
	case nodeAlt:
		pr.emitAlt(n.parts)
	case nodeRepeat:
		pr.emitRepeat(n.body, n.min, n.max)
	}
}

// compilePattern returns the compiled pattern, or nil when pattern is not a valid pattern.
func compilePattern(pattern string) *program {
	tree := parsePattern(pattern)
	if tree == nil {
		return nil
	}
	pr := program{}
	pr.emit(tree)
	pr = append(pr, inst{op: instMatch})
	return &pr
}

func inItem(it item, c rune) bool {
	hit := false
	for i := 0; i+1 < len(it.ranges) && !hit; i += 2 {
		hit = it.ranges[i] <= c && c <= it.ranges[i+1]
	}
	return hit != it.negate
}

func inSet(set *charSet, c rune) bool {
	for _, it := range set.items {
		if inItem(it, c) {
			return !set.negate
		}
	}
	return set.negate
}

// matches reports whether the pattern matches somewhere in text. Each instruction is visited once per
// position.
func (pr *program) matches(text string) bool {
	prog := *pr
	input := wtf8.CodePoints(text)
	length := len(input)
	seen := make([]int, len(prog))
	for i := range seen {
		seen[i] = -1
	}
	var stack []int
	current := []int{}
	next := []int{}

	// add puts pc and everything it reaches without reading on list; true when that reaches a match.
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
			case instMatch:
				stack = stack[:0]
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
			if in.op == instChar && inSet(in.set, c) && add(&next, pc+1, pos+1) {
				return true
			}
		}
		current, next = next, current
	}
}
