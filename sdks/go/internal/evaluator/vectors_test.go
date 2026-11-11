package evaluator

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// The vectors are read from the repository's fixtures, never a copy.
const fixtures = "../../../../fixtures/evaluation"

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

// resultValue writes a Result in the shape the vectors' expected field has.
func resultValue(r Result) jsonv.Value {
	reason := jsonv.NewObject()
	reason.Set("kind", jsonv.String(string(r.Reason.Kind)))
	switch r.Reason.Kind {
	case ReasonRuleMatch:
		reason.Set("ruleIndex", jsonv.Number(float64(r.Reason.RuleIndex)))
	case ReasonPrerequisiteFailed:
		reason.Set("flagKey", jsonv.String(r.Reason.FlagKey))
	case ReasonError:
		reason.Set("code", jsonv.String(string(r.Reason.Code)))
	}
	out := jsonv.NewObject()
	out.Set("value", r.Value)
	if r.Index < 0 {
		out.Set("variationIndex", jsonv.Null())
	} else {
		out.Set("variationIndex", jsonv.Number(float64(r.Index)))
	}
	out.Set("reason", jsonv.ObjectValue(reason))
	return jsonv.ObjectValue(out)
}

func TestVectors(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(fixtures, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	total := 0
	for _, file := range files {
		name := filepath.Base(file)
		if name == "buckets.json" {
			continue
		}
		list, _ := readJSON(t, file).Get("vectors").AsArray()
		if len(list) == 0 {
			t.Fatalf("%s holds no vectors", name)
		}
		for _, vector := range list {
			total++
			title, _ := vector.Get("name").AsString()
			flagKey, _ := vector.Get("flagKey").AsString()
			ruleset := jsonv.NewObject()
			ruleset.Set("flags", vector.Get("flags"))
			ruleset.Set("segments", vector.Get("segments"))
			got := Load(jsonv.ObjectValue(ruleset)).Evaluate(flagKey, vector.Get("context"))
			want := vector.Get("expected")
			if gotValue := resultValue(got); !jsonv.Equal(gotValue, want) {
				t.Errorf("%s: %s:\n got  %s\n want %s", name, title, jsonv.Marshal(gotValue), jsonv.Marshal(want))
			}
		}
	}
	if total == 0 {
		t.Fatal("no vector files were read")
	}
	fmt.Printf("COUNT vectors %d\n", total)
}

func TestBucketPins(t *testing.T) {
	list, _ := readJSON(t, filepath.Join(fixtures, "buckets.json")).Get("buckets").AsArray()
	if len(list) == 0 {
		t.Fatal("buckets.json holds no pins")
	}
	for _, pin := range list {
		flagKey, _ := pin.Get("flagKey").AsString()
		salt, _ := pin.Get("salt").AsString()
		contextKey, _ := pin.Get("contextKey").AsString()
		wantUint, _ := pin.Get("uint32").AsNumber()
		wantBucket, _ := pin.Get("bucket").AsNumber()
		gotUint, gotBucket := Bucket(flagKey, salt, contextKey)
		if float64(gotUint) != wantUint || float64(gotBucket) != wantBucket {
			t.Errorf("pin %q %q %q: got %d, %d; want %v, %v", flagKey, salt, contextKey, gotUint, gotBucket, wantUint, wantBucket)
		}
	}
	fmt.Printf("COUNT pins %d\n", len(list))
}
