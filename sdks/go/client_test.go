package toggleslash

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/eval"
	"github.com/madangryscientist/toggleslash/sdks/go/jsonval"
)

// flagSpec builds the ruleset JSON of one well formed flag. Salt is always
// set, because the evaluator treats a flag without one as malformed.
type flagSpec struct {
	key                  string
	version              int
	variations           string
	fallthroughVariation int
	rules                string
	on                   *bool
}

func (f flagSpec) json() string {
	on := "true"
	if f.on != nil {
		on = strconv.FormatBool(*f.on)
	}
	rules := f.rules
	if rules == "" {
		rules = "[]"
	}
	return `{"key":"` + f.key + `","on":` + on +
		`,"version":` + strconv.Itoa(f.version) +
		`,"salt":"salt-` + f.key + `","variations":` + f.variations +
		`,"offVariation":0,"targets":[],"prerequisites":[],"rules":` + rules +
		`,"fallthrough":{"variation":` + strconv.Itoa(f.fallthroughVariation) + `}}`
}

func rulesetOf(flags ...string) string {
	return `{"flags":[` + strings.Join(flags, ",") + `],"segments":[]}`
}

func boolFlag(key string, version int, fallthroughVariation int) string {
	return flagSpec{key: key, version: version, variations: `[false,true]`, fallthroughVariation: fallthroughVariation}.json()
}

// userContext is the single context a test evaluates for.
func userContext(key string) jsonval.Value {
	return jsonval.Object{"key": jsonval.String(key)}
}

// clockOptions are the options a fake clock test starts from: no stream, no
// jitter, and intervals long enough that only a deliberate step fires one.
func clockOptions(fake *fakeServer, clk *fakeClock) Options {
	off := false
	return Options{
		URL:            fake.url(),
		SDKKey:         "test-key",
		Stream:         &off,
		PollInterval:   time.Minute,
		FlushInterval:  time.Minute,
		RequestTimeout: 2 * time.Second,
		clock:          clk,
		jitter:         func() float64 { return 0 },
	}
}

// eventually waits for cond on the real clock. It observes state the SDK
// reaches asynchronously; it never steps a fake clock.
func eventually(t *testing.T, within time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: not reached within %v", what, within)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func newClient(t *testing.T, opts Options) *Client {
	t.Helper()
	client, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(client.Close)
	return client
}

// decodeBody reads a recorded events body as a generic document.
func decodeBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatalf("events body is not JSON: %v", err)
	}
	return document
}

func TestNotReadyBeforeTheFirstLoad(t *testing.T) {
	fake := newFakeServer(t)
	release := make(chan struct{})
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}
	opts := clockOptions(fake, newFakeClock())
	stream := false
	opts.Stream = &stream
	client := newClient(t, opts)
	defer close(release)

	detail := client.BoolVariationDetail("greeting", userContext("u1"), false)
	if detail.Value {
		t.Fatalf("a client with no ruleset served %v", detail.Value)
	}
	if detail.VariationIndex != nil {
		t.Fatalf("a fallback carried a variation index %d", *detail.VariationIndex)
	}
	if detail.Reason.Kind != ReasonError || detail.Reason.Code != CodeNotReady {
		t.Fatalf("reason = %+v, want ERROR NOT_READY", detail.Reason)
	}
	status := client.Status()
	if status.ETag != "" || status.LastSyncAt != nil || status.LastError != nil {
		t.Fatalf("status = %+v, want nothing loaded", status)
	}
	if status.Stream != StreamOff {
		t.Fatalf("stream = %q, want off", status.Stream)
	}
}

func TestReadyAndStatusAfterTheFirstLoad(t *testing.T) {
	fake := newFakeServer(t)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
	}
	client := newClient(t, clockOptions(fake, newFakeClock()))
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}
	status := client.Status()
	if status.ETag != `"v1"` {
		t.Fatalf("etag = %q, want %q", status.ETag, `"v1"`)
	}
	if status.LastSyncAt == nil {
		t.Fatalf("lastSyncAt is nil after a 200")
	}
	if status.LastError != nil {
		t.Fatalf("lastError = %v after a 200", *status.LastError)
	}
	if !client.BoolVariation("feature", userContext("u1"), false) {
		t.Fatalf("variation = false, want the served true")
	}
}

// TestReadyStaysFixedWithACancelledContext is contract 5.3's "the answer is
// fixed": once the first load settles true, a context that is already done must
// not win the select and answer false.
func TestReadyStaysFixedWithACancelledContext(t *testing.T) {
	fake := newFakeServer(t)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
	}
	client := newClient(t, clockOptions(fake, newFakeClock()))
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := 0; i < 200; i++ {
		if !client.Ready(ctx) {
			t.Fatalf("Ready = false with a cancelled context after it settled true")
		}
	}
}

// TestNetworkErrorMessageCarriesNoURL is contract 12 and the Go notes' "A
// network error's text carries the URL": onError's text is the fixed code.
func TestNetworkErrorMessageCarriesNoURL(t *testing.T) {
	off := false
	errors := make(chan error, 4)
	newClient(t, Options{
		URL:            "http://127.0.0.1:1",
		SDKKey:         "test-key",
		Stream:         &off,
		PollInterval:   time.Minute,
		FlushInterval:  time.Minute,
		RequestTimeout: 500 * time.Millisecond,
		clock:          newFakeClock(),
		OnError:        func(err error) { errors <- err },
	})
	select {
	case err := <-errors:
		text := err.Error()
		if strings.Contains(text, "127.0.0.1") || strings.Contains(text, "http") {
			t.Fatalf("onError text = %q, want a fixed code without the URL", text)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("no onError after a refused connection")
	}
}

// TestJSONAccessorReturnsACopy covers the mutation hazard: an Array or Object
// in a ruleset is shared storage, so the caller's value must be its own copy.
func TestJSONAccessorReturnsACopy(t *testing.T) {
	fake := newFakeServer(t)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("ETag", `"v1"`)
		flag := flagSpec{key: "list", version: 1, variations: `[[1,2],["other"]]`, fallthroughVariation: 0}
		_, _ = w.Write([]byte(rulesetOf(flag.json())))
	}
	client := newClient(t, clockOptions(fake, newFakeClock()))
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}
	first := client.JSONVariationDetail("list", userContext("u1"), nil)
	list, ok := first.Value.(jsonval.Array)
	if !ok || len(list) != 2 {
		t.Fatalf("first = %#v, want a two element array", first.Value)
	}
	list[0] = jsonval.Number(9)
	second := client.JSONVariationDetail("list", userContext("u1"), nil)
	again, ok := second.Value.(jsonval.Array)
	if !ok || len(again) != 2 || again[0] != jsonval.Number(1) {
		t.Fatalf("a caller's mutation changed the ruleset: %#v", second.Value)
	}
}

// roundTripperFunc is a bare http.RoundTripper for the default transport test.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestNewWithACustomDefaultTransport is contract 5.1: an application may
// replace http.DefaultTransport, and New must not panic at a type assertion.
func TestNewWithACustomDefaultTransport(t *testing.T) {
	saved := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = saved })
	http.DefaultTransport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("the test's transport is not meant to answer")
	})
	off := false
	client, err := New(Options{
		URL:     "http://127.0.0.1:1",
		SDKKey:  "test-key",
		Stream:  &off,
		OnError: func(error) {},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	client.Close()
}

func TestNotModifiedIsASync(t *testing.T) {
	fake := newFakeServer(t)
	var requests atomic.Int64
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		if requests.Add(1) == 1 {
			w.Header().Set("ETag", `"v1"`)
			_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
			return
		}
		if got := r.Header.Get("If-None-Match"); got != `"v1"` {
			t.Errorf("If-None-Match = %q, want the ETag held", got)
		}
		w.WriteHeader(http.StatusNotModified)
	}
	clk := newFakeClock()
	client := newClient(t, clockOptions(fake, clk))
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}
	first := client.Status().LastSyncAt

	clk.advanceUntil(t, 3*time.Second, func() bool { return requests.Load() >= 2 })

	// The server has the request; the client's sync store follows it a moment
	// later. This waits for the assertion rather than for the request, which a
	// loaded machine only widens (RUN.md, decision 59).
	eventually(t, 3*time.Second, func() bool {
		synced := client.Status().LastSyncAt
		return synced != nil && synced.After(*first)
	}, "lastSyncAt moving on a 304")

	status := client.Status()
	if status.LastError != nil {
		t.Fatalf("lastError = %v after a 304", *status.LastError)
	}
	if status.LastSyncAt == nil || !status.LastSyncAt.After(*first) {
		t.Fatalf("lastSyncAt did not move on a 304")
	}
}

func TestBadRulesetDoesNotReplaceTheOneHeld(t *testing.T) {
	fake := newFakeServer(t)
	bodies := []string{rulesetOf(boolFlag("feature", 1, 1)), `{"flags":{},"segments":[]}`, `not json`}
	var requests atomic.Int64
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		index := int(requests.Load())
		if index >= len(bodies) {
			index = len(bodies) - 1
		}
		requests.Add(1)
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(bodies[index]))
	}
	clk := newFakeClock()
	client := newClient(t, clockOptions(fake, clk))
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}

	for attempt := 0; attempt < 2; attempt++ {
		clk.advanceUntil(t, 3*time.Second, func() bool { return requests.Load() >= int64(attempt+2) })
	}
	status := client.Status()
	if status.LastError == nil || *status.LastError != LastErrorBadRuleset {
		t.Fatalf("lastError = %v, want bad_ruleset", status.LastError)
	}
	if status.ETag != `"v1"` {
		t.Fatalf("etag = %q after a bad ruleset, want the one held", status.ETag)
	}
	if !client.BoolVariation("feature", userContext("u1"), false) {
		t.Fatalf("the last good ruleset stopped answering")
	}
}

func TestRequestHeadersAndPrivacy(t *testing.T) {
	fake := newFakeServer(t)
	rulesetBody := rulesetOf(boolFlag("greeting", 1, 1))
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(rulesetBody))
	}
	fake.events = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.WriteHeader(http.StatusOK)
	}
	clk := newFakeClock()
	client := newClient(t, clockOptions(fake, clk))
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}
	client.BoolVariation("greeting", userContext("private-user"), false)
	client.Close()

	rulesetRequests := fake.recorded("/sdk/v1/ruleset")
	if len(rulesetRequests) == 0 {
		t.Fatalf("no ruleset request was made")
	}
	first := rulesetRequests[0]
	if got := first.headers.Get("Authorization"); got != "Bearer test-key" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := first.headers.Get("If-None-Match"); got != "" {
		t.Fatalf("If-None-Match = %q before any ruleset was held", got)
	}

	eventsRequests := fake.recorded("/sdk/v1/events")
	if len(eventsRequests) != 1 {
		t.Fatalf("events requests = %d, want one final flush", len(eventsRequests))
	}
	if got := eventsRequests[0].headers.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}

	fake.mu.Lock()
	seen := fake.requests
	fake.mu.Unlock()
	for _, record := range seen {
		if strings.Contains(string(record.body), "private-user") || strings.Contains(record.path, "private-user") {
			t.Fatalf("a request carried the context key: %s %s", record.method, record.path)
		}
		if record.headers.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("%s carried Authorization %q", record.path, record.headers.Get("Authorization"))
		}
	}
}

func TestStreamRequestHeaders(t *testing.T) {
	fake := newFakeServer(t)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
	}
	fake.stream = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: connected\ndata: \"v1\"\n\nevent: changed\ndata: \"v1\"\n\n"))
	}
	on := true
	opts := clockOptions(fake, newFakeClock())
	opts.Stream = &on
	client := newClient(t, opts)
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}
	eventually(t, 2*time.Second, func() bool { return fake.count("/sdk/v1/stream") >= 1 }, "a stream attempt")
	requests := fake.recorded("/sdk/v1/stream")
	if got := requests[0].headers.Get("Accept"); got != "text/event-stream" {
		t.Fatalf("Accept = %q", got)
	}
	if got := requests[0].headers.Get("Authorization"); got != "Bearer test-key" {
		t.Fatalf("Authorization = %q", got)
	}
}

// TestRequestHeaderAudit captures one request of each route the SDK uses on the
// loopback fake and prints every header the client put on it, Host included:
// the table CHECKLIST.md ("Code and documents") asks RUN.md to carry. It
// asserts the set too, so a header added later fails here before the table is
// written.
func TestRequestHeaderAudit(t *testing.T) {
	fake := newFakeServer(t)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		if r.Header.Get("If-None-Match") == "" {
			w.Header().Set("ETag", `"v1"`)
			_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
			return
		}
		w.WriteHeader(http.StatusNotModified)
	}
	fake.events = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.WriteHeader(http.StatusOK)
	}

	clk := newFakeClock()
	polling := newClient(t, clockOptions(fake, clk))
	if !polling.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}
	polling.BoolVariation("feature", userContext("header-audit"), false)
	clk.advanceUntil(t, 3*time.Second, func() bool { return fake.count("/sdk/v1/ruleset") >= 2 })
	polling.Close()

	fake.stream = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: connected\ndata: \"v1\"\n\n"))
	}
	on := true
	opts := clockOptions(fake, newFakeClock())
	opts.Stream = &on
	streaming := newClient(t, opts)
	if !streaming.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}
	eventually(t, 2*time.Second, func() bool { return fake.count("/sdk/v1/stream") >= 1 }, "a stream request")
	streaming.Close()

	rulesets := fake.recorded("/sdk/v1/ruleset")
	if len(rulesets) < 2 {
		t.Fatalf("ruleset requests = %d, want the first and a conditional one", len(rulesets))
	}
	conditional := -1
	for i, record := range rulesets {
		if record.headers.Get("If-None-Match") != "" {
			conditional = i
			break
		}
	}
	if conditional < 0 {
		t.Fatalf("no ruleset request carried If-None-Match")
	}
	streams := fake.recorded("/sdk/v1/stream")
	events := fake.recorded("/sdk/v1/events")
	if len(streams) == 0 || len(events) == 0 {
		t.Fatalf("routes captured: %d stream, %d events, want one each", len(streams), len(events))
	}

	// What the client puts on each route, and what is allowed beside it: the
	// headers Go's transport adds on its own.
	allowed := map[string]map[string]bool{
		"GET /sdk/v1/ruleset (first)":         {"Authorization": true},
		"GET /sdk/v1/ruleset (If-None-Match)": {"Authorization": true, "If-None-Match": true},
		"GET /sdk/v1/stream":                  {"Authorization": true, "Accept": true},
		"POST /sdk/v1/events":                 {"Authorization": true, "Content-Type": true},
	}
	transport := map[string]bool{"User-Agent": true, "Accept-Encoding": true, "Content-Length": true}
	printRecord := func(route string, record requestRecord) {
		names := make([]string, 0, len(record.headers))
		for name := range record.headers {
			names = append(names, name)
		}
		sort.Strings(names)
		parts := make([]string, 0, len(names))
		for _, name := range names {
			if !allowed[route][name] && !transport[name] {
				t.Errorf("%s carried %s, which the audit does not allow", route, name)
			}
			parts = append(parts, name+": "+strings.Join(record.headers[name], ","))
		}
		t.Logf("%s %s Host=%s | %s", record.method, record.path, record.host, strings.Join(parts, " | "))
	}
	printRecord("GET /sdk/v1/ruleset (first)", rulesets[0])
	printRecord("GET /sdk/v1/ruleset (If-None-Match)", rulesets[conditional])
	printRecord("GET /sdk/v1/stream", streams[0])
	printRecord("POST /sdk/v1/events", events[0])
}

func TestTypeMismatchAndNullFallback(t *testing.T) {
	fake := newFakeServer(t)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		_, _ = w.Write([]byte(rulesetOf(
			flagSpec{key: "greeting", version: 1, variations: `["hi"]`, fallthroughVariation: 0}.json(),
			flagSpec{key: "count", version: 1, variations: `[3]`, fallthroughVariation: 0}.json(),
		)))
	}
	client := newClient(t, clockOptions(fake, newFakeClock()))
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}

	asBool := client.BoolVariationDetail("greeting", userContext("u1"), false)
	if asBool.Value || asBool.Reason.Code != CodeTypeMismatch {
		t.Fatalf("served string as bool: %+v", asBool)
	}
	if asBool.VariationIndex != nil {
		t.Fatalf("a mismatch carried a variation index")
	}

	// A null fallback accepts any served value.
	nullFallback := client.JSONVariation("count", userContext("u1"), jsonval.Null{})
	if number, ok := nullFallback.(jsonval.Number); !ok || float64(number) != 3 {
		t.Fatalf("null fallback got %#v, want the served 3", nullFallback)
	}
	// A non-null fallback compares kinds: an object fallback against a served
	// number is a mismatch.
	mismatch := client.JSONVariationDetail("count", userContext("u1"), jsonval.Object{})
	if mismatch.Reason.Code != CodeTypeMismatch {
		t.Fatalf("object fallback against a number = %+v, want TYPE_MISMATCH", mismatch)
	}
	// The same kind is served.
	same := client.JSONVariationDetail("count", userContext("u1"), jsonval.Number(0))
	if same.Value == nil || same.VariationIndex == nil || same.Reason.Kind == ReasonError {
		t.Fatalf("number fallback against a number = %+v", same)
	}
}

func TestIntVariationTypedRules(t *testing.T) {
	fake := newFakeServer(t)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		_, _ = w.Write([]byte(rulesetOf(
			flagSpec{key: "whole", version: 1, variations: `[2]`, fallthroughVariation: 0}.json(),
			flagSpec{key: "fraction", version: 1, variations: `[2.5]`, fallthroughVariation: 0}.json(),
			flagSpec{key: "huge", version: 1, variations: `[1e19]`, fallthroughVariation: 0}.json(),
			flagSpec{key: "negative-huge", version: 1, variations: `[-1e19]`, fallthroughVariation: 0}.json(),
			flagSpec{key: "exact", version: 1, variations: `[9007199254740992]`, fallthroughVariation: 0}.json(),
		)))
	}
	client := newClient(t, clockOptions(fake, newFakeClock()))
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}

	cases := []struct {
		key      string
		value    int64
		mismatch bool
	}{
		{key: "whole", value: 2},
		{key: "fraction", mismatch: true},
		{key: "huge", mismatch: true},
		{key: "negative-huge", mismatch: true},
		{key: "exact", value: 9007199254740992},
	}
	for _, testCase := range cases {
		detail := client.IntVariationDetail(testCase.key, userContext("u1"), -7)
		mismatch := detail.Reason.Code == CodeTypeMismatch
		if mismatch != testCase.mismatch {
			t.Fatalf("%s: reason = %+v, want mismatch %v", testCase.key, detail.Reason, testCase.mismatch)
		}
		if !testCase.mismatch && detail.Value != testCase.value {
			t.Fatalf("%s: value = %d, want %d", testCase.key, detail.Value, testCase.value)
		}
	}
}

func TestEvaluationThatRaises(t *testing.T) {
	fake := newFakeServer(t)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
	}
	errors := make(chan error, 4)
	opts := clockOptions(fake, newFakeClock())
	opts.OnError = func(err error) { errors <- err }
	opts.evaluate = func(ruleset *eval.Ruleset, flagKey string, context jsonval.Value) eval.Result {
		panic("boom")
	}
	client := newClient(t, opts)
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}

	detail := client.BoolVariationDetail("feature", userContext("u1"), true)
	if !detail.Value {
		t.Fatalf("the caller did not get the fallback")
	}
	if detail.Reason.Kind != ReasonError || detail.Reason.Code != CodeEvaluationFailed {
		t.Fatalf("reason = %+v, want ERROR EVALUATION_FAILED", detail.Reason)
	}
	if detail.VariationIndex != nil {
		t.Fatalf("a failed evaluation carried a variation index")
	}
	select {
	case err := <-errors:
		if err == nil {
			t.Fatalf("onError was called with a nil error")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("no onError call for an evaluation that raised")
	}
}

func TestHookEventCarriesTheRulesetAndVersion(t *testing.T) {
	fake := newFakeServer(t)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("ETag", `"v7"`)
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 7, 1))))
	}
	events := make(chan Event, 4)
	opts := clockOptions(fake, newFakeClock())
	opts.Hooks.AfterEvaluation = func(event Event) { events <- event }
	client := newClient(t, opts)
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}
	client.BoolVariation("feature", userContext("u1"), false)

	event := <-events
	if event.ETag != `"v7"` {
		t.Fatalf("hook etag = %q", event.ETag)
	}
	if event.FlagVersion == nil || *event.FlagVersion != 7 {
		t.Fatalf("hook flagVersion = %v, want 7", event.FlagVersion)
	}
	if event.VariationIndex == nil || *event.VariationIndex != 1 {
		t.Fatalf("hook variationIndex = %v, want 1", event.VariationIndex)
	}
	if value, ok := event.Value.(jsonval.Bool); !ok || !bool(value) {
		t.Fatalf("hook value = %#v, want true", event.Value)
	}
	if event.FlagKey != "feature" {
		t.Fatalf("hook flagKey = %q", event.FlagKey)
	}
}

func TestHookValueForTheIntegerAccessor(t *testing.T) {
	fake := newFakeServer(t)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		_, _ = w.Write([]byte(rulesetOf(
			flagSpec{key: "big", version: 1, variations: `[1152921504606846976]`, fallthroughVariation: 0}.json(),
		)))
	}
	events := make(chan Event, 4)
	opts := clockOptions(fake, newFakeClock())
	opts.Hooks.AfterEvaluation = func(event Event) { events <- event }
	client := newClient(t, opts)
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}
	got := client.IntVariation("big", userContext("u1"), 0)
	if got != 1152921504606846976 {
		t.Fatalf("IntVariation = %d, want the exact integer", got)
	}
	event := <-events
	number, ok := event.Value.(jsonval.Number)
	if !ok {
		t.Fatalf("hook value type = %T, want the JSON number the mapping table names", event.Value)
	}
	if float64(number) != 1152921504606846976 {
		t.Fatalf("hook value = %v, want the nearest double of the integer", float64(number))
	}
}

func TestContextNumericTypesMatch(t *testing.T) {
	fake := newFakeServer(t)
	clause := `{"op":"gt","attribute":"age","values":[40]}`
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		_, _ = w.Write([]byte(rulesetOf(flagSpec{
			key:                  "adult",
			version:              1,
			variations:           `[false,true]`,
			fallthroughVariation: 0,
			rules:                `[{"clauses":[` + clause + `],"variation":1}]`,
		}.json())))
	}
	client := newClient(t, clockOptions(fake, newFakeClock()))
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}

	// Every numeric width Go has, converted the way a caller does: the JSON
	// number of the value, read as binary64 (contract 3).
	numbers := map[string]float64{
		"int":     float64(int(41)),
		"int8":    float64(int8(41)),
		"int16":   float64(int16(41)),
		"int32":   float64(int32(41)),
		"int64":   float64(int64(41)),
		"uint":    float64(uint(41)),
		"uint8":   float64(uint8(41)),
		"uint16":  float64(uint16(41)),
		"uint32":  float64(uint32(41)),
		"uint64":  float64(uint64(41)),
		"float32": float64(float32(41)),
		"float64": 41.0,
	}
	for name, number := range numbers {
		value := jsonval.Object{
			"key":        jsonval.String("u1"),
			"attributes": jsonval.Object{"age": jsonval.Number(number)},
		}
		if !client.BoolVariation("adult", value, false) {
			t.Fatalf("%s: the numeric attribute did not match as a JSON number", name)
		}
	}
}

// TestDelayFollowsTheContract is contract 7.3's backoff: pollInterval with the
// stream off, and min(pollInterval, 1s * 2^failures) halved by a jitter of 0.
func TestDelayFollowsTheContract(t *testing.T) {
	fake := newFakeServer(t)
	polling := newClient(t, clockOptions(fake, newFakeClock()))
	if got := polling.delay(5); got != time.Minute {
		t.Fatalf("delay with the stream off = %v, want the poll interval", got)
	}
	on := true
	opts := clockOptions(fake, newFakeClock())
	opts.Stream = &on
	opts.PollInterval = 3 * time.Second
	streaming := newClient(t, opts)
	cases := []struct {
		failures int
		want     time.Duration
	}{
		{0, 500 * time.Millisecond},
		{1, time.Second},
		{2, 1500 * time.Millisecond}, // 4 s capped at the 3 s interval
		{3, 1500 * time.Millisecond},
	}
	for _, tc := range cases {
		if got := streaming.delay(tc.failures); got != tc.want {
			t.Errorf("delay(%d) = %v, want %v", tc.failures, got, tc.want)
		}
	}
}

// TestParseRetryAfter is contract 7.4: whole decimal seconds, surrounding
// optional whitespace only, and a hold capped at 300 s.
func TestParseRetryAfter(t *testing.T) {
	cases := []struct {
		header string
		want   time.Duration
		ok     bool
	}{
		{"5", 5 * time.Second, true},
		{" 5\t", 5 * time.Second, true},
		{"\u00a05", 0, false},
		{"5.0", 0, false},
		{"-1", 0, false},
		{"", 0, false},
		{"1000", 300 * time.Second, true},
		{"999999999999999999999999", 300 * time.Second, true},
	}
	for _, tc := range cases {
		got, ok := parseRetryAfter(tc.header)
		if ok != tc.ok || got != tc.want {
			t.Errorf("parseRetryAfter(%q) = %v, %v, want %v, %v", tc.header, got, ok, tc.want, tc.ok)
		}
	}
}

// TestUsageRedirectIsNotFollowed covers decision 72: a 301 on the usage POST
// would be re-issued by Go as a bodyless GET, whose 2xx would read as sent and
// lose the counts. No redirect is followed, so nothing reaches the location.
func TestUsageRedirectIsNotFollowed(t *testing.T) {
	fake := newFakeServer(t)
	fake.ruleset = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(rulesetOf(boolFlag("feature", 1, 1))))
	}
	fake.events = func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("Location", "/elsewhere")
		w.WriteHeader(http.StatusMovedPermanently)
	}
	client := newClient(t, clockOptions(fake, newFakeClock()))
	if !client.Ready(context.Background()) {
		t.Fatalf("Ready = false, want true")
	}
	client.BoolVariation("feature", userContext("u1"), false)
	client.Close()
	if got := fake.count("/elsewhere"); got != 0 {
		t.Fatalf("the redirect was followed: %d requests reached the location", got)
	}
}
