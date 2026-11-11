package eval

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"

// maxPrerequisiteDepth is the prerequisite links from the requested flag; docs/evaluation.md pins it.
const maxPrerequisiteDepth = 32

type run struct {
	ruleset   *Ruleset
	contexts  Contexts
	active    map[string]bool
	completed map[string]Result
}

func failure(code ErrorCode) Result {
	return Result{VariationIndex: -1, Reason: Reason{Kind: ReasonError, Code: code}}
}

func served(variations []jsonvalue.Value, index int, reason Reason) Result {
	return Result{Value: variations[index], VariationIndex: index, Reason: reason}
}

func offResult(h flagHeader, reason Reason) Result {
	if h.off < 0 {
		return Result{VariationIndex: -1, Reason: reason}
	}
	return served(h.variations, h.off, reason)
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

func serveResult(f *flag, s serve, contexts Contexts, reason Reason) Result {
	index := s.variation
	if s.rollout != nil {
		index = rolloutVariation(s.rollout, kindBucket(f.key, f.salt, s.rollout.kind, contexts))
	}
	return served(f.variations, index, reason)
}

func segmentRuleMatches(r *segmentRule, ready *readySegment, contexts Contexts) bool {
	for i := range r.clauses {
		if !attributeClauseMatches(&r.clauses[i], contexts) {
			return false
		}
	}
	if r.weight < 0 {
		return true
	}
	return kindBucket(ready.seg.key, ready.seg.salt, r.rollKind, contexts) < r.weight
}

func segmentMatches(ready *readySegment, contexts Contexts) bool {
	if listsContext(ready.excluded, contexts) {
		return false
	}
	if listsContext(ready.included, contexts) {
		return true
	}
	for i := range ready.seg.rules {
		if segmentRuleMatches(&ready.seg.rules[i], ready, contexts) {
			return true
		}
	}
	return false
}

func (r *run) clauseMatches(cl *clause) bool {
	if cl.op != opSegmentMatch {
		return attributeClauseMatches(cl, r.contexts)
	}
	matched := false
	for _, key := range cl.segments {
		entry, ok := r.ruleset.segments[key]
		if ok && !entry.malformed && segmentMatches(entry.ready, r.contexts) {
			matched = true
			break
		}
	}
	if cl.negate {
		return !matched
	}
	return matched
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
			return offResult(f.flagHeader, Reason{Kind: ReasonPrerequisiteFailed, FlagKey: p.flagKey}), true
		}
	}
	return Result{}, false
}

func (r *run) evaluateEntry(entry *flagEntry, depth int) Result {
	if entry.status == statusMalformed {
		return failure(MalformedFlag)
	}
	if !entry.header.on {
		return offResult(entry.header, Reason{Kind: ReasonOff})
	}
	if entry.status == statusBroken {
		return failure(entry.code)
	}
	f := entry.flag
	if blocked, stop := r.checkPrerequisites(f, depth); stop {
		return blocked
	}
	if variation, ok := targetedVariation(entry.targets, r.contexts); ok {
		return served(f.variations, variation, Reason{Kind: ReasonTargetMatch})
	}
	for i := range f.rules {
		rl := &f.rules[i]
		all := true
		for j := range rl.clauses {
			if !r.clauseMatches(&rl.clauses[j]) {
				all = false
				break
			}
		}
		if all {
			return serveResult(f, rl.serve, r.contexts, Reason{Kind: ReasonRuleMatch, RuleIndex: i})
		}
	}
	return serveResult(f, f.fallthru, r.contexts, Reason{Kind: ReasonFallthrough})
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
	entry, ok := r.ruleset.flags[key]
	if !ok {
		return failure(FlagNotFound)
	}
	r.active[key] = true
	result := r.evaluateEntry(entry, depth)
	delete(r.active, key)
	r.completed[key] = result
	return result
}

// Evaluate evaluates one flag for one context. It never fails: every failure, including a prerequisite
// cycle, is an ERROR result. The order and the comparison rules are in docs/evaluation.md.
func (rs *Ruleset) Evaluate(flagKey string, context jsonvalue.Value) Result {
	contexts, ok := NormaliseContext(context)
	if !ok {
		return failure(InvalidContext)
	}
	return rs.EvaluateContexts(flagKey, contexts)
}

// EvaluateContexts is Evaluate for a context that NormaliseContext has already accepted.
func (rs *Ruleset) EvaluateContexts(flagKey string, contexts Contexts) Result {
	r := &run{ruleset: rs, contexts: contexts, active: map[string]bool{}, completed: map[string]Result{}}
	return r.evaluateFlag(flagKey, 0)
}
