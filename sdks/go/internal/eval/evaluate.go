package eval

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"

// maxPrerequisiteDepth is the prerequisite links from the requested flag; docs/evaluation.md pins the limit.
const maxPrerequisiteDepth = 32

// NoVariation is the variation index of a result that serves no variation.
const NoVariation = -1

// Reason kinds.
const (
	ReasonOff                = "OFF"
	ReasonTargetMatch        = "TARGET_MATCH"
	ReasonRuleMatch          = "RULE_MATCH"
	ReasonFallthrough        = "FALLTHROUGH"
	ReasonPrerequisiteFailed = "PREREQUISITE_FAILED"
	ReasonError              = "ERROR"
)

// Reason says why a result is what it is. RuleIndex is meaningful for RULE_MATCH only, FlagKey for
// PREREQUISITE_FAILED only and Code for ERROR only.
type Reason struct {
	Kind      string
	RuleIndex int
	FlagKey   string
	Code      string
}

// Result is the outcome of one evaluation. Value is null when nothing is served.
type Result struct {
	Value          jsonvalue.Value
	VariationIndex int
	Reason         Reason
}

type run struct {
	ruleset   *Ruleset
	contexts  contexts
	active    map[string]bool
	completed map[string]Result
}

func failure(code string) Result {
	return Result{VariationIndex: NoVariation, Reason: Reason{Kind: ReasonError, Code: code}}
}

func served(variations []jsonvalue.Value, index int, reason Reason) Result {
	return Result{Value: variations[index], VariationIndex: index, Reason: reason}
}

func offResult(entry *flagEntry, reason Reason) Result {
	if entry.off < 0 {
		return Result{VariationIndex: NoVariation, Reason: reason}
	}
	return served(entry.variations, entry.off, reason)
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

func segmentRuleMatches(r *segmentRule, seg *segmentEntry, ctxs contexts) bool {
	for i := range r.clauses {
		if !attributeClauseMatches(&r.clauses[i], ctxs) {
			return false
		}
	}
	if r.weight < 0 {
		return true
	}
	return kindBucket(seg.key, seg.salt, r.kind, ctxs) < r.weight
}

// listsContext: does the context have, in any of its kinds, a key that keys lists for that kind?
func listsContext(keys keysByKind, ctxs contexts) bool {
	for i := range ctxs {
		if _, listed := keys[ctxs[i].kind][ctxs[i].key]; listed {
			return true
		}
	}
	return false
}

func segmentMatches(seg *segmentEntry, ctxs contexts) bool {
	if listsContext(seg.excluded, ctxs) {
		return false
	}
	if listsContext(seg.included, ctxs) {
		return true
	}
	for i := range seg.rules {
		if segmentRuleMatches(&seg.rules[i], seg, ctxs) {
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
		if seg := r.ruleset.segments[key]; seg != nil && !seg.malformed && segmentMatches(seg, r.contexts) {
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

func (r *run) checkPrerequisites(entry *flagEntry, depth int) (Result, bool) {
	for _, p := range entry.flag.prerequisites {
		result := r.evaluateFlag(p.flagKey, depth+1)
		if result.Reason.Kind == ReasonError {
			return result, true
		}
		if !prerequisiteMet(p, result) {
			return offResult(entry, Reason{Kind: ReasonPrerequisiteFailed, FlagKey: p.flagKey}), true
		}
	}
	return Result{}, false
}

// targetedVariation is the variation of the first target in array order that matches the context.
func targetedVariation(targets map[string]map[string]targetHit, ctxs contexts) (int, bool) {
	var first targetHit
	found := false
	for i := range ctxs {
		if hit, ok := targets[ctxs[i].kind][ctxs[i].key]; ok && (!found || hit.order < first.order) {
			first, found = hit, true
		}
	}
	return first.variation, found
}

func (r *run) evaluateEntry(entry *flagEntry, depth int) Result {
	if entry.status == statusMalformed {
		return failure("MALFORMED_FLAG")
	}
	if !entry.on {
		return offResult(entry, Reason{Kind: ReasonOff})
	}
	if entry.status == statusBroken {
		return failure(entry.code)
	}
	if blocked, stop := r.checkPrerequisites(entry, depth); stop {
		return blocked
	}
	f := entry.flag
	if variation, ok := targetedVariation(f.targets, r.contexts); ok {
		return served(f.variations, variation, Reason{Kind: ReasonTargetMatch})
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
			return serveResult(f, f.rules[i].serve, r.contexts, Reason{Kind: ReasonRuleMatch, RuleIndex: i})
		}
	}
	return serveResult(f, f.defaultServe, r.contexts, Reason{Kind: ReasonFallthrough})
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
	entry := r.ruleset.flags[key]
	if entry == nil {
		return failure("FLAG_NOT_FOUND")
	}
	r.active[key] = true
	result := r.evaluateEntry(entry, depth)
	delete(r.active, key)
	r.completed[key] = result
	return result
}

// Evaluate evaluates one flag for one context (as JSON). Every failure, including a prerequisite cycle, is
// an ERROR result. The order and the comparison rules are in docs/evaluation.md.
func (r *Ruleset) Evaluate(flagKey string, context jsonvalue.Value) Result {
	ctxs, ok := normaliseContext(context)
	if !ok {
		return failure("INVALID_CONTEXT")
	}
	run := &run{ruleset: r, contexts: ctxs, active: map[string]bool{}, completed: map[string]Result{}}
	return run.evaluateFlag(flagKey, 0)
}
