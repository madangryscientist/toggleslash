package evaluator

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

const fixturesDir = "../../../../fixtures/evaluation"

func readJSON(t *testing.T, path string) jsonv.Value {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	v, err := jsonv.Parse(data)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return v
}

// deepEqual compares two JSON values: numbers by value (so -0 equals 0), objects without regard to
// member order.
func deepEqual(a, b jsonv.Value) bool {
	if a.Kind() != b.Kind() {
		return false
	}
	switch a.Kind() {
	case jsonv.KindNull:
		return true
	case jsonv.KindBool:
		x, _ := a.AsBool()
		y, _ := b.AsBool()
		return x == y
	case jsonv.KindNumber:
		x, _ := a.AsNumber()
		y, _ := b.AsNumber()
		return x == y || (math.IsNaN(x) && math.IsNaN(y))
	case jsonv.KindString:
		x, _ := a.AsString()
		y, _ := b.AsString()
		return x == y
	case jsonv.KindArray:
		if a.Len() != b.Len() {
			return false
		}
		for i, item := range a.Items() {
			if !deepEqual(item, b.Items()[i]) {
				return false
			}
		}
		return true
	}
	if a.Len() != b.Len() {
		return false
	}
	for _, k := range a.Keys() {
		av, _ := a.Get(k)
		bv, ok := b.Get(k)
		if !ok || !deepEqual(av, bv) {
			return false
		}
	}
	return true
}

// resultJSON is a result in the wire shape the vectors use.
func resultJSON(r Result) jsonv.Value {
	reason := map[string]jsonv.Value{"kind": jsonv.String(string(r.Reason.Kind))}
	switch r.Reason.Kind {
	case ReasonRuleMatch:
		reason["ruleIndex"] = jsonv.Number(float64(r.Reason.RuleIndex))
	case ReasonPrerequisiteFailed:
		reason["flagKey"] = jsonv.String(r.Reason.PrerequisiteKey)
	case ReasonError:
		reason["code"] = jsonv.String(string(r.Reason.ErrorCode))
	}
	index := jsonv.Null()
	if r.VariationIndex != NoIndex {
		index = jsonv.Number(float64(r.VariationIndex))
	}
	return jsonv.Object(map[string]jsonv.Value{
		"value":          r.Value,
		"variationIndex": index,
		"reason":         jsonv.Object(reason),
	})
}

func TestVectorsAndPins(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(fixturesDir, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixture files under %s (%v)", fixturesDir, err)
	}
	sort.Strings(files)
	vectors, pins := 0, 0
	for _, file := range files {
		doc := readJSON(t, file)
		if filepath.Base(file) == "buckets.json" {
			list, _ := doc.Get("buckets")
			for _, p := range list.Items() {
				flagKey, _ := member(p, "flagKey").AsString()
				salt, _ := member(p, "salt").AsString()
				contextKey, _ := member(p, "contextKey").AsString()
				want32, _ := member(p, "uint32").AsNumber()
				wantBucket, _ := member(p, "bucket").AsNumber()
				if got := hash32(flagKey, salt, contextKey); float64(got) != want32 {
					t.Errorf("pin %q %q %q: uint32 %d, want %v", flagKey, salt, contextKey, got, want32)
				}
				if got := Bucket(flagKey, salt, contextKey); float64(got) != wantBucket {
					t.Errorf("pin %q %q %q: bucket %d, want %v", flagKey, salt, contextKey, got, wantBucket)
				}
				pins++
			}
			continue
		}
		list, _ := doc.Get("vectors")
		for _, v := range list.Items() {
			name, _ := member(v, "name").AsString()
			vectors++
			flagKey, _ := member(v, "flagKey").AsString()
			root := jsonv.Object(map[string]jsonv.Value{"flags": member(v, "flags"), "segments": member(v, "segments")})
			got := resultJSON(NewRuleset(root).Evaluate(flagKey, member(v, "context")))
			want := member(v, "expected")
			if !deepEqual(got, want) {
				gb, wb := jsonv.Marshal(got), jsonv.Marshal(want)
				t.Errorf("%s: %s\n got  %s\n want %s", filepath.Base(file), name, gb, wb)
			}
		}
	}
	if vectors == 0 || pins == 0 {
		t.Fatalf("ran %d vectors and %d pins", vectors, pins)
	}
	// bin/test reads this line.
	fmt.Printf("vectors: %d pins: %d\n", vectors, pins)
}
