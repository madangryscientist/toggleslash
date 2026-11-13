package toggleslash

import (
	"bytes"
	"errors"
	"fmt"

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

// badRulesetPanic is a ruleset that made the reader or the evaluator's builder
// panic. It is errBadRuleset for every purpose, and carries the panic's value
// (FS-178).
type badRulesetPanic struct{ cause any }

func (e badRulesetPanic) Error() string {
	return fmt.Sprintf("%s: panic: %v", errBadRuleset, e.cause)
}

func (e badRulesetPanic) Unwrap() error { return errBadRuleset }

// newRuleset builds the evaluator's ruleset. A test replaces it to make the
// build panic.
var newRuleset = eval.NewRuleset

// parseRuleset is readRuleset with a panic while reading or building taken as a
// bad ruleset too, so the sync loop keeps the one it holds (contract 5, items 1
// and 5).
func parseRuleset(body []byte, etag string) (*snapshot, error) {
	var parsed *snapshot
	var err error
	if raised, panicked := catch(func() { parsed, err = readRuleset(body, etag) }); panicked {
		return nil, badRulesetPanic{cause: raised}
	}
	return parsed, err
}

// readRuleset reads a 200 body as JSON and builds a ruleset from it. Only a
// JSON object whose `flags` and `segments` are both arrays is a ruleset; a bad
// flag or segment inside that shape is the evaluator's business, not the
// loader's.
func readRuleset(body []byte, etag string) (*snapshot, error) {
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
	return &snapshot{ruleset: newRuleset(flags, segments), etag: etag}, nil
}
