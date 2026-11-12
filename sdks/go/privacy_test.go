package toggleslash

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

const sentinel = "SENTINEL-4f2a9c"

func TestNoRequestCarriesTheContext(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	var log errorLog
	c := newTestClient(t, s, WithOnError(log.add), WithAfterEvaluation(func(Event) {}))
	c.Ready()
	ctx := NewContext(sentinel, map[string]any{"email": sentinel + "@example.com"})
	for i := 0; i < 5; i++ {
		c.BoolVariation("a", ctx, false)
		c.BoolVariation("missing", ctx, false)
	}
	s.setFlags(boolFlag("a", false))
	waitFor(t, "stream flip", func() bool { return !c.BoolVariation("a", user("z"), true) })
	c.Close()
	for _, r := range s.all() {
		blob := r.Method + r.Path + string(r.Body)
		for k, vs := range r.Header {
			blob += k + strings.Join(vs, ",")
		}
		if strings.Contains(blob, sentinel) {
			t.Fatalf("request %s %s carries the context", r.Method, r.Path)
		}
	}
	for _, m := range log.messages() {
		if strings.Contains(m, sentinel) {
			t.Fatalf("error carries the context: %q", m)
		}
	}
	st := c.Status()
	if blob := string(st.Stream) + string(st.LastError) + st.ETag; strings.Contains(blob, sentinel) || strings.Contains(blob, testKey) {
		t.Fatalf("status = %+v", st)
	}
}

func TestHeaderAudit(t *testing.T) {
	s := newFakeServer(t, boolFlag("a", true))
	c := newTestClient(t, s)
	c.Ready()
	waitFor(t, "connected", func() bool { return c.Status().Stream == StreamConnected })
	c.BoolVariation("a", user("u"), false)
	c.refresh(false)
	c.Close()
	allowed := map[string]bool{
		"Authorization": true, "Accept": true, "If-None-Match": true, "Content-Type": true,
		"User-Agent": true, "Content-Length": true,
	}
	seen := map[string]bool{}
	for _, r := range s.all() {
		for k := range r.Header {
			seen[k] = true
			if !allowed[k] {
				t.Errorf("%s %s sent header %s", r.Method, r.Path, k)
			}
		}
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			t.Errorf("%s %s Authorization = %q", r.Method, r.Path, r.Header.Get("Authorization"))
		}
	}
	for _, r := range s.all() {
		if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "Go-http-client/") {
			t.Errorf("User-Agent = %q", ua)
		}
	}
	for _, k := range []string{"Authorization", "Accept", "If-None-Match", "Content-Type", "User-Agent", "Content-Length"} {
		if !seen[k] {
			t.Errorf("never saw %s", k)
		}
	}
}

func TestSourceNeverMentionsTheContextWhereRequestsAreBuilt(t *testing.T) {
	for _, file := range []string{"sync.go", "usage.go", "sse.go"} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		// The only Context there is the standard library's; the SDK's Context, its constructors and the hook's event
		// do not appear in the code that builds a request.
		text := strings.ReplaceAll(string(src), "context.Context", "")
		for _, banned := range []string{" Event{", "(Event{", "NewContext", "ParseContext", "afterEval", "Context)", " Context "} {
			if strings.Contains(text, banned) {
				t.Errorf("%s mentions %s", file, banned)
			}
		}
	}
}

func TestTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sdk/v1/ruleset" {
			w.Header().Set("ETag", `"t1"`)
			_, _ = w.Write([]byte(`{"flags":[` + boolFlag("a", true) + `],"segments":[]}`))
			return
		}
		w.WriteHeader(204)
	}))
	t.Cleanup(srv.Close)

	trusted := New(srv.URL, testKey, WithStream(false), WithRequestTimeout(2*time.Second), WithHTTPClient(srv.Client()))
	t.Cleanup(trusted.Close)
	if !trusted.Ready() || !trusted.BoolVariation("a", user("u"), false) {
		t.Fatal("a client that trusts the certificate did not load")
	}

	// The default client verifies certificates and the test server's is self-signed.
	var log errorLog
	strict := New(srv.URL, testKey, WithStream(false), WithRequestTimeout(2*time.Second), WithOnError(log.add))
	t.Cleanup(strict.Close)
	if strict.Ready() {
		t.Fatal("the default client accepted a self-signed certificate")
	}
	if got := strict.Status().LastError; got != LastErrorNetwork {
		t.Fatalf("lastError = %q", got)
	}
	_ = tls.VersionTLS12
}
