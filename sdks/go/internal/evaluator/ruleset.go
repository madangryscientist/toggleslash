package evaluator

import "github.com/madangryscientist/toggleslash/sdks/go/internal/wjson"

type entryStatus uint8

const (
	entryMalformed entryStatus = iota
	entryBroken
	entryReady
)

// targetHit is a target that lists a key: its place in targets and the
// variation it serves.
type targetHit struct {
	order     int
	variation int
}

// flagEntry is one flag after checking: malformed (no header), broken (a
// header, and the code its body or a segment reference gives) or ready.
type flagEntry struct {
	status  entryStatus
	header  *flagHeader
	code    ErrorCode
	body    *flagBody
	targets map[string]map[string]targetHit // by kind, then key
}

// segmentEntry is a segment, or nil segment for a malformed one.
type segmentEntry struct{ segment *segment }

// Ruleset is a ruleset checked and indexed once, so each evaluation skips
// that work. It is immutable and safe to share between goroutines.
type Ruleset struct {
	flags    map[string]*flagEntry
	segments map[string]*segmentEntry
}

// indexByKey keeps, for each item that is an object with a string "key", the
// first one of that key. Anything that is not an array is empty.
func indexByKey(items wjson.Value, add func(key string, item wjson.Value)) {
	seen := map[string]bool{}
	for _, item := range items.Array() {
		o := item.Object()
		if o == nil {
			continue
		}
		k, ok := o.Get("key")
		if !ok || k.Kind() != wjson.KindString || seen[k.String()] {
			continue
		}
		seen[k.String()] = true
		add(k.String(), item)
	}
}

// NewRuleset checks and indexes a ruleset value ({"flags": [...], "segments":
// [...]}). It never fails: a bad flag becomes an ERROR result when evaluated.
func NewRuleset(input wjson.Value) *Ruleset {
	rs := &Ruleset{flags: map[string]*flagEntry{}, segments: map[string]*segmentEntry{}}
	o := input.Object()
	if o == nil {
		return rs
	}
	indexByKey(field(o, "segments"), func(key string, item wjson.Value) {
		seg, _ := compileSegment(item)
		rs.segments[key] = &segmentEntry{segment: seg}
	})
	indexByKey(field(o, "flags"), func(key string, item wjson.Value) {
		rs.flags[key] = rs.compileFlag(item)
	})
	return rs
}

// segmentProblem is the first reference to a segment that is missing or
// malformed, in rule, clause, value order.
func (rs *Ruleset) segmentProblem(body *flagBody) ErrorCode {
	for _, r := range body.rules {
		for _, c := range r.clauses {
			for _, key := range c.segments {
				entry, ok := rs.segments[key]
				if !ok {
					return ErrSegmentNotFound
				}
				if entry.segment == nil {
					return ErrMalformedSegment
				}
			}
		}
	}
	return ""
}

func targetIndex(body *flagBody) map[string]map[string]targetHit {
	index := map[string]map[string]targetHit{}
	for order, t := range body.targets {
		kind := t.contextKind
		if kind == "" {
			kind = defaultKind
		}
		keys := index[kind]
		if keys == nil {
			keys = map[string]targetHit{}
			index[kind] = keys
		}
		for _, k := range t.contextKeys {
			if _, taken := keys[k]; !taken {
				keys[k] = targetHit{order: order, variation: t.variation}
			}
		}
	}
	return index
}

func (rs *Ruleset) compileFlag(item wjson.Value) *flagEntry {
	o := item.Object()
	header, ok := compileHeader(o)
	if !ok {
		return &flagEntry{status: entryMalformed}
	}
	body, ok := compileBody(o, len(header.variations))
	if !ok {
		return &flagEntry{status: entryBroken, header: header, code: ErrMalformedFlag}
	}
	if code := rs.segmentProblem(body); code != "" {
		return &flagEntry{status: entryBroken, header: header, code: code}
	}
	return &flagEntry{status: entryReady, header: header, body: body, targets: targetIndex(body)}
}

// FlagVersion is the flag's version in this ruleset, and false when the
// ruleset has no such flag or the flag is malformed.
func (rs *Ruleset) FlagVersion(key string) (int, bool) {
	entry, ok := rs.flags[key]
	if !ok || entry.status != entryReady {
		return 0, false
	}
	v := entry.body.version
	if v > 1<<53 {
		v = 1 << 53
	}
	return int(v), true
}

// FlagCount is how many flags the ruleset indexes.
func (rs *Ruleset) FlagCount() int { return len(rs.flags) }

func listsContext(keys keysByKind, cs contexts) bool {
	for i := range cs {
		if _, ok := keys[cs[i].kind][cs[i].key]; ok {
			return true
		}
	}
	return false
}

// targetedVariation is the variation of the first target in array order that
// matches the context.
func targetedVariation(targets map[string]map[string]targetHit, cs contexts) (int, bool) {
	var first *targetHit
	for i := range cs {
		hit, ok := targets[cs[i].kind][cs[i].key]
		if ok && (first == nil || hit.order < first.order) {
			h := hit
			first = &h
		}
	}
	if first == nil {
		return 0, false
	}
	return first.variation, true
}
