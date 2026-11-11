package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"

// run is one evaluation: the context and the flags already visited.
type run struct {
	ruleset   *Ruleset
	contexts  contexts
	active    map[string]bool
	completed map[string]Result
}

func served(variations []jsonv.Value, index int, reason Reason) Result {
	return Result{Value: variations[index], Index: index, Reason: reason}
}

func offResult(e *flagEntry, reason Reason) Result {
	if e.offVariation < 0 {
		return Result{Index: -1, Reason: reason}
	}
	return served(e.variations, e.offVariation, reason)
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

func serveResult(e *flagEntry, s serve, ctxs contexts, reason Reason) Result {
	index := s.variation
	if s.rollout != nil {
		index = rolloutVariation(s.rollout, kindBucket(e.key, e.salt, s.rollout.kind, ctxs))
	}
	return served(e.variations, index, reason)
}

func (k keysByKind) lists(ctxs contexts) bool {
	for _, c := range ctxs {
		if _, ok := k[c.kind][c.key]; ok {
			return true
		}
	}
	return false
}

func (s *segment) ruleMatches(r *segmentRule, ctxs contexts) bool {
	for i := range r.clauses {
		if !r.clauses[i].attributeMatches(ctxs) {
			return false
		}
	}
	if !r.hasWeight {
		return true
	}
	return kindBucket(s.key, s.salt, r.rolloutKind, ctxs) < r.weight
}

func (s *segment) matches(ctxs contexts) bool {
	if s.excluded.lists(ctxs) {
		return false
	}
	if s.included.lists(ctxs) {
		return true
	}
	for i := range s.rules {
		if s.ruleMatches(&s.rules[i], ctxs) {
			return true
		}
	}
	return false
}

func (r *run) clauseMatches(c *clause) bool {
	if c.op != opSegmentMatch {
		return c.attributeMatches(r.contexts)
	}
	matched := false
	for _, key := range c.segments {
		if s := r.ruleset.segments[key]; s != nil && s.matches(r.contexts) {
			matched = true
			break
		}
	}
	return matched != c.negate
}

func prerequisiteMet(p prerequisite, result Result) bool {
	switch result.Reason.Kind {
	case ReasonTargetMatch, ReasonRuleMatch, ReasonFallthrough:
		return float64(result.Index) == p.variation
	}
	return false
}

// checkPrerequisites is the result that ends the walk, or false when every prerequisite is met.
func (r *run) checkPrerequisites(e *flagEntry, depth int) (Result, bool) {
	for _, p := range e.prerequisites {
		result := r.evaluateFlag(p.flagKey, depth+1)
		if result.Reason.Kind == ReasonError {
			return result, true
		}
		if !prerequisiteMet(p, result) {
			return offResult(e, Reason{Kind: ReasonPrerequisiteFailed, FlagKey: p.flagKey}), true
		}
	}
	return Result{}, false
}

func (e *flagEntry) targeted(ctxs contexts) (int, bool) {
	first, found := targetHit{}, false
	for _, c := range ctxs {
		if hit, ok := e.targets[c.kind][c.key]; ok && (!found || hit.order < first.order) {
			first, found = hit, true
		}
	}
	return first.variation, found
}

func (r *run) evaluateEntry(e *flagEntry, depth int) Result {
	if e.status == statusMalformed {
		return failure(ErrMalformedFlag)
	}
	if !e.on {
		return offResult(e, Reason{Kind: ReasonOff})
	}
	if e.status == statusBroken {
		return failure(e.code)
	}
	if result, blocked := r.checkPrerequisites(e, depth); blocked {
		return result
	}
	if index, ok := e.targeted(r.contexts); ok {
		return served(e.variations, index, Reason{Kind: ReasonTargetMatch})
	}
	for i := range e.rules {
		rl := &e.rules[i]
		all := true
		for j := range rl.clauses {
			if !r.clauseMatches(&rl.clauses[j]) {
				all = false
				break
			}
		}
		if all {
			return serveResult(e, rl.serve, r.contexts, Reason{Kind: ReasonRuleMatch, RuleIndex: i})
		}
	}
	return serveResult(e, e.fallback, r.contexts, Reason{Kind: ReasonFallthrough})
}

func (r *run) evaluateFlag(key string, depth int) Result {
	if result, ok := r.completed[key]; ok {
		return result
	}
	if r.active[key] {
		return failure(ErrPrerequisiteCycle)
	}
	if depth > maxPrerequisiteDepth {
		return failure(ErrPrerequisiteDepthExceeds)
	}
	e, ok := r.ruleset.flags[key]
	if !ok {
		return failure(ErrFlagNotFound)
	}
	r.active[key] = true
	result := r.evaluateEntry(e, depth)
	delete(r.active, key)
	r.completed[key] = result
	return result
}

// Evaluate evaluates one flag for one context, to docs/evaluation.md. The context is the JSON the caller
// holds. It does not panic: every failure, including a prerequisite cycle, is an ERROR result.
func (rs *Ruleset) Evaluate(flagKey string, context jsonv.Value) Result {
	ctxs, ok := normaliseContext(context)
	if !ok {
		return failure(ErrInvalidContext)
	}
	r := &run{ruleset: rs, contexts: ctxs, active: map[string]bool{}, completed: map[string]Result{}}
	return r.evaluateFlag(flagKey, 0)
}
