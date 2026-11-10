// Command test-service wraps the Go SDK in the HTTP service the conformance harness drives
// (docs/sdk-test-service.md). It catches nothing the SDK panics with beyond what net/http does for a
// handler: a request that panics or fails answers 500 with the error text, and the harness fails the
// scenario.
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
	"strconv"
	"sync"
	"time"

	toggleslash "github.com/madangryscientist/toggleslash/sdks/go"
)

type entry struct {
	client *toggleslash.Client

	mu     sync.Mutex
	hooks  []toggleslash.Value
	errors []string
}

type service struct {
	mu      sync.Mutex
	clients map[string]*entry
	created int
}

func main() {
	svc := &service{clients: map[string]*entry{}}
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, "listen:", err)
		os.Exit(1)
	}
	srv := &http.Server{Handler: http.HandlerFunc(svc.serve)}
	fmt.Printf("listening http://127.0.0.1:%d\n", ln.Addr().(*net.TCPAddr).Port)
	if err := srv.Serve(ln); err != nil {
		fmt.Fprintln(os.Stderr, "serve:", err)
		os.Exit(1)
	}
}

var clientRoute = regexp.MustCompile(`^/clients/([^/]+)/([a-z-]+)$`)

func (s *service) serve(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			fail(w, fmt.Errorf("panic: %v", rec))
		}
	}()
	body, err := readBody(r)
	if err != nil {
		fail(w, err)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/":
		reply(w, http.StatusOK, toggleslash.Object(map[string]toggleslash.Value{
			"name":         toggleslash.String("toggleslash-sdk (Go)"),
			"capabilities": toggleslash.Array(toggleslash.String("integer-accessor")),
		}))
	case r.Method == http.MethodPost && r.URL.Path == "/clients":
		s.create(w, body)
	default:
		m := clientRoute.FindStringSubmatch(r.URL.Path)
		if m == nil {
			http.Error(w, "no such route", http.StatusNotFound)
			return
		}
		s.mu.Lock()
		e := s.clients[m[1]]
		s.mu.Unlock()
		if e == nil {
			http.Error(w, "no such client", http.StatusNotFound)
			return
		}
		s.clientAction(w, e, r.Method+" "+m[2], body)
	}
}

func readBody(r *http.Request) (toggleslash.Value, error) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return toggleslash.Null(), err
	}
	if len(data) == 0 {
		return toggleslash.Object(nil), nil
	}
	v, err := toggleslash.ParseJSON(data)
	if err != nil {
		return toggleslash.Null(), err
	}
	if !v.IsObject() {
		return toggleslash.Null(), errors.New("the body is not a JSON object")
	}
	return v, nil
}

func reply(w http.ResponseWriter, status int, v toggleslash.Value) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(toggleslash.MarshalJSON(v))
}

func fail(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func optString(body toggleslash.Value, name string) string {
	v, _ := body.Get(name)
	s, _ := v.AsString()
	return s
}

func optBool(body toggleslash.Value, name string) (value, present bool) {
	v, ok := body.Get(name)
	if !ok {
		return false, false
	}
	b, isBool := v.AsBool()
	return b, isBool
}

func optMillis(body toggleslash.Value, name string) (time.Duration, bool) {
	v, ok := body.Get(name)
	if !ok {
		return 0, false
	}
	n, isNum := v.AsNumber()
	if !isNum {
		return 0, false
	}
	return time.Duration(n * float64(time.Millisecond)), true
}

func (s *service) create(w http.ResponseWriter, body toggleslash.Value) {
	e := &entry{}
	var opts []toggleslash.Option
	if on, ok := optBool(body, "stream"); ok {
		opts = append(opts, toggleslash.WithStream(on))
	}
	if d, ok := optMillis(body, "pollIntervalMs"); ok {
		opts = append(opts, toggleslash.WithPollInterval(d))
	}
	if d, ok := optMillis(body, "flushIntervalMs"); ok {
		opts = append(opts, toggleslash.WithFlushInterval(d))
	}
	if d, ok := optMillis(body, "requestTimeoutMs"); ok {
		opts = append(opts, toggleslash.WithRequestTimeout(d))
	}
	onErrorThrows, _ := optBool(body, "onErrorThrows")
	opts = append(opts, toggleslash.WithOnError(func(err error) {
		e.mu.Lock()
		e.errors = append(e.errors, err.Error())
		e.mu.Unlock()
		if onErrorThrows {
			panic("onError threw, as the test asked")
		}
	}))
	hook, _ := optBool(body, "hook")
	hookThrows, _ := optBool(body, "hookThrows")
	if hook || hookThrows {
		opts = append(opts, toggleslash.WithAfterEvaluation(func(ev toggleslash.EvaluationEvent) {
			e.mu.Lock()
			e.hooks = append(e.hooks, eventJSON(ev))
			e.mu.Unlock()
			if hookThrows {
				panic("the hook panicked, as the test asked")
			}
		}))
	}
	e.client = toggleslash.Create(optString(body, "url"), optString(body, "sdkKey"), opts...)
	s.mu.Lock()
	s.created++
	id := strconv.Itoa(s.created)
	s.clients[id] = e
	s.mu.Unlock()
	reply(w, http.StatusCreated, toggleslash.Object(map[string]toggleslash.Value{"id": toggleslash.String(id)}))
}

func (s *service) clientAction(w http.ResponseWriter, e *entry, action string, body toggleslash.Value) {
	switch action {
	case "POST ready":
		reply(w, http.StatusOK, toggleslash.Object(map[string]toggleslash.Value{"ready": toggleslash.Bool(e.client.Ready())}))
	case "POST evaluate":
		reply(w, http.StatusOK, evaluate(e.client, body))
	case "GET status":
		reply(w, http.StatusOK, statusJSON(e.client.Status()))
	case "GET hook-events":
		e.mu.Lock()
		events := append([]toggleslash.Value(nil), e.hooks...)
		e.mu.Unlock()
		reply(w, http.StatusOK, toggleslash.Object(map[string]toggleslash.Value{"events": toggleslash.Array(events...)}))
	case "GET errors":
		e.mu.Lock()
		messages := make([]toggleslash.Value, len(e.errors))
		for i, m := range e.errors {
			messages[i] = toggleslash.String(m)
		}
		e.mu.Unlock()
		reply(w, http.StatusOK, toggleslash.Object(map[string]toggleslash.Value{
			"count":    toggleslash.Number(float64(len(messages))),
			"messages": toggleslash.Array(messages...),
		}))
	case "POST close":
		_ = e.client.Close()
		reply(w, http.StatusOK, toggleslash.Object(nil))
	default:
		http.Error(w, "no such route", http.StatusNotFound)
	}
}

func statusJSON(st toggleslash.Status) toggleslash.Value {
	etag, syncAt, lastError := toggleslash.Null(), toggleslash.Null(), toggleslash.Null()
	if st.ETag != "" {
		etag = toggleslash.String(st.ETag)
	}
	if !st.LastSyncAt.IsZero() {
		syncAt = toggleslash.Number(float64(st.LastSyncAt.UnixMilli()))
	}
	if st.LastError != "" {
		lastError = toggleslash.String(st.LastError)
	}
	return toggleslash.Object(map[string]toggleslash.Value{
		"etag":       etag,
		"lastSyncAt": syncAt,
		"stream":     toggleslash.String(string(st.Stream)),
		"lastError":  lastError,
	})
}

func indexJSON(i int) toggleslash.Value {
	if i < 0 {
		return toggleslash.Null()
	}
	return toggleslash.Number(float64(i))
}

func reasonJSON(r toggleslash.Reason) toggleslash.Value {
	m := map[string]toggleslash.Value{"kind": toggleslash.String(string(r.Kind))}
	switch r.Kind {
	case toggleslash.ReasonRuleMatch:
		m["ruleIndex"] = toggleslash.Number(float64(r.RuleIndex))
	case toggleslash.ReasonPrerequisiteFailed:
		m["flagKey"] = toggleslash.String(r.PrerequisiteKey)
	case toggleslash.ReasonError:
		m["code"] = toggleslash.String(string(r.ErrorCode))
	}
	return toggleslash.Object(m)
}

func eventJSON(ev toggleslash.EvaluationEvent) toggleslash.Value {
	etag, version := toggleslash.Null(), toggleslash.Null()
	if ev.ETag != "" {
		etag = toggleslash.String(ev.ETag)
	}
	if ev.FlagVersion != nil {
		version = toggleslash.Number(*ev.FlagVersion)
	}
	return toggleslash.Object(map[string]toggleslash.Value{
		"flagKey":        toggleslash.String(ev.FlagKey),
		"context":        ev.Context,
		"value":          ev.Value,
		"variationIndex": indexJSON(ev.VariationIndex),
		"reason":         reasonJSON(ev.Reason),
		"etag":           etag,
		"flagVersion":    version,
	})
}

func answer(value toggleslash.Value, index int, reason toggleslash.Reason) toggleslash.Value {
	return toggleslash.Object(map[string]toggleslash.Value{
		"value":          value,
		"variationIndex": indexJSON(index),
		"reason":         reasonJSON(reason),
	})
}

// evaluate maps the harness accessors onto the SDK's typed methods. The context and a json fallback pass
// through as raw JSON, valid or not.
func evaluate(c *toggleslash.Client, body toggleslash.Value) toggleslash.Value {
	flagKey := optString(body, "flagKey")
	ctx, _ := body.Get("context")
	fallback, _ := body.Get("fallback")
	accessor := optString(body, "accessor")
	switch accessor {
	case "bool":
		fb, _ := fallback.AsBool()
		d := c.BoolVariationDetail(flagKey, ctx, fb)
		return answer(toggleslash.Bool(d.Value), d.VariationIndex, d.Reason)
	case "string":
		fb, _ := fallback.AsString()
		d := c.StringVariationDetail(flagKey, ctx, fb)
		return answer(toggleslash.String(d.Value), d.VariationIndex, d.Reason)
	case "number":
		fb, _ := fallback.AsNumber()
		d := c.NumberVariationDetail(flagKey, ctx, fb)
		return answer(toggleslash.Number(d.Value), d.VariationIndex, d.Reason)
	case "int":
		fb, _ := fallback.AsNumber()
		d := c.IntVariationDetail(flagKey, ctx, int64(fb))
		return answer(toggleslash.Number(float64(d.Value)), d.VariationIndex, d.Reason)
	case "json", "any", "":
		d := c.JSONVariationDetail(flagKey, ctx, fallback)
		return answer(d.Value, d.VariationIndex, d.Reason)
	default:
		panic("unknown accessor " + accessor)
	}
}
