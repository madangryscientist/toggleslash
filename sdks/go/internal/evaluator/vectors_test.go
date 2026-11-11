package evaluator

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

const fixturesDir = "../../../../fixtures/evaluation"

func readJSON(t *testing.T, path string) jsonvalue.Value {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	v, err := jsonvalue.Parse(data)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return v
}

func fixtureFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(fixturesDir, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no evaluation fixtures under %s", fixturesDir)
	}
	sort.Strings(files)
	return files
}

func field(v jsonvalue.Value, name string) jsonvalue.Value {
	f, _ := v.Get(name)
	return f
}

func describe(r Result) jsonvalue.Value {
	reason := []jsonvalue.Member{{Key: "kind", Value: jsonvalue.FromString(r.Reason.Kind)}}
	switch r.Reason.Kind {
	case ReasonRuleMatch:
		reason = append(reason, jsonvalue.Member{Key: "ruleIndex", Value: jsonvalue.FromNumber(float64(r.Reason.RuleIndex))})
	case ReasonPrerequisiteFailed:
		reason = append(reason, jsonvalue.Member{Key: "flagKey", Value: jsonvalue.FromString(r.Reason.FlagKey)})
	case ReasonError:
		reason = append(reason, jsonvalue.Member{Key: "code", Value: jsonvalue.FromString(r.Reason.Code)})
	}
	index := jsonvalue.Null
	if r.Index >= 0 {
		index = jsonvalue.FromNumber(float64(r.Index))
	}
	return jsonvalue.FromObject(
		jsonvalue.Member{Key: "value", Value: r.Value},
		jsonvalue.Member{Key: "variationIndex", Value: index},
		jsonvalue.Member{Key: "reason", Value: jsonvalue.FromObject(reason...)},
	)
}

func TestVectors(t *testing.T) {
	ran := 0
	for _, file := range fixtureFiles(t) {
		if filepath.Base(file) == "buckets.json" {
			continue
		}
		vectors := field(readJSON(t, file), "vectors").Elements()
		for _, vec := range vectors {
			name, _ := field(vec, "name").AsString()
			flagKey, _ := field(vec, "flagKey").AsString()
			ran++
			rs := Load(jsonvalue.FromObject(
				jsonvalue.Member{Key: "flags", Value: field(vec, "flags")},
				jsonvalue.Member{Key: "segments", Value: field(vec, "segments")},
			))
			got := describe(rs.Evaluate(flagKey, NewContext(field(vec, "context"))))
			want := field(vec, "expected")
			if !got.Equal(want) {
				t.Errorf("%s: %s: got %s, want %s", filepath.Base(file), name, got.String(), want.String())
			}
		}
	}
	if ran == 0 {
		t.Fatal("no vectors ran")
	}
	t.Logf("vectors: %d", ran)
}

func TestBucketPins(t *testing.T) {
	pins := field(readJSON(t, filepath.Join(fixturesDir, "buckets.json")), "buckets").Elements()
	for _, p := range pins {
		key, _ := field(p, "flagKey").AsString()
		salt, _ := field(p, "salt").AsString()
		ck, _ := field(p, "contextKey").AsString()
		want, _ := field(p, "bucket").AsNumber()
		if got := Bucket(key, salt, ck); float64(got) != want {
			t.Errorf("bucket(%q, %q, %q) = %d, want %v", key, salt, ck, got, want)
		}
	}
	if len(pins) == 0 {
		t.Fatal("no bucket pins ran")
	}
	t.Logf("pins: %d", len(pins))
}
