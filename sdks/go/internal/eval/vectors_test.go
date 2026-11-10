package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

// fixtures is fixtures/evaluation, found from this package's directory.
var fixtures = filepath.Join("..", "..", "..", "..", "fixtures", "evaluation")

func readFixture(t *testing.T, name string) jsonvalue.Value {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtures, name))
	if err != nil {
		t.Fatal(err)
	}
	v, err := jsonvalue.Parse(raw)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return v
}

// resultValue is a Result in the shape a vector's "expected" has.
func resultValue(r Result) jsonvalue.Value {
	reason := []jsonvalue.Member{{Key: "kind", Value: jsonvalue.String(r.Reason.Kind)}}
	switch r.Reason.Kind {
	case ReasonRuleMatch:
		reason = append(reason, jsonvalue.Member{Key: "ruleIndex", Value: jsonvalue.Number(float64(r.Reason.RuleIndex))})
	case ReasonPrerequisiteFailed:
		reason = append(reason, jsonvalue.Member{Key: "flagKey", Value: jsonvalue.String(r.Reason.FlagKey)})
	case ReasonError:
		reason = append(reason, jsonvalue.Member{Key: "code", Value: jsonvalue.String(r.Reason.Code)})
	}
	index := jsonvalue.Null()
	if r.VariationIndex != NoVariation {
		index = jsonvalue.Number(float64(r.VariationIndex))
	}
	return jsonvalue.Object(
		jsonvalue.Member{Key: "value", Value: r.Value},
		jsonvalue.Member{Key: "variationIndex", Value: index},
		jsonvalue.Member{Key: "reason", Value: jsonvalue.Object(reason...)},
	)
}

func TestVectors(t *testing.T) {
	entries, err := os.ReadDir(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if e.Name() != "buckets.json" && filepath.Ext(e.Name()) == ".json" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	total := 0
	kinds := map[string]bool{}
	for _, name := range names {
		vectors, _ := readFixture(t, name).Field("vectors").Items()
		for _, v := range vectors {
			total++
			title, _ := v.Field("name").Text()
			flagKey, _ := v.Field("flagKey").Text()
			ruleset := Compile(jsonvalue.Object(
				jsonvalue.Member{Key: "flags", Value: v.Field("flags")},
				jsonvalue.Member{Key: "segments", Value: v.Field("segments")},
			))
			got := resultValue(ruleset.Evaluate(flagKey, v.Field("context")))
			want := v.Field("expected")
			if !jsonvalue.Equal(got, want) {
				gotJSON := got.AppendJSON(nil)
				wantJSON := want.AppendJSON(nil)
				t.Errorf("%s: %s\n got  %s\n want %s", name, title, gotJSON, wantJSON)
			}
			reason := want.Field("reason")
			kind, _ := reason.Field("kind").Text()
			if code, ok := reason.Field("code").Text(); ok {
				kind += ":" + code
			}
			kinds[kind] = true
		}
	}
	if total == 0 {
		t.Fatal("read no vectors")
	}
	if len(kinds) != 12 {
		t.Errorf("the vectors cover %d reasons and error codes, want 12: %v", len(kinds), kinds)
	}
	fmt.Printf("vectors: %d\n", total)
}

func TestBucketPins(t *testing.T) {
	pins, _ := readFixture(t, "buckets.json").Field("buckets").Items()
	if len(pins) == 0 {
		t.Fatal("read no bucket pins")
	}
	for _, p := range pins {
		key, _ := p.Field("flagKey").Text()
		salt, _ := p.Field("salt").Text()
		ctxKey, _ := p.Field("contextKey").Text()
		wantHash, _ := p.Field("uint32").Number()
		wantBucket, _ := p.Field("bucket").Number()
		if got := hash32(key, salt, ctxKey); float64(got) != wantHash {
			t.Errorf("hash32(%q,%q,%q) = %d, want %v", key, salt, ctxKey, got, wantHash)
		}
		if got := bucket(key, salt, ctxKey); float64(got) != wantBucket {
			t.Errorf("bucket(%q,%q,%q) = %d, want %v", key, salt, ctxKey, got, wantBucket)
		}
	}
	fmt.Printf("pins: %d\n", len(pins))
}
