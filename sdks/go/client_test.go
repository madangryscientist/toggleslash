package toggleslash

import (
	"errors"
	"math"
	"net/http"
	"testing"
	"time"
)

// start builds a client against a fake server and closes it when the test ends.
func start(t *testing.T, server *fakeServer, change func(*config)) *Client {
	t.Helper()
	cfg := testConfig(server.url())
	if change != nil {
		change(&cfg)
	}
	client := newClient(cfg)
	t.Cleanup(client.Close)
	return client
}

func TestTheFirstLoadServesAndTheNextRequestIsConditional(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	client := start(t, server, nil)
	if !client.Ready() {
		t.Fatal("ready answered false after a good first load")
	}
	if got := client.BoolVariation("checkout", Context{"key": "user-1"}, false); !got {
		t.Error("checkout served false, expected true")
	}
	detail := client.BoolVariationDetail("checkout", Context{"key": "user-1"}, false)
	if detail.VariationIndex == nil || *detail.VariationIndex != 1 || detail.Reason.Kind != ReasonFallthrough {
		t.Errorf("detail was index %v, reason %v", detail.VariationIndex, detail.Reason)
	}
	status := client.Status()
	if status.ETag != `"v1"` {
		t.Errorf("status etag was %q, expected \"v1\"", status.ETag)
	}
	if status.LastSyncAt.IsZero() {
		t.Error("status has no sync time after a load")
	}
	if status.Stream != StreamOff {
		t.Errorf("stream was %v with the stream off", status.Stream)
	}
	// A second request carries the tag held, as If-None-Match.
	waitFor(t, 2*time.Second, "a second ruleset request", func() bool {
		return server.countFor("/sdk/v1/ruleset") >= 2
	})
	conditional := false
	for _, request := range server.seenRequests() {
		if request.headers.Get("If-None-Match") == `"v1"` {
			conditional = true
		}
	}
	if !conditional {
		t.Error("no request carried If-None-Match with the tag held")
	}
	if got := client.BoolVariation("checkout", Context{"key": "user-1"}, false); !got {
		t.Error("a 304 lost the ruleset")
	}
}

func TestBeforeTheFirstLoadEveryAnswerIsNotReady(t *testing.T) {
	release := make(chan struct{})
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	server.set(func(writer http.ResponseWriter, request *http.Request) {
		<-release
		server.defaultAnswer(writer, request)
	})
	client := start(t, server, func(cfg *config) { cfg.requestTimeout = 5 * time.Second })
	monotonic := time.Now()
	detail := client.VariationDetail("checkout", Context{"key": "user-1"}, "fallback")
	if detail.Reason.Kind != ReasonError || detail.Reason.Code != CodeNotReady {
		t.Errorf("before a load the reason was %v", detail.Reason)
	}
	if detail.Value != "fallback" || detail.VariationIndex != nil {
		t.Errorf("before a load the answer was %v with index %v", detail.Value, detail.VariationIndex)
	}
	if time.Since(monotonic) > 100*time.Millisecond {
		t.Error("an evaluation before the first load waited")
	}
	close(release)
}

func TestAnAnswerThatIsNotARulesetReplacesNothing(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	calls := 0
	server.set(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/sdk/v1/ruleset" {
			server.defaultAnswer(writer, request)
			return
		}
		calls++
		if calls == 1 {
			server.defaultAnswer(writer, request)
			return
		}
		_, _ = writer.Write([]byte(`{"not":"a ruleset"}`))
	})
	var reported recorder
	client := start(t, server, func(cfg *config) {
		cfg.onError = reported.add
	})
	if !client.Ready() {
		t.Fatal("the first load should have worked")
	}
	waitFor(t, 2*time.Second, "a bad ruleset", func() bool {
		return client.Status().LastError == LastErrorBadRuleset
	})
	if got := client.BoolVariation("checkout", Context{"key": "user-1"}, false); !got {
		t.Error("a bad ruleset replaced the one held")
	}
}

func TestReadyIsFixedAtTheFirstAttempt(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	attempts := 0
	server.set(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/sdk/v1/ruleset" {
			server.defaultAnswer(writer, request)
			return
		}
		attempts++
		if attempts == 1 {
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		server.defaultAnswer(writer, request)
	})
	client := start(t, server, nil)
	if client.Ready() {
		t.Fatal("ready answered true after a refused first load")
	}
	if client.Status().LastError != "http_500" {
		t.Errorf("lastError was %q, expected http_500", client.Status().LastError)
	}
	waitFor(t, 2*time.Second, "a later load", func() bool {
		return client.BoolVariation("checkout", Context{"key": "user-1"}, false)
	})
	if client.Ready() {
		t.Error("ready changed its answer after a later load")
	}
	if client.Status().LastError != "" {
		t.Errorf("a later sync should have cleared lastError, got %q", client.Status().LastError)
	}
}

func TestTheAccessors(t *testing.T) {
	flags := `{"key":"bool","variations":[false,true],"on":true,"offVariation":0,"targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":1},"salt":"s","version":3}` + "," +
		`{"key":"text","variations":["a","served"],"on":true,"offVariation":0,"targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":1},"salt":"s","version":4}` + "," +
		`{"key":"ratio","variations":[1.5],"on":true,"offVariation":0,"targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":0},"salt":"s","version":5}` + "," +
		`{"key":"two","variations":[2.0],"on":true,"offVariation":0,"targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":0},"salt":"s","version":6}` + "," +
		`{"key":"whole","variations":[4],"on":true,"offVariation":0,"targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":0},"salt":"s","version":7}` + "," +
		`{"key":"huge","variations":[1e20],"on":true,"offVariation":0,"targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":0},"salt":"s","version":8}` + "," +
		`{"key":"edge","variations":[9223372036854774784],"on":true,"offVariation":0,"targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":0},"salt":"s","version":9}` + "," +
		`{"key":"array","variations":[["a"]],"on":true,"offVariation":0,"targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":0},"salt":"s","version":10}` + "," +
		`{"key":"nothing","variations":[null],"on":true,"offVariation":0,"targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":0},"salt":"s","version":11}`
	server := newFakeServer(t, flags)
	client := start(t, server, nil)
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	context := Context{"key": "user-1"}

	if got := client.BoolVariation("bool", context, false); !got {
		t.Error("the bool accessor served false")
	}
	if got := client.StringVariation("text", context, "fallback"); got != "served" {
		t.Errorf("the string accessor served %q", got)
	}
	if got := client.Float64Variation("ratio", context, 0.5); got != 1.5 {
		t.Errorf("the number accessor served %v", got)
	}
	if got := client.Int64Variation("two", context, 7); got != 2 {
		t.Errorf("a served 2.0 read as %d, expected 2", got)
	}
	if got := client.Int64Variation("whole", context, 7); got != 4 {
		t.Errorf("the integer accessor served %d", got)
	}
	if got := client.Int64Variation("edge", context, 7); got != 9223372036854774784 {
		t.Errorf("the largest integer a double holds read as %d", got)
	}
	if got := client.Int64Variation("ratio", context, 7); got != 7 {
		t.Error("the integer accessor did not refuse a fraction")
	}
	if detail := client.Int64VariationDetail("ratio", context, 7); detail.Reason.Code != CodeTypeMismatch {
		t.Errorf("a fraction on the integer accessor gave %v", detail.Reason)
	}
	if detail := client.Int64VariationDetail("huge", context, 7); detail.Reason.Code != CodeTypeMismatch {
		t.Errorf("1e20 on the integer accessor gave %v", detail.Reason)
	}
	// The generic accessor compares kinds, and a null fallback accepts anything.
	if got := client.Variation("array", context, nil); got == nil {
		t.Error("a null fallback did not accept an array")
	}
	if detail := client.VariationDetail("array", context, map[string]any{"a": 1.0}); detail.Reason.Code != CodeTypeMismatch {
		t.Errorf("an array against an object fallback gave %v", detail.Reason)
	}
	if detail := client.VariationDetail("nothing", context, "fallback"); detail.Reason.Code != CodeTypeMismatch {
		t.Errorf("a served null against a string fallback gave %v", detail.Reason)
	}
	if detail := client.VariationDetail("nothing", context, nil); detail.Reason.Code != "" {
		t.Errorf("a served null against a null fallback gave %v", detail.Reason)
	}
	// A fallback of no JSON kind matches nothing at all.
	if detail := client.VariationDetail("bool", context, []string{"x"}); detail.Reason.Code != CodeTypeMismatch {
		t.Errorf("a []string fallback gave %v, expected TYPE_MISMATCH", detail.Reason)
	}
	// Only a matched value counts.
	if got := client.usage.pendingCount(); got != 8 {
		t.Errorf("%d usage entries were recorded, expected 8", got)
	}
}

func TestAServedArrayIsACopy(t *testing.T) {
	server := newFakeServer(t, `{"key":"list","variations":[["a"],{"a":1}],"on":true,"offVariation":0,"targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":0},"salt":"s","version":1}`)
	client := start(t, server, nil)
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	first := client.Variation("list", Context{"key": "u"}, nil).([]any)
	first[0] = "changed"
	second := client.Variation("list", Context{"key": "u"}, nil).([]any)
	if second[0] != "a" {
		t.Error("changing a served array changed the ruleset")
	}
}

func TestTheHookEvent(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 7)+","+flagJSON("count", true, 8)+","+
		`{"key":"number","variations":[42],"on":true,"offVariation":0,"targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":0},"salt":"s","version":9}`)
	var events []EvaluationEvent
	client := start(t, server, func(cfg *config) {
		cfg.afterEvaluate = func(event EvaluationEvent) { events = append(events, event) }
	})
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	context := Context{"key": "user-1", "plan": "pro"}
	_ = client.BoolVariationDetail("checkout", context, false)
	if len(events) != 1 {
		t.Fatalf("the hook was called %d times", len(events))
	}
	event := events[0]
	if event.FlagKey != "checkout" || event.ETag != `"v1"` {
		t.Errorf("the event was %+v", event)
	}
	if event.VariationIndex == nil || *event.VariationIndex != 1 || event.Reason.Kind != ReasonFallthrough {
		t.Errorf("the event's index and reason were %v and %v", event.VariationIndex, event.Reason)
	}
	if event.FlagVersion == nil || *event.FlagVersion != 7 {
		t.Errorf("the event's flag version was %v", event.FlagVersion)
	}
	if event.Context["plan"] != "pro" || event.Value != true {
		t.Errorf("the event carried %v and %v", event.Context, event.Value)
	}
	// A fallback before the first load has no index, no tag and no version.
	other := newFakeServer(t, flagJSON("checkout", true, 7))
	other.set(func(writer http.ResponseWriter, request *http.Request) { <-request.Context().Done() })
	before := start(t, other, func(cfg *config) {
		cfg.requestTimeout = 5 * time.Second
		cfg.afterEvaluate = func(event EvaluationEvent) { events = append(events, event) }
	})
	_ = before.Variation("checkout", context, "fallback")
	fallbackEvent := events[len(events)-1]
	if fallbackEvent.VariationIndex != nil || fallbackEvent.ETag != "" || fallbackEvent.FlagVersion != nil {
		t.Errorf("a fallback before the load carried %v, %q and %v", fallbackEvent.VariationIndex, fallbackEvent.ETag, fallbackEvent.FlagVersion)
	}
	// The integer accessor's hook event carries the int64 itself.
	_ = client.Int64VariationDetail("number", context, 0)
	integer := events[len(events)-1]
	if value, ok := integer.Value.(int64); !ok || value != 42 {
		t.Errorf("the integer hook value was %T(%v), expected int64(42)", integer.Value, integer.Value)
	}
	// An unknown flag has no version; a type mismatch keeps the one the flag has.
	_ = client.StringVariationDetail("checkout", context, "fallback")
	mismatch := events[len(events)-1]
	if mismatch.Reason.Code != CodeTypeMismatch || mismatch.FlagVersion == nil {
		t.Errorf("a mismatch carried %v and %v", mismatch.Reason, mismatch.FlagVersion)
	}
	_ = client.Variation("missing", context, nil)
	unknown := events[len(events)-1]
	if unknown.FlagVersion != nil || unknown.Reason.Code != CodeFlagNotFound {
		t.Errorf("an unknown flag carried %v and %v", unknown.FlagVersion, unknown.Reason)
	}
}

func TestAHookThatPanicsGoesToOnError(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	var reported recorder
	client := start(t, server, func(cfg *config) {
		cfg.onError = reported.add
		cfg.afterEvaluate = func(EvaluationEvent) { panic("as the test asked") }
	})
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	detail := client.BoolVariationDetail("checkout", Context{"key": "u"}, false)
	if detail.Value != true || detail.Reason.Kind != ReasonFallthrough {
		t.Errorf("a panicking hook changed the answer to %v", detail)
	}
	if reported.count() != 1 {
		t.Fatalf("onError was told %d times, expected once", reported.count())
	}
}

func TestAnEvaluationThatRaisesIsEvaluationFailed(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	var reported recorder
	client := start(t, server, func(cfg *config) {
		cfg.onError = reported.add
		cfg.raiseEvaluation = true
	})
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	detail := client.VariationDetail("checkout", Context{"key": "u"}, "fallback")
	if detail.Reason.Code != CodeEvaluationFailed || detail.Value != "fallback" || detail.VariationIndex != nil {
		t.Errorf("a raising evaluator gave %v", detail)
	}
	if reported.count() != 1 {
		t.Errorf("onError was told %d times, expected once", reported.count())
	}
	if client.usage.pendingCount() != 0 {
		t.Error("a failed evaluation was counted")
	}
}

func TestEveryNumericTypeInAContextIsANumber(t *testing.T) {
	server := newFakeServer(t, `{"key":"five","variations":[true,false],"on":true,"offVariation":1,"targets":[],"prerequisites":[],"rules":[{"clauses":[{"attribute":"n","op":"in","values":[5]}],"variation":0}],"fallthrough":{"variation":1},"salt":"s","version":1}`)
	client := start(t, server, nil)
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	values := []any{5, int8(5), int16(5), int32(5), int64(5), uint(5), uint8(5), uint16(5), uint32(5), uint64(5), float32(5), 5.0}
	for _, value := range values {
		context := Context{"key": "u", "attributes": map[string]any{"n": value}}
		detail := client.BoolVariationDetail("five", context, false)
		if detail.Value != true {
			t.Errorf("a context attribute of type %T did not match the number 5", value)
		}
	}
	// A bool is not a number, and neither is a non-finite one.
	for _, value := range []any{true, math.Inf(1), math.NaN()} {
		context := Context{"key": "u", "attributes": map[string]any{"n": value}}
		if detail := client.BoolVariationDetail("five", context, false); detail.Value != false {
			t.Errorf("a context attribute of type %T matched the number 5", value)
		}
	}
}

func TestAnInvalidContextIsAnErrorNotAPanic(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	client := start(t, server, nil)
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	for _, context := range []Context{
		nil,
		{},
		{"key": ""},
		{"kind": "multi"},
		{"kind": "multi", "contexts": map[string]any{}},
		{"kind": "multi", "key": "u", "contexts": map[string]any{"user": map[string]any{"key": "u"}}},
		{"kind": 5, "key": "u"},
		{"key": "u", "attributes": "not an object"},
	} {
		detail := client.VariationDetail("checkout", context, "fallback")
		if detail.Reason.Code != CodeInvalidContext || detail.Value != "fallback" {
			t.Errorf("context %v gave %v", context, detail)
		}
	}
	// A multi context is accepted.
	multi := Context{"kind": "multi", "contexts": map[string]any{
		"user": map[string]any{"key": "u"},
		"org":  map[string]any{"key": "acme", "attributes": map[string]any{"seats": 40.0}},
	}}
	if detail := client.VariationDetail("checkout", multi, false); detail.Value != true {
		t.Errorf("a multi context gave %v", detail)
	}
}

func TestANetworkFailureIsReportedWithoutTheURL(t *testing.T) {
	var reported recorder
	client := newClient(func() config {
		cfg := testConfig("http://127.0.0.1:1")
		cfg.requestTimeout = 300 * time.Millisecond
		cfg.onError = reported.add
		return cfg
	}())
	t.Cleanup(client.Close)
	if client.Ready() {
		t.Error("ready answered true with no server")
	}
	waitFor(t, 2*time.Second, "a network error", func() bool { return reported.count() > 0 })
	if client.Status().LastError != LastErrorNetwork {
		t.Errorf("lastError was %q", client.Status().LastError)
	}
	message := reported.first().Error()
	if message == "" || message == "127.0.0.1" {
		t.Fatalf("the message was %q", message)
	}
	var carried *Error
	if !errors.As(reported.first(), &carried) || carried.Code != LastErrorNetwork {
		t.Errorf("the error was %T, expected *Error with the network code", reported.first())
	}
	for _, word := range []string{"127.0.0.1", "fs_server", "http://"} {
		if containsText(message, word) {
			t.Errorf("the error message %q carries %q", message, word)
		}
	}
}
