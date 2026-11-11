package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"

// maxPrerequisiteDepth is the prerequisite links from the requested flag that docs/evaluation.md allows.
const maxPrerequisiteDepth = 32

type run struct {
	ruleset   *Ruleset
	contexts  contexts
	active    map[string]struct{}
	completed map[string]Result
}

func offResult(h *header, reason Reason) Result {
	if h.offVariation < 0 {
		return Result{VariationIndex: -1, Reason: reason}
	}
	return served(h.variations, h.offVariation, reason)
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

func serveResult(f *flag, s serve, ctx contexts, reason Reason) Result {
	index := s.variation
	if s.roll != nil {
		index = rolloutVariation(s.roll, int(kindBucket(f.key, f.salt, s.roll.kind, ctx)))
	}
	return served(f.variations, index, reason)
}

// listsContext reports whether the context has, in any of its kinds, a key that keys lists for that kind.
func listsContext(keys map[string]map[string]struct{}, ctx contexts) bool {
	for kind, kc := range ctx {
		if _, listed := keys[kind][kc.key]; listed {
			return true
		}
	}
	return false
}

func segmentRuleMatches(r *segmentRule, s *segment, ctx contexts) bool {
	for i := range r.clauses {
		if !attributeClauseMatches(&r.clauses[i], ctx) {
			return false
		}
	}
	if r.weight < 0 {
		return true
	}
	return int(kindBucket(s.key, s.salt, r.kind, ctx)) < r.weight
}

func segmentMatches(s *segment, ctx contexts) bool {
	if listsContext(s.excluded, ctx) {
		return false
	}
	if listsContext(s.included, ctx) {
		return true
	}
	for i := range s.rules {
		if segmentRuleMatches(&s.rules[i], s, ctx) {
			return true
		}
	}
	return false
}

func (r *run) clauseMatches(c *clause) bool {
	if c.op != opSegmentMatch {
		return attributeClauseMatches(c, r.contexts)
	}
	matched := false
	for _, key := range c.segments {
		if s := r.ruleset.segments[key]; s != nil && segmentMatches(s, r.contexts) {
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
			h := &header{variations: f.variations, offVariation: f.offVariation}
			return offResult(h, Reason{Kind: ReasonPrerequisiteFailed, FlagKey: p.flagKey}), true
		}
	}
	return Result{}, false
}

func (f *flag) targeted(ctx contexts) (int, bool) {
	best, found := targetHit{}, false
	for kind, kc := range ctx {
		hit, ok := f.targetIndex[kind][kc.key]
		if ok && (!found || hit.order < best.order) {
			best, found = hit, true
		}
	}
	return best.variation, found
}

func (r *run) evaluateEntry(e *flagEntry, depth int) Result {
	if e.status == flagMalformed {
		return failure(CodeMalformedFlag)
	}
	if !e.header.on {
		return offResult(&e.header, Reason{Kind: ReasonOff})
	}
	if e.status == flagBroken {
		return failure(e.code)
	}
	f := e.flag
	if blocked, stop := r.checkPrerequisites(f, depth); stop {
		return blocked
	}
	if variation, ok := f.targeted(r.contexts); ok {
		return served(f.variations, variation, Reason{Kind: ReasonTargetMatch})
	}
	for index := range f.rules {
		rl := &f.rules[index]
		matched := true
		for i := range rl.clauses {
			if !r.clauseMatches(&rl.clauses[i]) {
				matched = false
				break
			}
		}
		if matched {
			return serveResult(f, rl.serve, r.contexts, Reason{Kind: ReasonRuleMatch, RuleIndex: index})
		}
	}
	return serveResult(f, f.fallthroughServe, r.contexts, Reason{Kind: ReasonFallthrough})
}

func (r *run) evaluateFlag(key string, depth int) Result {
	if done, ok := r.completed[key]; ok {
		return done
	}
	if _, cycle := r.active[key]; cycle {
		return failure(CodePrerequisiteCycle)
	}
	if depth > maxPrerequisiteDepth {
		return failure(CodePrerequisiteDepthExceeds)
	}
	entry, ok := r.ruleset.flags[key]
	if !ok {
		return failure(CodeFlagNotFound)
	}
	r.active[key] = struct{}{}
	result := r.evaluateEntry(entry, depth)
	delete(r.active, key)
	r.completed[key] = result
	return result
}

// Evaluate evaluates one flag for one context, as JSON hands it over. It never fails: every problem,
// including a prerequisite cycle, is an ERROR result. The order and the comparison rules are in
// docs/evaluation.md.
func (rs *Ruleset) Evaluate(flagKey string, context jsonvalue.Value) Result {
	ctx, ok := normalise(context)
	if !ok {
		return failure(CodeInvalidContext)
	}
	r := &run{ruleset: rs, contexts: ctx, active: map[string]struct{}{}, completed: map[string]Result{}}
	return r.evaluateFlag(flagKey, 0)
}
