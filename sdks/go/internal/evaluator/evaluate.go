// Package evaluator is the Go implementation of docs/evaluation.md: flags and segments in, a variation, its
// index and a reason out. It reads and writes jsonvalue.Value and imports nothing from the client.
package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"

// Reason kinds.
const (
	ReasonOff                = "OFF"
	ReasonPrerequisiteFailed = "PREREQUISITE_FAILED"
	ReasonTargetMatch        = "TARGET_MATCH"
	ReasonRuleMatch          = "RULE_MATCH"
	ReasonFallthrough        = "FALLTHROUGH"
	ReasonError              = "ERROR"
)

const maxPrerequisiteDepth = 32

// Reason says why a result is what it is. RuleIndex is meaningful for RULE_MATCH, FlagKey for
// PREREQUISITE_FAILED and Code for ERROR.
type Reason struct {
	Kind      string
	RuleIndex int
	FlagKey   string
	Code      string
}

// Result is the answer to one evaluation. Index is -1 when no variation was served.
type Result struct {
	Value  jsonvalue.Value
	Index  int
	Reason Reason
}

func errorResult(code string) Result {
	return Result{Value: jsonvalue.Null, Index: -1, Reason: Reason{Kind: ReasonError, Code: code}}
}

type evalState struct {
	rs     *Ruleset
	ctx    *Context
	done   map[string]Result
	active map[string]bool
}

// Evaluate evaluates one flag for one context, as docs/evaluation.md says. A nil context is not valid. It
// never panics on any ruleset Load accepted.
func (rs *Ruleset) Evaluate(flagKey string, ctx *Context) Result {
	if ctx == nil {
		return errorResult(CodeInvalidContext)
	}
	st := &evalState{rs: rs, ctx: ctx}
	return st.enter(flagKey, 0)
}

func (f *flag) serveIndex(i int) Result {
	return Result{Value: f.variations[i], Index: i}
}

func (f *flag) offResult(kind, flagKey string) Result {
	r := Result{Value: jsonvalue.Null, Index: -1, Reason: Reason{Kind: kind, FlagKey: flagKey}}
	if f.offVariation >= 0 {
		r.Value, r.Index = f.variations[f.offVariation], f.offVariation
	}
	return r
}

func (st *evalState) enter(key string, depth int) Result {
	if r, ok := st.done[key]; ok {
		return r
	}
	if st.active[key] {
		return errorResult(CodePrereqCycle)
	}
	if depth > maxPrerequisiteDepth {
		return errorResult(CodePrereqDepth)
	}
	f, ok := st.rs.flags[key]
	if !ok {
		return errorResult(CodeFlagNotFound)
	}
	if st.active == nil {
		st.active = map[string]bool{}
		st.done = map[string]Result{}
	}
	st.active[key] = true
	r := st.run(f, depth)
	delete(st.active, key)
	st.done[key] = r
	return r
}

func (st *evalState) run(f *flag, depth int) Result {
	if f.status == statusMalformed {
		return errorResult(CodeMalformedFlag)
	}
	if !f.on {
		return f.offResult(ReasonOff, "")
	}
	if f.status == statusBroken {
		return errorResult(f.brokenCode)
	}
	for _, p := range f.prereqs {
		pr := st.enter(p.flagKey, depth+1)
		switch pr.Reason.Kind {
		case ReasonError:
			return pr
		case ReasonTargetMatch, ReasonRuleMatch, ReasonFallthrough:
			if pr.Index >= 0 && float64(pr.Index) == p.variation {
				continue
			}
		}
		return f.offResult(ReasonPrerequisiteFailed, p.flagKey)
	}
	return st.serveFlag(f)
}

func (st *evalState) serveFlag(f *flag) Result {
	best, found := targetHit{}, false
	for kind, entry := range st.ctx.kinds {
		if hit, ok := f.targets[kind][entry.key]; ok && (!found || hit.order < best.order) {
			best, found = hit, true
		}
	}
	if found {
		r := f.serveIndex(best.variation)
		r.Reason = Reason{Kind: ReasonTargetMatch}
		return r
	}
	for i := range f.rules {
		rl := &f.rules[i]
		if st.rs.allMatch(rl.clauses, st.ctx) {
			r := f.serveFrom(rl.serve, st.ctx)
			r.Reason = Reason{Kind: ReasonRuleMatch, RuleIndex: i}
			return r
		}
	}
	r := f.serveFrom(f.fallthrough_, st.ctx)
	r.Reason = Reason{Kind: ReasonFallthrough}
	return r
}

func (rs *Ruleset) allMatch(clauses []*clause, ctx *Context) bool {
	for _, c := range clauses {
		if c.op == opSegmentMatch {
			if !c.matchesSegments(rs, ctx) {
				return false
			}
		} else if !c.matchesAttribute(ctx) {
			return false
		}
	}
	return true
}

func (f *flag) serveFrom(s serve, ctx *Context) Result {
	if !s.hasRoll {
		return f.serveIndex(s.variation)
	}
	bucket := float64(ctx.kindBucket(f.key, f.salt, s.kind))
	total := 0.0
	for _, w := range s.rollout {
		total += w.weight
		if bucket < total {
			return f.serveIndex(w.variation)
		}
	}
	return f.serveIndex(s.rollout[len(s.rollout)-1].variation) // unreachable: the weights sum to 100000
}
