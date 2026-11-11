// Command test-service wraps the Go SDK in the HTTP protocol of docs/sdk-test-service.md so the conformance
// harness (packages/sdk-conformance) can drive it from outside. The accessor mapping is in README.md. It
// absorbs nothing the SDK raises: a panic in a request answers 500 with the panic text, and the harness fails
// the scenario.
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
	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
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

func main() {
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	s := &service{clients: map[string]*entry{}}
	fmt.Printf("listening http://%s\n", ln.Addr())
	srv := &http.Server{Handler: http.HandlerFunc(s.handle), ReadHeaderTimeout: 10 * time.Second}
	if err := srv.Serve(ln); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func (s *service) handle(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if p := recover(); p != nil {
			http.Error(w, fmt.Sprint("panic: ", p), http.StatusInternalServerError)
		}
	}()
	path := strings.Trim(r.URL.Path, "/")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/":
		write(w, 200, obj(
			kv("name", jsonvalue.FromString("toggleslash-sdk (Go)")),
			kv("capabilities", jsonvalue.FromArray(jsonvalue.FromString("integer-accessor"))),
		))
	case r.Method == http.MethodPost && path == "clients":
		s.create(w, r)
	default:
		parts := strings.Split(path, "/")
		if len(parts) != 3 || parts[0] != "clients" {
			http.Error(w, "no such route", http.StatusNotFound)
			return
		}
		s.mu.Lock()
		e := s.clients[parts[1]]
		s.mu.Unlock()
		if e == nil {
			http.Error(w, "no such client", http.StatusNotFound)
			return
		}
		e.route(w, r, r.Method+" "+parts[2])
	}
}

func kv(k string, v jsonvalue.Value) jsonvalue.Member { return jsonvalue.Member{Key: k, Value: v} }
func obj(m ...jsonvalue.Member) jsonvalue.Value       { return jsonvalue.FromObject(m...) }

func write(w http.ResponseWriter, status int, v jsonvalue.Value) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(v.AppendJSON(nil))
}

func readBody(r *http.Request) (jsonvalue.Value, error) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return jsonvalue.Null, err
	}
	if len(raw) == 0 {
		return obj(), nil
	}
	v, err := jsonvalue.Parse(raw)
	if err != nil {
		return jsonvalue.Null, err
	}
	if v.Kind() != jsonvalue.KindObject {
		return jsonvalue.Null, errors.New("the body is not a JSON object")
	}
	return v, nil
}

func str(body jsonvalue.Value, name string) string {
	v, _ := body.Get(name)
	s, _ := v.AsString()
	return s
}

func flag(body jsonvalue.Value, name string) bool {
	v, _ := body.Get(name)
	b, _ := v.AsBool()
	return b
}

func millis(body jsonvalue.Value, name string) (time.Duration, bool) {
	v, ok := body.Get(name)
	if !ok {
		return 0, false
	}
	n, ok := v.AsNumber()
	if !ok {
		return 0, false
	}
	return time.Duration(n * float64(time.Millisecond)), true
}

func (s *service) create(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	e := &entry{}
	opts := []toggleslash.Option{
		toggleslash.WithOnError(func(err error) {
			e.mu.Lock()
			e.errors = append(e.errors, err.Error())
			e.mu.Unlock()
			if flag(body, "onErrorThrows") {
				panic("onError threw, as the test asked")
			}
		}),
	}
	if v, ok := body.Get("stream"); ok {
		if b, ok := v.AsBool(); ok {
			opts = append(opts, toggleslash.WithStream(b))
		}
	}
	if d, ok := millis(body, "pollIntervalMs"); ok {
		opts = append(opts, toggleslash.WithPollInterval(d))
	}
	if d, ok := millis(body, "flushIntervalMs"); ok {
		opts = append(opts, toggleslash.WithFlushInterval(d))
	}
	if d, ok := millis(body, "requestTimeoutMs"); ok {
		opts = append(opts, toggleslash.WithRequestTimeout(d))
	}
	if flag(body, "hook") || flag(body, "hookThrows") {
		throws := flag(body, "hookThrows")
		opts = append(opts, toggleslash.WithAfterEvaluation(func(ev toggleslash.EvaluationEvent) {
			e.mu.Lock()
			e.events = append(e.events, eventJSON(ev))
			e.mu.Unlock()
			if throws {
				panic("the hook threw, as the test asked")
			}
		}))
	}
	e.client = toggleslash.New(str(body, "url"), str(body, "sdkKey"), opts...)
	s.mu.Lock()
	s.created++
	id := strconv.Itoa(s.created)
	s.clients[id] = e
	s.mu.Unlock()
	write(w, http.StatusCreated, obj(kv("id", jsonvalue.FromString(id))))
}

func (e *entry) route(w http.ResponseWriter, r *http.Request, key string) {
	switch key {
	case "POST ready":
		write(w, 200, obj(kv("ready", jsonvalue.FromBool(e.client.Ready()))))
	case "POST evaluate":
		e.evaluate(w, r)
	case "GET status":
		write(w, 200, statusJSON(e.client.Status()))
	case "GET hook-events":
		e.mu.Lock()
		events := append([]jsonvalue.Value(nil), e.events...)
		e.mu.Unlock()
		write(w, 200, obj(kv("events", jsonvalue.FromArray(events...))))
	case "GET errors":
		e.mu.Lock()
		msgs := make([]jsonvalue.Value, len(e.errors))
		for i, m := range e.errors {
			msgs[i] = jsonvalue.FromString(m)
		}
		e.mu.Unlock()
		write(w, 200, obj(kv("count", jsonvalue.FromNumber(float64(len(msgs)))), kv("messages", jsonvalue.FromArray(msgs...))))
	case "POST close":
		e.client.Close()
		write(w, 200, obj())
	default:
		http.Error(w, "no such route", http.StatusNotFound)
	}
}

func (e *entry) evaluate(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	flagKey := str(body, "flagKey")
	ctxValue, _ := body.Get("context")
	// The context goes to the SDK as the harness sent it, valid or not: an invalid one is a case the SDK answers.
	ctx := toggleslash.ContextFromValue(ctxValue)
	fb, _ := body.Get("fallback")
	accessor := str(body, "accessor")
	if accessor == "" {
		accessor = "any"
	}

	var value jsonvalue.Value
	var index *int
	var reason toggleslash.Reason
	bad := func() { http.Error(w, "the fallback does not fit the accessor "+accessor, http.StatusBadRequest) }
	switch accessor {
	case "bool":
		b, ok := fb.AsBool()
		if !ok {
			bad()
			return
		}
		d := e.client.BoolVariationDetail(flagKey, ctx, b)
		value, index, reason = jsonvalue.FromBool(d.Value), d.VariationIndex, d.Reason
	case "string":
		sv, ok := fb.AsString()
		if !ok {
			bad()
			return
		}
		d := e.client.StringVariationDetail(flagKey, ctx, sv)
		value, index, reason = jsonvalue.FromString(d.Value), d.VariationIndex, d.Reason
	case "number":
		n, ok := fb.AsNumber()
		if !ok {
			bad()
			return
		}
		d := e.client.Float64VariationDetail(flagKey, ctx, n)
		value, index, reason = jsonvalue.FromNumber(d.Value), d.VariationIndex, d.Reason
	case "int":
		n, ok := fb.AsNumber()
		if !ok || n != float64(int64(n)) {
			bad()
			return
		}
		d := e.client.Int64VariationDetail(flagKey, ctx, int64(n))
		value, index, reason = jsonvalue.FromNumber(float64(d.Value)), d.VariationIndex, d.Reason
	case "json", "any":
		d := e.client.VariationDetail(flagKey, ctx, fb)
		value, index, reason = d.Value, d.VariationIndex, d.Reason
	default:
		http.Error(w, "unknown accessor "+accessor, http.StatusBadRequest)
		return
	}
	write(w, 200, obj(kv("value", value), kv("variationIndex", indexJSON(index)), kv("reason", reasonJSON(reason))))
}

func indexJSON(i *int) jsonvalue.Value {
	if i == nil {
		return jsonvalue.Null
	}
	return jsonvalue.FromNumber(float64(*i))
}

func reasonJSON(r toggleslash.Reason) jsonvalue.Value {
	m := []jsonvalue.Member{kv("kind", jsonvalue.FromString(string(r.Kind)))}
	switch r.Kind {
	case toggleslash.ReasonRuleMatch:
		m = append(m, kv("ruleIndex", jsonvalue.FromNumber(float64(r.RuleIndex))))
	case toggleslash.ReasonPrerequisiteFailed:
		m = append(m, kv("flagKey", jsonvalue.FromString(r.FlagKey)))
	case toggleslash.ReasonError:
		m = append(m, kv("code", jsonvalue.FromString(string(r.Code))))
	}
	return obj(m...)
}

func nullable(s string) jsonvalue.Value {
	if s == "" {
		return jsonvalue.Null
	}
	return jsonvalue.FromString(s)
}

func statusJSON(st toggleslash.Status) jsonvalue.Value {
	last := jsonvalue.Null
	if !st.LastSyncAt.IsZero() {
		last = jsonvalue.FromNumber(float64(st.LastSyncAt.UnixMilli()))
	}
	return obj(
		kv("etag", nullable(st.ETag)),
		kv("lastSyncAt", last),
		kv("stream", jsonvalue.FromString(string(st.Stream))),
		kv("lastError", nullable(st.LastError)),
	)
}

func eventJSON(ev toggleslash.EvaluationEvent) jsonvalue.Value {
	version := jsonvalue.Null
	if ev.FlagVersion != nil {
		version = jsonvalue.FromNumber(float64(*ev.FlagVersion))
	}
	ctx := ev.Context.Value()
	return obj(
		kv("flagKey", jsonvalue.FromString(ev.FlagKey)),
		kv("context", ctx),
		kv("value", ev.Value),
		kv("variationIndex", indexJSON(ev.VariationIndex)),
		kv("reason", reasonJSON(ev.Reason)),
		kv("etag", nullable(ev.ETag)),
		kv("flagVersion", version),
	)
}
