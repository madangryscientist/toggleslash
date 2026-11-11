package toggleslash

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/eval"
	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

func constFlag(key, value string) string {
	return fmt.Sprintf(`{"key":%q,"version":2,"on":true,"salt":"s","variations":[%s],"offVariation":0,"fallthrough":{"variation":0},"rules":[],"targets":[],"prerequisites":[]}`, key, value)
}

func valuesRuleset() string {
	flags := []string{
		constFlag("str", `"hello"`),
		constFlag("num", `7`),
		constFlag("frac", `1.5`),
		constFlag("whole", `2.0`),
		constFlag("big", `9.3e18`),
		constFlag("largest", `9223372036854774784`),
		constFlag("over", `9223372036854775808`),
		constFlag("neg", `-9223372036854775808`),
		constFlag("arr", `[1,2]`),
		constFlag("obj", `{"a":1}`),
		constFlag("nul", `null`),
		constFlag("bool", `true`),
	}
	return `{"flags":[` + strings.Join(flags, ",") + `],"segments":[]}`
}

func valuesClient(t *testing.T, opts ...Option) (*Client, *fakeServer) {
	t.Helper()
	f := newFakeServer(t)
	f.setRuleset(valuesRuleset(), `"vals"`)
	c := newTestClient(t, f, append([]Option{WithStream(false)}, opts...)...)
	if !c.Ready() {
		t.Fatal("not ready")
	}
	return c, f
}

func TestTypedAccessorsServeMatchingKinds(t *testing.T) {
	c, _ := valuesClient(t)
	if got := c.StringVariation("str", user, "fb"); got != "hello" {
		t.Errorf("string = %q", got)
	}
	if got := c.FloatVariation("frac", user, 0); got != 1.5 {
		t.Errorf("float = %v", got)
	}
	if got := c.FloatVariation("num", user, 0); got != 7 {
		t.Errorf("float = %v", got)
	}
	if got := c.IntVariation("num", user, -1); got != 7 {
		t.Errorf("int = %v", got)
	}
	if got := c.IntVariation("whole", user, -1); got != 2 {
		t.Errorf("2.0 as int = %v", got)
	}
	if got := c.IntVariation("largest", user, -1); got != 9223372036854774784 {
		t.Errorf("largest = %v", got)
	}
	if got := c.IntVariation("neg", user, 0); got != -9223372036854775808 {
		t.Errorf("min = %v", got)
	}
	if !c.BoolVariation("bool", user, false) {
		t.Error("bool")
	}
}

func TestIntegerAccessorRefusesFractionsAndOutOfRange(t *testing.T) {
	c, _ := valuesClient(t)
	for _, key := range []string{"frac", "big", "over", "str", "bool", "arr", "nul"} {
		d := c.IntVariationDetail(key, user, 41)
		if d.Value != 41 || d.VariationIndex != -1 || d.Reason.Kind != ReasonError || d.Reason.Code != ErrorTypeMismatch {
			t.Errorf("%s: %+v", key, d)
		}
	}
}

func TestMismatchGivesTheFallbackAndCountsNothing(t *testing.T) {
	c, f := valuesClient(t, WithFlushInterval(time.Hour))
	if c.BoolVariation("str", user, true) != true {
		t.Error("fallback lost")
	}
	d := c.StringVariationDetail("num", user, "fb")
	if d.Value != "fb" || d.Reason.Code != ErrorTypeMismatch || d.VariationIndex != -1 {
		t.Errorf("%+v", d)
	}
	c.Close()
	for _, r := range f.seen("events") {
		t.Errorf("a mismatch was counted: %s", r.body)
	}
}

func TestGenericAccessorKindRules(t *testing.T) {
	c, _ := valuesClient(t)
	cases := []struct {
		flag     string
		fallback jsonvalue.Value
		served   bool
	}{
		{"str", jsonvalue.FromString("x"), true},
		{"str", jsonvalue.FromInt(1), false},
		{"arr", jsonvalue.FromArray(), true},
		{"arr", jsonvalue.FromObject(), false},
		{"obj", jsonvalue.FromObject(), true},
		{"obj", jsonvalue.FromArray(), false},
		{"nul", jsonvalue.NullValue(), true},
		{"nul", jsonvalue.FromString("x"), false},
		{"str", jsonvalue.NullValue(), true},
		{"obj", jsonvalue.NullValue(), true},
		{"num", jsonvalue.FromBool(true), false},
	}
	for _, tc := range cases {
		d := c.VariationDetail(tc.flag, user, tc.fallback)
		got := d.Reason.Code != ErrorTypeMismatch
		if got != tc.served {
			t.Errorf("%s vs fallback kind %v: served=%v, want %v (%+v)", tc.flag, tc.fallback.Kind(), got, tc.served, d)
		}
		if tc.served && d.VariationIndex != 0 {
			t.Errorf("%s: index %d", tc.flag, d.VariationIndex)
		}
	}
	if d := c.JSONVariationDetail("arr", user, jsonvalue.FromArray(jsonvalue.FromString("anything"))); d.Reason.Code == ErrorTypeMismatch {
		t.Error("JSON accessor compared contents")
	}
}

func TestNoIndexKeepsTheEvaluatorsReason(t *testing.T) {
	c, f := valuesClient(t, WithFlushInterval(time.Hour))
	d := c.BoolVariationDetail("missing", user, true)
	if !d.Value || d.VariationIndex != -1 || d.Reason.Kind != ReasonError || d.Reason.Code != "FLAG_NOT_FOUND" {
		t.Errorf("%+v", d)
	}
	bad := c.BoolVariationDetail("bool", jsonvalue.FromInt(3), true)
	if bad.Reason.Code != "INVALID_CONTEXT" || bad.VariationIndex != -1 {
		t.Errorf("%+v", bad)
	}
	c.Close()
	if len(f.seen("events")) != 0 {
		t.Error("a fallback was counted")
	}
}

func TestEvaluatorPanicGivesEvaluationFailed(t *testing.T) {
	errs := &errorLog{}
	f := newFakeServer(t)
	c := newTestClient(t, f, WithStream(false), WithOnError(errs.add), withEvaluator(func(*eval.Ruleset, string, jsonvalue.Value) eval.Result {
		panic("boom")
	}))
	c.Ready()
	d := c.BoolVariationDetail("checkout", user, true)
	if !d.Value || d.VariationIndex != -1 || d.Reason.Code != ErrorEvaluationFailed {
		t.Errorf("%+v", d)
	}
	if errs.count() != 1 {
		t.Errorf("onError count = %d", errs.count())
	}
}

func TestNotReadyBeforeALoad(t *testing.T) {
	f := newFakeServer(t)
	f.script("ruleset", answer{hang: true})
	c := newTestClient(t, f, WithStream(false))
	d := c.StringVariationDetail("str", user, "fb")
	if d.Value != "fb" || d.Reason.Code != ErrorNotReady || d.VariationIndex != -1 {
		t.Errorf("%+v", d)
	}
}

func TestHookSeesTheSnapshotItEvaluatedAgainst(t *testing.T) {
	f := newFakeServer(t)
	var events []EvaluationEvent
	var mu sync.Mutex
	c := newTestClient(t, f, WithStream(false), WithAfterEvaluation(func(e EvaluationEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}))
	c.Ready()
	c.BoolVariation("checkout", user, false)
	c.BoolVariation("nope", user, false)
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 {
		t.Fatalf("events = %d", len(events))
	}
	e := events[0]
	if e.FlagKey != "checkout" || e.ETag != `"v1"` || e.FlagVersion != 3 || e.VariationIndex != 1 || e.Reason.Kind != ReasonFallthrough {
		t.Errorf("event = %+v", e)
	}
	if v, _ := e.Value.AsBool(); !v {
		t.Errorf("value = %v", e.Value)
	}
	if k, _ := e.Context.Get("key"); k.Kind() != jsonvalue.String {
		t.Error("context lost")
	}
	if events[1].FlagVersion != -1 || events[1].ETag != `"v1"` || events[1].VariationIndex != -1 {
		t.Errorf("event = %+v", events[1])
	}
}

func TestHookBeforeALoadHasNoETag(t *testing.T) {
	f := newFakeServer(t)
	f.script("ruleset", answer{hang: true})
	var got EvaluationEvent
	c := newTestClient(t, f, WithStream(false), WithAfterEvaluation(func(e EvaluationEvent) { got = e }))
	c.BoolVariation("checkout", user, false)
	if got.ETag != "" || got.FlagVersion != -1 || got.Reason.Code != ErrorNotReady {
		t.Errorf("event = %+v", got)
	}
}

func TestAPanickingHookAndAPanickingOnErrorChangeNothing(t *testing.T) {
	f := newFakeServer(t)
	var calls int
	c := newTestClient(t, f, WithStream(false),
		WithAfterEvaluation(func(EvaluationEvent) { panic("hook") }),
		WithOnError(func(error) { calls++; panic("onError") }))
	c.Ready()
	d := c.BoolVariationDetail("checkout", user, false)
	if !d.Value || d.VariationIndex != 1 {
		t.Errorf("%+v", d)
	}
	if calls != 1 {
		t.Errorf("onError calls = %d", calls)
	}
}

func TestHookEventETagAndVersionStayTogetherAcrossSwaps(t *testing.T) {
	f := newFakeServer(t)
	versions := map[string]float64{`"v1"`: 3}
	var bad atomic.Int32
	c := newTestClient(t, f, WithStream(false), WithPollInterval(time.Millisecond), WithAfterEvaluation(func(e EvaluationEvent) {
		if e.ETag == "" {
			return
		}
		want, ok := versions[e.ETag]
		if !ok {
			want = 5
		}
		if e.FlagVersion != want {
			bad.Add(1)
		}
	}))
	c.Ready()
	v2 := strings.Replace(twoFlags, `"version":3`, `"version":5`, 1)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					c.BoolVariation("checkout", user, false)
				}
			}
		}()
	}
	for i := 0; i < 40; i++ {
		tag := fmt.Sprintf(`"w%d"`, i)
		f.setRuleset(v2, tag)
		time.Sleep(2 * time.Millisecond)
	}
	close(stop)
	wg.Wait()
	if bad.Load() != 0 {
		t.Errorf("%d events paired an ETag with another ruleset's flag version", bad.Load())
	}
}
