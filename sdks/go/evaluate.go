package toggleslash

// Evaluation of one flag for one context, in the order docs/evaluation.md fixes. It never panics.

const maxPrerequisiteDepth = 32

// outcome is what an evaluation answers: the served JSON value, its variation index (when it has one) and the
// reason. ruleIndex is set for RULE_MATCH, flagKey for PREREQUISITE_FAILED and code for ERROR.
type outcome struct {
	value     any
	index     int
	hasIndex  bool
	kind      ReasonKind
	ruleIndex int
	flagKey   string
	code      ErrorCode
}

type evaluationRun struct {
	ruleset   *ruleset
	contexts  map[string]kindContext
	active    map[string]bool
	completed map[string]outcome
}

func failure(code ErrorCode) outcome {
	return outcome{kind: ReasonError, code: code}
}

func served(variations []any, index int, kind ReasonKind) outcome {
	var value any
	if index >= 0 && index < len(variations) {
		value = variations[index]
	}
	return outcome{value: value, index: index, hasIndex: true, kind: kind}
}

func offResult(flag map[string]any, reason outcome) outcome {
	variations, _ := flag["variations"].([]any)
	if flag["offVariation"] == nil {
		return outcome{kind: reason.kind, flagKey: reason.flagKey}
	}
	index, _ := numericValue(flag["offVariation"])
	result := served(variations, int(index), reason.kind)
	result.flagKey = reason.flagKey
	return result
}

func rolloutVariation(rollout map[string]any, position int) int {
	chosen := 0
	cumulative := 0.0
	variations, _ := rollout["variations"].([]any)
	for _, raw := range variations {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		index, _ := numericValue(entry["variation"])
		weight, _ := numericValue(entry["weight"])
		chosen = int(index)
		cumulative += weight
		if float64(position) < cumulative {
			break
		}
	}
	return chosen
}

func serveResult(flag map[string]any, serve map[string]any, run *evaluationRun, kind ReasonKind) outcome {
	variations, _ := flag["variations"].([]any)
	index := 0
	if rollout, ok := serve["rollout"].(map[string]any); ok {
		flagKey, _ := flag["key"].(string)
		salt, _ := flag["salt"].(string)
		rolloutKind, _ := rollout["contextKind"].(string)
		position := kindBucket(flagKey, salt, rolloutKind, run.contexts)
		index = rolloutVariation(rollout, position)
	} else {
		number, _ := numericValue(serve["variation"])
		index = int(number)
	}
	return served(variations, index, kind)
}

func segmentRuleMatches(rule map[string]any, ready *readySegment, run *evaluationRun) bool {
	clauses, _ := rule["clauses"].([]any)
	for _, raw := range clauses {
		clause, ok := raw.(map[string]any)
		if !ok || !attributeClauseMatches(clause, run.contexts) {
			return false
		}
	}
	if rule["weight"] == nil {
		return true
	}
	weight, _ := numericValue(rule["weight"])
	key, _ := ready.segment["key"].(string)
	salt, _ := ready.segment["salt"].(string)
	rolloutKind, _ := rule["rolloutContextKind"].(string)
	position := kindBucket(key, salt, rolloutKind, run.contexts)
	return float64(position) < weight
}

func segmentMatches(ready *readySegment, run *evaluationRun) bool {
	if listsContext(ready.excluded, run.contexts) {
		return false
	}
	if listsContext(ready.included, run.contexts) {
		return true
	}
	rules, _ := ready.segment["rules"].([]any)
	for _, raw := range rules {
		rule, ok := raw.(map[string]any)
		if ok && segmentRuleMatches(rule, ready, run) {
			return true
		}
	}
	return false
}

func clauseMatches(clause map[string]any, run *evaluationRun) bool {
	if clause["op"] != "segmentMatch" {
		return attributeClauseMatches(clause, run.contexts)
	}
	matched := false
	values, _ := clause["values"].([]any)
	for _, rawKey := range values {
		key, _ := rawKey.(string)
		ready, present := run.ruleset.segments[key]
		if present && ready != nil && segmentMatches(ready, run) {
			matched = true
			break
		}
	}
	if negate, ok := clause["negate"].(bool); ok && negate {
		return !matched
	}
	return matched
}

func prerequisiteMet(variation float64, result outcome) bool {
	switch result.kind {
	case ReasonTargetMatch, ReasonRuleMatch, ReasonFallthrough:
	default:
		return false
	}
	return result.hasIndex && float64(result.index) == variation
}

func checkPrerequisites(flag map[string]any, run *evaluationRun, depth int) (outcome, bool) {
	prerequisites, _ := flag["prerequisites"].([]any)
	for _, raw := range prerequisites {
		prerequisite, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		key, _ := prerequisite["flagKey"].(string)
		result := evaluateFlag(run, key, depth+1)
		if result.kind == ReasonError {
			return result, true
		}
		variation, _ := numericValue(prerequisite["variation"])
		if !prerequisiteMet(variation, result) {
			return offResult(flag, outcome{kind: ReasonPrerequisiteFailed, flagKey: key}), true
		}
	}
	return outcome{}, false
}

func evaluateEntry(entry *flagEntry, run *evaluationRun, depth int) outcome {
	flag := entry.flag
	if entry.status == flagMalformed || flag == nil {
		return failure(CodeMalformedFlag)
	}
	if on, _ := flag["on"].(bool); !on {
		return offResult(flag, outcome{kind: ReasonOff})
	}
	if entry.status == flagBroken {
		return failure(entry.code)
	}
	if blocked, stopped := checkPrerequisites(flag, run, depth); stopped {
		return blocked
	}
	if variation, found := targetedVariation(entry.targets, run.contexts); found {
		variations, _ := flag["variations"].([]any)
		return served(variations, variation, ReasonTargetMatch)
	}
	rules, _ := flag["rules"].([]any)
	for index, rawRule := range rules {
		rule, ok := rawRule.(map[string]any)
		if !ok {
			continue
		}
		clauses, _ := rule["clauses"].([]any)
		all := true
		for _, rawClause := range clauses {
			clause, ok := rawClause.(map[string]any)
			if !ok || !clauseMatches(clause, run) {
				all = false
				break
			}
		}
		if all {
			result := serveResult(flag, rule, run, ReasonRuleMatch)
			result.ruleIndex = index
			return result
		}
	}
	fallthroughServe, _ := flag["fallthrough"].(map[string]any)
	return serveResult(flag, fallthroughServe, run, ReasonFallthrough)
}

func evaluateFlag(run *evaluationRun, key string, depth int) outcome {
	if completed, ok := run.completed[key]; ok {
		return completed
	}
	if run.active[key] {
		return failure(CodePrerequisiteCycle)
	}
	if depth > maxPrerequisiteDepth {
		return failure(CodePrerequisiteDepthExceeded)
	}
	entry, ok := run.ruleset.flags[key]
	if !ok {
		return failure(CodeFlagNotFound)
	}
	run.active[key] = true
	result := evaluateEntry(entry, run, depth)
	delete(run.active, key)
	run.completed[key] = result
	return result
}

// evaluateOne evaluates one flag for one context. Every failure, a prerequisite cycle included, is an ERROR
// outcome.
func evaluateOne(ruleset *ruleset, flagKey string, context any) outcome {
	contexts, ok := normaliseContext(context)
	if !ok {
		return failure(CodeInvalidContext)
	}
	run := &evaluationRun{
		ruleset:   ruleset,
		contexts:  contexts,
		active:    map[string]bool{},
		completed: map[string]outcome{},
	}
	return evaluateFlag(run, flagKey, 0)
}
