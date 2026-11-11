package toggleslash

import (
	"errors"
	"math"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
)

// start makes a client against srv that is closed when the test ends, before the server is.
func start(t *testing.T, srv *fakeServer, opts ...Option) *Client {
	t.Helper()
	c := New(srv.URL, "sdk-secret", opts...)
	t.Cleanup(c.Close)
	return c
}

func TestReadyAndStatusAfterLoad(t *testing.T) {
	srv := newFakeServer(t, rulesetOf(flag("f", 1, 7)))
	c := start(t, srv, WithStream(false))
	if !c.Ready() {
		t.Fatal("Ready: want true")
	}
	st := c.Status()
	if st.ETag != `"v1"` || st.LastError != "" || st.Stream != StreamOff || st.LastSyncAt.IsZero() {
		t.Fatalf("Status: %+v", st)
	}
	d := c.BoolVariationDetail("f", shopper, false)
	if !d.Value || d.VariationIndex != 1 || d.Reason.Kind != ReasonFallthrough {
		t.Fatalf("detail: %+v", d)
	}
}

func TestNotReadyServesFallbacks(t *testing.T) {
	srv := newFakeServer(t, "")
	srv.handle(routeRuleset, drainThenHang)
	c := start(t, srv, WithStream(false), WithRequestTimeout(30*time.Millisecond))
	d := c.StringVariationDetail("f", shopper, "fb")
	if d.Value != "fb" || d.VariationIndex != -1 || d.Reason.Kind != ReasonError || d.Reason.Code != ErrNotReady {
		t.Fatalf("before the first load: %+v", d)
	}
	if c.Ready() {
		t.Fatal("Ready: want false after a timeout")
	}
	if got := c.Status().LastError; got != LastErrorTimeout {
		t.Fatalf("lastError %q", got)
	}
}

func TestFlagNotFoundAndInvalidContext(t *testing.T) {
	srv := newFakeServer(t, rulesetOf(flag("f", 1, 1)))
	c := start(t, srv, WithStream(false))
	c.Ready()
	d := c.BoolVariationDetail("missing", shopper, true)
	if !d.Value || d.VariationIndex != -1 || d.Reason.Code != ErrFlagNotFound {
		t.Fatalf("missing flag: %+v", d)
	}
	d = c.BoolVariationDetail("f", Context{}, true)
	if !d.Value || d.Reason.Code != ErrInvalidContext {
		t.Fatalf("zero context: %+v", d)
	}
	d = c.BoolVariationDetail("f", ParseContext([]byte(`{nope`)), true)
	if d.Reason.Code != ErrInvalidContext {
		t.Fatalf("unparsable context: %+v", d)
	}
}

func TestTypeMismatchKeepsTheEvaluatorOutOfTheCount(t *testing.T) {
	srv := newFakeServer(t, rulesetOf(flagWith("s", `["a","b"]`, 1, 1)))
	c := start(t, srv, WithStream(false))
	c.Ready()
	d := c.BoolVariationDetail("s", shopper, true)
	if !d.Value || d.VariationIndex != -1 || d.Reason.Kind != ReasonError || d.Reason.Code != ErrTypeMismatch {
		t.Fatalf("string flag read as bool: %+v", d)
	}
	if got := c.StringVariation("s", shopper, "x"); got != "b" {
		t.Fatalf("string: %q", got)
	}
	// A mismatch is not a served evaluation, so only the string read counts.
	if n := len(c.takeCounts()); n != 1 {
		t.Fatalf("counted entries: %d", n)
	}
}

func TestIntegerAccessor(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  int64
		code  ErrorCode
	}{
		{"integral", `3`, 3, ""},
		{"integral with a fraction part of zero", `2.0`, 2, ""},
		{"fractional", `2.5`, -1, ErrTypeMismatch},
		{"largest servable", `9223372036854774784`, 9223372036854774784, ""},
		{"2^63", `9223372036854775808`, -1, ErrTypeMismatch},
		{"far out of range", `1e30`, -1, ErrTypeMismatch},
		{"negative edge", `-9223372036854775808`, math.MinInt64, ""},
		{"beyond binary64", `1e999`, -1, ErrTypeMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newFakeServer(t, rulesetOf(flagWith("n", `[0,`+tc.value+`]`, 1, 1)))
			c := start(t, srv, WithStream(false))
			c.Ready()
			d := c.IntVariationDetail("n", shopper, -1)
			if d.Value != tc.want || d.Reason.Code != tc.code {
				t.Fatalf("got %+v", d)
			}
		})
	}
}

func TestFloatJSONAndGenericAccessors(t *testing.T) {
	flags := []string{
		flagWith("n", `[0,1.5]`, 1, 1),
		flagWith("j", `[null,{"a":[1,2]}]`, 1, 1),
		flagWith("b", `[false,true]`, 1, 1),
		flagWith("s", `["x","y"]`, 1, 1),
		flagWith("i", `[0,4]`, 1, 1),
	}
	srv := newFakeServer(t, rulesetOf(flags...))
	c := start(t, srv, WithStream(false))
	c.Ready()
	if got := c.FloatVariation("n", shopper, 0); got != 1.5 {
		t.Fatalf("float: %v", got)
	}
	if got := Variation(c, "n", shopper, float64(0)); got != 1.5 {
		t.Fatalf("generic float: %v", got)
	}
	if got := Variation(c, "b", shopper, false); !got {
		t.Fatal("generic bool")
	}
	if got := Variation(c, "s", shopper, ""); got != "y" {
		t.Fatalf("generic string: %q", got)
	}
	if got := Variation(c, "i", shopper, int64(0)); got != 4 {
		t.Fatalf("generic int: %d", got)
	}
	obj := ParseContext([]byte(`{"x":{}}`)).Value()
	got := Variation(c, "j", shopper, obj)
	if !got.IsObject() {
		t.Fatalf("generic json: kind %v", got.Kind())
	}
	// A null fallback accepts any type; a fallback of another type is a mismatch.
	if d := c.JSONVariationDetail("j", shopper, Value{}); !d.Value.IsObject() || d.VariationIndex != 1 {
		t.Fatalf("null fallback: %+v", d)
	}
	if d := VariationDetail(c, "j", shopper, ParseContext([]byte(`[]`)).Value()); d.Reason.Code != ErrTypeMismatch {
		t.Fatalf("array fallback: %+v", d)
	}
	if d := VariationDetail(c, "n", shopper, "str"); d.Reason.Code != ErrTypeMismatch || d.Value != "str" {
		t.Fatalf("string read of a number: %+v", d)
	}
}

func TestBadRulesetKeepsTheLastGoodOne(t *testing.T) {
	srv := newFakeServer(t, rulesetOf(flag("f", 1, 1)))
	log := &errorLog{}
	clk := newFakeClock(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC))
	c := start(t, srv, WithStream(false), withClock(clk), log.option())
	if !c.Ready() {
		t.Fatal("not ready")
	}
	for _, bad := range []string{`{"flags":[]}`, `[]`, `{"flags":{},"segments":[]}`, `not json`} {
		srv.publish(bad, `"bad"`)
		eventually(t, "the poll timer", func() bool { return clk.Armed() >= 2 })
		before := srv.count(routeRuleset)
		clk.Advance(time.Hour)
		eventually(t, "the poll", func() bool { return srv.count(routeRuleset) > before })
		eventually(t, "bad_ruleset", func() bool { return c.Status().LastError == LastErrorBadRuleset })
		if c.Status().ETag != `"v1"` || !c.BoolVariation("f", shopper, false) {
			t.Fatalf("the good ruleset was lost after %q", bad)
		}
		srv.publish(rulesetOf(flag("f", 1, 1)), `"v1"`)
		eventually(t, "the poll timer", func() bool { return clk.Armed() >= 2 })
		clk.Advance(time.Hour)
		eventually(t, "recovery", func() bool { return c.Status().LastError == "" })
	}
	eventually(t, "errors", func() bool { return len(log.all()) >= 4 })
}

func TestRedirectIsAFailureAndTheKeyIsNotSentOn(t *testing.T) {
	other := newFakeServer(t, rulesetOf(flag("f", 1, 1)))
	srv := newFakeServer(t, "")
	srv.handle(routeRuleset, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/sdk/v1/ruleset", http.StatusFound)
	})
	c := start(t, srv, WithStream(false))
	if c.Ready() {
		t.Fatal("a redirect must not be followed")
	}
	if got := c.Status().LastError; got != "http_302" {
		t.Fatalf("lastError %q", got)
	}
	if n := other.count(routeRuleset); n != 0 {
		t.Fatalf("the redirect target got %d requests", n)
	}
}

func TestNetworkFailureTextHoldsNoSecret(t *testing.T) {
	srv := newFakeServer(t, "")
	url := srv.URL
	srv.Close()
	log := &errorLog{}
	c := New(url, "sdk-secret-value", WithStream(false), log.option())
	t.Cleanup(c.Close)
	if c.Ready() {
		t.Fatal("ready against a closed server")
	}
	eventually(t, "the error", func() bool { return len(log.all()) > 0 })
	for _, m := range log.all() {
		if containsAny(m, "sdk-secret-value", "127.0.0.1", "Bearer") {
			t.Fatalf("error text leaks: %q", m)
		}
	}
	if got := c.Status().LastError; got != LastErrorNetwork {
		t.Fatalf("lastError %q", got)
	}
}

func TestHookSeesTheEvaluationAndItsTagAndVersion(t *testing.T) {
	srv := newFakeServer(t, rulesetOf(flag("f", 1, 42)))
	var mu sync.Mutex
	var events []EvaluationEvent
	c := start(t, srv, WithStream(false), WithAfterEvaluation(func(e EvaluationEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}))
	c.Ready()
	c.BoolVariation("f", shopper, false)
	c.BoolVariation("missing", shopper, false)
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 {
		t.Fatalf("events: %d", len(events))
	}
	if e := events[0]; e.ETag != `"v1"` || e.FlagVersion == nil || *e.FlagVersion != 42 || e.VariationIndex != 1 {
		t.Fatalf("served: %+v", e)
	}
	if e := events[1]; e.FlagVersion != nil || e.VariationIndex != -1 || e.Reason.Code != ErrFlagNotFound {
		t.Fatalf("fallback: %+v", e)
	}
}

func TestPanicsNeverReachTheCaller(t *testing.T) {
	srv := newFakeServer(t, rulesetOf(flag("f", 1, 1)))
	log := &errorLog{}
	c := start(t, srv, WithStream(false),
		WithAfterEvaluation(func(EvaluationEvent) { panic("hook") }),
		WithOnError(func(err error) { log.add(err); panic("onError") }),
	)
	c.Ready()
	if !c.BoolVariation("f", shopper, false) {
		t.Fatal("a panicking hook changed the answer")
	}
	eventually(t, "the hook panic", func() bool { return len(log.all()) > 0 })
	if m := log.all()[0]; m != "toggleslash: the AfterEvaluation function panicked: hook" {
		t.Fatalf("message %q", m)
	}
}

func TestEvaluationFailedWhenTheEvaluatorPanics(t *testing.T) {
	srv := newFakeServer(t, rulesetOf(flag("f", 1, 1)))
	var got error
	var mu sync.Mutex
	c := start(t, srv, WithStream(false),
		withEvaluate(func(*evaluator.Ruleset, string, Value) evaluator.Result { panic("boom") }),
		WithOnError(func(err error) { mu.Lock(); got = err; mu.Unlock() }),
	)
	c.Ready()
	d := c.BoolVariationDetail("f", shopper, true)
	if !d.Value || d.VariationIndex != -1 || d.Reason.Kind != ReasonError || d.Reason.Code != ErrEvaluationFailed {
		t.Fatalf("%+v", d)
	}
	mu.Lock()
	defer mu.Unlock()
	var pe *PanicError
	if !errors.As(got, &pe) || pe.Value != "boom" {
		t.Fatalf("onError got %v", got)
	}
}

func TestEvaluationIsNotBlockedByAHungRequest(t *testing.T) {
	srv := newFakeServer(t, rulesetOf(flag("f", 1, 1)))
	c := start(t, srv, WithStream(false), WithPollInterval(10*time.Millisecond), WithRequestTimeout(5*time.Second))
	c.Ready()
	srv.handle(routeRuleset, drainThenHang)
	eventually(t, "a hung poll", func() bool { return srv.count(routeRuleset) >= 2 })
	done := make(chan bool)
	go func() { done <- c.BoolVariation("f", shopper, false) }()
	select {
	case v := <-done:
		if !v {
			t.Fatal("wrong value")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("an evaluation waited on the network")
	}
	_ = c.Status()
}

func TestRulesetAndTagSwapTogether(t *testing.T) {
	srv := newFakeServer(t, rulesetOf(flag("f", 0, 1)))
	clk := newFakeClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	var mu sync.Mutex
	var bad []string
	c := start(t, srv, WithStream(false), withClock(clk),
		WithAfterEvaluation(func(e EvaluationEvent) {
			// Version n of the flag always ships under tag "vn" and serves variation n%2.
			want := map[string]float64{`"v1"`: 1, `"v2"`: 2}[e.ETag]
			if e.FlagVersion == nil || *e.FlagVersion != want {
				mu.Lock()
				bad = append(bad, e.ETag)
				mu.Unlock()
			}
		}))
	c.Ready()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					c.BoolVariation("f", shopper, false)
				}
			}
		}()
	}
	for i := range 20 {
		next := 2 - i%2
		srv.publish(rulesetOf(flag("f", next%2, next)), `"v`+string(rune('0'+next))+`"`)
		eventually(t, "the poll timer", func() bool { return clk.Armed() >= 2 })
		clk.Advance(time.Hour)
		eventually(t, "the swap", func() bool { return c.Status().ETag == `"v`+string(rune('0'+next))+`"` })
	}
	close(stop)
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(bad) > 0 {
		t.Fatalf("a tag and a flag version disagreed %d times, first under %s", len(bad), bad[0])
	}
}

func TestContextBuilders(t *testing.T) {
	c := NewContext("k", map[string]any{"key": "other", "n": 3, "tags": []string{"a"}})
	if got, _ := c.Value().Get("key"); got.StringValue() != "k" {
		t.Fatalf("key: %v", got)
	}
	if bad := NewContext("k", map[string]any{"x": make(chan int)}); !bad.Value().IsNull() {
		t.Fatal("an unconvertible attribute must give the invalid context")
	}
	k := NewKindContext("org", "o1", nil)
	if got, _ := k.Value().Get("kind"); got.StringValue() != "org" {
		t.Fatalf("kind: %v", got)
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
