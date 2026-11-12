package evaluator

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

const fixturesDir = "../../../../fixtures/evaluation"

func readJSON(t *testing.T, path string) jsonvalue.Value {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	v, err := jsonvalue.ParseBytes(data)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return v
}

func fixtureFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(fixturesDir, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixture files under %s (%v)", fixturesDir, err)
	}
	sort.Strings(files)
	return files
}

func str(v jsonvalue.Value) string {
	s, _ := v.AsString()
	return s
}

// compareResult returns "" when the result is the one a vector expects, else why not.
func compareResult(got Result, expected jsonvalue.Value) string {
	want := field(expected, "value")
	if !jsonvalue.Equal(got.Value, want) {
		return "value differs: got " + string(jsonvalue.Marshal(got.Value)) + " want " + string(jsonvalue.Marshal(want))
	}
	wantIndex := NoVariation
	if n, ok := field(expected, "variationIndex").AsNumber(); ok {
		wantIndex = int(n)
	}
	if got.VariationIndex != wantIndex {
		return "variationIndex differs"
	}
	reason := field(expected, "reason")
	if got.Reason.Kind != str(field(reason, "kind")) {
		return "reason kind is " + got.Reason.Kind + ", want " + str(field(reason, "kind"))
	}
	switch got.Reason.Kind {
	case ReasonRuleMatch:
		n, _ := field(reason, "ruleIndex").AsNumber()
		if got.Reason.RuleIndex != int(n) {
			return "ruleIndex differs"
		}
	case ReasonPrerequisiteFailed:
		if got.Reason.FlagKey != str(field(reason, "flagKey")) {
			return "reason flagKey differs"
		}
	case ReasonError:
		if got.Reason.Code != str(field(reason, "code")) {
			return "error code is " + got.Reason.Code + ", want " + str(field(reason, "code"))
		}
	}
	return ""
}

func TestVectors(t *testing.T) {
	total := 0
	for _, file := range fixtureFiles(t) {
		name := filepath.Base(file)
		if name == "buckets.json" {
			continue
		}
		doc := readJSON(t, file)
		vectors, ok := field(doc, "vectors").AsArray()
		if !ok {
			t.Fatalf("%s has no vectors", name)
		}
		for _, v := range vectors {
			total++
			ruleset := NewRuleset(jsonvalue.Object(
				jsonvalue.Member{Key: "flags", Value: field(v, "flags")},
				jsonvalue.Member{Key: "segments", Value: field(v, "segments")},
			))
			got := ruleset.Evaluate(str(field(v, "flagKey")), field(v, "context"))
			if why := compareResult(got, field(v, "expected")); why != "" {
				t.Errorf("%s: %s: %s", name, str(field(v, "name")), why)
			}
		}
	}
	if total == 0 {
		t.Fatal("no vectors were read")
	}
	t.Logf("vectors: %d", total)
}

func TestBucketPins(t *testing.T) {
	doc := readJSON(t, filepath.Join(fixturesDir, "buckets.json"))
	pins, ok := field(doc, "buckets").AsArray()
	if !ok || len(pins) == 0 {
		t.Fatal("no bucket pins were read")
	}
	for _, p := range pins {
		flagKey, salt, contextKey := str(field(p, "flagKey")), str(field(p, "salt")), str(field(p, "contextKey"))
		wantHash, _ := field(p, "uint32").AsNumber()
		wantBucket, _ := field(p, "bucket").AsNumber()
		if got := hashPrefix(flagKey, salt, contextKey); float64(got) != wantHash {
			t.Errorf("uint32 for %q %q %q: got %d want %v", flagKey, salt, contextKey, got, wantHash)
		}
		if got := Bucket(flagKey, salt, contextKey); float64(got) != wantBucket {
			t.Errorf("bucket for %q %q %q: got %d want %v", flagKey, salt, contextKey, got, wantBucket)
		}
	}
	t.Logf("pins: %d", len(pins))
}
