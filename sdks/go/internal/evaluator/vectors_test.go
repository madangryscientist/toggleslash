package evaluator

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

const fixturesDir = "../../../../fixtures/evaluation"

func readFixture(t *testing.T, name string) jsonv.Value {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixturesDir, name))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	v, err := jsonv.Parse(data)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return v
}

func field(t *testing.T, where string, obj jsonv.Value, name string) jsonv.Value {
	t.Helper()
	v, ok := obj.Get(name)
	if !ok {
		t.Fatalf("%s: no %q", where, name)
	}
	return v
}

// describe renders a result the way the vectors write it, for a failure message.
func describe(r Result) string {
	reason := string(r.Reason.Kind)
	switch r.Reason.Kind {
	case ReasonError:
		reason += ":" + string(r.Reason.Code)
	case ReasonRuleMatch:
		reason += fmt.Sprintf("[%d]", r.Reason.RuleIndex)
	case ReasonPrerequisiteFailed:
		reason += ":" + r.Reason.FlagKey
	}
	return fmt.Sprintf("value=%s index=%d reason=%s", jsonv.Marshal(r.Value), r.Index, reason)
}

// matches compares a result to a vector's expected object. Numbers compare as IEEE doubles through
// jsonv.Equal, so -0 and 0 are the same value.
func matches(r Result, expected jsonv.Value) bool {
	want, _ := expected.Get("value")
	if !jsonv.Equal(r.Value, want) {
		return false
	}
	idx, _ := expected.Get("variationIndex")
	if idx.IsNull() != (r.Index < 0) {
		return false
	}
	if n, ok := idx.Number(); ok && float64(r.Index) != n {
		return false
	}
	reason, _ := expected.Get("reason")
	kind, _ := reason.Get("kind")
	if k, _ := kind.Str(); k != string(r.Reason.Kind) {
		return false
	}
	switch r.Reason.Kind {
	case ReasonError:
		code, _ := reason.Get("code")
		c, _ := code.Str()
		return c == string(r.Reason.Code)
	case ReasonRuleMatch:
		ri, _ := reason.Get("ruleIndex")
		n, ok := ri.Number()
		return ok && n == float64(r.Reason.RuleIndex)
	case ReasonPrerequisiteFailed:
		fk, _ := reason.Get("flagKey")
		s, _ := fk.Str()
		return s == r.Reason.FlagKey
	}
	return true
}

func TestVectors(t *testing.T) {
	entries, err := os.ReadDir(fixturesDir)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") && e.Name() != "buckets.json" {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatal("no vector files found")
	}
	total := 0
	seen := map[string]bool{}
	for _, file := range files {
		doc := readFixture(t, file)
		list, ok := field(t, file, doc, "vectors").Array()
		if !ok {
			t.Fatalf("%s: vectors is not an array", file)
		}
		for i, v := range list {
			name, _ := field(t, file, v, "name").Str()
			where := fmt.Sprintf("%s: %s", file, name)
			if name == "" {
				where = fmt.Sprintf("%s: vector %d", file, i)
			}
			flagKey, _ := field(t, where, v, "flagKey").Str()
			ruleset := jsonv.NewObject(map[string]jsonv.Value{
				"flags":    field(t, where, v, "flags"),
				"segments": field(t, where, v, "segments"),
			})
			got := Load(ruleset).Evaluate(flagKey, field(t, where, v, "context"))
			expected := field(t, where, v, "expected")
			if !matches(got, expected) {
				t.Errorf("%s\n  got  %s\n  want %s", where, describe(got), jsonv.Marshal(expected))
			}
			total++
			seen[reasonName(got)] = true
		}
	}
	if total == 0 {
		t.Fatal("no vectors found")
	}
	for _, want := range []string{
		"ERROR:FLAG_NOT_FOUND", "ERROR:INVALID_CONTEXT", "ERROR:MALFORMED_FLAG", "ERROR:MALFORMED_SEGMENT",
		"ERROR:PREREQUISITE_CYCLE", "ERROR:PREREQUISITE_DEPTH_EXCEEDED", "ERROR:SEGMENT_NOT_FOUND",
		"FALLTHROUGH", "OFF", "PREREQUISITE_FAILED", "RULE_MATCH", "TARGET_MATCH",
	} {
		if !seen[want] {
			t.Errorf("no vector reaches %s", want)
		}
	}
	fmt.Printf("vectors: %d\n", total)
}

func reasonName(r Result) string {
	if r.Reason.Kind == ReasonError {
		return "ERROR:" + string(r.Reason.Code)
	}
	return string(r.Reason.Kind)
}

func TestBucketPins(t *testing.T) {
	doc := readFixture(t, "buckets.json")
	list, ok := field(t, "buckets.json", doc, "buckets").Array()
	if !ok || len(list) == 0 {
		t.Fatal("buckets.json has no pins")
	}
	for i, p := range list {
		where := fmt.Sprintf("buckets.json: pin %d", i)
		flagKey, _ := field(t, where, p, "flagKey").Str()
		salt, _ := field(t, where, p, "salt").Str()
		contextKey, _ := field(t, where, p, "contextKey").Str()
		wantU, _ := field(t, where, p, "uint32").Number()
		wantB, _ := field(t, where, p, "bucket").Number()
		u, b := Hash(flagKey, salt, contextKey)
		if float64(u) != wantU || float64(b) != wantB {
			t.Errorf("%s (%q, %q, %q): got uint32 %d bucket %d, want %v and %v", where, flagKey, salt, contextKey, u, b, wantU, wantB)
		}
	}
	fmt.Printf("pins: %d\n", len(list))
}
