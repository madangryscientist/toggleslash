package evaluator

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wjson"
)

// The shared vectors are read from fixtures/evaluation/ at test time, never
// copied: the fixtures are the spec's executable form.
const fixturesDir = "../../../../fixtures/evaluation"

func readJSON(t *testing.T, path string) *wjson.Object {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	v, err := wjson.Parse(data)
	if err != nil || v.Object() == nil {
		t.Fatalf("%s is not a JSON object: %v", path, err)
	}
	return v.Object()
}

func get(o *wjson.Object, name string) wjson.Value {
	v, _ := o.Get(name)
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

func checkVector(t *testing.T, v *wjson.Object) {
	t.Helper()
	input := wjson.NewObject()
	input.Set("flags", get(v, "flags"))
	input.Set("segments", get(v, "segments"))
	got := NewRuleset(wjson.ObjectValue(input)).Evaluate(get(v, "flagKey").String(), get(v, "context"))

	want := get(v, "expected").Object()
	if want == nil {
		t.Fatal("vector has no expected object")
	}
	if !get(want, "value").Equal(got.Value) {
		t.Errorf("value: got %s, want %s", wjson.Marshal(got.Value), wjson.Marshal(get(want, "value")))
	}
	wantIndex := get(want, "variationIndex")
	switch {
	case wantIndex.IsNull() && got.VariationIndex != nil:
		t.Errorf("variationIndex: got %d, want null", *got.VariationIndex)
	case !wantIndex.IsNull() && (got.VariationIndex == nil || float64(*got.VariationIndex) != wantIndex.Number()):
		t.Errorf("variationIndex: got %v, want %v", got.VariationIndex, wantIndex.Number())
	}
	reason := get(want, "reason").Object()
	if reason == nil {
		t.Fatal("vector has no expected reason")
	}
	if got.Reason.Kind != ReasonKind(get(reason, "kind").String()) {
		t.Errorf("reason kind: got %s, want %s", got.Reason.Kind, get(reason, "kind").String())
	}
	switch got.Reason.Kind {
	case ReasonError:
		if string(got.Reason.Code) != get(reason, "code").String() {
			t.Errorf("error code: got %s, want %s", got.Reason.Code, get(reason, "code").String())
		}
	case ReasonRuleMatch:
		if float64(got.Reason.RuleIndex) != get(reason, "ruleIndex").Number() {
			t.Errorf("ruleIndex: got %d, want %v", got.Reason.RuleIndex, get(reason, "ruleIndex").Number())
		}
	case ReasonPrerequisiteFailed:
		if got.Reason.FlagKey != get(reason, "flagKey").String() {
			t.Errorf("flagKey: got %s, want %s", got.Reason.FlagKey, get(reason, "flagKey").String())
		}
	}
}

// TestVectors runs every vector of every file but buckets.json, and the pins in
// buckets.json. It prints "vectors: <n>" for bin/test.
func TestVectors(t *testing.T) {
	total := 0
	reasons := map[string]bool{}
	for _, file := range fixtureFiles(t) {
		name := filepath.Base(file)
		doc := readJSON(t, file)
		if name == "buckets.json" {
			continue
		}
		vectors := get(doc, "vectors").Array()
		if len(vectors) == 0 {
			t.Fatalf("%s has no vectors", name)
		}
		for _, item := range vectors {
			v := item.Object()
			if v == nil {
				t.Fatalf("%s: a vector is not an object", name)
			}
			total++
			reason := get(get(v, "expected").Object(), "reason").Object()
			kind := get(reason, "kind").String()
			if kind == "ERROR" {
				kind += ":" + get(reason, "code").String()
			}
			reasons[kind] = true
			t.Run(name+"/"+get(v, "name").String(), func(t *testing.T) { checkVector(t, v) })
		}
	}
	if total == 0 {
		t.Fatal("no vectors ran")
	}
	if len(reasons) != 12 {
		t.Errorf("the vectors cover %d reasons and error codes, want 12", len(reasons))
	}
	t.Logf("vectors: %d", total)
}

func TestBucketPins(t *testing.T) {
	doc := readJSON(t, filepath.Join(fixturesDir, "buckets.json"))
	pins := get(doc, "buckets").Array()
	if len(pins) == 0 {
		t.Fatal("buckets.json has no pins")
	}
	for _, item := range pins {
		p := item.Object()
		b, n := Bucket(get(p, "flagKey").String(), get(p, "salt").String(), get(p, "contextKey").String())
		if float64(n) != get(p, "uint32").Number() || float64(b) != get(p, "bucket").Number() {
			t.Errorf("%s/%s/%s: got uint32 %d bucket %d, want %v and %v", get(p, "flagKey").String(),
				get(p, "salt").String(), get(p, "contextKey").String(), n, b, get(p, "uint32").Number(), get(p, "bucket").Number())
		}
	}
	t.Logf("pins: %d", len(pins))
}
