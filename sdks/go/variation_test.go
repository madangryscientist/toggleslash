package toggleslash

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// kinds is a ruleset with one flag per kind of value, plus a flag per interesting number.
func kinds() string {
	flags := []string{
		flagJSON("b", `[false,true]`, 1, 1),
		flagJSON("s", `["off","on"]`, 1, 1),
		flagJSON("null", `[null,null]`, 1, 1),
		flagJSON("arr", `[[1,2],[3]]`, 0, 1),
		flagJSON("obj", `[{"a":1},{"b":2}]`, 1, 1),
		flagJSON("whole", `[2.0]`, 0, 1),
		flagJSON("frac", `[1.5]`, 0, 1),
		flagJSON("big", `[9223372036854774784]`, 0, 1),
		flagJSON("over", `[9223372036854775808]`, 0, 1),
		flagJSON("min", `[-9223372036854775808]`, 0, 1),
		flagJSON("huge", `[1e19]`, 0, 1),
	}
	return rulesetJSON(flags...)
}

func readyClient(t *testing.T, body string) *Client {
	t.Helper()
	f := newFake(t)
	f.set(&f.ruleset, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"k"`)
		_, _ = w.Write([]byte(body))
	})
	c := f.client(t, WithStream(false))
	if !c.Ready() {
		t.Fatal("not ready")
	}
	return c
}

func TestNotReadyBeforeTheFirstLoad(t *testing.T) {
	f := newFake(t)
	release := make(chan struct{})
	f.set(&f.ruleset, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	c := f.client(t, WithStream(false))
	d := c.BoolVariationDetail("f", user, true)
	if d.Value != true || d.VariationIndex != nil || d.Reason.Kind != ReasonError || d.Reason.Code != ErrNotReady {
		t.Fatalf("got %+v", d)
	}
	if c.Status().Stream != StreamOff {
		t.Fatalf("stream %q", c.Status().Stream)
	}
	close(release)
}

func TestAccessorsServeTheirKind(t *testing.T) {
	c := readyClient(t, kinds())
	if d := c.BoolVariationDetail("b", user, false); !d.Value || d.VariationIndex == nil || *d.VariationIndex != 1 || d.Reason.Kind != ReasonFallthrough {
		t.Fatalf("bool %+v", d)
	}
	if got := c.StringVariation("s", user, "x"); got != "on" {
		t.Fatalf("string %q", got)
	}
	if got := c.FloatVariation("frac", user, 0); got != 1.5 {
		t.Fatalf("float %v", got)
	}
	if got := c.IntVariation("whole", user, 0); got != 2 {
		t.Fatalf("int %v", got)
	}
	obj := c.JSONVariation("obj", user, jsonv.Null())
	if o, ok := obj.AsObject(); !ok || o.Len() != 1 {
		t.Fatalf("json %v", obj)
	}
}

func TestIntegerAccessorRange(t *testing.T) {
	c := readyClient(t, kinds())
	cases := []struct {
		flag string
		want int64
		ok   bool
	}{
		{"whole", 2, true},
		{"frac", -1, false},
		{"big", 9223372036854774784, true},
		{"over", -1, false},
		{"min", -9223372036854775808, true},
		{"huge", -1, false},
	}
	for _, tc := range cases {
		d := c.IntVariationDetail(tc.flag, user, -1)
		if tc.ok {
			if d.Value != tc.want || d.VariationIndex == nil {
				t.Errorf("%s: %+v", tc.flag, d)
			}
			continue
		}
		if d.Value != -1 || d.VariationIndex != nil || d.Reason.Code != ErrTypeMismatch {
			t.Errorf("%s: %+v", tc.flag, d)
		}
	}
}

func TestTypeMismatch(t *testing.T) {
	c := readyClient(t, kinds())
	mismatch := func(name string, code ErrorCode, index *int) {
		t.Helper()
		if code != ErrTypeMismatch || index != nil {
			t.Errorf("%s: code %q index %v", name, code, index)
		}
	}
	a := c.StringVariationDetail("b", user, "fb")
	mismatch("bool as string", a.Reason.Code, a.VariationIndex)
	if a.Value != "fb" {
		t.Errorf("fallback not returned: %q", a.Value)
	}
	b := c.BoolVariationDetail("s", user, true)
	mismatch("string as bool", b.Reason.Code, b.VariationIndex)
	n := c.BoolVariationDetail("null", user, true)
	mismatch("null served against a bool fallback", n.Reason.Code, n.VariationIndex)
	arr := c.JSONVariationDetail("arr", user, jsonv.Array(nil))
	if arr.Reason.Code == ErrTypeMismatch {
		t.Errorf("array against array fallback: %+v", arr)
	}
	o := jsonv.NewObject()
	cross := c.JSONVariationDetail("arr", user, jsonv.ObjectValue(o))
	mismatch("array against an object fallback", cross.Reason.Code, cross.VariationIndex)
	cross2 := c.JSONVariationDetail("obj", user, jsonv.Array(nil))
	mismatch("object against an array fallback", cross2.Reason.Code, cross2.VariationIndex)
	any1 := c.JSONVariationDetail("null", user, jsonv.Null())
	if any1.VariationIndex == nil || !any1.Value.IsNull() {
		t.Errorf("null fallback accepts a null served value: %+v", any1)
	}
	any2 := c.JSONVariationDetail("s", user, jsonv.Null())
	if s, ok := any2.Value.AsString(); !ok || s != "on" {
		t.Errorf("null fallback accepts any kind: %+v", any2)
	}
}

func TestEvaluatorErrorsKeepTheirReason(t *testing.T) {
	c := readyClient(t, kinds())
	d := c.BoolVariationDetail("nope", user, true)
	if !d.Value || d.VariationIndex != nil || d.Reason.Code != ErrFlagNotFound {
		t.Fatalf("%+v", d)
	}
	for _, ctx := range []any{nil, map[string]any{"attributes": map[string]any{}}, map[string]any{"key": 5}, 42, "str", []any{}} {
		d := c.BoolVariationDetail("b", ctx, true)
		if d.Reason.Code != ErrInvalidContext || d.VariationIndex != nil {
			t.Errorf("context %v: %+v", ctx, d)
		}
	}
}

func TestContextForms(t *testing.T) {
	c := readyClient(t, kinds())
	parsed, err := ParseJSON([]byte(`{"key":"u","attributes":{"n":1e999}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, ctx := range []any{
		parsed,
		[]byte(`{"key":"u"}`),
		map[string]any{"key": "u", "attributes": map[string]any{"n": 1, "s": "x", "l": []any{1, 2.5, true, nil}}},
		struct {
			Key string `json:"key"`
		}{"u"},
	} {
		if d := c.BoolVariationDetail("b", ctx, false); d.Reason.Kind == ReasonError {
			t.Errorf("context %T: %+v", ctx, d)
		}
	}
}

func TestEvaluationFailureIsOneOnError(t *testing.T) {
	f := newFake(t)
	var log errorLog
	c := f.client(t, WithStream(false), WithOnError(log.add),
		withEvaluate(func(*evaluator.Ruleset, string, jsonv.Value) evaluator.Result { panic("secret-context-value") }))
	c.Ready()
	d := c.BoolVariationDetail("f", user, true)
	if !d.Value || d.VariationIndex != nil || d.Reason.Code != ErrEvaluationFailed {
		t.Fatalf("%+v", d)
	}
	waitFor(t, "the onError", func() bool { return log.count() >= 1 })
	if log.count() != 1 {
		t.Fatalf("onError called %d times", log.count())
	}
	if strings.Contains(log.text(), "secret-context-value") {
		t.Fatalf("the panic value reached onError: %q", log.text())
	}
	var pe *PanicError
	if !errors.As(log.all()[0], &pe) {
		t.Fatalf("not a PanicError: %v", log.all()[0])
	}
}

func TestHookSeesTheSnapshotItEvaluated(t *testing.T) {
	f := newFake(t)
	var events []Event
	c := f.client(t, WithStream(false), WithAfterEvaluation(func(e Event) { events = append(events, e) }))
	c.Ready()
	c.BoolVariation("f", user, false)
	c.BoolVariation("gone", user, false)
	if len(events) != 2 {
		t.Fatalf("%d events", len(events))
	}
	e := events[0]
	if e.ETag != `"v1"` || e.FlagVersion == nil || *e.FlagVersion != 7 || e.FlagKey != "f" || e.Value != true || e.VariationIndex == nil {
		t.Fatalf("%+v", e)
	}
	if events[1].FlagVersion != nil || events[1].ETag != `"v1"` {
		t.Fatalf("absent flag: %+v", events[1])
	}
}

func TestHookAndRulesetStayPaired(t *testing.T) {
	f := newFake(t)
	var n int
	f.set(&f.ruleset, func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		n++
		v := n
		f.mu.Unlock()
		w.Header().Set("ETag", `"v`+strconv.Itoa(v)+`"`)
		_, _ = w.Write([]byte(rulesetJSON(flagJSON("f", `[false,true]`, 1, v))))
	})
	bad := make(chan Event, 1)
	c := f.client(t, WithStream(false), WithPollInterval(time.Millisecond), WithAfterEvaluation(func(e Event) {
		if e.ETag != "" && (e.FlagVersion == nil || e.ETag != `"v`+strconv.Itoa(int(*e.FlagVersion))+`"`) {
			select {
			case bad <- e:
			default:
			}
		}
	}))
	c.Ready()
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		c.BoolVariation("f", user, false)
	}
	select {
	case e := <-bad:
		t.Fatalf("an event mixed two rulesets: %+v", e)
	default:
	}
	if f.count("/sdk/v1/ruleset") < 3 {
		t.Fatalf("the ruleset changed %d times; the test proves nothing", f.count("/sdk/v1/ruleset"))
	}
}

func TestHookPanicGoesToOnError(t *testing.T) {
	f := newFake(t)
	var log errorLog
	c := f.client(t, WithStream(false), WithOnError(log.add), WithAfterEvaluation(func(Event) { panic("hook broke") }))
	c.Ready()
	if !c.BoolVariation("f", user, false) {
		t.Fatal("the hook changed the answer")
	}
	if log.count() != 1 || !strings.Contains(log.text(), "hook broke") {
		t.Fatalf("onError: %q", log.text())
	}
}

func TestOnErrorPanicIsIgnored(t *testing.T) {
	f := newFake(t)
	f.set(&f.ruleset, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	c := f.client(t, WithStream(false), WithOnError(func(error) { panic("onError broke") }))
	if c.Ready() {
		t.Fatal("ready")
	}
	if c.Status().LastError != "http_500" {
		t.Fatalf("%+v", c.Status())
	}
	if c.BoolVariation("f", user, true) != true {
		t.Fatal("fallback")
	}
}

func TestNeverCountsAFallback(t *testing.T) {
	c := readyClient(t, kinds())
	c.StringVariation("b", user, "x") // type mismatch
	c.BoolVariation("nope", user, true)
	c.BoolVariation("b", nil, true)
	c.usageMu.Lock()
	n := len(c.pending)
	c.usageMu.Unlock()
	if n != 0 {
		t.Fatalf("%d pending", n)
	}
	c.BoolVariation("b", user, true)
	c.usageMu.Lock()
	n = len(c.pending)
	c.usageMu.Unlock()
	if n != 1 {
		t.Fatalf("%d pending", n)
	}
}

func TestHookSeesNotReady(t *testing.T) {
	f := newFake(t)
	release := make(chan struct{})
	f.set(&f.ruleset, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	var events []Event
	c := f.client(t, WithStream(false), WithAfterEvaluation(func(e Event) { events = append(events, e) }))
	c.BoolVariation("f", user, false)
	close(release)
	if len(events) != 1 || events[0].ETag != "" || events[0].FlagVersion != nil || events[0].Reason.Code != ErrNotReady {
		t.Fatalf("%+v", events)
	}
}
