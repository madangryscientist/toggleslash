package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"

// maxPrerequisiteDepth is the limit on prerequisite links from the requested flag; docs/evaluation.md pins it.
const maxPrerequisiteDepth = 32

type run struct {
	ruleset   *Ruleset
	ctx       contexts
	active    map[string]bool
	completed map[string]Result
}

func served(f *flag, index int, reason Reason) Result {
	return Result{Value: f.variations[index], VariationIndex: index, Reason: reason}
}

func offResult(f *flag, reason Reason) Result {
	if f.offVariation == NoVariation {
		return Result{VariationIndex: NoVariation, Reason: reason}
	}
	return served(f, f.offVariation, reason)
}

func rolloutVariation(r *rollout, position int) int {
	chosen, cumulative := 0, 0
	for _, e := range r.entries {
		chosen = e.variation
		cumulative += e.weight
		if position < cumulative {
			break
		}
	}
	return chosen
}

func serveResult(f *flag, s serve, ctx contexts, reason Reason) Result {
	index := s.variation
	if s.rollout != nil {
		index = rolloutVariation(s.rollout, kindBucket(f.key, f.salt, s.rollout.kind, ctx))
	}
	return served(f, index, reason)
}

func segmentRuleMatches(r *segmentRule, seg *segment, ctx contexts) bool {
	for i := range r.clauses {
		if !attributeClauseMatches(&r.clauses[i], ctx) {
			return false
		}
	}
	if !r.hasWeight {
		return true
	}
	return kindBucket(seg.key, seg.salt, r.rolloutKind, ctx) < r.weight
}

// listsContext reports whether the context has, in any of its kinds, a key that keys lists for that kind.
func listsContext(keys keysByKind, ctx contexts) bool {
	for kind, entry := range ctx {
		if _, ok := keys[kind][entry.key]; ok {
			return true
		}
	}
	return false
}

func segmentMatches(seg *segment, ctx contexts) bool {
	if listsContext(seg.excluded, ctx) {
		return false
	}
	if listsContext(seg.included, ctx) {
		return true
	}
	for i := range seg.rules {
		if segmentRuleMatches(&seg.rules[i], seg, ctx) {
			return true
		}
	}
	return false
}

func (r *run) clauseMatches(c *clause) bool {
	if !c.segment {
		return attributeClauseMatches(c, r.ctx)
	}
	matched := false
	for _, key := range c.segmentKeys {
		if entry, ok := r.ruleset.segments[key]; ok && entry.segment != nil && segmentMatches(entry.segment, r.ctx) {
			matched = true
			break
		}
	}
	return matched != c.negate
}

func prerequisiteMet(p prerequisite, result Result) bool {
	switch result.Reason.Kind {
	case ReasonTargetMatch, ReasonRuleMatch, ReasonFallthrough:
		return result.VariationIndex != NoVariation && float64(result.VariationIndex) == p.variation
	}
	return false
}

// checkPrerequisites returns the result that stops the evaluation, and false when every prerequisite is met.
func (r *run) checkPrerequisites(f *flag, depth int) (Result, bool) {
	for _, p := range f.prerequisites {
		result := r.evaluateFlag(p.flagKey, depth+1)
		if result.Reason.Kind == ReasonError {
			return result, true
		}
		if !prerequisiteMet(p, result) {
			return offResult(f, Reason{Kind: ReasonPrerequisiteFailed, FlagKey: p.flagKey}), true
		}
	}
	return Result{}, false
}

// targetedVariation is the variation of the first target in array order that matches the context.
func targetedVariation(hits map[string]map[string]targetHit, ctx contexts) (int, bool) {
	var first *targetHit
	for kind, entry := range ctx {
		if hit, ok := hits[kind][entry.key]; ok && (first == nil || hit.order < first.order) {
			h := hit
			first = &h
		}
	}
	if first == nil {
		return 0, false
	}
	return first.variation, true
}

func (r *run) evaluateEntry(entry *flagEntry, depth int) Result {
	if entry.status == statusMalformed {
		return failure(CodeMalformedFlag)
	}
	f := entry.flag
	if !f.on {
		return offResult(f, Reason{Kind: ReasonOff})
	}
	if entry.status == statusBroken {
		return failure(entry.code)
	}
	if blocked, stop := r.checkPrerequisites(f, depth); stop {
		return blocked
	}
	if index, ok := targetedVariation(entry.hits, r.ctx); ok {
		return served(f, index, Reason{Kind: ReasonTargetMatch})
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
			return serveResult(f, f.rules[i].serve, r.ctx, Reason{Kind: ReasonRuleMatch, RuleIndex: i})
		}
	}
	return serveResult(f, f.fall, r.ctx, Reason{Kind: ReasonFallthrough})
}

func (r *run) evaluateFlag(key string, depth int) Result {
	if done, ok := r.completed[key]; ok {
		return done
	}
	if r.active[key] {
		return failure(CodePrerequisiteCycle)
	}
	if depth > maxPrerequisiteDepth {
		return failure(CodePrerequisiteDepthExceed)
	}
	entry, ok := r.ruleset.flags[key]
	if !ok {
		return failure(CodeFlagNotFound)
	}
	r.active[key] = true
	result := r.evaluateEntry(entry, depth)
	delete(r.active, key)
	r.completed[key] = result
	return result
}

// Evaluate evaluates one flag for one context. Every failure, including a prerequisite cycle, is an ERROR
// result. The order and the comparison rules are in docs/evaluation.md.
func (rs *Ruleset) Evaluate(flagKey string, context jsonvalue.Value) Result {
	ctx, ok := normalise(context)
	if !ok {
		return failure(CodeInvalidContext)
	}
	r := &run{ruleset: rs, ctx: ctx, active: map[string]bool{}, completed: map[string]Result{}}
	return r.evaluateFlag(flagKey, 0)
}
