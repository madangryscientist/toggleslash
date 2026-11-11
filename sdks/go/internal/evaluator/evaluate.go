package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"

// maxPrerequisiteDepth is the limit on prerequisite links from the requested flag, to
// docs/evaluation.md.
const maxPrerequisiteDepth = 32

type run struct {
	rs        *Ruleset
	cs        contexts
	active    map[string]bool
	completed map[string]Result
}

func served(variations []jsonv.Value, index int, reason Reason) Result {
	return Result{Value: variations[index], VariationIndex: index, Reason: reason}
}

func offResult(f *flag, reason Reason) Result {
	if f.offVariation < 0 {
		return Result{VariationIndex: NoVariation, Reason: reason}
	}
	return served(f.variations, f.offVariation, reason)
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

func serveResult(f *flag, s serve, cs contexts, reason Reason) Result {
	index := s.variation
	if s.rollout != nil {
		index = rolloutVariation(s.rollout, kindBucket(f.key, f.salt, s.rollout.kind, cs))
	}
	return served(f.variations, index, reason)
}

func listsContext(keys keySets, cs contexts) bool {
	for _, c := range cs {
		if _, ok := keys[c.kind][c.key]; ok {
			return true
		}
	}
	return false
}

func segmentRuleMatches(r *segmentRule, s *segment, cs contexts) bool {
	for i := range r.clauses {
		if !attributeClauseMatches(&r.clauses[i], cs) {
			return false
		}
	}
	if r.weight < 0 {
		return true
	}
	return kindBucket(s.key, s.salt, r.kind, cs) < r.weight
}

func segmentMatches(s *segment, cs contexts) bool {
	if listsContext(s.excluded, cs) {
		return false
	}
	if listsContext(s.included, cs) {
		return true
	}
	for i := range s.rules {
		if segmentRuleMatches(&s.rules[i], s, cs) {
			return true
		}
	}
	return false
}

func (r *run) clauseMatches(c *clause) bool {
	if c.op != opSegmentMatch {
		return attributeClauseMatches(c, r.cs)
	}
	matched := false
	for _, key := range c.segments {
		if e, ok := r.rs.segments[key]; ok && e.segment != nil && segmentMatches(e.segment, r.cs) {
			matched = true
			break
		}
	}
	return matched != c.negate
}

func targetedVariation(t targetIndex, cs contexts) (int, bool) {
	best, found := targetHit{}, false
	for _, c := range cs {
		if hit, ok := t[c.kind][c.key]; ok && (!found || hit.order < best.order) {
			best, found = hit, true
		}
	}
	return best.variation, found
}

func prerequisiteMet(p prerequisite, res Result) bool {
	switch res.Reason.Kind {
	case ReasonTargetMatch, ReasonRuleMatch, ReasonFallthrough:
		return res.VariationIndex == p.variation
	}
	return false
}

func (r *run) checkPrerequisites(f *flag, depth int) (Result, bool) {
	for _, p := range f.prereqs {
		res := r.evaluateFlag(p.flagKey, depth+1)
		if res.Reason.Kind == ReasonError {
			return res, true
		}
		if !prerequisiteMet(p, res) {
			return offResult(f, Reason{Kind: ReasonPrerequisiteFailed, FlagKey: p.flagKey}), true
		}
	}
	return Result{}, false
}

func (r *run) evaluateEntry(e *flagEntry, depth int) Result {
	if e.status == statusMalformed {
		return failure(MalformedFlag)
	}
	f := e.flag
	if !f.on {
		return offResult(f, Reason{Kind: ReasonOff})
	}
	if e.status == statusBroken {
		return failure(e.code)
	}
	if blocked, ok := r.checkPrerequisites(f, depth); ok {
		return blocked
	}
	if v, ok := targetedVariation(f.targets, r.cs); ok {
		return served(f.variations, v, Reason{Kind: ReasonTargetMatch})
	}
	for i := range f.rules {
		matched := true
		for j := range f.rules[i].clauses {
			if !r.clauseMatches(&f.rules[i].clauses[j]) {
				matched = false
				break
			}
		}
		if matched {
			return serveResult(f, f.rules[i].serve, r.cs, Reason{Kind: ReasonRuleMatch, RuleIndex: i})
		}
	}
	return serveResult(f, f.fallthrough_, r.cs, Reason{Kind: ReasonFallthrough})
}

func (r *run) evaluateFlag(key string, depth int) Result {
	if done, ok := r.completed[key]; ok {
		return done
	}
	if r.active[key] {
		return failure(PrerequisiteCycle)
	}
	if depth > maxPrerequisiteDepth {
		return failure(PrerequisiteDepthExceeded)
	}
	e, ok := r.rs.flags[key]
	if !ok {
		return failure(FlagNotFound)
	}
	r.active[key] = true
	res := r.evaluateEntry(e, depth)
	delete(r.active, key)
	r.completed[key] = res
	return res
}

// Evaluate evaluates one flag for one context (a JSON value). It never panics on a ruleset or a
// context: every failure, a prerequisite cycle included, is an ERROR result. The order and the
// comparison rules are in docs/evaluation.md.
func Evaluate(rs *Ruleset, flagKey string, context jsonv.Value) Result {
	cs, ok := normalise(context)
	if !ok {
		return failure(InvalidContext)
	}
	r := &run{rs: rs, cs: cs, active: map[string]bool{}, completed: map[string]Result{}}
	return r.evaluateFlag(flagKey, 0)
}
