package eval

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"

// maxPrerequisiteDepth is the number of prerequisite links from the requested flag that docs/evaluation.md allows.
const maxPrerequisiteDepth = 32

type run struct {
	rs        *Ruleset
	ctx       contexts
	active    map[string]struct{}
	completed map[string]Result
}

func served(variations []jsonv.Value, index int, reason Reason) Result {
	return Result{Value: variations[index], VariationIndex: index, Reason: reason}
}

func offResult(h header, reason Reason) Result {
	if h.off < 0 {
		return Result{VariationIndex: -1, Reason: reason}
	}
	return served(h.variations, h.off, reason)
}

func rolloutVariation(r *rollout, position int) int {
	chosen, cumulative := 0, 0
	for _, w := range r.variations {
		chosen = w.variation
		cumulative += w.weight
		if position < cumulative {
			break
		}
	}
	return chosen
}

func serveResult(f *flag, s serve, c contexts, reason Reason) Result {
	index := s.variation
	if s.rollout != nil {
		index = rolloutVariation(s.rollout, kindBucket(f.header.key, f.salt, s.rollout.kind, c))
	}
	return served(f.header.variations, index, reason)
}

func listsContext(keys map[string]map[string]struct{}, c contexts) bool {
	for kind, entry := range c {
		if set, ok := keys[kind]; ok {
			if _, in := set[entry.key]; in {
				return true
			}
		}
	}
	return false
}

func segmentRuleMatches(r *segmentRule, s *segment, c contexts) bool {
	for _, cl := range r.clauses {
		if !cl.attributeMatches(c) {
			return false
		}
	}
	if r.weight < 0 {
		return true
	}
	return kindBucket(s.key, s.salt, r.kind, c) < r.weight
}

func segmentMatches(s *segment, c contexts) bool {
	if listsContext(s.excluded, c) {
		return false
	}
	if listsContext(s.included, c) {
		return true
	}
	for i := range s.rules {
		if segmentRuleMatches(&s.rules[i], s, c) {
			return true
		}
	}
	return false
}

func (r *run) clauseMatches(c *clause) bool {
	if c.op != opSegmentMatch {
		return c.attributeMatches(r.ctx)
	}
	matched := false
	for _, key := range c.segments {
		if s, ok := r.rs.segments[key]; ok && !s.malformed && segmentMatches(s.segment, r.ctx) {
			matched = true
			break
		}
	}
	return matched != c.negate
}

func prerequisiteMet(p prerequisite, result Result) bool {
	switch result.Reason.Kind {
	case ReasonTargetMatch, ReasonRuleMatch, ReasonFallthrough:
		return result.VariationIndex >= 0 && float64(result.VariationIndex) == p.variation
	}
	return false
}

func (r *run) checkPrerequisites(f *flag, depth int) (Result, bool) {
	for _, p := range f.prerequisites {
		result := r.evaluateFlag(p.flagKey, depth+1)
		if result.Reason.Kind == ReasonError {
			return result, true
		}
		if !prerequisiteMet(p, result) {
			return offResult(f.header, Reason{Kind: ReasonPrerequisiteFailed, FlagKey: p.flagKey}), true
		}
	}
	return Result{}, false
}

func (r *run) targeted(e *flagEntry) (int, bool) {
	best, variation := -1, 0
	for kind, entry := range r.ctx {
		if hit, ok := e.targets[kind][entry.key]; ok && (best < 0 || hit.order < best) {
			best, variation = hit.order, hit.variation
		}
	}
	return variation, best >= 0
}

func (r *run) evaluateEntry(e *flagEntry, depth int) Result {
	if e.status == statusMalformed {
		return failure(CodeMalformedFlag)
	}
	if !e.header.on {
		return offResult(e.header, Reason{Kind: ReasonOff})
	}
	if e.status == statusBroken {
		return failure(e.code)
	}
	f := e.flag
	if blocked, ok := r.checkPrerequisites(f, depth); ok {
		return blocked
	}
	if v, ok := r.targeted(e); ok {
		return served(f.header.variations, v, Reason{Kind: ReasonTargetMatch})
	}
	for i := range f.rules {
		rl := &f.rules[i]
		all := true
		for _, c := range rl.clauses {
			if !r.clauseMatches(c) {
				all = false
				break
			}
		}
		if all {
			return serveResult(f, rl.serve, r.ctx, Reason{Kind: ReasonRuleMatch, RuleIndex: i})
		}
	}
	return serveResult(f, f.fallthrough_, r.ctx, Reason{Kind: ReasonFallthrough})
}

func (r *run) evaluateFlag(key string, depth int) Result {
	if done, ok := r.completed[key]; ok {
		return done
	}
	if _, ok := r.active[key]; ok {
		return failure(CodePrerequisiteCycle)
	}
	if depth > maxPrerequisiteDepth {
		return failure(CodePrerequisiteDepthExceeded)
	}
	e, ok := r.rs.flags[key]
	if !ok {
		return failure(CodeFlagNotFound)
	}
	r.active[key] = struct{}{}
	result := r.evaluateEntry(e, depth)
	delete(r.active, key)
	r.completed[key] = result
	return result
}

// Evaluate evaluates one flag for one context. It never panics: every failure, including a prerequisite
// cycle, is an ERROR result. The order and the comparison rules are in docs/evaluation.md.
func (rs *Ruleset) Evaluate(flagKey string, context jsonv.Value) Result {
	c := normaliseContext(context)
	if c == nil {
		return failure(CodeInvalidContext)
	}
	r := &run{rs: rs, ctx: c, active: map[string]struct{}{}, completed: map[string]Result{}}
	return r.evaluateFlag(flagKey, 0)
}
