package toggleslash

import (
	"math"
	"strings"
)

// A ruleset, checked and indexed once (docs/evaluation.md, "Loading a ruleset"). Loading never fails: a flag
// that does not pass is an ERROR result when it is evaluated, and a flag whose header passes still serves its
// off variation. Null and absent are the same for an optional field.

// attributeOps are the ops an attribute clause may name.
var attributeOps = map[string]bool{
	"in": true, "contains": true, "startsWith": true, "endsWith": true,
	"lt": true, "lte": true, "gt": true, "gte": true,
	"semverEq": true, "semverLt": true, "semverLte": true, "semverGt": true, "semverGte": true,
	"before": true, "after": true, "matches": true,
}

func isKeyValue(value any) bool {
	text, ok := value.(string)
	return ok && text != ""
}

// isCount reports whether a value is a non-negative integer; 2.0 is one, as it is in JSON.
func isCount(value any) bool {
	number, ok := numericValue(value)
	if !ok || !isFiniteNumber(number) || number < 0 {
		return false
	}
	return number == math.Trunc(number)
}

func isVariationIndex(value any, count int) bool {
	if !isCount(value) {
		return false
	}
	number, _ := numericValue(value)
	return number < float64(count)
}

func isStringArray(value any) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if _, ok := item.(string); !ok {
			return false
		}
	}
	return true
}

func isClauseValue(value any) bool {
	if _, ok := value.(bool); ok {
		return true
	}
	if _, ok := value.(string); ok {
		return true
	}
	number, ok := numericValue(value)
	return ok && isFiniteNumber(number)
}

func isNegate(value any) bool {
	if value == nil {
		return true
	}
	_, ok := value.(bool)
	return ok
}

func isAttributeName(value any) bool {
	if !isKeyValue(value) {
		return false
	}
	text, _ := value.(string)
	if !strings.HasPrefix(text, "/") {
		return true
	}
	_, ok := parseReference(text)
	return ok
}

func isAttributeClause(value any) bool {
	clause, ok := value.(map[string]any)
	if !ok {
		return false
	}
	op, ok := clause["op"].(string)
	if !ok || !attributeOps[op] {
		return false
	}
	values, ok := clause["values"].([]any)
	if !ok {
		return false
	}
	for _, item := range values {
		if !isClauseValue(item) {
			return false
		}
	}
	return isAttributeName(clause["attribute"]) &&
		isNegate(clause["negate"]) &&
		isOptionalKind(clause["contextKind"])
}

func isSegmentClause(value any) bool {
	clause, ok := value.(map[string]any)
	if !ok || clause["op"] != "segmentMatch" {
		return false
	}
	return isNegate(clause["negate"]) && isStringArray(clause["values"])
}

func isWeightedVariation(value any, count int) bool {
	entry, ok := value.(map[string]any)
	if !ok {
		return false
	}
	weight, ok := numericValue(entry["weight"])
	if !ok || !isCount(entry["weight"]) || weight > bucketCount {
		return false
	}
	return isVariationIndex(entry["variation"], count)
}

func isRollout(value any, count int) bool {
	rollout, ok := value.(map[string]any)
	if !ok || !isOptionalKind(rollout["contextKind"]) {
		return false
	}
	variations, ok := rollout["variations"].([]any)
	if !ok || len(variations) == 0 {
		return false
	}
	total := 0.0
	for _, item := range variations {
		if !isWeightedVariation(item, count) {
			return false
		}
		entry, _ := item.(map[string]any)
		weight, _ := numericValue(entry["weight"])
		total += weight
	}
	return total == bucketCount
}

func isServe(value any, count int) bool {
	serve, ok := value.(map[string]any)
	if !ok {
		return false
	}
	if serve["rollout"] == nil {
		return isVariationIndex(serve["variation"], count)
	}
	return serve["variation"] == nil && isRollout(serve["rollout"], count)
}

func isRule(value any, count int) bool {
	rule, ok := value.(map[string]any)
	if !ok {
		return false
	}
	clauses, ok := rule["clauses"].([]any)
	if !ok {
		return false
	}
	for _, clause := range clauses {
		if !isAttributeClause(clause) && !isSegmentClause(clause) {
			return false
		}
	}
	return isServe(rule, count)
}

func isTarget(value any, count int) bool {
	target, ok := value.(map[string]any)
	if !ok {
		return false
	}
	return isVariationIndex(target["variation"], count) &&
		isStringArray(target["contextKeys"]) &&
		isOptionalKind(target["contextKind"])
}

func isPrerequisite(value any) bool {
	entry, ok := value.(map[string]any)
	if !ok {
		return false
	}
	return isKeyValue(entry["flagKey"]) && isCount(entry["variation"])
}

func everyInList(value any, check func(any) bool) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if !check(item) {
			return false
		}
	}
	return true
}

// isFlagHeader is what serving the off variation needs. It is checked apart so that on: false survives a bad
// rule.
func isFlagHeader(value any) bool {
	flag, ok := value.(map[string]any)
	if !ok {
		return false
	}
	if _, ok := flag["on"].(bool); !ok {
		return false
	}
	variations, ok := flag["variations"].([]any)
	if !ok || len(variations) == 0 || !isKeyValue(flag["key"]) {
		return false
	}
	if flag["offVariation"] == nil {
		return true
	}
	return isVariationIndex(flag["offVariation"], len(variations))
}

func isFlagBody(flag map[string]any, count int) bool {
	if _, ok := flag["salt"].(string); !ok {
		return false
	}
	if !isCount(flag["version"]) {
		return false
	}
	if !everyInList(flag["targets"], func(item any) bool { return isTarget(item, count) }) {
		return false
	}
	if !everyInList(flag["prerequisites"], isPrerequisite) {
		return false
	}
	if !everyInList(flag["rules"], func(item any) bool { return isRule(item, count) }) {
		return false
	}
	return isServe(flag["fallthrough"], count)
}

func isSegmentRule(value any) bool {
	rule, ok := value.(map[string]any)
	if !ok {
		return false
	}
	if weight := rule["weight"]; weight != nil {
		number, ok := numericValue(weight)
		if !ok || !isCount(weight) || number > bucketCount {
			return false
		}
	}
	if !isOptionalKind(rule["rolloutContextKind"]) {
		return false
	}
	return everyInList(rule["clauses"], isAttributeClause)
}

func isContextList(value any) bool {
	entry, ok := value.(map[string]any)
	if !ok {
		return false
	}
	return isKind(entry["contextKind"]) && isStringArray(entry["values"])
}

func isOptionalContextLists(value any) bool {
	return value == nil || everyInList(value, isContextList)
}

func isSegment(value any) bool {
	segment, ok := value.(map[string]any)
	if !ok {
		return false
	}
	if _, ok := segment["salt"].(string); !ok {
		return false
	}
	return isKeyValue(segment["key"]) &&
		isStringArray(segment["included"]) &&
		isStringArray(segment["excluded"]) &&
		isOptionalContextLists(segment["includedContexts"]) &&
		isOptionalContextLists(segment["excludedContexts"]) &&
		everyInList(segment["rules"], isSegmentRule)
}

// keysByKind indexes included or excluded keys by the kind they belong to.
type keysByKind map[string]map[string]struct{}

func keysByKindOf(userKeys []any, lists []any) keysByKind {
	index := keysByKind{}
	users := map[string]struct{}{}
	for _, key := range userKeys {
		text, _ := key.(string)
		users[text] = struct{}{}
	}
	index[defaultKind] = users
	for _, raw := range lists {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := entry["contextKind"].(string)
		values, _ := entry["values"].([]any)
		set := index[kind]
		if set == nil {
			set = map[string]struct{}{}
			index[kind] = set
		}
		for _, value := range values {
			text, _ := value.(string)
			set[text] = struct{}{}
		}
	}
	return index
}

// readySegment is a valid segment with its included and excluded keys indexed by kind.
type readySegment struct {
	segment  map[string]any
	included keysByKind
	excluded keysByKind
}

func newReadySegment(segment map[string]any) *readySegment {
	included, _ := segment["included"].([]any)
	excluded, _ := segment["excluded"].([]any)
	contextsIn, _ := segment["includedContexts"].([]any)
	contextsOut, _ := segment["excludedContexts"].([]any)
	return &readySegment{
		segment:  segment,
		included: keysByKindOf(included, contextsIn),
		excluded: keysByKindOf(excluded, contextsOut),
	}
}

func listsContext(keys keysByKind, contexts map[string]kindContext) bool {
	for kind, context := range contexts {
		listed := keys[kind]
		if listed == nil {
			continue
		}
		if _, ok := listed[context.key]; ok {
			return true
		}
	}
	return false
}

// targetHit is a target that lists a key: its place in targets and the variation it serves.
type targetHit struct {
	order     int
	variation int
}

type targetIndex map[string]map[string]targetHit

func targetedVariation(targets targetIndex, contexts map[string]kindContext) (int, bool) {
	best, found := targetHit{}, false
	for kind, context := range contexts {
		hit, ok := targets[kind][context.key]
		if !ok {
			continue
		}
		if !found || hit.order < best.order {
			best, found = hit, true
		}
	}
	return best.variation, found
}

func targetIndexOf(flag map[string]any) targetIndex {
	index := targetIndex{}
	targets, _ := flag["targets"].([]any)
	for order, raw := range targets {
		target, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		kind := defaultKind
		if text, ok := target["contextKind"].(string); ok {
			kind = text
		}
		keys := index[kind]
		if keys == nil {
			keys = map[string]targetHit{}
			index[kind] = keys
		}
		variation, _ := numericValue(target["variation"])
		contextKeys, _ := target["contextKeys"].([]any)
		for _, rawKey := range contextKeys {
			key, _ := rawKey.(string)
			if _, seen := keys[key]; !seen {
				keys[key] = targetHit{order: order, variation: int(variation)}
			}
		}
	}
	return index
}

// flagStatus is how far a flag got when the ruleset was loaded.
type flagStatus uint8

const (
	flagMalformed flagStatus = iota // the header is invalid: ERROR MALFORMED_FLAG
	flagBroken                      // the header serves OFF; otherwise ERROR with code
	flagReady                       // evaluates
)

// flagEntry is a flag in a ruleset.
type flagEntry struct {
	status  flagStatus
	flag    map[string]any
	code    ErrorCode
	targets targetIndex
}

// versionOf is the flag's version when it is ready, and nil for a flag the evaluator cannot trust.
func (e *flagEntry) versionOf() *int64 {
	if e == nil || e.status != flagReady || e.flag == nil {
		return nil
	}
	number, ok := numericValue(e.flag["version"])
	if !ok {
		return nil
	}
	version := int64(number)
	return &version
}

// ruleset is {flags, segments} validated and indexed.
type ruleset struct {
	flags    map[string]*flagEntry
	segments map[string]*readySegment // a key whose segment is malformed maps to nil
}

func segmentProblem(flag map[string]any, segments map[string]*readySegment) (ErrorCode, bool) {
	rules, _ := flag["rules"].([]any)
	for _, rawRule := range rules {
		rule, ok := rawRule.(map[string]any)
		if !ok {
			continue
		}
		clauses, _ := rule["clauses"].([]any)
		for _, rawClause := range clauses {
			clause, ok := rawClause.(map[string]any)
			if !ok || clause["op"] != "segmentMatch" {
				continue
			}
			values, _ := clause["values"].([]any)
			for _, rawKey := range values {
				key, _ := rawKey.(string)
				ready, present := segments[key]
				if !present {
					return CodeSegmentNotFound, true
				}
				if ready == nil {
					return CodeMalformedSegment, true
				}
			}
		}
	}
	return "", false
}

func compileFlag(value any, segments map[string]*readySegment) *flagEntry {
	flag, ok := value.(map[string]any)
	if !ok || !isFlagHeader(flag) {
		return &flagEntry{status: flagMalformed}
	}
	variations, _ := flag["variations"].([]any)
	if !isFlagBody(flag, len(variations)) {
		return &flagEntry{status: flagBroken, flag: flag, code: CodeMalformedFlag}
	}
	if code, bad := segmentProblem(flag, segments); bad {
		return &flagEntry{status: flagBroken, flag: flag, code: code}
	}
	return &flagEntry{status: flagReady, flag: flag, targets: targetIndexOf(flag)}
}

// createRuleset validates and indexes a {flags, segments} value as the JSON reader hands it over. It never
// fails.
func createRuleset(data any) *ruleset {
	record, _ := data.(map[string]any)
	segments := map[string]*readySegment{}
	for _, item := range listOf(record["segments"]) {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		key, ok := entry["key"].(string)
		if !ok {
			continue
		}
		if _, seen := segments[key]; seen {
			continue
		}
		if isSegment(entry) {
			segments[key] = newReadySegment(entry)
		} else {
			segments[key] = nil
		}
	}
	flags := map[string]*flagEntry{}
	for _, item := range listOf(record["flags"]) {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		key, ok := entry["key"].(string)
		if !ok {
			continue
		}
		if _, seen := flags[key]; seen {
			continue
		}
		flags[key] = compileFlag(entry, segments)
	}
	return &ruleset{flags: flags, segments: segments}
}

// listOf is a JSON array or nothing, which is how a non-array flags or segments counts as empty.
func listOf(value any) []any {
	items, _ := value.([]any)
	return items
}
