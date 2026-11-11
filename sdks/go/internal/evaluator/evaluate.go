package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"

// maxPrerequisiteDepth is the prerequisite links from the requested flag; docs/evaluation.md pins it.
const maxPrerequisiteDepth = 32

// run is the state of one Evaluate call.
type run struct {
	rs        *Ruleset
	ctx       contexts
	active    map[string]struct{}
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

func serveResult(e *flagEntry, s serve, ctx contexts, reason Reason) Result {
	index := s.variation
	if s.rollout != nil {
		index = rolloutVariation(s.rollout, kindBucket(e.key, e.salt, s.rollout.kind, ctx))
	}
	return served(e.variations, index, reason)
}

func (rs *Ruleset) segmentRuleMatches(r *segmentRule, seg *segmentEntry, ctx contexts) bool {
	for i := range r.clauses {
		if !r.clauses[i].attributeMatches(ctx) {
			return false
		}
	}
	if r.weight < 0 {
		return true
	}
	return kindBucket(seg.key, seg.salt, r.rolloutKind, ctx) < r.weight
}

// listsContext reports whether the context has, in any of its kinds, a key that keys lists for that kind.
func listsContext(keys keysByKind, ctx contexts) bool {
	for _, kc := range ctx {
		if _, ok := keys[kc.kind][kc.key]; ok {
			return true
		}
	}
	return false
}

func (rs *Ruleset) segmentMatches(seg *segmentEntry, ctx contexts) bool {
	if listsContext(seg.excluded, ctx) {
		return false
	}
	if listsContext(seg.included, ctx) {
		return true
	}
	for i := range seg.rules {
		if rs.segmentRuleMatches(&seg.rules[i], seg, ctx) {
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
		if seg, ok := r.rs.segments[key]; ok && seg.ok && r.rs.segmentMatches(seg, r.ctx) {
			matched = true
			break
		}
	}
	return matched != c.negate
}

func prerequisiteMet(p prerequisite, result Result) bool {
	switch result.Reason.Kind {
	case ReasonTargetMatch, ReasonRuleMatch, ReasonFallthrough:
		return result.Index >= 0 && float64(result.Index) == p.variation
	}
	return false
}

// checkPrerequisites returns the result that blocks the flag, if any.
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

// targetedVariation is the variation of the first target in array order that matches the context.
func targetedVariation(t targetIndex, ctx contexts) (int, bool) {
	found, order, variation := false, 0, 0
	for _, kc := range ctx {
		if hit, ok := t[kc.kind][kc.key]; ok && (!found || hit.order < order) {
			found, order, variation = true, hit.order, hit.variation
		}
	}
	return variation, found
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
	if blocked, ok := r.checkPrerequisites(e, depth); ok {
		return blocked
	}
	if v, ok := targetedVariation(e.targets, r.ctx); ok {
		return served(e.variations, v, Reason{Kind: ReasonTargetMatch})
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
			return serveResult(e, rl.serve, r.ctx, Reason{Kind: ReasonRuleMatch, RuleIndex: i})
		}
	}
	return serveResult(e, e.fall, r.ctx, Reason{Kind: ReasonFallthrough})
}

func (r *run) evaluateFlag(key string, depth int) Result {
	if done, ok := r.completed[key]; ok {
		return done
	}
	if _, active := r.active[key]; active {
		return failure(ErrPrerequisiteCycle)
	}
	if depth > maxPrerequisiteDepth {
		return failure(ErrPrerequisiteDepthExceeded)
	}
	e, ok := r.rs.flags[key]
	if !ok {
		return failure(ErrFlagNotFound)
	}
	r.active[key] = struct{}{}
	result := r.evaluateEntry(e, depth)
	delete(r.active, key)
	r.completed[key] = result
	return result
}

// Evaluate evaluates one flag for one context (a JSON object). It never panics at the caller: every
// failure, including a prerequisite cycle, is an ERROR result. The order and the comparison rules are in
// docs/evaluation.md.
func (rs *Ruleset) Evaluate(flagKey string, context jsonv.Value) Result {
	ctx, ok := normalise(context)
	if !ok {
		return failure(ErrInvalidContext)
	}
	r := &run{rs: rs, ctx: ctx, active: map[string]struct{}{}, completed: map[string]Result{}}
	return r.evaluateFlag(flagKey, 0)
}
