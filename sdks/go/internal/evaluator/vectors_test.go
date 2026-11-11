package evaluator

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

// fixtures is docs/evaluation.md's executable form, read by relative path at test time.
const fixtures = "../../../../fixtures/evaluation"

func readJSON(t *testing.T, name string) jsonvalue.Value {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtures, name))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	v, err := jsonvalue.Parse(data)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return v
}

func fixtureFiles(t *testing.T) []string {
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

// reasonValue is a reason as the vectors spell it: only the fields its kind carries.
func reasonValue(r Reason) jsonvalue.Value {
	o := jsonvalue.NewObject()
	o.Set("kind", jsonvalue.String(string(r.Kind)))
	switch r.Kind {
	case ReasonRuleMatch:
		o.Set("ruleIndex", jsonvalue.Number(float64(r.RuleIndex)))
	case ReasonPrerequisiteFailed:
		o.Set("flagKey", jsonvalue.String(r.FlagKey))
	case ReasonError:
		o.Set("code", jsonvalue.String(string(r.Code)))
	}
	return jsonvalue.ObjectValue(o)
}

func resultValue(r Result) jsonvalue.Value {
	o := jsonvalue.NewObject()
	o.Set("value", r.Value)
	index := jsonvalue.Null()
	if r.VariationIndex >= 0 {
		index = jsonvalue.Number(float64(r.VariationIndex))
	}
	o.Set("variationIndex", index)
	o.Set("reason", reasonValue(r.Reason))
	return jsonvalue.ObjectValue(o)
}

func TestVectors(t *testing.T) {
	count := 0
	for _, file := range fixtureFiles(t) {
		if file == "buckets.json" {
			continue
		}
		vectors := field(readJSON(t, file), "vectors").Items()
		if len(vectors) == 0 {
			t.Fatalf("%s has no vectors", file)
		}
		for _, v := range vectors {
			count++
			name := field(v, "name").StringValue()
			root := jsonvalue.NewObject()
			root.Set("flags", field(v, "flags"))
			root.Set("segments", field(v, "segments"))
			rs := NewRuleset(jsonvalue.ObjectValue(root))
			got := resultValue(rs.Evaluate(field(v, "flagKey").StringValue(), field(v, "context")))
			want := field(v, "expected")
			if !jsonvalue.Equal(got, want) {
				gb := jsonvalue.Marshal(got)
				wb := jsonvalue.Marshal(want)
				t.Errorf("%s: %q:\n got  %s\n want %s", file, name, gb, wb)
			}
		}
	}
	if count == 0 {
		t.Fatal("no vectors were read")
	}
	t.Logf("vectors: %d", count)
}

func TestBucketPins(t *testing.T) {
	pins := field(readJSON(t, "buckets.json"), "buckets").Items()
	if len(pins) == 0 {
		t.Fatal("no bucket pins were read")
	}
	for _, p := range pins {
		flagKey, salt := field(p, "flagKey").StringValue(), field(p, "salt").StringValue()
		contextKey := field(p, "contextKey").StringValue()
		wantHash, wantBucket := field(p, "uint32").NumberValue(), field(p, "bucket").NumberValue()
		if got := hash32(flagKey, salt, contextKey); float64(got) != wantHash {
			t.Errorf("uint32 %q %q %q: got %d, want %v", flagKey, salt, contextKey, got, wantHash)
		}
		if got := bucketOf(flagKey, salt, contextKey); float64(got) != wantBucket {
			t.Errorf("bucket %q %q %q: got %d, want %v", flagKey, salt, contextKey, got, wantBucket)
		}
	}
	t.Logf("pins: %d", len(pins))
}
