package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/wjson"

// maxPrerequisiteDepth is the prerequisite links from the requested flag;
// docs/evaluation.md pins the limit.
const maxPrerequisiteDepth = 32

type run struct {
	ruleset   *Ruleset
	contexts  contexts
	active    map[string]bool
	completed map[string]Result
}

func served(variations []wjson.Value, index int, reason Reason) Result {
	i := index
	var value wjson.Value
	if index >= 0 && index < len(variations) {
		value = variations[index]
	}
	return Result{Value: value, VariationIndex: &i, Reason: reason}
}

func offResult(h *flagHeader, reason Reason) Result {
	if h.offVariation < 0 {
		return Result{Reason: reason}
	}
	return served(h.variations, h.offVariation, reason)
}

func rolloutVariation(r *rollout, position int) int {
	chosen, cumulative := 0, 0
	for _, wv := range r.variations {
		chosen = wv.variation
		cumulative += wv.weight
		if position < cumulative {
			break
		}
	}
	return chosen
}

func serveResult(h *flagHeader, body *flagBody, s serve, cs contexts, reason Reason) Result {
	index := s.variation
	if s.rollout != nil {
		position := kindBucket(h.key, body.salt, s.rollout.contextKind, cs)
		index = rolloutVariation(s.rollout, position)
	}
	return served(h.variations, index, reason)
}

func (rs *Ruleset) segmentMatches(seg *segment, cs contexts) bool {
	if listsContext(seg.excluded, cs) {
		return false
	}
	if listsContext(seg.included, cs) {
		return true
	}
	for _, r := range seg.rules {
		if segmentRuleMatches(seg, r, cs) {
			return true
		}
	}
	return false
}

func segmentRuleMatches(seg *segment, r segmentRule, cs contexts) bool {
	for _, c := range r.clauses {
		if !c.matches(cs) {
			return false
		}
	}
	if r.weight < 0 {
		return true
	}
	return kindBucket(seg.key, seg.salt, r.rolloutKind, cs) < r.weight
}

func (r *run) clauseMatches(c clause) bool {
	if c.attribute != nil {
		return c.attribute.matches(r.contexts)
	}
	matched := false
	for _, key := range c.segments {
		entry, ok := r.ruleset.segments[key]
		if ok && entry.segment != nil && r.ruleset.segmentMatches(entry.segment, r.contexts) {
			matched = true
			break
		}
	}
	return matched != c.negate
}

func prerequisiteMet(p prerequisite, result Result) bool {
	switch result.Reason.Kind {
	case ReasonTargetMatch, ReasonRuleMatch, ReasonFallthrough:
		return result.VariationIndex != nil && float64(*result.VariationIndex) == p.variation
	}
	return false
}

func (r *run) checkPrerequisites(entry *flagEntry, depth int) (Result, bool) {
	for _, p := range entry.body.prerequisites {
		result := r.evaluateFlag(p.flagKey, depth+1)
		if result.Reason.Kind == ReasonError {
			return result, true
		}
		if !prerequisiteMet(p, result) {
			return offResult(entry.header, Reason{Kind: ReasonPrerequisiteFailed, FlagKey: p.flagKey}), true
		}
	}
	return Result{}, false
}

func (r *run) evaluateEntry(entry *flagEntry, depth int) Result {
	if entry.status == entryMalformed {
		return failure(ErrMalformedFlag)
	}
	if !entry.header.on {
		return offResult(entry.header, Reason{Kind: ReasonOff})
	}
	if entry.status == entryBroken {
		return failure(entry.code)
	}
	if blocked, ok := r.checkPrerequisites(entry, depth); ok {
		return blocked
	}
	h, body := entry.header, entry.body
	if variation, ok := targetedVariation(entry.targets, r.contexts); ok {
		return served(h.variations, variation, Reason{Kind: ReasonTargetMatch})
	}
	for index, rl := range body.rules {
		matched := true
		for _, c := range rl.clauses {
			if !r.clauseMatches(c) {
				matched = false
				break
			}
		}
		if matched {
			return serveResult(h, body, rl.serve, r.contexts, Reason{Kind: ReasonRuleMatch, RuleIndex: index})
		}
	}
	return serveResult(h, body, body.fallThrough, r.contexts, Reason{Kind: ReasonFallthrough})
}

func (r *run) evaluateFlag(key string, depth int) Result {
	if done, ok := r.completed[key]; ok {
		return done
	}
	if r.active[key] {
		return failure(ErrPrerequisiteCycle)
	}
	if depth > maxPrerequisiteDepth {
		return failure(ErrPrerequisiteDepthExceeds)
	}
	entry, ok := r.ruleset.flags[key]
	if !ok {
		return failure(ErrFlagNotFound)
	}
	r.active[key] = true
	result := r.evaluateEntry(entry, depth)
	delete(r.active, key)
	r.completed[key] = result
	return result
}

// Evaluate answers one flag for one context (a JSON value, as the spec reads
// it). It never panics: every failure, including a prerequisite cycle, is an
// ERROR result. The order and the comparison rules are in docs/evaluation.md.
func (rs *Ruleset) Evaluate(flagKey string, context wjson.Value) Result {
	cs, ok := normaliseContext(context)
	if !ok {
		return failure(ErrInvalidContext)
	}
	r := &run{ruleset: rs, contexts: cs, active: map[string]bool{}, completed: map[string]Result{}}
	return r.evaluateFlag(flagKey, 0)
}
