package toggleslash

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonx"
)

// fixturesDir is the repository's vectors, read from the tree at test time and never copied.
const fixturesDir = "../../fixtures/evaluation"

func readJSONFile(t *testing.T, path string) any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	return value
}

// sameJSON is the spec's equality: numbers compare as binary64, so -0 equals 0, and arrays and objects compare
// member by member.
func sameJSON(left, right any) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	if l, ok := numericValue(left); ok {
		r, ok := numericValue(right)
		return ok && l == r
	}
	switch l := left.(type) {
	case bool:
		r, ok := right.(bool)
		return ok && l == r
	case string:
		r, ok := right.(string)
		return ok && sameText(l, r)
	case []any:
		r, ok := right.([]any)
		if !ok || len(l) != len(r) {
			return false
		}
		for i := range l {
			if !sameJSON(l[i], r[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		r, ok := right.(map[string]any)
		if !ok || len(l) != len(r) {
			return false
		}
		for key, value := range l {
			other, present := r[key]
			if !present || !sameJSON(value, other) {
				return false
			}
		}
		return true
	}
	return false
}

// checkExpected compares one evaluation with a vector's expected result, naming every field that differs.
func checkExpected(result outcome, expected map[string]any) string {
	if want, present := expected["value"]; present {
		if !sameJSON(result.value, want) {
			return fmt.Sprintf("value was %v, expected %v", result.value, want)
		}
	}
	if want, present := expected["variationIndex"]; present {
		if want == nil {
			if result.hasIndex {
				return fmt.Sprintf("variationIndex was %d, expected null", result.index)
			}
		} else if !result.hasIndex {
			return fmt.Sprintf("variationIndex was null, expected %v", want)
		} else if number, ok := numericValue(want); !ok || int(number) != result.index {
			return fmt.Sprintf("variationIndex was %d, expected %v", result.index, want)
		}
	}
	want, present := expected["reason"].(map[string]any)
	if !present {
		return "the vector has no reason"
	}
	if kind, _ := want["kind"].(string); kind != string(result.kind) {
		return fmt.Sprintf("reason kind was %s, expected %s", result.kind, kind)
	}
	if code, present := want["code"]; present && code != string(result.code) {
		return fmt.Sprintf("error code was %s, expected %v", result.code, code)
	}
	if index, present := want["ruleIndex"]; present {
		number, ok := numericValue(index)
		if !ok || int(number) != result.ruleIndex {
			return fmt.Sprintf("ruleIndex was %d, expected %v", result.ruleIndex, index)
		}
	}
	if key, present := want["flagKey"]; present && key != result.flagKey {
		return fmt.Sprintf("flagKey was %s, expected %v", result.flagKey, key)
	}
	return ""
}

// TestVectors runs every vector in fixtures/evaluation, but buckets.json, through the evaluator.
func TestVectors(t *testing.T) {
	entries, err := os.ReadDir(fixturesDir)
	if err != nil {
		t.Fatalf("reading the vectors: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".json" && entry.Name() != "buckets.json" {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatal("no vector files were read")
	}
	ran := 0
	for _, name := range names {
		document, ok := readJSONFile(t, filepath.Join(fixturesDir, name)).(map[string]any)
		if !ok {
			t.Fatalf("%s is not an object", name)
		}
		vectors, ok := document["vectors"].([]any)
		if !ok {
			t.Fatalf("%s holds no vectors", name)
		}
		for _, raw := range vectors {
			vector, ok := raw.(map[string]any)
			if !ok {
				t.Fatalf("%s holds a vector that is not an object", name)
			}
			label, _ := vector["name"].(string)
			flagKey, _ := vector["flagKey"].(string)
			ruleset := createRuleset(map[string]any{"flags": vector["flags"], "segments": vector["segments"]})
			result := evaluateOne(ruleset, flagKey, vector["context"])
			expected, _ := vector["expected"].(map[string]any)
			if problem := checkExpected(result, expected); problem != "" {
				t.Errorf("%s: %s: %s", name, label, problem)
			}
			ran++
		}
	}
	if ran == 0 {
		t.Fatal("no vectors were run")
	}
	t.Logf("vectors: %d", ran)
}

// TestBucketPins checks both numbers of every bucket pin, which come from an independent SHA-256.
func TestBucketPins(t *testing.T) {
	document, ok := readJSONFile(t, filepath.Join(fixturesDir, "buckets.json")).(map[string]any)
	if !ok {
		t.Fatal("buckets.json is not an object")
	}
	pins, ok := document["buckets"].([]any)
	if !ok || len(pins) == 0 {
		t.Fatal("buckets.json holds no pins")
	}
	for _, raw := range pins {
		pin, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("a pin is not an object")
		}
		key, _ := pin["flagKey"].(string)
		salt, _ := pin["salt"].(string)
		contextKey, _ := pin["contextKey"].(string)
		wantUint32, _ := numericValue(pin["uint32"])
		wantBucket, _ := numericValue(pin["bucket"])
		if got := uint32Of(key, salt, contextKey); float64(got) != wantUint32 {
			t.Errorf("%s: uint32 was %d, expected %v", key, got, wantUint32)
		}
		if got := bucketOf(key, salt, contextKey); float64(got) != wantBucket {
			t.Errorf("%s: bucket was %d, expected %v", key, got, wantBucket)
		}
	}
	t.Logf("pins: %d", len(pins))
}
