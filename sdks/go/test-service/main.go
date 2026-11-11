// Command test-service wraps the SDK in the small HTTP server of docs/sdk-test-service.md, so the
// harness in packages/sdk-conformance can drive it from outside. It catches nothing the SDK does
// wrong: a call that raises or panics answers 500 with the error text. The accessor mapping is in
// README.md next to this file.
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	toggleslash "github.com/madangryscientist/toggleslash/sdks/go"
)

type entry struct {
	client *toggleslash.Client

	mu     sync.Mutex
	events []toggleslash.EvaluationEvent
	errors []string
}

type service struct {
	mu      sync.Mutex
	clients map[string]*entry
	created int
}

func main() {
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	svc := &service{clients: map[string]*entry{}}
	fmt.Printf("listening http://127.0.0.1:%d\n", ln.Addr().(*net.TCPAddr).Port)
	srv := &http.Server{Handler: http.HandlerFunc(svc.serve)}
	if err := srv.Serve(ln); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// clientRoute splits /clients/{id}/{op}; ok is false for any other path.
func clientRoute(path string) (id, op string, ok bool) {
	rest, found := strings.CutPrefix(path, "/clients/")
	if !found {
		return "", "", false
	}
	id, op, found = strings.Cut(rest, "/")
	if !found || id == "" || op == "" || strings.Contains(op, "/") {
		return "", "", false
	}
	return id, op, true
}

func (s *service) serve(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if p := recover(); p != nil {
			fail(w, http.StatusInternalServerError, fmt.Sprint("panic: ", p))
		}
	}()
	if r.Method == http.MethodGet && r.URL.Path == "/" {
		write(w, http.StatusOK, obj(
			"name", toggleslash.String("toggleslash-sdk (Go)"),
			"capabilities", toggleslash.Array(toggleslash.String("integer-accessor")),
		))
		return
	}
	if r.Method == http.MethodPost && r.URL.Path == "/clients" {
		s.create(w, r)
		return
	}
	id, op, ok := clientRoute(r.URL.Path)
	if !ok {
		fail(w, http.StatusNotFound, "no such route")
		return
	}
	s.mu.Lock()
	e := s.clients[id]
	s.mu.Unlock()
	if e == nil {
		fail(w, http.StatusNotFound, "no such client")
		return
	}
	e.route(w, r, op)
}

func fail(w http.ResponseWriter, status int, text string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, text)
}

func write(w http.ResponseWriter, status int, v toggleslash.Value) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(toggleslash.MarshalJSON(v))
}

func obj(kv ...any) toggleslash.Value {
	keys := make([]string, 0, len(kv)/2)
	values := make([]toggleslash.Value, 0, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		keys = append(keys, kv[i].(string))
		values = append(values, kv[i+1].(toggleslash.Value))
	}
	return toggleslash.Object(keys, values)
}

func readBody(r *http.Request) (toggleslash.Value, error) {
	text, err := io.ReadAll(r.Body)
	if err != nil {
		return toggleslash.Value{}, err
	}
	if len(text) == 0 {
		return obj(), nil
	}
	v, err := toggleslash.ParseJSON(text)
	if err != nil {
		return toggleslash.Value{}, err
	}
	if v.Kind() != toggleslash.KindObject {
		return toggleslash.Value{}, fmt.Errorf("the body is not a JSON object")
	}
	return v, nil
}

func durationOption(body toggleslash.Value, name string) (time.Duration, bool) {
	v := body.Field(name)
	if v.Kind() != toggleslash.KindNumber {
		return 0, false
	}
	return time.Duration(v.Number() * float64(time.Millisecond)), true
}

func (s *service) create(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	e := &entry{}
	opts := []toggleslash.Option{
		toggleslash.WithOnError(func(err error) {
			e.mu.Lock()
			e.errors = append(e.errors, err.Error())
			e.mu.Unlock()
			if body.Field("onErrorThrows").Kind() == toggleslash.KindBool && body.Field("onErrorThrows").Bool() {
				panic("onError threw, as the test asked")
			}
		}),
	}
	if v := body.Field("stream"); v.Kind() == toggleslash.KindBool {
		opts = append(opts, toggleslash.WithStream(v.Bool()))
	}
	if d, ok := durationOption(body, "pollIntervalMs"); ok {
		opts = append(opts, toggleslash.WithPollInterval(d))
	}
	if d, ok := durationOption(body, "flushIntervalMs"); ok {
		opts = append(opts, toggleslash.WithFlushInterval(d))
	}
	if d, ok := durationOption(body, "requestTimeoutMs"); ok {
		opts = append(opts, toggleslash.WithRequestTimeout(d))
	}
	hook, hookThrows := flag(body, "hook"), flag(body, "hookThrows")
	if hook || hookThrows {
		opts = append(opts, toggleslash.WithAfterEvaluation(func(ev toggleslash.EvaluationEvent) {
			e.mu.Lock()
			e.events = append(e.events, ev)
			e.mu.Unlock()
			if hookThrows {
				panic("the hook threw, as the test asked")
			}
		}))
	}
	e.client = toggleslash.New(body.Field("url").String(), body.Field("sdkKey").String(), opts...)
	s.mu.Lock()
	s.created++
	id := strconv.Itoa(s.created)
	s.clients[id] = e
	s.mu.Unlock()
	write(w, http.StatusCreated, obj("id", toggleslash.String(id)))
}

func flag(body toggleslash.Value, name string) bool {
	v := body.Field(name)
	return v.Kind() == toggleslash.KindBool && v.Bool()
}

func (e *entry) route(w http.ResponseWriter, r *http.Request, action string) {
	switch r.Method + " " + action {
	case "POST ready":
		write(w, http.StatusOK, obj("ready", toggleslash.Bool(e.client.Ready())))
	case "POST evaluate":
		e.evaluate(w, r)
	case "GET status":
		write(w, http.StatusOK, statusValue(e.client.Status()))
	case "GET hook-events":
		e.mu.Lock()
		events := make([]toggleslash.Value, 0, len(e.events))
		for _, ev := range e.events {
			events = append(events, eventValue(ev))
		}
		e.mu.Unlock()
		write(w, http.StatusOK, obj("events", toggleslash.Array(events...)))
	case "GET errors":
		e.mu.Lock()
		messages := make([]toggleslash.Value, 0, len(e.errors))
		for _, m := range e.errors {
			messages = append(messages, toggleslash.String(m))
		}
		count := len(e.errors)
		e.mu.Unlock()
		write(w, http.StatusOK, obj(
			"count", toggleslash.Number(float64(count)),
			"messages", toggleslash.Array(messages...),
		))
	case "POST close":
		e.client.Close()
		write(w, http.StatusOK, obj())
	default:
		fail(w, http.StatusNotFound, "no such route")
	}
}

func (e *entry) evaluate(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	flagKey := body.Field("flagKey").String()
	ctx := body.Field("context")
	fallback := body.Field("fallback")
	var d toggleslash.Detail
	switch accessor := body.Field("accessor").String(); accessor {
	case "", "any", "json":
		d = e.client.VariationDetail(flagKey, ctx, fallback)
	case "bool":
		if fallback.Kind() != toggleslash.KindBool {
			fail(w, http.StatusBadRequest, "the bool accessor needs a boolean fallback")
			return
		}
		t := e.client.BoolVariationDetail(flagKey, ctx, fallback.Bool())
		d = toggleslash.Detail{Value: toggleslash.Bool(t.Value), VariationIndex: t.VariationIndex, Reason: t.Reason}
	case "string":
		if fallback.Kind() != toggleslash.KindString {
			fail(w, http.StatusBadRequest, "the string accessor needs a string fallback")
			return
		}
		t := e.client.StringVariationDetail(flagKey, ctx, fallback.String())
		d = toggleslash.Detail{Value: toggleslash.String(t.Value), VariationIndex: t.VariationIndex, Reason: t.Reason}
	case "number":
		if fallback.Kind() != toggleslash.KindNumber {
			fail(w, http.StatusBadRequest, "the number accessor needs a number fallback")
			return
		}
		t := e.client.NumberVariationDetail(flagKey, ctx, fallback.Number())
		d = toggleslash.Detail{Value: toggleslash.Number(t.Value), VariationIndex: t.VariationIndex, Reason: t.Reason}
	case "int":
		if fallback.Kind() != toggleslash.KindNumber || fallback.Number() != float64(int64(fallback.Number())) {
			fail(w, http.StatusBadRequest, "the int accessor needs an integer fallback")
			return
		}
		t := e.client.IntVariationDetail(flagKey, ctx, int64(fallback.Number()))
		d = toggleslash.Detail{Value: toggleslash.Number(float64(t.Value)), VariationIndex: t.VariationIndex, Reason: t.Reason}
	default:
		fail(w, http.StatusBadRequest, "unknown accessor "+accessor)
		return
	}
	write(w, http.StatusOK, obj(
		"value", d.Value,
		"variationIndex", indexValue(d.VariationIndex),
		"reason", reasonValue(d.Reason),
	))
}

func indexValue(i int) toggleslash.Value {
	if i == toggleslash.NoVariation {
		return toggleslash.Null()
	}
	return toggleslash.Number(float64(i))
}

func reasonValue(r toggleslash.Reason) toggleslash.Value {
	keys := []string{"kind"}
	values := []toggleslash.Value{toggleslash.String(string(r.Kind))}
	switch r.Kind {
	case toggleslash.ReasonRuleMatch:
		keys, values = append(keys, "ruleIndex"), append(values, toggleslash.Number(float64(r.RuleIndex)))
	case toggleslash.ReasonPrerequisiteFailed:
		keys, values = append(keys, "flagKey"), append(values, toggleslash.String(r.FlagKey))
	case toggleslash.ReasonError:
		keys, values = append(keys, "code"), append(values, toggleslash.String(string(r.Code)))
	}
	return toggleslash.Object(keys, values)
}

func optString(s string) toggleslash.Value {
	if s == "" {
		return toggleslash.Null()
	}
	return toggleslash.String(s)
}

func statusValue(st toggleslash.Status) toggleslash.Value {
	synced := toggleslash.Null()
	if st.LastSyncAt != nil {
		synced = toggleslash.Number(float64(st.LastSyncAt.UnixMilli()))
	}
	return obj(
		"etag", optString(st.ETag),
		"lastSyncAt", synced,
		"stream", toggleslash.String(string(st.Stream)),
		"lastError", optString(st.LastError),
	)
}

func eventValue(ev toggleslash.EvaluationEvent) toggleslash.Value {
	version := toggleslash.Null()
	if ev.FlagVersion != nil {
		version = toggleslash.Number(float64(*ev.FlagVersion))
	}
	return obj(
		"flagKey", toggleslash.String(ev.FlagKey),
		"context", ev.Context,
		"value", ev.Value,
		"variationIndex", indexValue(ev.VariationIndex),
		"reason", reasonValue(ev.Reason),
		"etag", optString(ev.ETag),
		"flagVersion", version,
	)
}
