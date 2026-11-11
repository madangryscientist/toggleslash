package eval

import "github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"

// maxPrerequisiteDepth is the limit on prerequisite links from the requested flag; docs/evaluation.md pins it.
const maxPrerequisiteDepth = 32

type run struct {
	ruleset   *Ruleset
	contexts  contexts
	active    map[string]bool
	completed map[string]Result
}

func served(variations []jsonvalue.Value, index int, reason Reason) Result {
	return Result{Value: variations[index], VariationIndex: index, Reason: reason}
}

func plain(kind string) Reason { return Reason{Kind: kind, RuleIndex: -1} }

func offResult(h flagHeader, reason Reason) Result {
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

func serveResult(f *flag, s serve, ctxs contexts, reason Reason) Result {
	index := s.variation
	if s.rollout != nil {
		index = rolloutVariation(s.rollout, kindBucket(f.key, f.salt, s.rollout.kind, ctxs))
	}
	return served(f.variations, index, reason)
}

func (r segmentRule) matches(seg *segment, ctxs contexts) bool {
	for _, c := range r.clauses {
		if !c.matches(ctxs) {
			return false
		}
	}
	if r.weight < 0 {
		return true
	}
	return kindBucket(seg.key, seg.salt, r.rolloutKind, ctxs) < r.weight
}

// listsContext reports whether the context has, in any of its kinds, a key that keys lists for that kind.
func listsContext(keys keysByKind, ctxs contexts) bool {
	for kind, kc := range ctxs {
		if _, ok := keys[kind][kc.key]; ok {
			return true
		}
	}
	return false
}

func (s *segment) matches(ctxs contexts) bool {
	if listsContext(s.excluded, ctxs) {
		return false
	}
	if listsContext(s.included, ctxs) {
		return true
	}
	for _, rl := range s.rules {
		if rl.matches(s, ctxs) {
			return true
		}
	}
	return false
}

func (c *clause) matches(rn *run) bool {
	if !c.segment {
		return c.attr.matches(rn.contexts)
	}
	matched := false
	for _, key := range c.segmentKeys {
		if entry := rn.ruleset.segments[key]; entry.seg != nil && entry.seg.matches(rn.contexts) {
			matched = true
			break
		}
	}
	return matched != c.negate
}

func prerequisiteMet(p prerequisite, result Result) bool {
	switch result.Reason.Kind {
	case KindTargetMatch, KindRuleMatch, KindFallthrough:
		return float64(result.VariationIndex) == p.variation
	}
	return false
}

func (rn *run) checkPrerequisites(f *flag, depth int) (Result, bool) {
	for _, p := range f.prerequisites {
		result := rn.evaluateFlag(p.flagKey, depth+1)
		if result.Reason.Kind == KindError {
			return result, true
		}
		if !prerequisiteMet(p, result) {
			return offResult(f.flagHeader, Reason{Kind: KindPrerequisiteFailed, RuleIndex: -1, FlagKey: p.flagKey}), true
		}
	}
	return Result{}, false
}

// targetedVariation is the variation of the first target in array order that matches the context.
func targetedVariation(index targetIndex, ctxs contexts) (int, bool) {
	best, found := targetHit{}, false
	for kind, kc := range ctxs {
		if hit, ok := index[kind][kc.key]; ok && (!found || hit.order < best.order) {
			best, found = hit, true
		}
	}
	return best.variation, found
}

func (rn *run) evaluateEntry(e *flagEntry, depth int) Result {
	if e.status == entryMalformed {
		return failure(CodeMalformedFlag)
	}
	if !e.header.on {
		return offResult(e.header, plain(KindOff))
	}
	if e.status == entryBroken {
		return failure(e.code)
	}
	f := e.flag
	if blocked, ok := rn.checkPrerequisites(f, depth); ok {
		return blocked
	}
	if idx, ok := targetedVariation(f.targets, rn.contexts); ok {
		return served(f.variations, idx, plain(KindTargetMatch))
	}
	for i := range f.rules {
		rl := &f.rules[i]
		all := true
		for j := range rl.clauses {
			if !rl.clauses[j].matches(rn) {
				all = false
				break
			}
		}
		if all {
			return serveResult(f, rl.serve, rn.contexts, Reason{Kind: KindRuleMatch, RuleIndex: i})
		}
	}
	return serveResult(f, f.fallthroughServe, rn.contexts, plain(KindFallthrough))
}

func (rn *run) evaluateFlag(key string, depth int) Result {
	if result, ok := rn.completed[key]; ok {
		return result
	}
	if rn.active[key] {
		return failure(CodePrerequisiteCycle)
	}
	if depth > maxPrerequisiteDepth {
		return failure(CodePrerequisiteDepthExceeds)
	}
	entry := rn.ruleset.flags[key]
	if entry == nil {
		return failure(CodeFlagNotFound)
	}
	rn.active[key] = true
	result := rn.evaluateEntry(entry, depth)
	delete(rn.active, key)
	rn.completed[key] = result
	return result
}

// Evaluate evaluates one flag for one context. It never fails: every failure, including a prerequisite
// cycle, is an ERROR result. The order and the comparison rules are in docs/evaluation.md.
func (r *Ruleset) Evaluate(flagKey string, ctx jsonvalue.Value) Result {
	ctxs, ok := normaliseContext(ctx)
	if !ok {
		return failure(CodeInvalidContext)
	}
	rn := &run{ruleset: r, contexts: ctxs, active: map[string]bool{}, completed: map[string]Result{}}
	return rn.evaluateFlag(flagKey, 0)
}
