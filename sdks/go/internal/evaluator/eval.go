package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"

// run is one evaluation. The two maps are made only when a flag has prerequisites.
type run struct {
	rs        *Ruleset
	ctxs      *contexts
	active    map[string]struct{}
	completed map[string]Result
}

func served(e *flagEntry, index int, reason Reason) Result {
	return Result{Value: e.variations[index], VariationIndex: index, Reason: reason}
}

func offResult(e *flagEntry, reason Reason) Result {
	if e.off < 0 {
		return Result{VariationIndex: -1, Reason: reason}
	}
	return served(e, e.off, reason)
}

func (r *rollout) pick(position int) int {
	chosen, cumulative := 0, 0
	for _, w := range r.items {
		chosen = w.variation
		cumulative += w.weight
		if position < cumulative {
			break
		}
	}
	return chosen
}

func serveResult(e *flagEntry, s serve, ctxs *contexts, reason Reason) Result {
	index := s.variation
	if s.rollout != nil {
		index = s.rollout.pick(ctxs.kindBucket(e.key, e.salt, s.rollout.kind))
	}
	return served(e, index, reason)
}

// listsContext reports whether the context has, in any of its kinds, a key that keys lists for that kind.
func listsContext(keys keysByKind, ctxs *contexts) bool {
	for i := range ctxs.list {
		kc := &ctxs.list[i]
		if _, ok := keys[kc.kind][kc.key]; ok {
			return true
		}
	}
	return false
}

func (s *segment) ruleMatches(r *segmentRule, ctxs *contexts) bool {
	for _, c := range r.clauses {
		if !c.matches(ctxs) {
			return false
		}
	}
	if !r.weighted {
		return true
	}
	return ctxs.kindBucket(s.key, s.salt, r.kind) < r.weight
}

func (s *segment) matches(ctxs *contexts) bool {
	if listsContext(s.excluded, ctxs) {
		return false
	}
	if listsContext(s.included, ctxs) {
		return true
	}
	for i := range s.rules {
		if s.ruleMatches(&s.rules[i], ctxs) {
			return true
		}
	}
	return false
}

func (c *clause) matches(ctxs *contexts) bool {
	if c.attr != nil {
		return c.attr.matches(ctxs)
	}
	matched := false
	for _, s := range c.seg.segments {
		if s.matches(ctxs) {
			matched = true
			break
		}
	}
	return matched != c.seg.negate
}

func (r *rule) matches(ctxs *contexts) bool {
	for i := range r.clauses {
		if !r.clauses[i].matches(ctxs) {
			return false
		}
	}
	return true
}

// targeted is the variation of the first target, in array order, that matches the context.
func (e *flagEntry) targeted(ctxs *contexts) (int, bool) {
	best, found := targetHit{}, false
	for i := range ctxs.list {
		kc := &ctxs.list[i]
		if hit, ok := e.targets[kc.kind][kc.key]; ok && (!found || hit.order < best.order) {
			best, found = hit, true
		}
	}
	return best.variation, found
}

func prerequisiteMet(p prerequisite, res Result) bool {
	switch res.Reason.Kind {
	case ReasonTargetMatch, ReasonRuleMatch, ReasonFallthrough:
		return float64(res.VariationIndex) == p.variation && res.VariationIndex >= 0
	}
	return false
}

func (r *run) checkPrerequisites(e *flagEntry, depth int) (Result, bool) {
	for _, p := range e.prereqs {
		res := r.flag(p.flagKey, depth+1)
		if res.Reason.Kind == ReasonError {
			return res, true
		}
		if !prerequisiteMet(p, res) {
			return offResult(e, Reason{Kind: ReasonPrerequisiteFailed, FlagKey: p.flagKey}), true
		}
	}
	return Result{}, false
}

func (r *run) entry(e *flagEntry, depth int) Result {
	if e.status == statusMalformed {
		return Failure(ErrMalformedFlag)
	}
	if !e.on {
		return offResult(e, Reason{Kind: ReasonOff})
	}
	if e.status == statusBroken {
		return Failure(e.code)
	}
	if blocked, ok := r.checkPrerequisites(e, depth); ok {
		return blocked
	}
	if v, ok := e.targeted(r.ctxs); ok {
		return served(e, v, Reason{Kind: ReasonTargetMatch})
	}
	for i := range e.rules {
		if e.rules[i].matches(r.ctxs) {
			return serveResult(e, e.rules[i].serve, r.ctxs, Reason{Kind: ReasonRuleMatch, RuleIndex: i})
		}
	}
	return serveResult(e, e.fallthru, r.ctxs, Reason{Kind: ReasonFallthrough})
}

// flag evaluates a flag reached through a prerequisite, at most once per evaluation.
func (r *run) flag(key string, depth int) Result {
	if res, ok := r.completed[key]; ok {
		return res
	}
	if _, ok := r.active[key]; ok {
		return Failure(ErrPrerequisiteCycle)
	}
	if depth > maxPrerequisiteDepth {
		return Failure(ErrPrerequisiteDepthExceeded)
	}
	e := r.rs.flags[key]
	if e == nil {
		return Failure(ErrFlagNotFound)
	}
	r.active[key] = struct{}{}
	res := r.entry(e, depth)
	delete(r.active, key)
	r.completed[key] = res
	return res
}

// Evaluate evaluates one flag for one context. It never panics on its input: every failure, including a
// prerequisite cycle, is an ERROR result. The order and the comparison rules are in docs/evaluation.md.
func (rs *Ruleset) Evaluate(flagKey string, context jsonvalue.Value) Result {
	ctxs, ok := normalise(context)
	if !ok {
		return Failure(ErrInvalidContext)
	}
	return rs.evaluateIn(flagKey, &ctxs)
}

func (rs *Ruleset) evaluateIn(flagKey string, ctxs *contexts) Result {
	e := rs.flags[flagKey]
	if e == nil {
		return Failure(ErrFlagNotFound)
	}
	r := &run{rs: rs, ctxs: ctxs}
	// A flag that reaches no prerequisite needs neither map: nothing else is evaluated, so no cycle or repeat.
	if e.status != statusReady || !e.on || len(e.prereqs) == 0 {
		return r.entry(e, 0)
	}
	r.active = map[string]struct{}{}
	r.completed = map[string]Result{}
	return r.flag(flagKey, 0)
}

// FlagVersion is the "version" of a flag that loaded whole (the one an evaluation can serve from), and false
// for a flag the ruleset lacks or one that did not load. A version is a count, so it is a float64 that holds an
// integer.
func (rs *Ruleset) FlagVersion(flagKey string) (float64, bool) {
	e := rs.flags[flagKey]
	if e == nil || e.status != statusReady {
		return 0, false
	}
	return e.version, true
}
