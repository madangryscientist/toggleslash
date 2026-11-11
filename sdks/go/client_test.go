package toggleslash

import (
	"context"
	"math"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// readDoc reads a repository document, for the tests that hold a spelling against the spec.
func readDoc(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("../../docs/" + name)
	if err != nil {
		t.Fatalf("reading docs/%s: %v", name, err)
	}
	return string(b)
}

// TestConstantsSpelling holds every closed set's constants against the spelling the spec uses (Go has
// no enum: a string constant is a closed set only by convention).
func TestConstantsSpelling(t *testing.T) {
	spec := readDoc(t, "evaluation.md") + readDoc(t, "sdk-contract.md")
	all := map[string]string{
		"ReasonOff": string(ReasonOff), "ReasonPrerequisiteFailed": string(ReasonPrerequisiteFailed),
		"ReasonTargetMatch": string(ReasonTargetMatch), "ReasonRuleMatch": string(ReasonRuleMatch),
		"ReasonFallthrough": string(ReasonFallthrough), "ReasonError": string(ReasonError),
		"ErrInvalidContext": string(ErrInvalidContext), "ErrFlagNotFound": string(ErrFlagNotFound),
		"ErrSegmentNotFound": string(ErrSegmentNotFound), "ErrMalformedFlag": string(ErrMalformedFlag),
		"ErrMalformedSegment": string(ErrMalformedSegment), "ErrPrerequisiteCycle": string(ErrPrerequisiteCycle),
		"ErrPrerequisiteDepthExceeded": string(ErrPrerequisiteDepthExceeded), "ErrNotReady": string(ErrNotReady),
		"ErrTypeMismatch": string(ErrTypeMismatch), "ErrEvaluationFailed": string(ErrEvaluationFailed),
		"StreamOff": string(StreamOff), "StreamConnecting": string(StreamConnecting),
		"StreamConnected": string(StreamConnected), "StreamDisconnected": string(StreamDisconnected),
		"StreamClosed": string(StreamClosed), "LastErrorTimeout": string(LastErrorTimeout),
		"LastErrorBadRuleset": string(LastErrorBadRuleset), "LastErrorNetwork": string(LastErrorNetwork),
	}
	for name, value := range all {
		if !strings.Contains(spec, "`"+value+"`") {
			t.Errorf("%s = %q is not spelled that way, in backticks, in the spec", name, value)
		}
	}
	if got := string(httpError(503)); got != "http_503" {
		t.Errorf("httpError(503) = %q", got)
	}
}

func readyClient(t *testing.T, h *ruleHolder, opts ...Option) (*Client, *fakeServer) {
	t.Helper()
	f := newFake(t, h.serve)
	opts = append([]Option{WithStream(false), WithPollInterval(time.Hour), WithFlushInterval(time.Hour)}, opts...)
	c := newTestClient(t, f, opts...)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !c.Ready(ctx) {
		t.Fatalf("not ready; status %+v", c.Status())
	}
	return c, f
}

func TestLoadAndEvaluate(t *testing.T) {
	h := &ruleHolder{}
	h.set(`"v1"`, rulesetJSON(
		boolFlagJSON("checkout", 3),
		valueFlagJSON("name", `"blue"`),
		valueFlagJSON("three", `3`),
		valueFlagJSON("two", `2.0`),
		valueFlagJSON("ratio", `1.5`),
		valueFlagJSON("blob", `{"a":1}`),
	))
	c, _ := readyClient(t, h)

	if !c.BoolVariation("checkout", userCtx, false) {
		t.Error("checkout should be true")
	}
	d := c.BoolVariationDetail("checkout", userCtx, false)
	if d.VariationIndex == nil || *d.VariationIndex != 1 || d.Reason.Kind != ReasonFallthrough {
		t.Errorf("detail = %+v", d)
	}
	if got := c.StringVariation("name", userCtx, "x"); got != "blue" {
		t.Errorf("name = %q", got)
	}
	if got := c.IntVariation("three", userCtx, 0); got != 3 {
		t.Errorf("three = %d", got)
	}
	if got := c.IntVariation("two", userCtx, 0); got != 2 {
		t.Errorf("2.0 must be the integer 2, got %d", got)
	}
	if got := c.FloatVariation("ratio", userCtx, 0); got != 1.5 {
		t.Errorf("ratio = %v", got)
	}
	blob := c.JSONVariation("blob", userCtx, NewObject(nil))
	if v, ok := blob.Get("a"); !ok || v.Kind() != jsonv.Number {
		t.Errorf("blob = %s", jsonv.Marshal(blob))
	}

	st := c.Status()
	if !st.HasETag || st.ETag != `"v1"` || st.LastSyncAt.IsZero() || st.Stream != StreamOff || st.LastError != LastErrorNone {
		t.Errorf("status = %+v", st)
	}
}

func TestTypeMismatchAndIntegerRange(t *testing.T) {
	h := &ruleHolder{}
	h.set(`"v1"`, rulesetJSON(
		valueFlagJSON("str", `"s"`),
		valueFlagJSON("half", `1.5`),
		valueFlagJSON("max", `9223372036854775808`),   // 2^63
		valueFlagJSON("below", `9223372036854774784`), // the largest double under 2^63
		valueFlagJSON("min", `-9223372036854775808`),
		valueFlagJSON("huge", `1e20`),
		valueFlagJSON("arr", `[1]`),
		valueFlagJSON("nul", `null`),
	))
	c, _ := readyClient(t, h)

	mismatch := func(what string, got Reason, idx *int) {
		t.Helper()
		if got.Kind != ReasonError || got.Code != ErrTypeMismatch || idx != nil {
			t.Errorf("%s: reason %+v, index %v", what, got, idx)
		}
	}
	b := c.BoolVariationDetail("str", userCtx, true)
	mismatch("bool on a string", b.Reason, b.VariationIndex)
	if !b.Value {
		t.Error("a mismatch gives the fallback")
	}
	for _, key := range []string{"str", "half", "max", "huge"} {
		d := c.IntVariationDetail(key, userCtx, 42)
		mismatch("int on "+key, d.Reason, d.VariationIndex)
		if d.Value != 42 {
			t.Errorf("int on %s gave %d, not the fallback", key, d.Value)
		}
	}
	if got := c.IntVariation("below", userCtx, 0); got != 9223372036854774784 {
		t.Errorf("below = %d", got)
	}
	if got := c.IntVariation("min", userCtx, 0); got != math.MinInt64 {
		t.Errorf("min = %d", got)
	}
	j := c.JSONVariationDetail("arr", userCtx, NewObject(nil))
	mismatch("an object fallback on an array", j.Reason, j.VariationIndex)
	if j := c.JSONVariationDetail("arr", userCtx, NewNull()); j.VariationIndex == nil || j.Value.Kind() != jsonv.Array {
		t.Errorf("a null fallback accepts any kind: %+v", j)
	}
	if j := c.JSONVariationDetail("nul", userCtx, NewString("x")); j.Reason.Code != ErrTypeMismatch {
		t.Errorf("a null served against a non-null fallback is a mismatch: %+v", j)
	}
	if j := c.JSONVariationDetail("nul", userCtx, NewNull()); j.VariationIndex == nil || !j.Value.IsNull() {
		t.Errorf("null against null is a hit: %+v", j)
	}
	f := c.BoolVariationDetail("missing", userCtx, true)
	if f.Reason.Kind != ReasonError || f.Reason.Code != ErrFlagNotFound || f.VariationIndex != nil || !f.Value {
		t.Errorf("an unknown flag: %+v", f)
	}
}

func TestNotReadyAnswersAtOnceWhileTheRulesetRequestHangs(t *testing.T) {
	f := newFake(t, func(f *fakeServer, w http.ResponseWriter, r *http.Request) { f.hang() })
	errs := &errorLog{}
	c := newTestClient(t, f, WithRequestTimeout(400*time.Millisecond), WithOnError(errs.add))

	within(t, 100*time.Millisecond, "variationDetail while the ruleset request hangs", func() {
		d := c.BoolVariationDetail("checkout", userCtx, true)
		if !d.Value || d.VariationIndex != nil || d.Reason.Code != ErrNotReady || d.Reason.Kind != ReasonError {
			t.Errorf("detail = %+v", d)
		}
	})
	within(t, 100*time.Millisecond, "status", func() { _ = c.Status() })
	if st := c.Status(); st.Stream != StreamConnecting {
		t.Errorf("stream = %q during the first load", st.Stream)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	if c.Ready(ctx) {
		t.Error("Ready should be false")
	}
	if took := time.Since(start); took < 300*time.Millisecond || took > 2*time.Second {
		t.Errorf("Ready took %v; the first attempt is bounded by the request timeout", took)
	}
	if st := c.Status(); st.LastError != LastErrorTimeout {
		t.Errorf("lastError = %q", st.LastError)
	}
	if n := errs.count(); n != 1 {
		t.Errorf("onError calls = %d: %v", n, errs.all())
	}
	for _, msg := range errs.all() {
		if strings.Contains(msg, f.URL) || strings.Contains(msg, "test-key") {
			t.Errorf("an error carries the URL or key: %q", msg)
		}
	}
	// The answer is fixed once settled.
	if c.Ready(ctx) {
		t.Error("Ready changed its answer")
	}
}

func TestReadyReturnsFalseWhenItsContextEndsFirst(t *testing.T) {
	f := newFake(t, func(f *fakeServer, w http.ResponseWriter, r *http.Request) { f.hang() })
	c := newTestClient(t, f, WithRequestTimeout(5*time.Second))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if c.Ready(ctx) {
		t.Fatal("Ready should be false")
	}
	if time.Since(start) > time.Second {
		t.Error("Ready ignored its context")
	}
}

func TestEvaluationThatRaisesGivesTheFallbackAndOneError(t *testing.T) {
	h := &ruleHolder{}
	h.set(`"v1"`, rulesetJSON(boolFlagJSON("checkout", 1)))
	errs := &errorLog{}
	var events atomic.Int32
	c, f := readyClient(t, h,
		WithOnError(errs.add),
		WithAfterEvaluation(func(EvaluationEvent) { events.Add(1) }),
		withEvaluator(func(*evaluator.Ruleset, string, Value) evaluator.Result { panic("boom with secret-context") }),
	)
	d := c.BoolVariationDetail("checkout", userCtx, false)
	if d.Value || d.VariationIndex != nil || d.Reason.Kind != ReasonError || d.Reason.Code != ErrEvaluationFailed {
		t.Errorf("detail = %+v", d)
	}
	if n := errs.count(); n != 1 {
		t.Fatalf("onError calls = %d", n)
	}
	if strings.Contains(errs.all()[0], "secret-context") {
		t.Errorf("the error carries what was raised: %q", errs.all()[0])
	}
	if events.Load() != 1 {
		t.Errorf("the hook ran %d times, want once", events.Load())
	}
	c.Flush(context.Background())
	if n := len(f.requests("/sdk/v1/events")); n != 0 {
		t.Errorf("a failed evaluation was counted: %d events requests", n)
	}
}
