package toggleslash

import (
	"bytes"
	"errors"

	"github.com/madangryscientist/toggleslash/sdks/go/eval"
	"github.com/madangryscientist/toggleslash/sdks/go/jsonval"
)

// snapshot is the ruleset held and the ETag the server gave it. One Store
// swaps both together, so a reader sees the old pair or the new one (contract
// 6.3).
type snapshot struct {
	ruleset *eval.Ruleset
	etag    string
}

// errBadRuleset is a 200 whose body is not a ruleset (contract 5, item 5).
var errBadRuleset = errors.New("toggleslash: bad ruleset")

// parseRuleset reads a 200 body as JSON and builds a ruleset from it. Only a
// JSON object whose `flags` and `segments` are both arrays is a ruleset; a bad
// flag or segment inside that shape is the evaluator's business, not the
// loader's.
func parseRuleset(body []byte, etag string) (*snapshot, error) {
	value, err := jsonval.Read(bytes.NewReader(body), maxRulesetDepth)
	if err != nil {
		return nil, errBadRuleset
	}
	object, ok := value.(jsonval.Object)
	if !ok {
		return nil, errBadRuleset
	}
	flags, flagsOK := object["flags"].(jsonval.Array)
	segments, segmentsOK := object["segments"].(jsonval.Array)
	if !flagsOK || !segmentsOK {
		return nil, errBadRuleset
	}
	return &snapshot{ruleset: eval.NewRuleset(flags, segments), etag: etag}, nil
}
