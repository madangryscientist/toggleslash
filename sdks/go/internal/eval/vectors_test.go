package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

// fixturesDir is the shared executable form of docs/evaluation.md, read in place: never a copy.
const fixturesDir = "../../../../fixtures/evaluation"

type fixtureSet struct {
	vectors []namedVector
	pins    []namedPin
}

type namedVector struct {
	file string
	body jsonvalue.Value
}

type namedPin struct {
	index int
	body  jsonvalue.Value
}

var (
	loadOnce sync.Once
	loaded   fixtureSet
	loadErr  error
)

func loadFixtures() (fixtureSet, error) {
	loadOnce.Do(func() {
		names, err := filepath.Glob(filepath.Join(fixturesDir, "*.json"))
		if err != nil {
			loadErr = err
			return
		}
		sort.Strings(names)
		for _, path := range names {
			data, err := os.ReadFile(path)
			if err != nil {
				loadErr = err
				return
			}
			doc, err := jsonvalue.Parse(data)
			if err != nil {
				loadErr = fmt.Errorf("%s: %w", path, err)
				return
			}
			base := filepath.Base(path)
			if base == "buckets.json" {
				list, _ := field(doc, "buckets").AsArray()
				for i, b := range list {
					loaded.pins = append(loaded.pins, namedPin{index: i, body: b})
				}
				continue
			}
			list, _ := field(doc, "vectors").AsArray()
			for _, v := range list {
				loaded.vectors = append(loaded.vectors, namedVector{file: base, body: v})
			}
		}
	})
	return loaded, loadErr
}

func resultValue(r Result) jsonvalue.Value {
	reason := []jsonvalue.Member{{Key: "kind", Value: jsonvalue.FromString(r.Reason.Kind)}}
	if r.Reason.Kind == KindRuleMatch {
		reason = append(reason, jsonvalue.Member{Key: "ruleIndex", Value: jsonvalue.FromInt(int64(r.Reason.RuleIndex))})
	}
	if r.Reason.FlagKey != "" {
		reason = append(reason, jsonvalue.Member{Key: "flagKey", Value: jsonvalue.FromString(r.Reason.FlagKey)})
	}
	if r.Reason.Code != "" {
		reason = append(reason, jsonvalue.Member{Key: "code", Value: jsonvalue.FromString(r.Reason.Code)})
	}
	index := jsonvalue.NullValue()
	if r.VariationIndex >= 0 {
		index = jsonvalue.FromInt(int64(r.VariationIndex))
	}
	return jsonvalue.FromObject(
		jsonvalue.Member{Key: "value", Value: r.Value},
		jsonvalue.Member{Key: "variationIndex", Value: index},
		jsonvalue.Member{Key: "reason", Value: jsonvalue.FromObject(reason...)},
	)
}

func TestVectors(t *testing.T) {
	set, err := loadFixtures()
	if err != nil {
		t.Fatal(err)
	}
	if len(set.vectors) == 0 {
		t.Fatal("read no evaluation vectors")
	}
	for _, v := range set.vectors {
		name, _ := field(v.body, "name").AsString()
		flagKey, _ := field(v.body, "flagKey").AsString()
		ruleset := NewRuleset(jsonvalue.FromObject(
			jsonvalue.Member{Key: "flags", Value: field(v.body, "flags")},
			jsonvalue.Member{Key: "segments", Value: field(v.body, "segments")},
		))
		got := resultValue(ruleset.Evaluate(flagKey, field(v.body, "context")))
		want := field(v.body, "expected")
		if !jsonvalue.Equal(got, want) {
			t.Errorf("%s: %q:\n got  %s\n want %s", v.file, name, got, want)
		}
	}
}

func TestBucketPins(t *testing.T) {
	set, err := loadFixtures()
	if err != nil {
		t.Fatal(err)
	}
	if len(set.pins) == 0 {
		t.Fatal("read no bucket pins")
	}
	for _, p := range set.pins {
		flagKey, _ := field(p.body, "flagKey").AsString()
		salt, _ := field(p.body, "salt").AsString()
		contextKey, _ := field(p.body, "contextKey").AsString()
		wantUint, _ := field(p.body, "uint32").AsNumber()
		wantBucket, _ := field(p.body, "bucket").AsNumber()
		sum := Bucket(flagKey, salt, contextKey)
		if float64(sum) != wantUint || float64(sum%BucketCount) != wantBucket {
			t.Errorf("buckets.json pin %d (%q %q %q): got %d / %d, want %v / %v",
				p.index, flagKey, salt, contextKey, sum, sum%BucketCount, wantUint, wantBucket)
		}
	}
}

// TestFixtureCounts prints the one line bin/test reports: how many vectors and pins ran.
func TestFixtureCounts(t *testing.T) {
	set, err := loadFixtures()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fmt.Fprintf(os.Stdout, "vectors: %d pins: %d\n", len(set.vectors), len(set.pins))
}
