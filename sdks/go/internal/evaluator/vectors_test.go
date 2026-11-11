package evaluator

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

const fixtures = "../../../../fixtures/evaluation"

func readFixture(t *testing.T, path string) jsonvalue.Value {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	value, err := jsonvalue.Parse(data)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return value
}

func fixtureFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(fixtures, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures under %s (%v)", fixtures, err)
	}
	sort.Strings(files)
	return files
}

// resultValue is a result as the spec writes it: value, variationIndex, reason.
func resultValue(r Result) jsonvalue.Value {
	reason := jsonvalue.NewObject()
	reason.Set("kind", jsonvalue.String(r.Reason.Kind))
	switch r.Reason.Kind {
	case ReasonRuleMatch:
		reason.Set("ruleIndex", jsonvalue.Number(float64(r.Reason.RuleIndex)))
	case ReasonPrerequisiteFailed:
		reason.Set("flagKey", jsonvalue.String(r.Reason.FlagKey))
	case ReasonError:
		reason.Set("code", jsonvalue.String(r.Reason.Code))
	}
	out := jsonvalue.NewObject()
	out.Set("value", r.Value)
	index := jsonvalue.Null()
	if r.VariationIndex >= 0 {
		index = jsonvalue.Number(float64(r.VariationIndex))
	}
	out.Set("variationIndex", index)
	out.Set("reason", jsonvalue.ObjectValue(reason))
	return jsonvalue.ObjectValue(out)
}

func TestVectors(t *testing.T) {
	total := 0
	for _, path := range fixtureFiles(t) {
		name := filepath.Base(path)
		if name == "buckets.json" {
			continue
		}
		file := readFixture(t, path)
		vectors, ok := file.Field("vectors")
		items, isArray := vectors.AsArray()
		if !ok || !isArray || len(items) == 0 {
			t.Fatalf("%s holds no vectors", name)
		}
		t.Run(name, func(t *testing.T) {
			for _, vector := range items {
				total++
				label, _ := field(vector, "name").AsString()
				input := jsonvalue.NewObject()
				input.Set("flags", field(vector, "flags"))
				input.Set("segments", field(vector, "segments"))
				ruleset := New(jsonvalue.ObjectValue(input))
				flagKey, _ := field(vector, "flagKey").AsString()
				got := resultValue(ruleset.Evaluate(flagKey, field(vector, "context")))
				want := field(vector, "expected")
				if !jsonvalue.Equal(got, want) {
					gotText := jsonvalue.Marshal(got)
					wantText := jsonvalue.Marshal(want)
					t.Errorf("%s: got %s, want %s", label, gotText, wantText)
				}
			}
		})
	}
	if total == 0 {
		t.Fatal("no vectors ran")
	}
	t.Logf("vectors: %d", total)
}

func TestBucketPins(t *testing.T) {
	file := readFixture(t, filepath.Join(fixtures, "buckets.json"))
	pins, ok := field(file, "buckets").AsArray()
	if !ok || len(pins) == 0 {
		t.Fatal("buckets.json holds no pins")
	}
	for _, pin := range pins {
		flagKey, _ := field(pin, "flagKey").AsString()
		salt, _ := field(pin, "salt").AsString()
		contextKey, _ := field(pin, "contextKey").AsString()
		wantHash, _ := field(pin, "uint32").AsNumber()
		wantBucket, _ := field(pin, "bucket").AsNumber()
		if got := Hash32(flagKey, salt, contextKey); float64(got) != wantHash {
			t.Errorf("hash32(%q, %q, %q) = %d, want %v", flagKey, salt, contextKey, got, wantHash)
		}
		if got := Bucket(flagKey, salt, contextKey); float64(got) != wantBucket {
			t.Errorf("bucket(%q, %q, %q) = %d, want %v", flagKey, salt, contextKey, got, wantBucket)
		}
	}
	t.Logf("pins: %d", len(pins))
}
