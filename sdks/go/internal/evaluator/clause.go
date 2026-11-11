package evaluator

import (
	"math"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

// matchOne reports whether one candidate matches one clause value under the clause's op.
func matchOne(op opKind, a jsonvalue.Value, v *operand) bool {
	switch op {
	case opIn:
		if a.Kind() != v.value.Kind() {
			return false
		}
		switch a.Kind() {
		case jsonvalue.KindString:
			x, _ := a.AsString()
			y, _ := v.value.AsString()
			return x == y
		case jsonvalue.KindNumber:
			x, _ := a.AsNumber()
			y, _ := v.value.AsNumber()
			return finite(x) && x == y
		case jsonvalue.KindBool:
			x, _ := a.AsBool()
			y, _ := v.value.AsBool()
			return x == y
		}
		return false
	case opContains, opStartsWith, opEndsWith:
		x, ok1 := a.AsString()
		y, ok2 := v.value.AsString()
		if !ok1 || !ok2 {
			return false
		}
		switch op {
		case opContains:
			return wtf8.Contains(x, y)
		case opStartsWith:
			return wtf8.HasPrefix(x, y)
		}
		return wtf8.HasSuffix(x, y)
	case opLt, opLte, opGt, opGte:
		return matchOrder(op, a, v.value)
	case opSemverEq, opSemverLt, opSemverLte, opSemverGt, opSemverGte:
		x, ok := a.AsString()
		if !ok || !v.hasVersion {
			return false
		}
		av, ok := parseSemver(x)
		if !ok {
			return false
		}
		c := compareSemver(av, v.version)
		switch op {
		case opSemverEq:
			return c == 0
		case opSemverLt:
			return c < 0
		case opSemverLte:
			return c <= 0
		case opSemverGt:
			return c > 0
		}
		return c >= 0
	case opBefore, opAfter:
		if !v.hasInstant {
			return false
		}
		t, ok := instantOf(a)
		if !ok {
			return false
		}
		if op == opBefore {
			return t < v.instant
		}
		return t > v.instant
	case opMatches:
		x, ok := a.AsString()
		if !ok || v.pattern == nil {
			return false
		}
		return v.pattern.Match(codePoints(x))
	}
	return false
}

func finite(n float64) bool { return !math.IsInf(n, 0) && !math.IsNaN(n) }

func matchOrder(op opKind, a, v jsonvalue.Value) bool {
	var c int
	switch {
	case a.Kind() == jsonvalue.KindNumber && v.Kind() == jsonvalue.KindNumber:
		x, _ := a.AsNumber()
		y, _ := v.AsNumber()
		if !finite(x) || !finite(y) {
			return false
		}
		switch {
		case x < y:
			c = -1
		case x > y:
			c = 1
		}
	case a.Kind() == jsonvalue.KindString && v.Kind() == jsonvalue.KindString:
		x, _ := a.AsString()
		y, _ := v.AsString()
		c = wtf8.Compare(x, y)
	default:
		return false
	}
	switch op {
	case opLt:
		return c < 0
	case opLte:
		return c <= 0
	case opGt:
		return c > 0
	}
	return c >= 0
}

// matchAttribute is the base match of an attribute clause: whether any candidate matches any value. ok is
// false when the attribute is missing or null, which fails the clause whatever negate says.
func (c *clause) matchAttribute(ctx *Context) (matched, present bool) {
	a, ok := ctx.read(c.kind, c.ref)
	if !ok || a.IsNull() {
		return false, false
	}
	if a.Kind() == jsonvalue.KindArray {
		for _, cand := range a.Elements() {
			if c.anyValue(cand) {
				return true, true
			}
		}
		return false, true
	}
	return c.anyValue(a), true
}

func (c *clause) anyValue(cand jsonvalue.Value) bool {
	switch cand.Kind() {
	case jsonvalue.KindString, jsonvalue.KindNumber, jsonvalue.KindBool:
	default:
		return false
	}
	for i := range c.operands {
		if matchOne(c.op, cand, &c.operands[i]) {
			return true
		}
	}
	return false
}

// matchesAttribute reports whether an attribute clause holds for the context.
func (c *clause) matchesAttribute(ctx *Context) bool {
	matched, present := c.matchAttribute(ctx)
	if !present {
		return false
	}
	return matched != c.negate
}

// matchesSegments reports whether a segmentMatch clause holds. Every segment it names was checked at load.
func (c *clause) matchesSegments(rs *Ruleset, ctx *Context) bool {
	matched := false
	for _, key := range c.segments {
		if seg := rs.segments[key]; seg != nil && seg.contains(ctx) {
			matched = true
			break
		}
	}
	return matched != c.negate
}

func (k keySets) has(ctx *Context) bool {
	for kind, keys := range k {
		e, ok := ctx.kinds[kind]
		if !ok {
			continue
		}
		if _, in := keys[e.key]; in {
			return true
		}
	}
	return false
}

// contains is membership in a segment: excluded, then included, then the rules.
func (s *segment) contains(ctx *Context) bool {
	if s.excluded.has(ctx) {
		return false
	}
	if s.included.has(ctx) {
		return true
	}
	for i := range s.rules {
		r := &s.rules[i]
		all := true
		for _, c := range r.clauses {
			if !c.matchesAttribute(ctx) {
				all = false
				break
			}
		}
		if !all {
			continue
		}
		if !r.hasWeight || r.weight > float64(ctx.kindBucket(s.key, s.salt, r.kind)) {
			return true
		}
	}
	return false
}
