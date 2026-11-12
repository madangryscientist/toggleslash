// Command test-service wraps the Go SDK in the protocol of docs/sdk-test-service.md, so the conformance harness
// (packages/sdk-conformance) can drive it from outside. The accessor mapping is in README.md. It catches nothing
// the SDK raises: a call that panics answers 500 with the text, and the harness fails the scenario.
package main

import (
	"bufio"
	"context"
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
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

type entry struct {
	client *toggleslash.Client
	mu     sync.Mutex
	events []toggleslash.Event
	errors []string
}

type service struct {
	mu      sync.Mutex
	clients map[string]*entry
	created int
}

var clientRoute = regexp.MustCompile(`^/clients/([^/]+)/([a-z-]+)$`)

func main() {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	svc := &service{clients: map[string]*entry{}}
	out := bufio.NewWriter(os.Stdout)
	_, _ = fmt.Fprintf(out, "listening http://127.0.0.1:%d\n", listener.Addr().(*net.TCPAddr).Port)
	_ = out.Flush()
	server := &http.Server{Handler: svc, ReadHeaderTimeout: 30 * time.Second}
	if err := server.Serve(listener); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func (s *service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if p := recover(); p != nil {
			http.Error(w, fmt.Sprint("panic: ", p), http.StatusInternalServerError)
		}
	}()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/":
		reply(w, 200, jsonvalue.Object(
			jsonvalue.Member{Key: "name", Value: jsonvalue.String("toggleslash-sdk (Go)")},
			jsonvalue.Member{Key: "capabilities", Value: jsonvalue.Array([]jsonvalue.Value{jsonvalue.String("integer-accessor")})},
		))
	case r.Method == http.MethodPost && r.URL.Path == "/clients":
		s.create(w, r)
	default:
		m := clientRoute.FindStringSubmatch(r.URL.Path)
		if m == nil {
			http.NotFound(w, r)
			return
		}
		s.mu.Lock()
		e := s.clients[m[1]]
		s.mu.Unlock()
		if e == nil {
			http.Error(w, "no such client", http.StatusNotFound)
			return
		}
		e.route(w, r, m[2])
	}
}

func reply(w http.ResponseWriter, status int, v jsonvalue.Value) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(jsonvalue.Marshal(v))
}

func readBody(r *http.Request) (jsonvalue.Value, error) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return jsonvalue.Null(), err
	}
	if len(raw) == 0 {
		return jsonvalue.Object(), nil
	}
	v, err := jsonvalue.ParseBytes(raw)
	if err != nil {
		return jsonvalue.Null(), err
	}
	if !v.IsObject() {
		return jsonvalue.Null(), fmt.Errorf("the body is not a JSON object")
	}
	return v, nil
}

func str(body jsonvalue.Value, name string) string {
	v, _ := body.Get(name)
	s, _ := v.AsString()
	return s
}

// millis reads an option in whole milliseconds, or reports that it is absent.
func millis(body jsonvalue.Value, name string) (time.Duration, bool) {
	v, _ := body.Get(name)
	f, ok := v.AsNumber()
	return time.Duration(f * float64(time.Millisecond)), ok
}

func flag(body jsonvalue.Value, name string) bool {
	v, _ := body.Get(name)
	b, _ := v.AsBool()
	return b
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
		if b, isBool := v.AsBool(); isBool {
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
		opts = append(opts, toggleslash.WithAfterEvaluation(func(ev toggleslash.Event) {
			e.mu.Lock()
			e.events = append(e.events, ev)
			e.mu.Unlock()
			if flag(body, "hookThrows") {
				panic("the hook panicked, as the test asked")
			}
		}))
	}
	e.client = toggleslash.New(str(body, "url"), str(body, "sdkKey"), opts...)
	s.mu.Lock()
	s.created++
	id := strconv.Itoa(s.created)
	s.clients[id] = e
	s.mu.Unlock()
	reply(w, http.StatusCreated, jsonvalue.Object(jsonvalue.Member{Key: "id", Value: jsonvalue.String(id)}))
}

func (e *entry) route(w http.ResponseWriter, r *http.Request, action string) {
	switch r.Method + " " + action {
	case "POST ready":
		reply(w, 200, jsonvalue.Object(jsonvalue.Member{Key: "ready", Value: jsonvalue.Bool(e.client.Ready())}))
	case "POST evaluate":
		e.evaluate(w, r)
	case "GET status":
		reply(w, 200, statusValue(e.client.Status()))
	case "GET hook-events":
		e.mu.Lock()
		events := make([]jsonvalue.Value, len(e.events))
		for i, ev := range e.events {
			events[i] = eventValue(ev)
		}
		e.mu.Unlock()
		reply(w, 200, jsonvalue.Object(jsonvalue.Member{Key: "events", Value: jsonvalue.Array(events)}))
	case "GET errors":
		e.mu.Lock()
		messages := make([]jsonvalue.Value, len(e.errors))
		for i, m := range e.errors {
			messages[i] = jsonvalue.String(m)
		}
		count := len(e.errors)
		e.mu.Unlock()
		reply(w, 200, jsonvalue.Object(
			jsonvalue.Member{Key: "count", Value: jsonvalue.Number(float64(count))},
			jsonvalue.Member{Key: "messages", Value: jsonvalue.Array(messages)},
		))
	case "POST close":
		e.client.Close()
		reply(w, 200, jsonvalue.Object())
	default:
		http.NotFound(w, r)
	}
}

func nullable(s string) jsonvalue.Value {
	if s == "" {
		return jsonvalue.Null()
	}
	return jsonvalue.String(s)
}

func statusValue(st toggleslash.Status) jsonvalue.Value {
	synced := jsonvalue.Null()
	if !st.LastSyncAt.IsZero() {
		synced = jsonvalue.Number(float64(st.LastSyncAt.UnixMilli()))
	}
	return jsonvalue.Object(
		jsonvalue.Member{Key: "etag", Value: nullable(st.ETag)},
		jsonvalue.Member{Key: "lastSyncAt", Value: synced},
		jsonvalue.Member{Key: "stream", Value: jsonvalue.String(string(st.Stream))},
		jsonvalue.Member{Key: "lastError", Value: nullable(string(st.LastError))},
	)
}

func indexValue(i int) jsonvalue.Value {
	if i == toggleslash.NoVariation {
		return jsonvalue.Null()
	}
	return jsonvalue.Number(float64(i))
}

func reasonValue(r toggleslash.Reason) jsonvalue.Value {
	members := []jsonvalue.Member{{Key: "kind", Value: jsonvalue.String(string(r.Kind))}}
	switch r.Kind {
	case toggleslash.ReasonRuleMatch:
		members = append(members, jsonvalue.Member{Key: "ruleIndex", Value: jsonvalue.Number(float64(r.RuleIndex))})
	case toggleslash.ReasonPrerequisiteFailed:
		members = append(members, jsonvalue.Member{Key: "flagKey", Value: jsonvalue.String(r.FlagKey)})
	case toggleslash.ReasonError:
		members = append(members, jsonvalue.Member{Key: "code", Value: jsonvalue.String(string(r.Code))})
	}
	return jsonvalue.Object(members...)
}

func eventValue(ev toggleslash.Event) jsonvalue.Value {
	version := jsonvalue.Null()
	if ev.FlagVersion != toggleslash.NoVersion {
		version = jsonvalue.Number(float64(ev.FlagVersion))
	}
	return jsonvalue.Object(
		jsonvalue.Member{Key: "flagKey", Value: jsonvalue.String(ev.FlagKey)},
		jsonvalue.Member{Key: "context", Value: ev.Context},
		jsonvalue.Member{Key: "value", Value: ev.Value},
		jsonvalue.Member{Key: "variationIndex", Value: indexValue(ev.VariationIndex)},
		jsonvalue.Member{Key: "reason", Value: reasonValue(ev.Reason)},
		jsonvalue.Member{Key: "etag", Value: nullable(ev.ETag)},
		jsonvalue.Member{Key: "flagVersion", Value: version},
	)
}

func answer(value jsonvalue.Value, index int, reason toggleslash.Reason) jsonvalue.Value {
	return jsonvalue.Object(
		jsonvalue.Member{Key: "value", Value: value},
		jsonvalue.Member{Key: "variationIndex", Value: indexValue(index)},
		jsonvalue.Member{Key: "reason", Value: reasonValue(reason)},
	)
}

// evaluate calls the accessor the request names. Every typed accessor takes a fallback of its own kind; the
// harness sends none of another kind, so a mismatch is a 400.
func (e *entry) evaluate(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	flagKey := str(body, "flagKey")
	ctx, _ := body.Get("context")
	fallback, _ := body.Get("fallback")
	accessor := str(body, "accessor")
	bad := func() { http.Error(w, "the fallback does not fit the "+accessor+" accessor", http.StatusBadRequest) }
	switch accessor {
	case "bool":
		fb, ok := fallback.AsBool()
		if !ok {
			bad()
			return
		}
		d := e.client.BoolVariationDetail(flagKey, ctx, fb)
		reply(w, 200, answer(jsonvalue.Bool(d.Value), d.VariationIndex, d.Reason))
	case "string":
		fb, ok := fallback.AsString()
		if !ok {
			bad()
			return
		}
		d := e.client.StringVariationDetail(flagKey, ctx, fb)
		reply(w, 200, answer(jsonvalue.String(d.Value), d.VariationIndex, d.Reason))
	case "number":
		fb, ok := fallback.AsNumber()
		if !ok {
			bad()
			return
		}
		d := e.client.Float64VariationDetail(flagKey, ctx, fb)
		reply(w, 200, answer(jsonvalue.Number(d.Value), d.VariationIndex, d.Reason))
	case "int":
		fb, ok := fallback.AsNumber()
		if !ok {
			bad()
			return
		}
		// The answer goes back through a double: no scenario serves an integer a double cannot hold.
		d := e.client.IntVariationDetail(flagKey, ctx, int64(fb))
		reply(w, 200, answer(jsonvalue.Number(float64(d.Value)), d.VariationIndex, d.Reason))
	case "json", "any":
		d := e.client.JSONVariationDetail(flagKey, ctx, fallback)
		reply(w, 200, answer(d.Value, d.VariationIndex, d.Reason))
	default:
		http.Error(w, "unknown accessor "+accessor, http.StatusBadRequest)
	}
}
