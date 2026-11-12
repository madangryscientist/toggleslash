package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"

type flagStatus uint8

const (
	statusMalformed flagStatus = iota // the header is invalid: no off variation can be served
	statusBroken                      // the header is valid and something later is not
	statusReady
)

type flagEntry struct {
	status flagStatus
	code   string // for a broken flag
	flag   *flag  // the header always, the whole flag when ready
	hits   map[string]map[string]targetHit
}

// targetHit is a target that lists a key: its place in `targets` and the variation it serves.
type targetHit struct {
	order     int
	variation int
}

type segmentEntry struct {
	segment *segment // nil when malformed
}

// Ruleset is one environment's flags and segments, validated once so each evaluation skips that work. It is
// immutable and safe for concurrent use.
type Ruleset struct {
	flags    map[string]*flagEntry
	segments map[string]*segmentEntry
}

// NewRuleset validates and indexes a ruleset (`{ flags, segments }`, as JSON hands it over). It never fails: a bad
// flag becomes an ERROR result when it is evaluated, a `flags` or `segments` that is not an array counts as
// empty, and so does a ruleset that is not an object. If two entries share a key the first one wins.
func NewRuleset(input jsonvalue.Value) *Ruleset {
	rs := &Ruleset{flags: map[string]*flagEntry{}, segments: map[string]*segmentEntry{}}
	if !input.IsObject() {
		return rs
	}
	if items, ok := field(input, "segments").AsArray(); ok {
		for _, item := range items {
			key, ok := keyOf(item)
			if _, taken := rs.segments[key]; ok && !taken {
				s, _ := readSegment(item)
				rs.segments[key] = &segmentEntry{segment: s}
			}
		}
	}
	if items, ok := field(input, "flags").AsArray(); ok {
		for _, item := range items {
			key, ok := keyOf(item)
			if _, taken := rs.flags[key]; ok && !taken {
				rs.flags[key] = rs.compileFlag(item)
			}
		}
	}
	return rs
}

// keyOf is the string `key` of an object, which may be empty. An entry without one cannot be referred to.
func keyOf(item jsonvalue.Value) (string, bool) {
	if !item.IsObject() {
		return "", false
	}
	return field(item, "key").AsString()
}

func (rs *Ruleset) compileFlag(v jsonvalue.Value) *flagEntry {
	f, ok := readFlagHeader(v)
	if !ok {
		return &flagEntry{status: statusMalformed}
	}
	if !readFlagBody(v, f) {
		return &flagEntry{status: statusBroken, code: CodeMalformedFlag, flag: f}
	}
	if code := rs.segmentProblem(f); code != "" {
		return &flagEntry{status: statusBroken, code: code, flag: f}
	}
	return &flagEntry{status: statusReady, flag: f, hits: targetIndex(f)}
}

// segmentProblem is the code for the first reference to a segment that is missing or malformed, in rule, clause
// and value order, or "" when there is none.
func (rs *Ruleset) segmentProblem(f *flag) string {
	for _, r := range f.rules {
		for _, c := range r.clauses {
			if !c.segment {
				continue
			}
			for _, key := range c.segmentKeys {
				entry, ok := rs.segments[key]
				if !ok {
					return CodeSegmentNotFound
				}
				if entry.segment == nil {
					return CodeMalformedSegment
				}
			}
		}
	}
	return ""
}

// targetIndex lists targets by kind, then by key; the first target in array order holds each key.
func targetIndex(f *flag) map[string]map[string]targetHit {
	index := map[string]map[string]targetHit{}
	for order, t := range f.targets {
		keys := index[t.kind]
		if keys == nil {
			keys = map[string]targetHit{}
			index[t.kind] = keys
		}
		for _, k := range t.keys {
			if _, taken := keys[k]; !taken {
				keys[k] = targetHit{order: order, variation: t.variation}
			}
		}
	}
	return index
}

// Version is the flag's `version`, and false when the ruleset has no such flag or the flag is malformed: its
// own shape is invalid, or a segment it names is missing or malformed. A flag whose problem shows only at
// evaluation (a prerequisite cycle, say) keeps its version.
func (rs *Ruleset) Version(key string) (float64, bool) {
	entry, ok := rs.flags[key]
	if !ok || entry.status != statusReady {
		return 0, false
	}
	return entry.flag.version, true
}

// Len is the number of flags the ruleset holds, malformed ones included.
func (rs *Ruleset) Len() int { return len(rs.flags) }
