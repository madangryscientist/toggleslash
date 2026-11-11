package evaluator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// fixtures is the shared evaluation vectors, at the repository root.
const fixtures = "../../../../fixtures/evaluation"

func readJSON(t *testing.T, path string) jsonv.Value {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	v, err := jsonv.ParseBody(body)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return v
}

func sameResult(got Result, want jsonv.Value) string {
	expected := want.Field("reason")
	if kind := expected.Field("kind").String(); kind != string(got.Reason.Kind) {
		return "reason kind " + string(got.Reason.Kind) + ", want " + kind
	}
	switch got.Reason.Kind {
	case ReasonRuleMatch:
		if float64(got.Reason.RuleIndex) != expected.Field("ruleIndex").Number() {
			return "rule index"
		}
	case ReasonPrerequisiteFailed:
		if got.Reason.FlagKey != expected.Field("flagKey").String() {
			return "prerequisite flag key " + got.Reason.FlagKey
		}
	case ReasonError:
		if string(got.Reason.Code) != expected.Field("code").String() {
			return "error code " + string(got.Reason.Code)
		}
	}
	index := want.Field("variationIndex")
	if index.Kind() == jsonv.Null {
		if got.VariationIndex != NoVariation {
			return "served a variation"
		}
	} else if float64(got.VariationIndex) != index.Number() {
		return "variation index"
	}
	if !jsonv.Equal(got.Value, want.Field("value")) {
		return "value"
	}
	return ""
}

// TestVectors runs every vector in fixtures/evaluation except the bucket pins, and fails when it
// finds none, so a wrong path cannot pass.
func TestVectors(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(fixtures, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no vector files under %s", fixtures)
	}
	count := 0
	for _, file := range files {
		if filepath.Base(file) == "buckets.json" {
			continue
		}
		for _, vec := range readJSON(t, file).Field("vectors").Items() {
			count++
			name := filepath.Base(file) + ": " + vec.Field("name").String()
			rs := Load(jsonv.NewObject(
				[]string{"flags", "segments"},
				[]jsonv.Value{vec.Field("flags"), vec.Field("segments")},
			))
			got := Evaluate(rs, vec.Field("flagKey").String(), vec.Field("context"))
			if problem := sameResult(got, vec.Field("expected")); problem != "" {
				t.Errorf("%s: %s (got %+v)", name, problem, got)
			}
		}
	}
	if count == 0 {
		t.Fatal("no vectors")
	}
	t.Logf("vectors: %d", count)
}

// TestBucketPins checks Hash32 and Bucket against fixtures/evaluation/buckets.json.
func TestBucketPins(t *testing.T) {
	pins := readJSON(t, filepath.Join(fixtures, "buckets.json")).Field("buckets").Items()
	if len(pins) == 0 {
		t.Fatal("no bucket pins")
	}
	for _, pin := range pins {
		key, salt, ctx := pin.Field("flagKey").String(), pin.Field("salt").String(), pin.Field("contextKey").String()
		if got := Hash32(key, salt, ctx); float64(got) != pin.Field("uint32").Number() {
			t.Errorf("Hash32(%q, %q, %q) = %d, want %v", key, salt, ctx, got, pin.Field("uint32").Number())
		}
		if got := Bucket(key, salt, ctx); float64(got) != pin.Field("bucket").Number() {
			t.Errorf("Bucket(%q, %q, %q) = %d, want %v", key, salt, ctx, got, pin.Field("bucket").Number())
		}
	}
	t.Logf("pins: %d", len(pins))
}
