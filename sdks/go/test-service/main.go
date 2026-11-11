// Command test-service is the Go SDK's conformance test service: an HTTP server that wraps toggleslash.New so
// the harness in packages/sdk-conformance can drive the SDK from outside. The protocol is
// docs/sdk-test-service.md and the accessor mapping is in README.md next to this file. It catches nothing the
// SDK raises: a call that panics answers 500 with the panic text, and the harness fails the scenario.
package main

import (
	"context"
	"errors"
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
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

type entry struct {
	client *toggleslash.Client

	mu     sync.Mutex
	events []jsonvalue.Value
	errors []string
}

type service struct {
	mu      sync.Mutex
	clients map[string]*entry
	created int
}

// reply writes a JSON answer.
func reply(w http.ResponseWriter, status int, v jsonvalue.Value) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(jsonvalue.Marshal(v))
}

func object(pairs ...any) jsonvalue.Value {
	o := jsonvalue.NewObject()
	for i := 0; i < len(pairs); i += 2 {
		o.Set(pairs[i].(string), pairs[i+1].(jsonvalue.Value))
	}
	return jsonvalue.ObjectValue(o)
}

func nullable(s string) jsonvalue.Value {
	if s == "" {
		return jsonvalue.Null()
	}
	return jsonvalue.String(s)
}

func number(n float64) jsonvalue.Value { return jsonvalue.Number(n) }

func readBody(r *http.Request) (jsonvalue.Value, error) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return jsonvalue.Null(), err
	}
	if len(data) == 0 {
		return jsonvalue.ObjectValue(jsonvalue.NewObject()), nil
	}
	v, err := jsonvalue.ParseBody(data)
	if err != nil {
		return jsonvalue.Null(), err
	}
	if !v.IsObject() {
		return jsonvalue.Null(), errors.New("the body is not a JSON object")
	}
	return v, nil
}

func reasonJSON(r toggleslash.Reason) jsonvalue.Value {
	o := jsonvalue.NewObject()
	o.Set("kind", jsonvalue.String(string(r.Kind)))
	switch r.Kind {
	case toggleslash.ReasonRuleMatch:
		o.Set("ruleIndex", number(float64(r.RuleIndex)))
	case toggleslash.ReasonPrerequisiteFailed:
		o.Set("flagKey", jsonvalue.String(r.FlagKey))
	case toggleslash.ReasonError:
		o.Set("code", jsonvalue.String(string(r.Code)))
	}
	return jsonvalue.ObjectValue(o)
}

func indexJSON(i int) jsonvalue.Value {
	if i < 0 {
		return jsonvalue.Null()
	}
	return number(float64(i))
}

func optionMillis(body jsonvalue.Value, name string) (time.Duration, bool) {
	v, ok := body.Get(name)
	if !ok || !v.IsNumber() {
		return 0, false
	}
	return time.Duration(v.NumberValue() * float64(time.Millisecond)), true
}

func flag(body jsonvalue.Value, name string) bool {
	v, ok := body.Get(name)
	return ok && v.IsBool() && v.BoolValue()
}

func text(body jsonvalue.Value, name string) string {
	v, _ := body.Get(name)
	return v.StringValue()
}

func (s *service) create(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	e := &entry{}
	var opts []toggleslash.Option
	onErrorThrows := flag(body, "onErrorThrows")
	opts = append(opts, toggleslash.WithOnError(func(err error) {
		e.mu.Lock()
		e.errors = append(e.errors, err.Error())
		e.mu.Unlock()
		if onErrorThrows {
			panic("onError threw, as the test asked")
		}
	}))
	if v, ok := body.Get("stream"); ok && v.IsBool() {
		opts = append(opts, toggleslash.WithStream(v.BoolValue()))
	}
	if d, ok := optionMillis(body, "pollIntervalMs"); ok {
		opts = append(opts, toggleslash.WithPollInterval(d))
	}
	if d, ok := optionMillis(body, "flushIntervalMs"); ok {
		opts = append(opts, toggleslash.WithFlushInterval(d))
	}
	if d, ok := optionMillis(body, "requestTimeoutMs"); ok {
		opts = append(opts, toggleslash.WithRequestTimeout(d))
	}
	hookThrows := flag(body, "hookThrows")
	if flag(body, "hook") || hookThrows {
		opts = append(opts, toggleslash.WithAfterEvaluation(func(ev toggleslash.EvaluationEvent) {
			version := jsonvalue.Null()
			if ev.FlagVersion != nil {
				version = number(*ev.FlagVersion)
			}
			rec := object(
				"flagKey", jsonvalue.String(ev.FlagKey), "context", ev.Context.Value(), "value", ev.Value,
				"variationIndex", indexJSON(ev.VariationIndex), "reason", reasonJSON(ev.Reason),
				"etag", nullable(ev.ETag), "flagVersion", version,
			)
			e.mu.Lock()
			e.events = append(e.events, rec)
			e.mu.Unlock()
			if hookThrows {
				panic("the hook threw, as the test asked")
			}
		}))
	}
	e.client = toggleslash.New(text(body, "url"), text(body, "sdkKey"), opts...)
	s.mu.Lock()
	s.created++
	id := strconv.Itoa(s.created)
	s.clients[id] = e
	s.mu.Unlock()
	reply(w, http.StatusCreated, object("id", jsonvalue.String(id)))
}

// evaluate maps the harness's accessor names onto the SDK's methods (see README.md).
func (e *entry) evaluate(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	context, _ := body.Get("context")
	fallback, _ := body.Get("fallback")
	key := text(body, "flagKey")
	accessor := "any"
	if a := text(body, "accessor"); a != "" {
		accessor = a
	}
	ctx := toggleslash.ContextFromValue(context)
	var d toggleslash.Detail[toggleslash.Value]
	switch accessor {
	case "bool":
		x := e.client.BoolVariationDetail(key, ctx, fallback.BoolValue())
		d = toggleslash.Detail[toggleslash.Value]{Value: jsonvalue.Bool(x.Value), VariationIndex: x.VariationIndex, Reason: x.Reason}
	case "string":
		x := e.client.StringVariationDetail(key, ctx, fallback.StringValue())
		d = toggleslash.Detail[toggleslash.Value]{Value: jsonvalue.String(x.Value), VariationIndex: x.VariationIndex, Reason: x.Reason}
	case "number":
		x := e.client.FloatVariationDetail(key, ctx, fallback.NumberValue())
		d = toggleslash.Detail[toggleslash.Value]{Value: number(x.Value), VariationIndex: x.VariationIndex, Reason: x.Reason}
	case "int":
		x := e.client.IntVariationDetail(key, ctx, int64(fallback.NumberValue()))
		d = toggleslash.Detail[toggleslash.Value]{Value: number(float64(x.Value)), VariationIndex: x.VariationIndex, Reason: x.Reason}
	case "json", "any":
		d = e.client.JSONVariationDetail(key, ctx, fallback)
	default:
		http.Error(w, "no such accessor", http.StatusBadRequest)
		return
	}
	reply(w, http.StatusOK, object("value", d.Value, "variationIndex", indexJSON(d.VariationIndex), "reason", reasonJSON(d.Reason)))
}

func (e *entry) status(w http.ResponseWriter) {
	st := e.client.Status()
	syncAt := jsonvalue.Null()
	if !st.LastSyncAt.IsZero() {
		syncAt = number(float64(st.LastSyncAt.UnixMilli()))
	}
	reply(w, http.StatusOK, object(
		"etag", nullable(st.ETag), "lastSyncAt", syncAt,
		"stream", jsonvalue.String(string(st.Stream)), "lastError", nullable(string(st.LastError)),
	))
}

func (e *entry) route(w http.ResponseWriter, r *http.Request, action string) {
	switch r.Method + " " + action {
	case "POST ready":
		reply(w, http.StatusOK, object("ready", jsonvalue.Bool(e.client.Ready())))
	case "POST evaluate":
		e.evaluate(w, r)
	case "GET status":
		e.status(w)
	case "GET hook-events":
		e.mu.Lock()
		events := append([]jsonvalue.Value{}, e.events...)
		e.mu.Unlock()
		reply(w, http.StatusOK, object("events", jsonvalue.Array(events)))
	case "GET errors":
		e.mu.Lock()
		messages := make([]jsonvalue.Value, len(e.errors))
		for i, m := range e.errors {
			messages[i] = jsonvalue.String(m)
		}
		e.mu.Unlock()
		reply(w, http.StatusOK, object("count", number(float64(len(messages))), "messages", jsonvalue.Array(messages)))
	case "POST close":
		e.client.Close()
		reply(w, http.StatusOK, object())
	default:
		http.Error(w, "no such route", http.StatusNotFound)
	}
}

func (s *service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if p := recover(); p != nil {
			http.Error(w, fmt.Sprint("the SDK panicked: ", p), http.StatusInternalServerError)
		}
	}()
	path := r.URL.Path
	if r.Method == http.MethodGet && path == "/" {
		reply(w, http.StatusOK, object(
			"name", jsonvalue.String("toggleslash-sdk (Go)"),
			"capabilities", jsonvalue.Array([]jsonvalue.Value{jsonvalue.String("integer-accessor")}),
		))
		return
	}
	if r.Method == http.MethodPost && path == "/clients" {
		s.create(w, r)
		return
	}
	parts := strings.Split(strings.TrimPrefix(path, "/clients/"), "/")
	if !strings.HasPrefix(path, "/clients/") || len(parts) != 2 {
		http.Error(w, "no such client", http.StatusNotFound)
		return
	}
	id, action := parts[0], parts[1]
	s.mu.Lock()
	e := s.clients[id]
	s.mu.Unlock()
	if e == nil || action == "" {
		http.Error(w, "no such client", http.StatusNotFound)
		return
	}
	e.route(w, r, action)
}

func main() {
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("listening http://%s\n", ln.Addr())
	srv := &http.Server{Handler: &service{clients: map[string]*entry{}}}
	if err := srv.Serve(ln); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
