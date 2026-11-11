package eval

import (
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

type entryStatus uint8

const (
	statusMalformed entryStatus = iota // the header is invalid
	statusBroken                       // the header is valid, the body or a segment reference is not
	statusReady
)

// targetHit is a target that lists a key: its place in targets and the variation it serves.
type targetHit struct {
	order     int
	variation int
}

type flagEntry struct {
	status  entryStatus
	header  header
	code    string // statusBroken: the error code
	flag    *flag
	targets map[string]map[string]targetHit
}

type segmentEntry struct {
	segment   *segment
	malformed bool
}

// Ruleset is one environment's flags and segments, validated and indexed once. It is immutable after Load,
// so any number of goroutines may evaluate against it.
type Ruleset struct {
	flags    map[string]*flagEntry
	segments map[string]*segmentEntry
}

// Load validates and indexes a ruleset ({flags, segments}). It never fails: a bad flag becomes an ERROR
// when it is evaluated, a flags or segments that is not an array counts as empty, as does a ruleset that is
// not an object, the first of two entries with one key wins, and an entry with no string key is dropped.
func Load(input jsonv.Value) *Ruleset {
	rs := &Ruleset{flags: map[string]*flagEntry{}, segments: map[string]*segmentEntry{}}
	o, ok := input.AsObject()
	if !ok {
		return rs
	}
	if items, ok := field(o, "segments").AsArray(); ok {
		for _, item := range items {
			key, ok := entryKey(item)
			if !ok {
				continue
			}
			if _, dup := rs.segments[key]; dup {
				continue
			}
			s, valid := parseSegment(item)
			rs.segments[key] = &segmentEntry{segment: s, malformed: !valid}
		}
	}
	if items, ok := field(o, "flags").AsArray(); ok {
		for _, item := range items {
			key, ok := entryKey(item)
			if !ok {
				continue
			}
			if _, dup := rs.flags[key]; dup {
				continue
			}
			rs.flags[key] = rs.compileFlag(item)
		}
	}
	return rs
}

// entryKey reads the key of a flag or segment entry: a string, which may be empty (the entry is then
// indexed under "" and fails validation when evaluated, but nothing can ask for it by a non-empty key).
func entryKey(v jsonv.Value) (string, bool) {
	o, ok := v.AsObject()
	if !ok {
		return "", false
	}
	return field(o, "key").AsString()
}

func (rs *Ruleset) compileFlag(v jsonv.Value) *flagEntry {
	o, _ := v.AsObject()
	h, ok := parseHeader(o)
	if !ok {
		return &flagEntry{status: statusMalformed}
	}
	f := &flag{header: h}
	if !parseBody(o, f) {
		return &flagEntry{status: statusBroken, header: h, code: CodeMalformedFlag}
	}
	if code := rs.segmentProblem(f); code != "" {
		return &flagEntry{status: statusBroken, header: h, code: code, flag: f}
	}
	return &flagEntry{status: statusReady, header: h, flag: f, targets: targetIndex(f)}
}

// segmentProblem finds the first reference to a segment that is missing or malformed, in rule, clause, value order.
func (rs *Ruleset) segmentProblem(f *flag) string {
	for _, r := range f.rules {
		for _, c := range r.clauses {
			for _, key := range c.segments {
				s, ok := rs.segments[key]
				if !ok {
					return CodeSegmentNotFound
				}
				if s.malformed {
					return CodeMalformedSegment
				}
			}
		}
	}
	return ""
}

func targetIndex(f *flag) map[string]map[string]targetHit {
	index := map[string]map[string]targetHit{}
	for order, t := range f.targets {
		keys := index[t.kind]
		if keys == nil {
			keys = map[string]targetHit{}
			index[t.kind] = keys
		}
		for _, k := range t.keys {
			if _, ok := keys[k]; !ok {
				keys[k] = targetHit{order: order, variation: t.variation}
			}
		}
	}
	return index
}

// FlagVersion is the version of the flag with this key, when the ruleset has it and its header and body
// validate (a flag that refers to a missing or malformed segment still has one).
func (rs *Ruleset) FlagVersion(key string) (float64, bool) {
	e, ok := rs.flags[key]
	if !ok || e.flag == nil {
		return 0, false
	}
	return e.flag.version, true
}

// Count is the number of flags the ruleset holds (distinct keys).
func (rs *Ruleset) Count() int { return len(rs.flags) }
