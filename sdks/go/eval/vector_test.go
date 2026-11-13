package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonval"
)

// TestVectors runs every vector in fixtures/evaluation and every bucket pin. The
// files are read from the repository at test time, never from a copy.
func TestVectors(t *testing.T) {
	dir := fixtureDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	vectors := 0
	pins := 0
	files := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".json" {
			continue
		}
		files++
		value := readFixture(t, filepath.Join(dir, name))
		if name == "buckets.json" {
			pins += runPins(t, name, value)
			continue
		}
		vectors += runVectors(t, name, value)
	}
	if files == 0 {
		t.Fatalf("no vector files in %s", dir)
	}
	if vectors == 0 {
		t.Fatalf("no vectors in %s", dir)
	}
	if pins == 0 {
		t.Fatalf("no bucket pins in %s", dir)
	}
	fmt.Printf("vectors: %d pins: %d\n", vectors, pins)
}

// fixtureDir walks up from the package directory to the repository's
// fixtures/evaluation, so nothing is copied into sdks/go.
func fixtureDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(dir, "fixtures", "evaluation")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("no fixtures/evaluation above %s", dir)
	return ""
}

func readFixture(t *testing.T, path string) jsonval.Value {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Fatalf("closing %s: %v", path, err)
		}
	}()
	value, err := jsonval.Read(file, 1024)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	return value
}

func runVectors(t *testing.T, file string, doc jsonval.Value) int {
	t.Helper()
	root, ok := doc.(jsonval.Object)
	if !ok {
		t.Fatalf("%s: not an object", file)
	}
	list, ok := root["vectors"].(jsonval.Array)
	if !ok {
		t.Fatalf("%s: no vectors array", file)
	}
	for _, item := range list {
		vector, ok := item.(jsonval.Object)
		if !ok {
			t.Fatalf("%s: a vector is not an object", file)
		}
		name, _ := vector["name"].(jsonval.String)
		flagKey, ok := vector["flagKey"].(jsonval.String)
		if !ok {
			t.Fatalf("%s/%s: no flagKey", file, name)
		}
		expected, ok := vector["expected"]
		if !ok {
			t.Fatalf("%s/%s: no expected", file, name)
		}
		ruleset := NewRuleset(vector["flags"], vector["segments"])
		result := ruleset.Evaluate(string(flagKey), vector["context"])
		got := jsonval.Value(result.ToJSON())
		if !sameJSON(got, expected) {
			t.Errorf("%s: %s: got %s, want %s", file, name, render(got), render(expected))
		}
	}
	return len(list)
}

func runPins(t *testing.T, file string, doc jsonval.Value) int {
	t.Helper()
	root, ok := doc.(jsonval.Object)
	if !ok {
		t.Fatalf("%s: not an object", file)
	}
	list, ok := root["buckets"].(jsonval.Array)
	if !ok {
		t.Fatalf("%s: no buckets array", file)
	}
	for _, item := range list {
		pin, ok := item.(jsonval.Object)
		if !ok {
			t.Fatalf("%s: a pin is not an object", file)
		}
		flagKey, ok := pin["flagKey"].(jsonval.String)
		if !ok {
			t.Fatalf("%s: a pin has no flagKey", file)
		}
		salt, ok := pin["salt"].(jsonval.String)
		if !ok {
			t.Fatalf("%s: a pin has no salt", file)
		}
		contextKey, ok := pin["contextKey"].(jsonval.String)
		if !ok {
			t.Fatalf("%s: a pin has no contextKey", file)
		}
		wantedUint32, ok := pin["uint32"].(jsonval.Number)
		if !ok {
			t.Fatalf("%s: a pin has no uint32", file)
		}
		wantedBucket, ok := pin["bucket"].(jsonval.Number)
		if !ok {
			t.Fatalf("%s: a pin has no bucket", file)
		}
		raw, bucket := bucketHash(string(flagKey), string(salt), string(contextKey))
		if float64(raw) != float64(wantedUint32) {
			t.Errorf("%s: %s.%s.%s: uint32 %d, want %v",
				file, flagKey, salt, contextKey, raw, float64(wantedUint32))
		}
		if float64(bucket) != float64(wantedBucket) {
			t.Errorf("%s: %s.%s.%s: bucket %d, want %v",
				file, flagKey, salt, contextKey, bucket, float64(wantedBucket))
		}
	}
	return len(list)
}

// sameJSON compares two values deeply, with numbers by IEEE-754, so -0 equals 0
// and 1.0 equals 1.
func sameJSON(a, b jsonval.Value) bool {
	switch left := a.(type) {
	case jsonval.Null:
		_, ok := b.(jsonval.Null)
		return ok
	case jsonval.Bool:
		right, ok := b.(jsonval.Bool)
		return ok && left == right
	case jsonval.Number:
		right, ok := b.(jsonval.Number)
		return ok && float64(left) == float64(right)
	case jsonval.String:
		right, ok := b.(jsonval.String)
		return ok && left == right
	case jsonval.Array:
		right, ok := b.(jsonval.Array)
		if !ok || len(left) != len(right) {
			return false
		}
		for i := range left {
			if !sameJSON(left[i], right[i]) {
				return false
			}
		}
		return true
	case jsonval.Object:
		right, ok := b.(jsonval.Object)
		if !ok || len(left) != len(right) {
			return false
		}
		for key, value := range left {
			other, present := right[key]
			if !present || !sameJSON(value, other) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// render writes a value for a failure message. It is not the SDK's encoder.
func render(value jsonval.Value) string {
	var out strings.Builder
	writeRendered(&out, value)
	return out.String()
}

func writeRendered(out *strings.Builder, value jsonval.Value) {
	switch typed := value.(type) {
	case jsonval.Null:
		out.WriteString("null")
	case jsonval.Bool:
		fmt.Fprintf(out, "%t", bool(typed))
	case jsonval.Number:
		fmt.Fprintf(out, "%v", float64(typed))
	case jsonval.String:
		fmt.Fprintf(out, "%q", string(typed))
	case jsonval.Array:
		out.WriteString("[")
		for i, item := range typed {
			if i > 0 {
				out.WriteString(",")
			}
			writeRendered(out, item)
		}
		out.WriteString("]")
	case jsonval.Object:
		out.WriteString("{")
		first := true
		for key, item := range typed {
			if !first {
				out.WriteString(",")
			}
			first = false
			fmt.Fprintf(out, "%q:", key)
			writeRendered(out, item)
		}
		out.WriteString("}")
	default:
		out.WriteString("?")
	}
}
