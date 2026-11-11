// Command test-service wraps the Go SDK in the HTTP protocol of
// docs/sdk-test-service.md so the conformance harness can drive it from outside.
// The accessor mapping is in this directory's README. It catches nothing the SDK
// does wrong: a call that panics answers 500 with the error text, and the
// harness fails the scenario.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	toggleslash "github.com/madangryscientist/toggleslash/sdks/go"
)

// entry is one client the service holds, with what its callbacks recorded.
type entry struct {
	client *toggleslash.Client
	mu     sync.Mutex
	events []toggleslash.Value
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
	srv := &http.Server{Handler: http.HandlerFunc(s.serve), ReadHeaderTimeout: 30 * time.Second}
	_, _ = fmt.Fprintf(os.Stdout, "listening http://%s\n", ln.Addr().String())
	_ = os.Stdout.Sync()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var clientRoute = regexp.MustCompile(`^/clients/([^/]+)/([a-z-]+)$`)

func (s *service) serve(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			http.Error(w, fmt.Sprint("panic: ", rec), http.StatusInternalServerError)
		}
	}()
	if err := s.route(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *service) route(w http.ResponseWriter, r *http.Request) error {
	switch {
	case r.Method == "GET" && r.URL.Path == "/":
		return reply(w, 200, object(
			"name", toggleslash.StringValue("toggleslash-sdk (Go)"),
			"capabilities", toggleslash.ArrayValue(toggleslash.StringValue("integer-accessor")),
		))
	case r.Method == "POST" && r.URL.Path == "/clients":
		return s.create(w, r)
	}
	m := clientRoute.FindStringSubmatch(r.URL.Path)
	if m == nil {
		http.Error(w, "no such route", http.StatusNotFound)
		return nil
	}
	s.mu.Lock()
	e := s.clients[m[1]]
	s.mu.Unlock()
	if e == nil {
		http.Error(w, "no such client", http.StatusNotFound)
		return nil
	}
	switch r.Method + " " + m[2] {
	case "POST ready":
		return reply(w, 200, object("ready", toggleslash.BoolValue(e.client.Ready())))
	case "POST evaluate":
		return e.evaluate(w, r)
	case "GET status":
		return reply(w, 200, statusValue(e.client.Status()))
	case "GET hook-events":
		e.mu.Lock()
		events := append([]toggleslash.Value(nil), e.events...)
		e.mu.Unlock()
		return reply(w, 200, object("events", toggleslash.ArrayValue(events...)))
	case "GET errors":
		e.mu.Lock()
		messages := make([]toggleslash.Value, len(e.errors))
		for i, m := range e.errors {
			messages[i] = toggleslash.StringValue(m)
		}
		e.mu.Unlock()
		return reply(w, 200, object(
			"count", toggleslash.NumberValue(float64(len(messages))),
			"messages", toggleslash.ArrayValue(messages...),
		))
	case "POST close":
		e.client.Close()
		return reply(w, 200, object())
	}
	http.Error(w, "no such route", http.StatusNotFound)
	return nil
}

func (s *service) create(w http.ResponseWriter, r *http.Request) error {
	body, err := readObject(r)
	if err != nil {
		return err
	}
	e := &entry{}
	opts := []toggleslash.Option{toggleslash.WithOnError(func(err error) {
		e.mu.Lock()
		e.errors = append(e.errors, err.Error())
		e.mu.Unlock()
		if flag(body, "onErrorThrows") {
			panic("onError threw, as the test asked")
		}
	})}
	if v, ok := body.Get("stream"); ok && v.Kind() == toggleslash.KindBool {
		opts = append(opts, toggleslash.WithStream(v.Bool()))
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
			e.events = append(e.events, eventValue(ev))
			e.mu.Unlock()
			if throws {
				panic("the hook panicked, as the test asked")
			}
		}))
	}
	url := stringField(body, "url")
	key := stringField(body, "sdkKey")
	e.client = toggleslash.New(url, key, opts...)
	s.mu.Lock()
	s.created++
	id := fmt.Sprint(s.created)
	s.clients[id] = e
	s.mu.Unlock()
	return reply(w, 201, object("id", toggleslash.StringValue(id)))
}

// evaluate maps the protocol's accessors onto the SDK's typed methods; see the README.
func (e *entry) evaluate(w http.ResponseWriter, r *http.Request) error {
	body, err := readObject(r)
	if err != nil {
		return err
	}
	flagKey := stringField(body, "flagKey")
	cv, _ := body.Get("context")
	ctx := toggleslash.ContextFromValue(cv)
	fallback, _ := body.Get("fallback")
	accessor := stringField(body, "accessor")

	var (
		value toggleslash.Value
		index *int
		rsn   toggleslash.Reason
	)
	switch accessor {
	case "bool":
		d := e.client.BoolVariationDetail(flagKey, ctx, fallback.Bool())
		value, index, rsn = toggleslash.BoolValue(d.Value), d.VariationIndex, d.Reason
	case "string":
		d := e.client.StringVariationDetail(flagKey, ctx, fallback.String())
		value, index, rsn = toggleslash.StringValue(d.Value), d.VariationIndex, d.Reason
	case "number":
		d := e.client.FloatVariationDetail(flagKey, ctx, fallback.Number())
		value, index, rsn = toggleslash.NumberValue(d.Value), d.VariationIndex, d.Reason
	case "int":
		d := e.client.IntVariationDetail(flagKey, ctx, int64(fallback.Number()))
		value, index, rsn = toggleslash.NumberValue(float64(d.Value)), d.VariationIndex, d.Reason
	case "json", "any", "":
		d := e.client.JSONVariationDetail(flagKey, ctx, fallback)
		value, index, rsn = d.Value, d.VariationIndex, d.Reason
	default:
		http.Error(w, "unknown accessor "+accessor, http.StatusBadRequest)
		return nil
	}
	return reply(w, 200, object("value", value, "variationIndex", indexValue(index), "reason", reasonValue(rsn)))
}

func reasonValue(r toggleslash.Reason) toggleslash.Value {
	o := toggleslash.NewObject()
	o.Set("kind", toggleslash.StringValue(string(r.Kind)))
	switch r.Kind {
	case toggleslash.ReasonRuleMatch:
		o.Set("ruleIndex", toggleslash.NumberValue(float64(r.RuleIndex)))
	case toggleslash.ReasonPrerequisiteFailed:
		o.Set("flagKey", toggleslash.StringValue(r.FlagKey))
	case toggleslash.ReasonError:
		o.Set("code", toggleslash.StringValue(string(r.Code)))
	}
	return toggleslash.ObjectValue(o)
}

func indexValue(i *int) toggleslash.Value {
	if i == nil {
		return toggleslash.NullValue()
	}
	return toggleslash.NumberValue(float64(*i))
}

func statusValue(s toggleslash.Status) toggleslash.Value {
	etag, lastError, syncAt := toggleslash.NullValue(), toggleslash.NullValue(), toggleslash.NullValue()
	if s.ETag != "" {
		etag = toggleslash.StringValue(s.ETag)
	}
	if s.LastError != "" {
		lastError = toggleslash.StringValue(s.LastError)
	}
	if !s.LastSyncAt.IsZero() {
		syncAt = toggleslash.NumberValue(float64(s.LastSyncAt.UnixMilli()))
	}
	return object("etag", etag, "lastSyncAt", syncAt, "stream", toggleslash.StringValue(string(s.Stream)), "lastError", lastError)
}

func eventValue(ev toggleslash.EvaluationEvent) toggleslash.Value {
	etag, version := toggleslash.NullValue(), toggleslash.NullValue()
	if ev.ETag != "" {
		etag = toggleslash.StringValue(ev.ETag)
	}
	if ev.FlagVersion != nil {
		version = toggleslash.NumberValue(float64(*ev.FlagVersion))
	}
	return object(
		"flagKey", toggleslash.StringValue(ev.FlagKey),
		"context", ev.Context.Value(),
		"value", ev.Value,
		"variationIndex", indexValue(ev.VariationIndex),
		"reason", reasonValue(ev.Reason),
		"etag", etag,
		"flagVersion", version,
	)
}

func object(pairs ...any) toggleslash.Value {
	o := toggleslash.NewObject()
	for i := 0; i+1 < len(pairs); i += 2 {
		o.Set(pairs[i].(string), pairs[i+1].(toggleslash.Value))
	}
	return toggleslash.ObjectValue(o)
}

func reply(w http.ResponseWriter, status int, v toggleslash.Value) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, err := w.Write(toggleslash.MarshalValue(v))
	return err
}

func readObject(r *http.Request) (*toggleslash.Object, error) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return toggleslash.NewObject(), nil
	}
	v, err := toggleslash.ParseValue(data)
	if err != nil {
		return nil, err
	}
	o := v.Object()
	if o == nil {
		return nil, errors.New("the body is not a JSON object")
	}
	return o, nil
}

func stringField(o *toggleslash.Object, name string) string {
	v, ok := o.Get(name)
	if !ok || v.Kind() != toggleslash.KindString {
		return ""
	}
	return v.String()
}

func flag(o *toggleslash.Object, name string) bool {
	v, ok := o.Get(name)
	return ok && v.Kind() == toggleslash.KindBool && v.Bool()
}

func millis(o *toggleslash.Object, name string) (time.Duration, bool) {
	v, ok := o.Get(name)
	if !ok || v.Kind() != toggleslash.KindNumber {
		return 0, false
	}
	return time.Duration(v.Number() * float64(time.Millisecond)), true
}
