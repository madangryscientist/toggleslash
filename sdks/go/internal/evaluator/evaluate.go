package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"

func jsonvalueString(s string) value { return jsonvalue.NewString(s) }

// maxPrerequisiteDepth is the prerequisite links from the requested flag; docs/evaluation.md pins
// the limit.
const maxPrerequisiteDepth = 32

type run struct {
	ruleset   *Ruleset
	contexts  contexts
	active    map[string]bool
	completed map[string]Result
}

func failure(code string) Result {
	return Result{Reason: Reason{Kind: ReasonError, Code: code}}
}

func served(variations []value, index int, reason Reason) Result {
	r := Result{HasVariation: true, VariationIndex: index, Reason: reason}
	if index < len(variations) {
		r.Value = variations[index]
	}
	return r
}

func offResult(h *flagHeader, reason Reason) Result {
	if h.offVariation < 0 {
		return Result{Reason: reason}
	}
	return served(h.variations, h.offVariation, reason)
}

func rolloutVariation(r *rollout, position int) int {
	chosen, cumulative := 0, 0
	for _, entry := range r.variations {
		chosen = entry.variation
		cumulative += entry.weight
		if position < cumulative {
			break
		}
	}
	return chosen
}

func serveResult(entry *flagEntry, key string, s serve, cs contexts, reason Reason) Result {
	index := s.variation
	if s.rollout != nil {
		index = rolloutVariation(s.rollout, kindBucket(key, entry.body.salt, s.rollout.kind, cs))
	}
	return served(entry.header.variations, index, reason)
}

func listsContext(keys map[string]map[string]struct{}, cs contexts) bool {
	for kind, kc := range cs {
		if _, ok := keys[kind][kc.key]; ok {
			return true
		}
	}
	return false
}

func segmentRuleMatches(r *segmentRule, s *segment, cs contexts) bool {
	for _, c := range r.clauses {
		if !attributeClauseMatches(c, cs) {
			return false
		}
	}
	if r.weight < 0 {
		return true
	}
	return kindBucket(s.key, s.salt, r.rolloutKind, cs) < r.weight
}

func segmentMatches(s *segment, cs contexts) bool {
	if listsContext(s.excluded, cs) {
		return false
	}
	if listsContext(s.included, cs) {
		return true
	}
	for _, r := range s.rules {
		if segmentRuleMatches(r, s, cs) {
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
	for _, key := range c.segmentKeys {
		entry, ok := r.ruleset.segments[key]
		if ok && !entry.malformed && segmentMatches(entry.segment, r.contexts) {
			matched = true
			break
		}
	}
	return matched != c.negate
}

func prerequisiteMet(p prerequisite, result Result) bool {
	switch result.Reason.Kind {
	case ReasonTargetMatch, ReasonRuleMatch, ReasonFallthrough:
		return result.HasVariation && float64(result.VariationIndex) == p.variation
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

// targetedVariation is the variation of the first target in array order that matches the context.
func targetedVariation(targets map[string]map[string]targetHit, cs contexts) (int, bool) {
	var first targetHit
	found := false
	for kind, kc := range cs {
		if hit, ok := targets[kind][kc.key]; ok && (!found || hit.order < first.order) {
			first, found = hit, true
		}
	}
	return first.variation, found
}

func (r *run) evaluateEntry(key string, entry *flagEntry, depth int) Result {
	if entry.status == statusMalformed {
		return failure("MALFORMED_FLAG")
	}
	if !entry.header.on {
		return offResult(entry.header, Reason{Kind: ReasonOff})
	}
	if entry.status == statusBroken {
		return failure(entry.code)
	}
	if blocked, ok := r.checkPrerequisites(entry, depth); ok {
		return blocked
	}
	body := entry.body
	if index, ok := targetedVariation(entry.targets, r.contexts); ok {
		return served(entry.header.variations, index, Reason{Kind: ReasonTargetMatch})
	}
	for i, rl := range body.rules {
		all := true
		for _, c := range rl.clauses {
			if !r.clauseMatches(c) {
				all = false
				break
			}
		}
		if all {
			ruleIndex := i
			return serveResult(entry, key, rl.serve, r.contexts, Reason{Kind: ReasonRuleMatch, RuleIndex: &ruleIndex})
		}
	}
	return serveResult(entry, key, body.fallThrough, r.contexts, Reason{Kind: ReasonFallthrough})
}

func (r *run) evaluateFlag(key string, depth int) Result {
	if done, ok := r.completed[key]; ok {
		return done
	}
	if r.active[key] {
		return failure("PREREQUISITE_CYCLE")
	}
	if depth > maxPrerequisiteDepth {
		return failure("PREREQUISITE_DEPTH_EXCEEDED")
	}
	entry, ok := r.ruleset.flags[key]
	if !ok {
		return failure("FLAG_NOT_FOUND")
	}
	r.active[key] = true
	result := r.evaluateEntry(key, entry, depth)
	delete(r.active, key)
	r.completed[key] = result
	return result
}

// Evaluate evaluates one flag for one context. Every failure, including a prerequisite cycle, is an
// ERROR result. The order and the comparison rules are in docs/evaluation.md.
func (rs *Ruleset) Evaluate(flagKey string, context jsonvalue.Value) Result {
	cs, ok := normaliseContext(context)
	if !ok {
		return failure("INVALID_CONTEXT")
	}
	r := &run{ruleset: rs, contexts: cs, active: map[string]bool{}, completed: map[string]Result{}}
	return r.evaluateFlag(flagKey, 0)
}
