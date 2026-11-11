package toggleslash

import (
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

func TestReadyAndVariation(t *testing.T) {
	f := newFakeServer(t, boolFlag("checkout", true, 3))
	c := newTestClient(t, f)
	if !c.Ready() {
		t.Fatal("Ready is false")
	}
	d := c.BoolVariationDetail("checkout", user, false)
	if !d.Value || d.VariationIndex == nil || *d.VariationIndex != 1 || d.Reason.Kind != ReasonFallthrough {
		t.Fatalf("detail = %+v", d)
	}
	st := c.Status()
	if st.ETag != `"v1"` || st.LastSyncAt.IsZero() || st.Stream != StreamOff || st.LastError != "" {
		t.Fatalf("status = %+v", st)
	}
}

func TestTrailingSlashesAreRemoved(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	c := New(f.url()+"///", testKey, WithStream(false))
	defer c.Close()
	if !c.Ready() {
		t.Fatal("Ready is false")
	}
	if got := len(f.requests("/sdk/v1/ruleset")); got != 1 {
		t.Fatalf("ruleset requests = %d", got)
	}
}

func TestNotReadyBeforeAndAfterAHungLoad(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	f.rulesetFn = hang
	errs := &errorLog{}
	c := New(f.url(), testKey, WithStream(false), WithRequestTimeout(150*time.Millisecond), WithOnError(errs.add))
	defer c.Close()
	d := c.BoolVariationDetail("a", user, true)
	if !d.Value || d.VariationIndex != nil || d.Reason.Kind != ReasonError || d.Reason.Code != ErrNotReady {
		t.Fatalf("before the first load: %+v", d)
	}
	start := time.Now()
	if c.Ready() {
		t.Fatal("Ready is true with no ruleset")
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("Ready took %v", took)
	}
	eventually(t, 2*time.Second, "the timeout error", func() bool { return errs.count() >= 1 })
	if st := c.Status(); st.LastError != LastErrorTimeout {
		t.Fatalf("lastError = %q", st.LastError)
	}
	if !strings.Contains(errs.all()[0].Error(), "timeout") {
		t.Fatalf("error = %v", errs.all()[0])
	}
}

func TestReadyIsFixedAtTheFirstSettle(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	f.rulesetFn = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }
	c := newTestClient(t, f, WithPollInterval(20*time.Millisecond))
	if c.Ready() {
		t.Fatal("Ready is true after a 503")
	}
	f.mu.Lock()
	f.rulesetFn = nil
	f.mu.Unlock()
	eventually(t, 3*time.Second, "a later poll", func() bool { return c.Status().ETag != "" })
	if c.Ready() {
		t.Fatal("Ready changed after the first settle")
	}
	if got := c.BoolVariation("a", user, false); !got {
		t.Fatal("evaluation did not use the ruleset the poll loaded")
	}
}

func TestResultOrder(t *testing.T) {
	f := newFakeServer(t,
		boolFlag("on", true, 1),
		valueFlag("num", "2"), valueFlag("frac", "2.5"), valueFlag("whole", "2.0"),
		valueFlag("huge", "1e30"), valueFlag("big", "9223372036854774784"), valueFlag("neg", "-9223372036854775808"),
		valueFlag("str", `"hi"`), valueFlag("arr", "[1]"), valueFlag("obj", `{"a":1}`),
	)
	c := newTestClient(t, f)
	c.Ready()

	mismatch := func(d Detail[jsonvalue.Value]) bool {
		return d.VariationIndex == nil && d.Reason.Kind == ReasonError && d.Reason.Code == ErrTypeMismatch
	}
	if d := c.VariationDetail("on", user, jsonvalue.FromString("x")); !mismatch(d) || d.Value.String() != `"x"` {
		t.Fatalf("bool served to a string fallback: %+v", d)
	}
	if d := c.VariationDetail("arr", user, jsonvalue.FromObject()); !mismatch(d) {
		t.Fatalf("array against an object fallback: %+v", d)
	}
	if d := c.VariationDetail("obj", user, jsonvalue.FromArray()); !mismatch(d) {
		t.Fatalf("object against an array fallback: %+v", d)
	}
	if d := c.VariationDetail("arr", user, jsonvalue.FromArray()); d.VariationIndex == nil || d.Value.String() != "[1]" {
		t.Fatalf("array against an array fallback: %+v", d)
	}
	if d := c.VariationDetail("str", user, jsonvalue.Null); d.VariationIndex == nil || d.Value.String() != `"hi"` {
		t.Fatalf("a null fallback must accept any kind: %+v", d)
	}
	// A null served value against a non-null fallback is a mismatch; flag "num" serves variation 1, and the
	// off variation (null) is reached with no rule on.
	f.publish(`{"key":"n","variations":[null],"on":true,"offVariation":0,"targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":0},"salt":"s","version":1}`)
	c2 := newTestClient(t, f)
	c2.Ready()
	if d := c2.VariationDetail("n", user, jsonvalue.FromBool(true)); !mismatch(d) {
		t.Fatalf("null served against a bool fallback: %+v", d)
	}
	if d := c2.VariationDetail("n", user, jsonvalue.Null); d.VariationIndex == nil || !d.Value.IsNull() {
		t.Fatalf("null served against a null fallback: %+v", d)
	}

	// The evaluator's own reason is kept when there is no index.
	d := c.BoolVariationDetail("missing", user, true)
	if !d.Value || d.VariationIndex != nil || d.Reason.Code != ErrFlagNotFound {
		t.Fatalf("unknown flag: %+v", d)
	}
	d = c.BoolVariationDetail("on", Context{}, true)
	if !d.Value || d.VariationIndex != nil || d.Reason.Code != ErrInvalidContext {
		t.Fatalf("zero context: %+v", d)
	}
}

func TestIntegerAccessor(t *testing.T) {
	f := newFakeServer(t, valueFlag("num", "2"), valueFlag("frac", "2.5"), valueFlag("whole", "2.0"),
		valueFlag("huge", "1e30"), valueFlag("big", "9223372036854774784"), valueFlag("edge", "9223372036854775808"),
		valueFlag("neg", "-9223372036854775808"), valueFlag("inf", "1e999"), valueFlag("str", `"2"`))
	c := newTestClient(t, f)
	c.Ready()
	cases := []struct {
		key      string
		want     int64
		mismatch bool
	}{
		{"num", 2, false}, {"whole", 2, false}, {"frac", 7, true}, {"huge", 7, true},
		{"big", 9223372036854774784, false}, {"edge", 7, true}, {"neg", -9223372036854775808, false},
		{"inf", 7, true}, {"str", 7, true},
	}
	for _, tc := range cases {
		d := c.Int64VariationDetail(tc.key, user, 7)
		if d.Value != tc.want {
			t.Errorf("%s: value = %d, want %d", tc.key, d.Value, tc.want)
		}
		if got := d.Reason.Code == ErrTypeMismatch; got != tc.mismatch {
			t.Errorf("%s: mismatch = %v (%+v)", tc.key, got, d)
		}
		if tc.mismatch && d.VariationIndex != nil {
			t.Errorf("%s: a fallback has an index", tc.key)
		}
	}
	// The float accessor has no integer rule.
	if got := c.Float64Variation("frac", user, 0); got != 2.5 {
		t.Errorf("Float64Variation = %v", got)
	}
	if got := c.StringVariation("str", user, ""); got != "2" {
		t.Errorf("StringVariation = %q", got)
	}
}

func TestEvaluationFailedGoesToOnErrorOnceAndIsNotCounted(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	errs := &errorLog{}
	var events atomic.Int32
	c := New(f.url(), testKey, WithStream(false), WithOnError(errs.add),
		WithAfterEvaluation(func(EvaluationEvent) { events.Add(1) }),
		func(cfg *config) { cfg.beforeEvaluate = func(string) { panic("boom") } })
	defer c.Close()
	c.Ready()
	d := c.BoolVariationDetail("a", user, false)
	if d.Value || d.VariationIndex != nil || d.Reason.Code != ErrEvaluationFailed {
		t.Fatalf("detail = %+v", d)
	}
	if errs.count() != 1 {
		t.Fatalf("onError calls = %d, want 1", errs.count())
	}
	var pe *PanicError
	if !errors.As(errs.all()[0], &pe) || pe.Value != "boom" {
		t.Fatalf("error = %#v", errs.all()[0])
	}
	if events.Load() != 1 {
		t.Fatalf("hook calls = %d", events.Load())
	}
	c.usageMu.Lock()
	pending := len(c.counts)
	c.usageMu.Unlock()
	if pending != 0 {
		t.Fatalf("a failed evaluation was counted: %d entries", pending)
	}
}
