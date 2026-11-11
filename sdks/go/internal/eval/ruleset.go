// Package eval is the Go implementation of docs/evaluation.md: a ruleset loaded from JSON, and
// Evaluate. It has no network, clock or global state, so it is safe to call from many goroutines on one
// Ruleset, which is immutable once built.
package eval

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"

// keysByKind holds keys by kind: the kind, then the keys of that kind.
type keysByKind map[string]map[string]struct{}

type readySegment struct {
	seg      *segment
	included keysByKind
	excluded keysByKind
}

// segmentEntry is a segment that validated, or one that did not.
type segmentEntry struct {
	malformed bool
	ready     *readySegment
}

// targetHit is a target that lists a key: its place in `targets` and the variation it serves.
type targetHit struct {
	order     int
	variation int
}

type targetIndex map[string]map[string]targetHit

type entryStatus uint8

const (
	statusMalformed entryStatus = iota
	statusBroken
	statusReady
)

type flagEntry struct {
	status  entryStatus
	header  flagHeader
	code    ErrorCode // when broken
	flag    *flag
	targets targetIndex
}

// Ruleset is a validated, indexed ruleset.
type Ruleset struct {
	flags    map[string]*flagEntry
	segments map[string]segmentEntry
}

func buildKeys(userKeys []string, lists []contextList) keysByKind {
	index := keysByKind{defaultKind: {}}
	for _, k := range userKeys {
		index[defaultKind][k] = struct{}{}
	}
	for _, l := range lists {
		keys, ok := index[l.kind]
		if !ok {
			keys = map[string]struct{}{}
			index[l.kind] = keys
		}
		for _, v := range l.values {
			keys[v] = struct{}{}
		}
	}
	return index
}

func compileSegment(v jsonvalue.Value) segmentEntry {
	s, ok := readSegment(v)
	if !ok {
		return segmentEntry{malformed: true}
	}
	return segmentEntry{ready: &readySegment{
		seg:      s,
		included: buildKeys(s.included, s.incCtx),
		excluded: buildKeys(s.excluded, s.excCtx),
	}}
}

// listsContext reports whether the context has, in any of its kinds, a key that keys lists for that kind.
func listsContext(keys keysByKind, contexts Contexts) bool {
	for kind, c := range contexts {
		if _, ok := keys[kind][c.key]; ok {
			return true
		}
	}
	return false
}

// segmentProblem is the first reference to a segment that is missing or malformed, in rule, clause,
// value order.
func segmentProblem(f *flag, segments map[string]segmentEntry) ErrorCode {
	for _, r := range f.rules {
		for _, c := range r.clauses {
			if c.op != opSegmentMatch {
				continue
			}
			for _, key := range c.segments {
				entry, ok := segments[key]
				if !ok {
					return SegmentNotFound
				}
				if entry.malformed {
					return MalformedSegment
				}
			}
		}
	}
	return ""
}

func buildTargetIndex(f *flag) targetIndex {
	index := targetIndex{}
	for order, t := range f.targets {
		kind := t.kind
		if kind == "" {
			kind = defaultKind
		}
		keys, ok := index[kind]
		if !ok {
			keys = map[string]targetHit{}
			index[kind] = keys
		}
		for _, k := range t.keys {
			if _, taken := keys[k]; !taken {
				keys[k] = targetHit{order: order, variation: t.variation}
			}
		}
	}
	return index
}

// targetedVariation is the variation of the first target in array order that matches the context.
func targetedVariation(targets targetIndex, contexts Contexts) (int, bool) {
	first := targetHit{order: -1}
	for kind, c := range contexts {
		if hit, ok := targets[kind][c.key]; ok && (first.order < 0 || hit.order < first.order) {
			first = hit
		}
	}
	return first.variation, first.order >= 0
}

func compileFlag(v jsonvalue.Value, segments map[string]segmentEntry) *flagEntry {
	header, ok := readFlagHeader(v)
	if !ok {
		return &flagEntry{status: statusMalformed}
	}
	f, ok := readFlagBody(v, header)
	if !ok {
		return &flagEntry{status: statusBroken, header: header, code: MalformedFlag}
	}
	if code := segmentProblem(f, segments); code != "" {
		return &flagEntry{status: statusBroken, header: header, code: code}
	}
	return &flagEntry{status: statusReady, header: header, flag: f, targets: buildTargetIndex(f)}
}

// NewRuleset validates and indexes a ruleset (`{ flags, segments }`, as JSON hands it over) once, so each
// Evaluate skips that work. It never fails: a bad flag becomes an ERROR result when it is evaluated.
func NewRuleset(input jsonvalue.Value) *Ruleset {
	rs := &Ruleset{flags: map[string]*flagEntry{}, segments: map[string]segmentEntry{}}
	rawFlags, _ := input.Get("flags")
	rawSegments, _ := input.Get("segments")
	// The first entry with a key wins, even when it is the malformed one. An entry with no string key
	// cannot be referred to and is dropped.
	for _, item := range rawSegments.A {
		key, _ := item.Get("key")
		if key.Kind != jsonvalue.String {
			continue
		}
		if _, taken := rs.segments[key.S]; !taken {
			rs.segments[key.S] = compileSegment(item)
		}
	}
	for _, item := range rawFlags.A {
		key, _ := item.Get("key")
		if key.Kind != jsonvalue.String {
			continue
		}
		if _, taken := rs.flags[key.S]; !taken {
			rs.flags[key.S] = compileFlag(item, rs.segments)
		}
	}
	return rs
}

// FlagCount is the number of distinct flag keys the ruleset holds.
func (rs *Ruleset) FlagCount() int { return len(rs.flags) }

// Version is the flag's `version` when the ruleset has the flag and it validated far enough to be served.
func (rs *Ruleset) Version(flagKey string) (float64, bool) {
	entry, ok := rs.flags[flagKey]
	if !ok || entry.status != statusReady {
		return 0, false
	}
	return entry.flag.version, true
}
