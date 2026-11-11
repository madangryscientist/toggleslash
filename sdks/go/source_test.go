package toggleslash

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// sourceFiles lists the non-test Go files under dir.
func sourceFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "build" || d.Name() == "test-service") {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func readAll(t *testing.T, files []string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		out[f] = string(b)
	}
	return out
}

// The only request headers the SDK sets are the four the contract lists. net/http adds its own (Host,
// User-Agent, Accept-Encoding, Content-Length), which RUN.md lists per route from a loopback capture.
func TestSourceSetsNoRequestHeaderBeyondTheFourTheContractNames(t *testing.T) {
	allowed := []string{"Authorization", "Accept", "If-None-Match", "Content-Type"}
	call := regexp.MustCompile(`Header\.(?:Set|Add|Del)\(\s*"([^"]+)"`)
	index := regexp.MustCompile(`Header\[\s*"([^"]+)"\s*\]\s*=`)
	literal := regexp.MustCompile(`http\.Header\{`)
	found := map[string]bool{}
	for file, src := range readAll(t, sourceFiles(t, ".")) {
		for _, re := range []*regexp.Regexp{call, index} {
			for _, m := range re.FindAllStringSubmatch(src, -1) {
				found[m[1]] = true
				if !slices.Contains(allowed, m[1]) {
					t.Errorf("%s sets the header %q", file, m[1])
				}
			}
		}
		if literal.MatchString(src) {
			t.Errorf("%s builds a header map by hand", file)
		}
	}
	for _, name := range allowed {
		if !found[name] {
			t.Errorf("expected the SDK to set %s", name)
		}
	}
}

// Nothing in the SDK writes to a log, stdout or stderr: a context in an error or a log line is a leak.
func TestSourceNeverLogs(t *testing.T) {
	banned := regexp.MustCompile(`\blog\.|"log"|"log/slog"|fmt\.(?:Print|Fprint)|os\.Std(?:out|err)|println\(`)
	for file, src := range readAll(t, sourceFiles(t, ".")) {
		if banned.MatchString(src) {
			t.Errorf("%s writes somewhere it should not: %s", file, banned.FindString(src))
		}
	}
}

// The evaluator has its own pattern matcher, to docs/evaluation.md, and no platform regex.
func TestEvaluatorUsesNoPlatformRegex(t *testing.T) {
	for file, src := range readAll(t, sourceFiles(t, "internal")) {
		if strings.Contains(src, `"regexp`) {
			t.Errorf("%s imports a regexp package", file)
		}
	}
	for file, src := range readAll(t, sourceFiles(t, ".")) {
		if strings.Contains(src, `"regexp`) {
			t.Errorf("%s imports a regexp package", file)
		}
	}
}

// A runtime dependency would need a DESIGN.md line; there are none, so go.mod has no require.
func TestNoRuntimeDependencies(t *testing.T) {
	b, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "require") {
		t.Errorf("go.mod requires a module:\n%s", b)
	}
}

const helperEnv = "TOGGLESLASH_TEST_NEVER_CLOSED_URL"

// TestHelperNeverClosedClient is the child of TestANeverClosedClientDoesNotKeepTheProcessAlive: it runs
// only when the parent sets the URL, makes a client, uses it and returns without Close.
func TestHelperNeverClosedClient(t *testing.T) {
	url := os.Getenv(helperEnv)
	if url == "" {
		t.Skip("a helper for TestANeverClosedClientDoesNotKeepTheProcessAlive")
	}
	c := NewClient(url, "k", WithPollInterval(time.Hour), WithFlushInterval(time.Hour))
	if !c.Ready() || !c.BoolVariation("f", user("u"), false) {
		t.Fatal("the helper's client did not serve")
	}
}

func TestANeverClosedClientDoesNotKeepTheProcessAlive(t *testing.T) {
	f := newFakeServer(t)
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestHelperNeverClosedClient$", "-test.count=1")
	cmd.Env = append(os.Environ(), helperEnv+"="+f.url())
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the helper process failed: %v", err)
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		t.Fatal("the helper process was still running 10 s after its test returned")
	}
	if f.count("/sdk/v1/stream") == 0 {
		t.Error("the helper never opened the stream, so it proved nothing")
	}
}
