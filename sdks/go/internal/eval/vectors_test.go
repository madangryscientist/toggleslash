package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

const fixturesDir = "../../../../fixtures/evaluation"

func readFixture(t *testing.T, name string) jsonv.Value {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixturesDir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	v, err := jsonv.Parse(data)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return v
}

func fixtureNames(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(fixturesDir, "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no fixtures in %s: %v", fixturesDir, err)
	}
	names := make([]string, len(paths))
	for i, p := range paths {
		names[i] = filepath.Base(p)
	}
	sort.Strings(names)
	return names
}

// resultValue renders a Result the way the vectors write it.
func resultValue(r Result) jsonv.Value {
	reason := jsonv.NewMembers()
	reason.Set("kind", jsonv.StringValue(r.Reason.Kind))
	switch r.Reason.Kind {
	case ReasonRuleMatch:
		reason.Set("ruleIndex", jsonv.NumberValue(float64(r.Reason.RuleIndex)))
	case ReasonPrerequisiteFailed:
		reason.Set("flagKey", jsonv.StringValue(r.Reason.FlagKey))
	case ReasonError:
		reason.Set("code", jsonv.StringValue(r.Reason.Code))
	}
	out := jsonv.NewMembers()
	out.Set("value", r.Value)
	index := jsonv.NullValue()
	if r.VariationIndex >= 0 {
		index = jsonv.NumberValue(float64(r.VariationIndex))
	}
	out.Set("variationIndex", index)
	out.Set("reason", jsonv.ObjectValue(reason))
	return jsonv.ObjectValue(out)
}

func TestVectors(t *testing.T) {
	total := 0
	for _, name := range fixtureNames(t) {
		if name == "buckets.json" {
			continue
		}
		file := readFixture(t, name)
		o, _ := file.AsObject()
		vectors, ok := field(o, "vectors").AsArray()
		if !ok {
			t.Fatalf("%s has no vectors array", name)
		}
		for _, v := range vectors {
			vo, _ := v.AsObject()
			vname, _ := field(vo, "name").AsString()
			total++
			t.Run(name+"/"+vname, func(t *testing.T) {
				ruleset := jsonv.NewMembers()
				ruleset.Set("flags", field(vo, "flags"))
				ruleset.Set("segments", field(vo, "segments"))
				flagKey, _ := field(vo, "flagKey").AsString()
				got := resultValue(Load(jsonv.ObjectValue(ruleset)).Evaluate(flagKey, field(vo, "context")))
				want := field(vo, "expected")
				if !jsonv.Equal(got, want) {
					t.Errorf("got %s, want %s", jsonv.Append(nil, got), jsonv.Append(nil, want))
				}
			})
		}
	}
	if total == 0 {
		t.Fatal("no vectors ran")
	}
}

func TestBucketPins(t *testing.T) {
	o, _ := readFixture(t, "buckets.json").AsObject()
	pins, ok := field(o, "buckets").AsArray()
	if !ok || len(pins) == 0 {
		t.Fatal("no pins")
	}
	for _, p := range pins {
		po, _ := p.AsObject()
		flagKey, _ := field(po, "flagKey").AsString()
		salt, _ := field(po, "salt").AsString()
		contextKey, _ := field(po, "contextKey").AsString()
		wantU, _ := field(po, "uint32").AsNumber()
		wantB, _ := field(po, "bucket").AsNumber()
		u, b := Bucket(flagKey, salt, contextKey)
		if float64(u) != wantU || float64(b) != wantB {
			t.Errorf("%s.%s.%s: got %d/%d, want %v/%v", flagKey, salt, contextKey, u, b, wantU, wantB)
		}
	}
}

// TestVectorCounts prints the counts bin/test reports: every vector and every pin the files hold.
func TestVectorCounts(t *testing.T) {
	vectors, pins := 0, 0
	for _, name := range fixtureNames(t) {
		o, _ := readFixture(t, name).AsObject()
		if name == "buckets.json" {
			items, _ := field(o, "buckets").AsArray()
			pins += len(items)
			continue
		}
		items, _ := field(o, "vectors").AsArray()
		vectors += len(items)
	}
	fmt.Printf("vectors: %d pins: %d\n", vectors, pins)
}
