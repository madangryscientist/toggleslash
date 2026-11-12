package toggleslash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sdkSources is every non-test Go file of the SDK, the package and internal/jsonx, keyed by path. The test
// service is a main package of its own and is left out.
func sdkSources(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if path == ".build" || path == "test-service" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[path] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("walking the sources: %v", err)
	}
	if len(out) < 10 {
		t.Fatalf("only %d source files were read", len(out))
	}
	return out
}

func TestTheSDKImportsNoRegularExpressionPackage(t *testing.T) {
	for path, source := range sdkSources(t) {
		for _, forbidden := range []string{`"regexp"`, `"regexp/syntax"`, `"path/filepath".*Match`} {
			if strings.Contains(source, forbidden) {
				t.Errorf("%s imports a regular expression package: %s", path, forbidden)
			}
		}
	}
}

func TestTheSDKReadsNoEnvironmentVariableAndNoGlobalConfig(t *testing.T) {
	for path, source := range sdkSources(t) {
		for _, forbidden := range []string{"os.Getenv", "os.Environ", "os.ExpandEnv", "flag.Parse", "flag.String"} {
			if strings.Contains(source, forbidden) {
				t.Errorf("%s reads process-wide configuration: %s", path, forbidden)
			}
		}
	}
}

func TestTheSDKWritesNoLogLine(t *testing.T) {
	for path, source := range sdkSources(t) {
		for _, forbidden := range []string{"fmt.Print", "fmt.Fprint(os", "log.Print", "log.Fatal", "os.Stdout", "os.Stderr", "println("} {
			if strings.Contains(source, forbidden) {
				t.Errorf("%s writes a log line or a stream: %s", path, forbidden)
			}
		}
	}
}

// TestTheSDKNamesOnlyTheProtocolHeaders finds every header the SDK sets by name. The ones net/http adds on its
// own are not in the source at all, and the wire capture in http_test.go lists them.
func TestTheSDKNamesOnlyTheProtocolHeaders(t *testing.T) {
	for path, source := range sdkSources(t) {
		for _, marker := range []string{`.Header().Set(`, `.Header.Set(`} {
			rest := source
			for {
				at := strings.Index(rest, marker)
				if at < 0 {
					break
				}
				rest = rest[at+len(marker):]
				if !strings.HasPrefix(rest, `"`) {
					continue // a variable: the names are checked below
				}
				end := strings.IndexByte(rest[1:], '"')
				if end < 0 {
					continue
				}
				name := rest[1 : end+1]
				if !allowedHeaders[lower(name)] {
					t.Errorf("%s sets a header outside the protocol: %s", path, name)
				}
			}
		}
	}
	// The named headers the SDK hands to its own request builder must be the protocol's four.
	for path, source := range sdkSources(t) {
		for _, name := range []string{"Authorization", "If-None-Match", "Accept", "Content-Type"} {
			if strings.Contains(source, `"`+name+`"`) && !allowedHeaders[lower(name)] {
				t.Errorf("%s names %s", path, name)
			}
		}
	}
}

func TestTheSDKReachesOnlyTheThreeRoutes(t *testing.T) {
	seen := map[string]bool{}
	for _, source := range sdkSources(t) {
		rest := source
		for {
			at := strings.Index(rest, `"/sdk/`)
			if at < 0 {
				break
			}
			rest = rest[at+1:]
			end := strings.IndexByte(rest, '"')
			if end < 0 {
				break
			}
			route := rest[:end]
			seen[route] = true
			rest = rest[end:]
		}
	}
	want := map[string]bool{"/sdk/v1/ruleset": true, "/sdk/v1/stream": true, "/sdk/v1/events": true}
	if len(seen) == 0 {
		t.Fatal("no route was found in the sources")
	}
	for route := range seen {
		if !want[route] {
			t.Errorf("the SDK reaches a route outside the protocol: %s", route)
		}
	}
	for route := range want {
		if !seen[route] {
			t.Errorf("the sources name no %s route", route)
		}
	}
}
