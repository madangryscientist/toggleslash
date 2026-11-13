package eval

import "github.com/madangryscientist/toggleslash/sdks/go/jsonval"

// Reason kinds, as docs/evaluation.md "Result" spells them.
const (
	ReasonOff                = "OFF"
	ReasonPrerequisiteFailed = "PREREQUISITE_FAILED"
	ReasonTargetMatch        = "TARGET_MATCH"
	ReasonRuleMatch          = "RULE_MATCH"
	ReasonFallthrough        = "FALLTHROUGH"
	ReasonError              = "ERROR"
)

// maxPrerequisiteDepth is how many prerequisite links the walk follows from
// the requested flag, which is at 0.
const maxPrerequisiteDepth = 32

// Reason says why a value was served, or which error stopped the evaluation.
type Reason struct {
	Kind      string
	RuleIndex *int
	FlagKey   string
	Code      string
}

// Result is one evaluation: the value, the variation it came from, and why.
type Result struct {
	Value          jsonval.Value
	VariationIndex *int
	Reason         Reason
}

// ToJSON writes the result as the object the SDK returns: `value`,
// `variationIndex` and `reason`.
func (r Result) ToJSON() jsonval.Object {
	index := jsonval.Value(jsonval.Null{})
	if r.VariationIndex != nil {
		index = jsonval.Number(float64(*r.VariationIndex))
	}
	reason := jsonval.Object{"kind": jsonval.String(r.Reason.Kind)}
	if r.Reason.RuleIndex != nil {
		reason["ruleIndex"] = jsonval.Number(float64(*r.Reason.RuleIndex))
	}
	if r.Reason.FlagKey != "" {
		reason["flagKey"] = jsonval.String(r.Reason.FlagKey)
	}
	if r.Reason.Code != "" {
		reason["code"] = jsonval.String(r.Reason.Code)
	}
	value := r.Value
	if value == nil {
		value = jsonval.Null{}
	}
	return jsonval.Object{
		"value":          value,
		"variationIndex": index,
		"reason":         reason,
	}
}

func failure(code string) Result {
	return Result{Value: jsonval.Null{}, Reason: Reason{Kind: ReasonError, Code: code}}
}

func served(variations []jsonval.Value, index int, reason Reason) Result {
	value := variations[index]
	if value == nil {
		value = jsonval.Null{}
	}
	return Result{Value: value, VariationIndex: &index, Reason: reason}
}

func off(header flagHeader, reason Reason) Result {
	if header.offVariation == nil {
		return Result{Value: jsonval.Null{}, Reason: reason}
	}
	return served(header.variations, *header.offVariation, reason)
}

// Evaluate evaluates one flag for one context. It never panics on bad input and
// never throws: every failure, a prerequisite cycle included, is an ERROR.
func (r *Ruleset) Evaluate(flagKey string, context jsonval.Value) Result {
	contexts, ok := normaliseContext(context)
	if !ok {
		return failure(CodeInvalidContext)
	}
	run := &run{
		ruleset:   r,
		contexts:  contexts,
		active:    map[string]bool{},
		completed: map[string]Result{},
	}
	return run.flag(flagKey, 0)
}

type run struct {
	ruleset   *Ruleset
	contexts  contextMap
	active    map[string]bool
	completed map[string]Result
}

func (rn *run) flag(key string, depth int) Result {
	if done, ok := rn.completed[key]; ok {
		return done
	}
	if rn.active[key] {
		return failure(CodePrerequisiteCycle)
	}
	if depth > maxPrerequisiteDepth {
		return failure(CodePrerequisiteDepthExceeded)
	}
	entry, ok := rn.ruleset.flags[key]
	if !ok {
		return failure(CodeFlagNotFound)
	}
	rn.active[key] = true
	result := rn.entry(entry, depth)
	delete(rn.active, key)
	rn.completed[key] = result
	return result
}

func (rn *run) entry(entry *flagEntry, depth int) Result {
	if !entry.headerOK {
		return failure(CodeMalformedFlag)
	}
	if !entry.header.on {
		return off(entry.header, Reason{Kind: ReasonOff})
	}
	if entry.code != "" {
		return failure(entry.code)
	}
	body := entry.body
	if blocked, stopped := rn.checkPrerequisites(entry.header, body, depth); stopped {
		return blocked
	}
	if variation, matched := rn.targeted(entry.targets); matched {
		return served(entry.header.variations, variation, Reason{Kind: ReasonTargetMatch})
	}
	for index, rule := range body.rules {
		if rn.ruleMatches(rule) {
			ruleIndex := index
			return rn.serve(entry.header, body, rule.serve,
				Reason{Kind: ReasonRuleMatch, RuleIndex: &ruleIndex})
		}
	}
	return rn.serve(entry.header, body, body.fallthroughSv, Reason{Kind: ReasonFallthrough})
}

func (rn *run) checkPrerequisites(header flagHeader, body *flagBody, depth int) (Result, bool) {
	for _, prerequisite := range body.prerequisites {
		result := rn.flag(prerequisite.flagKey, depth+1)
		if result.Reason.Kind == ReasonError {
			return result, true
		}
		if !prerequisiteMet(prerequisite, result) {
			return off(header, Reason{
				Kind:    ReasonPrerequisiteFailed,
				FlagKey: prerequisite.flagKey,
			}), true
		}
	}
	return Result{}, false
}

func prerequisiteMet(prerequisite prerequisite, result Result) bool {
	switch result.Reason.Kind {
	case ReasonTargetMatch, ReasonRuleMatch, ReasonFallthrough:
	default:
		return false
	}
	if result.VariationIndex == nil {
		return false
	}
	return float64(*result.VariationIndex) == prerequisite.variation
}

func (rn *run) serve(header flagHeader, body *flagBody, sv serve, reason Reason) Result {
	if sv.rollout == nil {
		return served(header.variations, *sv.variation, reason)
	}
	position := bucketOf(body.key, body.salt, sv.rollout.kind, rn.contexts)
	total := 0
	chosen := 0
	for _, entry := range sv.rollout.entries {
		chosen = entry.variation
		total += entry.weight
		if position < total {
			break
		}
	}
	return served(header.variations, chosen, reason)
}

func (rn *run) targeted(index map[string]map[string]targetHit) (int, bool) {
	var first *targetHit
	for kind, context := range rn.contexts {
		hits, ok := index[kind]
		if !ok {
			continue
		}
		hit, ok := hits[context.key]
		if !ok {
			continue
		}
		if first == nil || hit.order < first.order {
			first = &hit
		}
	}
	if first == nil {
		return 0, false
	}
	return first.variation, true
}

func (rn *run) ruleMatches(rule rule) bool {
	for _, c := range rule.clauses {
		if !rn.clauseMatches(c) {
			return false
		}
	}
	return true
}

func (rn *run) clauseMatches(c clause) bool {
	if c.op != "segmentMatch" {
		return rn.attributeMatches(c)
	}
	matched := false
	for _, key := range c.segmentKeys {
		entry, ok := rn.ruleset.segments[key]
		if !ok || !entry.ready {
			continue
		}
		if rn.segmentMatches(entry) {
			matched = true
			break
		}
	}
	if c.negate {
		return !matched
	}
	return matched
}

func (rn *run) segmentMatches(entry *segmentEntry) bool {
	if listsContext(entry.excluded, rn.contexts) {
		return false
	}
	if listsContext(entry.included, rn.contexts) {
		return true
	}
	for _, rule := range entry.segment.rules {
		if rn.segmentRuleMatches(rule, entry.segment) {
			return true
		}
	}
	return false
}

func (rn *run) segmentRuleMatches(rule segmentRule, seg *segment) bool {
	for _, c := range rule.clauses {
		if !rn.attributeMatches(c) {
			return false
		}
	}
	if rule.weight == nil {
		return true
	}
	position := bucketOf(seg.key, seg.salt, rule.rolloutKind, rn.contexts)
	return position < *rule.weight
}

// listsContext reports whether the context has, in any of its kinds, a key the
// list holds for that kind.
func listsContext(keys map[string]map[string]struct{}, contexts contextMap) bool {
	for kind, context := range contexts {
		list, ok := keys[kind]
		if !ok {
			continue
		}
		if _, listed := list[context.key]; listed {
			return true
		}
	}
	return false
}
