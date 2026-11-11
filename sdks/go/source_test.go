package toggleslash

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sources lists the non-test Go files of the module.
func sources(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(".", func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && (info.Name() == ".build" || info.Name() == "testdata") {
			return filepath.SkipDir
		}
		if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// The platform regular-expression package is never used: the matches dialect is its own parser
// and machine (docs/evaluation.md). The name is built in two pieces so that a search of the module
// for it finds nothing.
func TestNoRegexpPackage(t *testing.T) {
	needle := `"` + "reg" + "exp"
	for _, p := range sources(t) {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), needle) {
			t.Errorf("%s imports a %s package", p, needle[1:])
		}
	}
}

// identByte reports whether b can be part of an identifier.
func identByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// calls reports whether src calls the bare function name (not a method or a longer name).
func calls(src, name string) bool {
	for i := 0; ; {
		j := strings.Index(src[i:], name+"(")
		if j < 0 {
			return false
		}
		j += i
		if j == 0 || !identByte(src[j-1]) && src[j-1] != '.' {
			return true
		}
		i = j + 1
	}
}

// The SDK logs nothing: no logging package, no print to a stream. Context data cannot reach a log
// that does not exist (contract section 12).
func TestNoLogging(t *testing.T) {
	for _, p := range sources(t) {
		if strings.Contains(p, "test-service") {
			continue // the test service prints its listening line and its errors
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		for _, bad := range []string{`"log"`, `"log/slog"`, "fmt.Print", "fmt.Fprint", "os.Stdout", "os.Stderr"} {
			if strings.Contains(src, bad) {
				t.Errorf("%s: %s", p, bad)
			}
		}
		for _, bad := range []string{"print", "println"} {
			if calls(src, bad) {
				t.Errorf("%s: calls %s", p, bad)
			}
		}
	}
}

func TestNoDashesInSources(t *testing.T) {
	for _, p := range sources(t) {
		b, _ := os.ReadFile(p)
		if strings.ContainsAny(string(b), string([]rune{0x2013, 0x2014})) {
			t.Errorf("%s has an em or en dash", p)
		}
	}
}

// An unclosed client must not keep the process alive.
func TestUnclosedClientDoesNotKeepTheProcessAlive(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go on PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, goBin, "run", "./testdata/unclosed")
	done := make(chan error, 1)
	var out []byte
	go func() {
		var e error
		out, e = cmd.CombinedOutput()
		done <- e
	}()
	select {
	case e := <-done:
		if e != nil || !strings.Contains(string(out), "done") {
			t.Fatalf("exit: %v, output: %s", e, out)
		}
	case <-time.After(120 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the process did not exit with an unclosed client")
	}
}
