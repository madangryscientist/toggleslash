package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// fixturesDir is the repository's shared evaluation fixtures, read in place: never a copy.
const fixturesDir = "../../../../fixtures/evaluation"

func readJSON(t *testing.T, path string) jsonvalue.Value {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	v, err := jsonvalue.Parse(wtf8.Repair(raw))
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return v
}

func (r Reason) toValue() jsonvalue.Value {
	o := jsonvalue.NewObject()
	o.Set("kind", jsonvalue.Str(string(r.Kind)))
	switch r.Kind {
	case ReasonRuleMatch:
		o.Set("ruleIndex", jsonvalue.Num(float64(r.RuleIndex)))
	case ReasonPrerequisiteFailed:
		o.Set("flagKey", jsonvalue.Str(r.FlagKey))
	case ReasonError:
		o.Set("code", jsonvalue.Str(string(r.Code)))
	}
	return o
}

func (r Result) toValue() jsonvalue.Value {
	o := jsonvalue.NewObject()
	o.Set("value", r.Value)
	index := jsonvalue.Value{}
	if r.VariationIndex >= 0 {
		index = jsonvalue.Num(float64(r.VariationIndex))
	}
	o.Set("variationIndex", index)
	o.Set("reason", r.Reason.toValue())
	return o
}

// runVectors runs every vector in the fixtures and returns how many files and vectors it ran.
func runVectors(t *testing.T) (files, vectors int) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(fixturesDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	for _, path := range paths {
		name := filepath.Base(path)
		if name == "buckets.json" {
			continue
		}
		files++
		list, _ := readJSON(t, path).Get("vectors")
		for _, vec := range list.A {
			vectors++
			label, _ := vec.Get("name")
			flagKey, _ := vec.Get("flagKey")
			flags, _ := vec.Get("flags")
			segments, _ := vec.Get("segments")
			context, _ := vec.Get("context")
			expected, _ := vec.Get("expected")
			input := jsonvalue.NewObject()
			input.Set("flags", flags)
			input.Set("segments", segments)
			got := NewRuleset(input).Evaluate(flagKey.S, context).toValue()
			// The expected value is compared whole, so an extra or missing reason field fails too.
			if !jsonvalue.Equal(got, expected) {
				t.Errorf("%s: %q:\n  got      %s\n  expected %s", name, label.S, jsonvalue.Write(got), jsonvalue.Write(expected))
			}
		}
	}
	return files, vectors
}

func TestVectors(t *testing.T) {
	files, vectors := runVectors(t)
	if files == 0 || vectors == 0 {
		t.Fatalf("read %d files and %d vectors", files, vectors)
	}
	t.Logf("%d vector files, %d vectors", files, vectors)
}

func TestBucketPins(t *testing.T) {
	pins, _ := readJSON(t, filepath.Join(fixturesDir, "buckets.json")).Get("buckets")
	if len(pins.A) == 0 {
		t.Fatal("read no bucket pins")
	}
	for i, pin := range pins.A {
		flagKey, _ := pin.Get("flagKey")
		salt, _ := pin.Get("salt")
		contextKey, _ := pin.Get("contextKey")
		wantU, _ := pin.Get("uint32")
		wantB, _ := pin.Get("bucket")
		id := fmt.Sprintf("pin %d (%q %q %q)", i, flagKey.S, salt.S, contextKey.S)
		if got := Uint32(flagKey.S, salt.S, contextKey.S); float64(got) != wantU.N {
			t.Errorf("%s: uint32 %d, want %v", id, got, wantU.N)
		}
		if got := Bucket(flagKey.S, salt.S, contextKey.S); float64(got) != wantB.N {
			t.Errorf("%s: bucket %d, want %v", id, got, wantB.N)
		}
	}
	t.Logf("%d pins", len(pins.A))
}

// TestCounts prints the numbers bin/test reports: it runs the vectors again, which costs milliseconds.
func TestCounts(t *testing.T) {
	_, vectors := runVectors(t)
	pins, _ := readJSON(t, filepath.Join(fixturesDir, "buckets.json")).Get("buckets")
	_, _ = fmt.Fprintf(os.Stdout, "vectors: %d pins: %d\n", vectors, len(pins.A))
}
