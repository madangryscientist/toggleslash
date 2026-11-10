package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"

// maxPrerequisiteDepth: prerequisite links from the requested flag; docs/evaluation.md pins the limit.
const maxPrerequisiteDepth = 32

// run holds one Evaluate call. The two maps are made on the first flag that has prerequisites, so a
// flag without any allocates none.
type run struct {
	ruleset   *Ruleset
	ctxs      contexts
	active    map[string]struct{}
	completed map[string]Result
}

func variationValue(variations []jsonv.Value, index int) jsonv.Value {
	if index < 0 || index >= len(variations) {
		return jsonv.Value{}
	}
	return variations[index]
}

func served(variations []jsonv.Value, index int, reason Reason) Result {
	return Result{Value: variationValue(variations, index), VariationIndex: index, Reason: reason}
}

func reasonOf(kind ReasonKind) Reason { return Reason{Kind: kind, RuleIndex: -1} }

func offResult(e *flagEntry, reason Reason) Result {
	if e.offVariation < 0 {
		return Result{VariationIndex: NoIndex, Reason: reason}
	}
	return served(e.variations, e.offVariation, reason)
}

func rolloutVariation(r *rollout, position int) int {
	chosen := 0
	cumulative := 0
	for _, wv := range r.variations {
		chosen = wv.variation
		cumulative += wv.weight
		if position < cumulative {
			break
		}
	}
	return chosen
}

func serveResult(e *flagEntry, s serve, ctxs contexts, reason Reason) Result {
	index := s.variation
	if s.rollout != nil {
		index = rolloutVariation(s.rollout, kindBucket(e.key, e.salt, s.rollout.contextKind, ctxs))
	}
	return served(e.variations, index, reason)
}

func listsContext(keys keysByKind, ctxs contexts) bool {
	for i := range ctxs {
		if set, ok := keys[ctxs[i].kind]; ok {
			if _, found := set[ctxs[i].key]; found {
				return true
			}
		}
	}
	return false
}

func segmentRuleMatches(r *segmentRule, s *segment, ctxs contexts) bool {
	for _, c := range r.clauses {
		if !attributeClauseMatches(c, ctxs) {
			return false
		}
	}
	if r.weight < 0 {
		return true
	}
	return kindBucket(s.key, s.salt, r.rolloutKind, ctxs) < r.weight
}

func segmentMatches(s *segment, ctxs contexts) bool {
	if listsContext(s.excluded, ctxs) {
		return false
	}
	if listsContext(s.included, ctxs) {
		return true
	}
	for i := range s.rules {
		if segmentRuleMatches(&s.rules[i], s, ctxs) {
			return true
		}
	}
	return false
}

func (r *run) clauseMatches(c *clause) bool {
	if c.attr != nil {
		return attributeClauseMatches(c.attr, r.ctxs)
	}
	matched := false
	for _, key := range c.seg.values {
		if s := r.ruleset.segments[key]; s != nil && segmentMatches(s, r.ctxs) {
			matched = true
			break
		}
	}
	return matched != c.seg.negate
}

func prerequisiteMet(p prerequisite, res Result) bool {
	switch res.Reason.Kind {
	case ReasonTargetMatch, ReasonRuleMatch, ReasonFallthrough:
		return res.VariationIndex >= 0 && float64(res.VariationIndex) == p.variation
	}
	return false
}

func (r *run) checkPrerequisites(e *flagEntry, depth int) (Result, bool) {
	for _, p := range e.prerequisites {
		res := r.evaluateFlag(p.flagKey, depth+1)
		if res.Reason.Kind == ReasonError {
			return res, true
		}
		if !prerequisiteMet(p, res) {
			reason := reasonOf(ReasonPrerequisiteFailed)
			reason.PrerequisiteKey = p.flagKey
			return offResult(e, reason), true
		}
	}
	return Result{}, false
}

func targetedVariation(targets map[string]map[string]targetHit, ctxs contexts) (int, bool) {
	var first targetHit
	found := false
	for i := range ctxs {
		hit, ok := targets[ctxs[i].kind][ctxs[i].key]
		if ok && (!found || hit.order < first.order) {
			first, found = hit, true
		}
	}
	return first.variation, found
}

func (r *run) evaluateEntry(e *flagEntry, depth int) Result {
	if e.status == statusMalformed {
		return errorResult(ErrMalformedFlag)
	}
	if !e.on {
		return offResult(e, reasonOf(ReasonOff))
	}
	if e.status == statusBroken {
		return errorResult(e.code)
	}
	if blocked, stop := r.checkPrerequisites(e, depth); stop {
		return blocked
	}
	if v, ok := targetedVariation(e.targets, r.ctxs); ok {
		return served(e.variations, v, reasonOf(ReasonTargetMatch))
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
			reason := reasonOf(ReasonRuleMatch)
			reason.RuleIndex = i
			return serveResult(e, rl.serve, r.ctxs, reason)
		}
	}
	return serveResult(e, e.fallthru, r.ctxs, reasonOf(ReasonFallthrough))
}

func (r *run) evaluateFlag(key string, depth int) Result {
	if res, ok := r.completed[key]; ok {
		return res
	}
	if _, cycle := r.active[key]; cycle {
		return errorResult(ErrPrerequisiteCycle)
	}
	if depth > maxPrerequisiteDepth {
		return errorResult(ErrPrerequisiteDepthExceeded)
	}
	e, ok := r.ruleset.flags[key]
	if !ok {
		return errorResult(ErrFlagNotFound)
	}
	if len(e.prerequisites) == 0 && depth == 0 {
		return r.evaluateEntry(e, depth)
	}
	// A result is kept so a flag reached again is not evaluated twice, and a result kept at a shallow
	// depth is reused at a deeper one (docs/evaluation.md). Only a flag with prerequisites starts the maps.
	if r.active == nil {
		r.active = map[string]struct{}{}
		r.completed = map[string]Result{}
	}
	r.active[key] = struct{}{}
	res := r.evaluateEntry(e, depth)
	delete(r.active, key)
	r.completed[key] = res
	return res
}

// Evaluate evaluates one flag for one context (a JSON value, valid or not). Every failure, including a
// prerequisite cycle, is an ERROR result. The order and the comparison rules are in docs/evaluation.md.
func (rs *Ruleset) Evaluate(flagKey string, context jsonv.Value) Result {
	ctxs, ok := normaliseContext(context)
	if !ok {
		return errorResult(ErrInvalidContext)
	}
	r := run{ruleset: rs, ctxs: ctxs}
	return r.evaluateFlag(flagKey, 0)
}
