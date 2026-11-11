// Command test-service is the Go SDK's conformance test service: an HTTP server that wraps the client so
// the harness in packages/sdk-conformance can drive it from outside. The protocol is
// docs/sdk-test-service.md and the accessor mapping is in README.md next to this file. It catches nothing
// the SDK raises: a call that panics answers 500 with the error text and the harness fails the scenario.
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	toggleslash "github.com/madangryscientist/toggleslash/sdks/go"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

type entry struct {
	client *toggleslash.Client
	mu     sync.Mutex
	events []toggleslash.Value
	errors []string
}

type registry struct {
	mu      sync.Mutex
	clients map[string]*entry
	created int
}

func main() {
	reg := &registry{clients: map[string]*entry{}}
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, "test-service:", err)
		os.Exit(1)
	}
	server := &http.Server{Handler: http.HandlerFunc(reg.serve)}
	_, _ = fmt.Fprintf(os.Stdout, "listening http://%s\n", listener.Addr())
	if err := server.Serve(listener); err != nil {
		fmt.Fprintln(os.Stderr, "test-service:", err)
		os.Exit(1)
	}
}

func writeJSON(w http.ResponseWriter, status int, v toggleslash.Value) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(jsonv.Append(nil, v))
}

func fail(w http.ResponseWriter, status int, text string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, text)
}

func (reg *registry) serve(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if p := recover(); p != nil {
			fail(w, http.StatusInternalServerError, fmt.Sprintf("panic: %v\n%s", p, debug.Stack()))
		}
	}()
	path := strings.Trim(r.URL.Path, "/")
	switch {
	case r.Method == http.MethodGet && path == "":
		writeJSON(w, 200, toggleslash.Object(
			toggleslash.Member{Key: "name", Value: toggleslash.String("toggleslash-sdk (Go)")},
			toggleslash.Member{Key: "capabilities", Value: toggleslash.Array(toggleslash.String("integer-accessor"))},
		))
	case r.Method == http.MethodPost && path == "clients":
		reg.create(w, r)
	default:
		parts := strings.Split(path, "/")
		if len(parts) != 3 || parts[0] != "clients" {
			fail(w, http.StatusNotFound, "no such route")
			return
		}
		reg.mu.Lock()
		e := reg.clients[parts[1]]
		reg.mu.Unlock()
		if e == nil {
			fail(w, http.StatusNotFound, "no such client")
			return
		}
		e.route(w, r, r.Method+" "+parts[2])
	}
}

// readBody reads a JSON object body; an empty body is an empty object.
func readBody(r *http.Request) (*jsonv.Members, error) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return jsonv.NewMembers(), nil
	}
	v, err := toggleslash.ParseJSON(raw)
	if err != nil {
		return nil, err
	}
	o, ok := v.AsObject()
	if !ok {
		return nil, fmt.Errorf("the body is not a JSON object")
	}
	return o, nil
}

func str(o *jsonv.Members, name string) string {
	v, _ := o.Get(name)
	if s, ok := v.AsString(); ok {
		return s
	}
	return "undefined"
}

func ms(o *jsonv.Members, name string) (time.Duration, bool) {
	v, _ := o.Get(name)
	n, ok := v.AsNumber()
	if !ok {
		return 0, false
	}
	return time.Duration(n * float64(time.Millisecond)), true
}

func flag(o *jsonv.Members, name string) bool {
	v, _ := o.Get(name)
	b, _ := v.AsBool()
	return b
}

func (reg *registry) create(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	e := &entry{}
	onErrorThrows := flag(body, "onErrorThrows")
	opts := []toggleslash.Option{toggleslash.WithOnError(func(err error) {
		e.mu.Lock()
		e.errors = append(e.errors, err.Error())
		e.mu.Unlock()
		if onErrorThrows {
			panic("onError threw, as the test asked")
		}
	})}
	if v, ok := body.Get("stream"); ok {
		if b, isBool := v.AsBool(); isBool {
			opts = append(opts, toggleslash.WithStream(b))
		}
	}
	if d, ok := ms(body, "pollIntervalMs"); ok {
		opts = append(opts, toggleslash.WithPollInterval(d))
	}
	if d, ok := ms(body, "flushIntervalMs"); ok {
		opts = append(opts, toggleslash.WithFlushInterval(d))
	}
	if d, ok := ms(body, "requestTimeoutMs"); ok {
		opts = append(opts, toggleslash.WithRequestTimeout(d))
	}
	hookThrows := flag(body, "hookThrows")
	if flag(body, "hook") || hookThrows {
		opts = append(opts, toggleslash.WithAfterEvaluation(func(ev toggleslash.EvaluationEvent) {
			e.mu.Lock()
			e.events = append(e.events, eventValue(ev))
			e.mu.Unlock()
			if hookThrows {
				panic("the hook threw, as the test asked")
			}
		}))
	}
	e.client = toggleslash.NewClient(str(body, "url"), str(body, "sdkKey"), opts...)
	reg.mu.Lock()
	reg.created++
	id := strconv.Itoa(reg.created)
	reg.clients[id] = e
	reg.mu.Unlock()
	writeJSON(w, http.StatusCreated, toggleslash.Object(toggleslash.Member{Key: "id", Value: toggleslash.String(id)}))
}

func indexValue(i *int) toggleslash.Value {
	if i == nil {
		return toggleslash.Null()
	}
	return toggleslash.Number(float64(*i))
}

func reasonValue(r toggleslash.Reason) toggleslash.Value {
	members := []toggleslash.Member{{Key: "kind", Value: toggleslash.String(string(r.Kind))}}
	switch r.Kind {
	case toggleslash.ReasonRuleMatch:
		members = append(members, toggleslash.Member{Key: "ruleIndex", Value: toggleslash.Number(float64(r.RuleIndex))})
	case toggleslash.ReasonPrerequisiteFailed:
		members = append(members, toggleslash.Member{Key: "flagKey", Value: toggleslash.String(r.FlagKey)})
	case toggleslash.ReasonError:
		members = append(members, toggleslash.Member{Key: "code", Value: toggleslash.String(string(r.Code))})
	}
	return toggleslash.Object(members...)
}

func eventValue(ev toggleslash.EvaluationEvent) toggleslash.Value {
	etag := toggleslash.Null()
	if ev.ETag != "" {
		etag = toggleslash.String(ev.ETag)
	}
	version := toggleslash.Null()
	if ev.FlagVersion != nil {
		version = toggleslash.Number(*ev.FlagVersion)
	}
	return toggleslash.Object(
		toggleslash.Member{Key: "flagKey", Value: toggleslash.String(ev.FlagKey)},
		toggleslash.Member{Key: "context", Value: ev.Context},
		toggleslash.Member{Key: "value", Value: ev.Value},
		toggleslash.Member{Key: "variationIndex", Value: indexValue(ev.VariationIndex)},
		toggleslash.Member{Key: "reason", Value: reasonValue(ev.Reason)},
		toggleslash.Member{Key: "etag", Value: etag},
		toggleslash.Member{Key: "flagVersion", Value: version},
	)
}

func (e *entry) route(w http.ResponseWriter, r *http.Request, key string) {
	switch key {
	case "POST ready":
		ready := e.client.Ready()
		writeJSON(w, 200, toggleslash.Object(toggleslash.Member{Key: "ready", Value: toggleslash.Bool(ready)}))
	case "POST evaluate":
		e.evaluate(w, r)
	case "GET status":
		writeJSON(w, 200, statusValue(e.client.Status()))
	case "GET hook-events":
		e.mu.Lock()
		events := append([]toggleslash.Value(nil), e.events...)
		e.mu.Unlock()
		writeJSON(w, 200, toggleslash.Object(toggleslash.Member{Key: "events", Value: toggleslash.Array(events...)}))
	case "GET errors":
		e.mu.Lock()
		messages := make([]toggleslash.Value, len(e.errors))
		for i, m := range e.errors {
			messages[i] = toggleslash.String(m)
		}
		e.mu.Unlock()
		writeJSON(w, 200, toggleslash.Object(
			toggleslash.Member{Key: "count", Value: toggleslash.Number(float64(len(messages)))},
			toggleslash.Member{Key: "messages", Value: toggleslash.Array(messages...)},
		))
	case "POST close":
		e.client.Close()
		writeJSON(w, 200, toggleslash.Object())
	default:
		fail(w, http.StatusNotFound, "no such route")
	}
}

func statusValue(s toggleslash.Status) toggleslash.Value {
	etag, syncedAt, lastError := toggleslash.Null(), toggleslash.Null(), toggleslash.Null()
	if s.ETag != "" {
		etag = toggleslash.String(s.ETag)
	}
	if !s.LastSyncAt.IsZero() {
		syncedAt = toggleslash.Number(float64(s.LastSyncAt.UnixMilli()))
	}
	if s.LastError != "" {
		lastError = toggleslash.String(string(s.LastError))
	}
	return toggleslash.Object(
		toggleslash.Member{Key: "etag", Value: etag},
		toggleslash.Member{Key: "lastSyncAt", Value: syncedAt},
		toggleslash.Member{Key: "stream", Value: toggleslash.String(string(s.Stream))},
		toggleslash.Member{Key: "lastError", Value: lastError},
	)
}

// answer is the three fields every accessor returns, built from a typed detail.
func answer[T any](d toggleslash.Detail[T], wrap func(T) toggleslash.Value) toggleslash.Value {
	return toggleslash.Object(
		toggleslash.Member{Key: "value", Value: wrap(d.Value)},
		toggleslash.Member{Key: "variationIndex", Value: indexValue(d.VariationIndex)},
		toggleslash.Member{Key: "reason", Value: reasonValue(d.Reason)},
	)
}

func (e *entry) evaluate(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	flagKey := str(body, "flagKey")
	context, _ := body.Get("context")
	fallback, _ := body.Get("fallback")
	accessor := str(body, "accessor")
	c := e.client

	var out toggleslash.Value
	switch accessor {
	case "bool":
		b, ok := fallback.AsBool()
		if !ok {
			fail(w, http.StatusBadRequest, "the fallback is not a boolean")
			return
		}
		out = answer(c.BoolVariationDetail(flagKey, context, b), toggleslash.Bool)
	case "string":
		s, ok := fallback.AsString()
		if !ok {
			fail(w, http.StatusBadRequest, "the fallback is not a string")
			return
		}
		out = answer(c.StringVariationDetail(flagKey, context, s), toggleslash.String)
	case "number":
		n, ok := fallback.AsNumber()
		if !ok {
			fail(w, http.StatusBadRequest, "the fallback is not a number")
			return
		}
		out = answer(c.FloatVariationDetail(flagKey, context, n), toggleslash.Number)
	case "int":
		n, ok := fallback.AsNumber()
		if !ok || n != float64(int64(n)) {
			fail(w, http.StatusBadRequest, "the fallback is not an integer")
			return
		}
		out = answer(c.IntVariationDetail(flagKey, context, int64(n)), func(i int64) toggleslash.Value {
			return toggleslash.Number(float64(i))
		})
	case "json", "any":
		out = answer(c.JSONVariationDetail(flagKey, context, fallback), func(v toggleslash.Value) toggleslash.Value { return v })
	default:
		fail(w, http.StatusBadRequest, "no such accessor: "+accessor)
		return
	}
	writeJSON(w, 200, out)
}
