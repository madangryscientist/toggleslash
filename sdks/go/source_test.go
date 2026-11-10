package toggleslash

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// librarySources is every non-test Go file of the SDK itself (not test-service, which is a program).
func librarySources(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == "test-service" || path == ".build" || path == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			out[path] = string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 10 {
		t.Fatalf("found only %d source files; the walk is wrong", len(out))
	}
	return out
}

// docs/evaluation.md has its own regular expression dialect, so nothing may reach for the platform's.
func TestNoPlatformRegexpInTheSDK(t *testing.T) {
	for path, src := range librarySources(t) {
		if strings.Contains(src, `"regexp"`) || strings.Contains(src, `"regexp/syntax"`) {
			t.Errorf("%s imports the platform regexp package", path)
		}
	}
}

// No log line, no print, no error value built from a context, a key or an attribute.
func TestNoLoggingAndNoContextInErrors(t *testing.T) {
	forbidden := []string{`"log"`, `"log/slog"`, "fmt.Print", "println(", "os.Stderr", "os.Stdout"}
	for path, src := range librarySources(t) {
		for _, f := range forbidden {
			if strings.Contains(src, f) {
				t.Errorf("%s uses %s", path, f)
			}
		}
	}
	// Every *Error the SDK builds names an operation and a fixed message. None may interpolate anything but
	// a status code or a count.
	build := regexp.MustCompile(`&Error\{[^}]*Message:\s*([^,}]*)`)
	for path, src := range librarySources(t) {
		for _, m := range build.FindAllStringSubmatch(src, -1) {
			msg := m[1]
			for _, bad := range []string{"flagKey", "ctx", "context", "sdkKey", "cfg.url", "c.cfg.url", "attr", "key"} {
				if strings.Contains(msg, bad) {
					t.Errorf("%s: an Error message mentions %q: %s", path, bad, msg)
				}
			}
		}
	}
}

// The SDK sets exactly the four headers the contract allows.
func TestOnlyTheAllowedHeadersAreSet(t *testing.T) {
	set := regexp.MustCompile(`Header\.(?:Set|Add)\("([^"]+)"`)
	allowed := map[string]bool{"Authorization": true, "Accept": true, "If-None-Match": true, "Content-Type": true}
	found := map[string]bool{}
	for path, src := range librarySources(t) {
		for _, m := range set.FindAllStringSubmatch(src, -1) {
			found[m[1]] = true
			if !allowed[m[1]] {
				t.Errorf("%s sets header %s", path, m[1])
			}
		}
		if strings.Contains(src, "Header = ") || strings.Contains(src, "Header[") {
			t.Errorf("%s writes a header map directly", path)
		}
	}
	for name := range allowed {
		if !found[name] {
			t.Errorf("header %s is never set", name)
		}
	}
}

// The seams are fields of an unexported config, never package variables or environment variables.
func TestNoEnvironmentVariablesAreRead(t *testing.T) {
	for path, src := range librarySources(t) {
		for _, line := range strings.Split(src, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue // the package example reads the key from the environment, as a caller would
			}
			if strings.Contains(line, "os.Getenv") || strings.Contains(line, "os.LookupEnv") || strings.Contains(line, "os.Environ") {
				t.Errorf("%s reads the environment: %s", path, line)
			}
		}
	}
}
