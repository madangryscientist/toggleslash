package evaluator

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

// fixtures is fixtures/evaluation, relative to this package's directory.
const fixtures = "../../../../fixtures/evaluation"

func resultValue(r Result) jsonvalue.Value {
	reason := map[string]jsonvalue.Value{"kind": jsonvalue.NewString(r.Reason.Kind)}
	if r.Reason.RuleIndex != nil {
		reason["ruleIndex"] = jsonvalue.NewNumber(float64(*r.Reason.RuleIndex))
	}
	if r.Reason.Kind == ReasonPrerequisiteFailed {
		reason["flagKey"] = jsonvalue.NewString(r.Reason.FlagKey)
	}
	if r.Reason.Kind == ReasonError {
		reason["code"] = jsonvalue.NewString(r.Reason.Code)
	}
	index := jsonvalue.Value{}
	if r.HasVariation {
		index = jsonvalue.NewNumber(float64(r.VariationIndex))
	}
	return jsonvalue.NewObject(map[string]jsonvalue.Value{
		"value":          r.Value,
		"variationIndex": index,
		"reason":         jsonvalue.NewObject(reason),
	})
}

func readFixture(t *testing.T, name string) jsonvalue.Value {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtures, name))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	v, err := jsonvalue.Parse(raw)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return v
}

func fixtureNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// TestVectors runs every vector in every evaluation fixture but buckets.json, and the bucket pins
// of buckets.json. It fails if it read no file or no vector, and it always reports its counts.
func TestVectors(t *testing.T) {
	vectors, pins, files := 0, 0, 0
	for _, name := range fixtureNames(t) {
		files++
		doc := readFixture(t, name)
		if name == "buckets.json" {
			items, _ := doc.Get("buckets").Array()
			for i, item := range items {
				pins++
				key, _ := item.Get("flagKey").String()
				salt, _ := item.Get("salt").String()
				contextKey, _ := item.Get("contextKey").String()
				wantU, _ := item.Get("uint32").Number()
				wantB, _ := item.Get("bucket").Number()
				got := bucket(key, salt, contextKey)
				if float64(got) != wantB {
					t.Errorf("buckets.json pin %d (%s/%s/%s): bucket %d, want %v", i, key, salt, contextKey, got, wantB)
				}
				if uint32(wantU)%bucketCount != uint32(wantB) {
					t.Errorf("buckets.json pin %d: uint32 %v does not give bucket %v", i, wantU, wantB)
				}
			}
			continue
		}
		items, _ := doc.Get("vectors").Array()
		for _, item := range items {
			vectors++
			label, _ := item.Get("name").String()
			flagKey, _ := item.Get("flagKey").String()
			ruleset := Compile(jsonvalue.NewObject(map[string]jsonvalue.Value{
				"flags":    item.Get("flags"),
				"segments": item.Get("segments"),
			}))
			got := resultValue(ruleset.Evaluate(flagKey, item.Get("context")))
			want := item.Get("expected")
			if !jsonvalue.Equal(got, want) {
				t.Errorf("%s: %q\n got  %s\n want %s", name, label, jsonvalue.Marshal(got), jsonvalue.Marshal(want))
			}
		}
	}
	if files == 0 || vectors == 0 || pins == 0 {
		t.Fatalf("read %d files, %d vectors, %d pins", files, vectors, pins)
	}
	t.Logf("vectors: %d pins: %d", vectors, pins)
}
