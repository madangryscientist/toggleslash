package toggleslash

import (
	"strings"
	"testing"
	"time"
)

const testKey = "fs_server_test"

// newTestClient starts a client on the fake server and closes it when the test ends.
func newTestClient(t *testing.T, f *fakeServer, opts ...Option) *Client {
	t.Helper()
	c := New(f.url(), testKey, opts...)
	t.Cleanup(c.Close)
	return c
}

func TestFirstLoadSendsKeyAndServes(t *testing.T) {
	f := newFakeServer(t)
	c := newTestClient(t, f, WithStream(false))
	if !c.Ready() {
		t.Fatal("Ready() = false, want true")
	}
	d := c.BoolVariationDetail("checkout", user, false)
	if !d.Value || d.VariationIndex != 1 || d.Reason.Kind != ReasonFallthrough {
		t.Fatalf("checkout = %+v", d)
	}
	reqs := f.seen("ruleset")
	if len(reqs) != 1 || reqs[0].method != "GET" {
		t.Fatalf("ruleset requests = %d", len(reqs))
	}
	if got := reqs[0].header.Get("Authorization"); got != "Bearer "+testKey {
		t.Errorf("Authorization = %q", got)
	}
	if _, has := reqs[0].header["If-None-Match"]; has {
		t.Errorf("first load sent If-None-Match")
	}
	st := c.Status()
	if st.ETag != `"v1"` || st.LastSyncAt <= 0 || st.Stream != StreamOff || st.LastError != "" {
		t.Errorf("status = %+v", st)
	}
}

func TestURLEndingInSlashIsJoinedOnce(t *testing.T) {
	f := newFakeServer(t)
	c := New(f.url()+"///", testKey, WithStream(false))
	defer c.Close()
	if !c.Ready() {
		t.Fatal("not ready")
	}
	if got := f.seen("ruleset")[0].path; got != "/sdk/v1/ruleset" {
		t.Errorf("path = %q", got)
	}
}

func TestPollSendsIfNoneMatchAndA304KeepsTheRuleset(t *testing.T) {
	f := newFakeServer(t)
	c := newTestClient(t, f, WithStream(false), WithPollInterval(30*time.Millisecond))
	c.Ready()
	eventually(t, 3*time.Second, "a second ruleset request", func() bool { return len(f.seen("ruleset")) >= 3 })
	second := f.seen("ruleset")[1]
	if got := second.header.Get("If-None-Match"); got != `"v1"` {
		t.Errorf("If-None-Match = %q", got)
	}
	if !c.BoolVariation("checkout", user, false) {
		t.Error("ruleset lost after a 304")
	}
	before := c.Status().LastSyncAt
	eventually(t, 3*time.Second, "lastSyncAt to move", func() bool { return c.Status().LastSyncAt > before })
}

func TestAChangeIsPickedUpAtTheNextPoll(t *testing.T) {
	f := newFakeServer(t)
	c := newTestClient(t, f, WithStream(false), WithPollInterval(30*time.Millisecond))
	c.Ready()
	f.setRuleset(strings.Replace(twoFlags, `"fallthrough":{"variation":1}`, `"fallthrough":{"variation":0}`, 1), `"v2"`)
	eventually(t, 3*time.Second, "the new ruleset", func() bool { return !c.BoolVariation("checkout", user, true) })
	if c.Status().ETag != `"v2"` {
		t.Errorf("etag = %q", c.Status().ETag)
	}
}

func TestETagIsKeptExactlyAsReceived(t *testing.T) {
	f := newFakeServer(t)
	f.setRuleset(`{"flags":[],"segments":[]}`, `W/"odd tag"`)
	c := newTestClient(t, f, WithStream(false), WithPollInterval(30*time.Millisecond))
	c.Ready()
	if got := c.Status().ETag; got != `W/"odd tag"` {
		t.Errorf("etag = %q", got)
	}
	eventually(t, 3*time.Second, "a second request", func() bool { return len(f.seen("ruleset")) >= 2 })
	if got := f.seen("ruleset")[1].header.Get("If-None-Match"); got != `W/"odd tag"` {
		t.Errorf("If-None-Match = %q", got)
	}
}

func TestAnEmptyETagIsNeverSentBack(t *testing.T) {
	f := newFakeServer(t)
	f.setRuleset(`{"flags":[],"segments":[]}`, "")
	c := newTestClient(t, f, WithStream(false), WithPollInterval(30*time.Millisecond))
	c.Ready()
	eventually(t, 3*time.Second, "a second request", func() bool { return len(f.seen("ruleset")) >= 2 })
	if _, has := f.seen("ruleset")[1].header["If-None-Match"]; has {
		t.Error("an empty ETag was sent as If-None-Match")
	}
	if c.Status().ETag != "" {
		t.Errorf("etag = %q", c.Status().ETag)
	}
}

func TestServerNotReachedServesTheFallbackThenLoads(t *testing.T) {
	f := newFakeServer(t)
	f.script("ruleset", answer{status: 503}, answer{status: 503})
	errs := &errorLog{}
	c := newTestClient(t, f, WithStream(false), WithPollInterval(30*time.Millisecond), WithOnError(errs.add))
	if c.Ready() {
		t.Fatal("Ready() = true after a 503")
	}
	d := c.BoolVariationDetail("checkout", user, false)
	if d.Value || d.VariationIndex != -1 || d.Reason.Kind != ReasonError || d.Reason.Code != ErrorNotReady {
		t.Errorf("detail = %+v", d)
	}
	if st := c.Status(); st.ETag != "" || st.LastSyncAt != -1 || st.LastError != "http_503" {
		t.Errorf("status = %+v", st)
	}
	if !errs.has("http_503") {
		t.Errorf("errors = %v", errs.messages())
	}
	eventually(t, 3*time.Second, "the ruleset to load", func() bool { return c.BoolVariation("checkout", user, false) })
	if st := c.Status(); st.LastError != "" || st.ETag != `"v1"` {
		t.Errorf("status = %+v", st)
	}
	if !c.Ready() {
		// ready is fixed after the first attempt
	} else {
		t.Error("Ready() changed after the first attempt")
	}
}

func TestEvaluatingWhileTheFirstLoadHangsIsNotReadyAtOnce(t *testing.T) {
	f := newFakeServer(t)
	f.script("ruleset", answer{hang: true})
	c := newTestClient(t, f, WithStream(false), WithRequestTimeout(600*time.Millisecond))
	start := time.Now()
	d := c.BoolVariationDetail("checkout", user, false)
	if time.Since(start) > 200*time.Millisecond {
		t.Errorf("variation took %s", time.Since(start))
	}
	if d.Reason.Code != ErrorNotReady {
		t.Errorf("reason = %+v", d.Reason)
	}
	if c.Ready() {
		t.Error("Ready() = true")
	}
	if got := c.Status().LastError; got != SyncTimeout {
		t.Errorf("lastError = %q", got)
	}
}

func TestAHangingBodyIsCutOffByTheTimeout(t *testing.T) {
	f := newFakeServer(t)
	f.script("ruleset", answer{hangBody: true})
	c := newTestClient(t, f, WithStream(false), WithRequestTimeout(300*time.Millisecond))
	start := time.Now()
	if c.Ready() {
		t.Fatal("ready")
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("took %s", time.Since(start))
	}
	if got := c.Status().LastError; got != SyncTimeout {
		t.Errorf("lastError = %q", got)
	}
}

func TestBadRulesetsAreRejectedAndTheLastGoodOneKept(t *testing.T) {
	for _, body := range []string{`[]`, `{"flags":[]}`, `{"flags":{},"segments":[]}`, `{"flags":[],"segments":3}`, `not json`, " ", `null`} {
		f := newFakeServer(t)
		f.script("ruleset", answer{body: body})
		c := New(f.url(), testKey, WithStream(false), WithPollInterval(time.Hour))
		if c.Ready() {
			t.Errorf("%q: ready", body)
		}
		if got := c.Status().LastError; got != SyncBadRuleset {
			t.Errorf("%q: lastError = %q", body, got)
		}
		c.Close()
	}
	// A bad ruleset after a good one leaves the good one.
	f := newFakeServer(t)
	c := newTestClient(t, f, WithStream(false), WithPollInterval(30*time.Millisecond))
	c.Ready()
	f.setRuleset(`{"flags":7,"segments":[]}`, `"v2"`)
	eventually(t, 3*time.Second, "bad_ruleset", func() bool { return c.Status().LastError == SyncBadRuleset })
	if !c.BoolVariation("checkout", user, false) || c.Status().ETag != `"v1"` {
		t.Error("the good ruleset was replaced")
	}
}

func TestA304WithNoRulesetHeldIsAnError(t *testing.T) {
	f := newFakeServer(t)
	f.script("ruleset", answer{status: 304})
	c := newTestClient(t, f, WithStream(false), WithPollInterval(time.Hour))
	if c.Ready() {
		t.Error("ready")
	}
	if got := c.Status().LastError; got != "http_304" {
		t.Errorf("lastError = %q", got)
	}
}

func TestARedirectIsNotFollowed(t *testing.T) {
	f := newFakeServer(t)
	other := newFakeServer(t)
	f.script("ruleset", answer{status: 302, header: map[string]string{"Location": other.url() + "/sdk/v1/ruleset"}})
	c := newTestClient(t, f, WithStream(false), WithPollInterval(time.Hour))
	if c.Ready() {
		t.Error("ready")
	}
	if got := c.Status().LastError; got != "http_302" {
		t.Errorf("lastError = %q", got)
	}
	if n := len(other.all()); n != 0 {
		t.Errorf("the redirect target saw %d requests", n)
	}
}

func TestAnUnreachableServerIsNetwork(t *testing.T) {
	f := newFakeServer(t)
	url := f.url()
	f.close()
	c := New(url, testKey, WithStream(false), WithPollInterval(time.Hour))
	defer c.Close()
	if c.Ready() {
		t.Error("ready")
	}
	if got := c.Status().LastError; got != SyncNetwork {
		t.Errorf("lastError = %q", got)
	}
}

func TestNonPositiveDurationsFallBackToTheDefaults(t *testing.T) {
	f := newFakeServer(t)
	c := New(f.url(), testKey, WithPollInterval(-1), WithFlushInterval(0), WithRequestTimeout(-5*time.Second))
	defer c.Close()
	if c.cfg.pollInterval != defaultInterval || c.cfg.flushInterval != defaultInterval || c.cfg.requestTimeout != defaultTimeout {
		t.Errorf("cfg = %+v", c.cfg)
	}
	if !c.Ready() {
		t.Error("not ready")
	}
}

func TestANilOptionAndABadURLNeverPanic(t *testing.T) {
	c := New("", "", nil, WithStream(false))
	defer c.Close()
	if c.Ready() {
		t.Error("ready")
	}
	if got := c.BoolVariation("x", user, true); !got {
		t.Error("fallback lost")
	}
	c2 := New("http://[::1", "k", WithStream(false))
	defer c2.Close()
	if c2.Ready() {
		t.Error("ready")
	}
}

func TestStatusStreamStates(t *testing.T) {
	f := newFakeServer(t)
	c := newTestClient(t, f)
	c.Ready()
	eventually(t, 3*time.Second, "connected", func() bool { return c.Status().Stream == StreamConnected })
	c.Close()
	if got := c.Status().Stream; got != StreamClosed {
		t.Errorf("stream = %q", got)
	}
	off := newTestClient(t, f, WithStream(false))
	if got := off.Status().Stream; got != StreamOff {
		t.Errorf("stream = %q", got)
	}
}
