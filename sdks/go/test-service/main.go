// Command test-service is the Go SDK's conformance test service: an HTTP server that wraps toggleslash.New so
// the harness in packages/sdk-conformance can drive it from outside. The protocol is docs/sdk-test-service.md;
// the accessor mapping is in this directory's README. It catches nothing the SDK raises: a call that panics
// answers 500 with the panic text, and the harness fails the scenario.
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
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	toggleslash "github.com/madangryscientist/toggleslash/sdks/go"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// entry is one client and what the service recorded about it.
type entry struct {
	client *toggleslash.Client

	mu     sync.Mutex
	events []toggleslash.Event
	errors []string
}

var (
	mu      sync.Mutex
	clients = map[string]*entry{}
	created int
)

var clientPath = regexp.MustCompile(`^/clients/([^/]+)/([a-z-]+)$`)

// badRequest is a request the service cannot map onto the SDK: it answers 400.
type badRequest string

func (b badRequest) Error() string { return string(b) }

func main() {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// No timeouts of any kind: ready and close wait as long as the SDK takes.
	server := &http.Server{Handler: http.HandlerFunc(handle)}
	fmt.Printf("listening http://%s\n", listener.Addr())
	if err := server.Serve(listener); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func handle(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if p := recover(); p != nil {
			reply(w, http.StatusInternalServerError, "text/plain", []byte(fmt.Sprintf("%v\n%s", p, debug.Stack())))
		}
	}()
	body, err := route(r)
	var bad badRequest
	var missing notFound
	switch {
	case err == nil:
		status := http.StatusOK
		if r.Method == http.MethodPost && r.URL.Path == "/clients" {
			status = http.StatusCreated
		}
		reply(w, status, "application/json", body)
	case errors.As(err, &bad):
		reply(w, http.StatusBadRequest, "text/plain", []byte(bad))
	case errors.As(err, &missing):
		reply(w, http.StatusNotFound, "text/plain", []byte(missing))
	default:
		reply(w, http.StatusInternalServerError, "text/plain", []byte(err.Error()))
	}
}

func reply(w http.ResponseWriter, status int, contentType string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

type notFound string

func (n notFound) Error() string { return string(n) }

func route(r *http.Request) ([]byte, error) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/":
		return []byte(`{"name":"toggleslash-sdk (Go)","capabilities":["integer-accessor"]}`), nil
	case r.Method == http.MethodPost && r.URL.Path == "/clients":
		return create(r)
	}
	m := clientPath.FindStringSubmatch(r.URL.Path)
	if m == nil {
		return nil, notFound("no such route")
	}
	mu.Lock()
	e := clients[m[1]]
	mu.Unlock()
	if e == nil {
		return nil, notFound("no such client")
	}
	return e.route(m[2], r)
}

func readBody(r *http.Request) (toggleslash.Value, error) {
	text, err := io.ReadAll(r.Body)
	if err != nil {
		return toggleslash.Value{}, err
	}
	if len(text) == 0 {
		text = []byte("{}")
	}
	v, err := toggleslash.ParseJSON(text)
	if err != nil {
		return toggleslash.Value{}, badRequest(err.Error())
	}
	if _, ok := v.AsObject(); !ok {
		return toggleslash.Value{}, badRequest("the body is not a JSON object")
	}
	return v, nil
}

func durationOption(body toggleslash.Value, name string, option func(time.Duration) toggleslash.Option, into *[]toggleslash.Option) {
	if n, ok := body.Get(name).AsNumber(); ok {
		*into = append(*into, option(time.Duration(n*float64(time.Millisecond))))
	}
}

func create(r *http.Request) ([]byte, error) {
	body, err := readBody(r)
	if err != nil {
		return nil, err
	}
	e := &entry{}
	url, _ := body.Get("url").AsString()
	key, _ := body.Get("sdkKey").AsString()
	onErrorThrows, _ := body.Get("onErrorThrows").AsBool()
	hook, _ := body.Get("hook").AsBool()
	hookThrows, _ := body.Get("hookThrows").AsBool()

	opts := []toggleslash.Option{toggleslash.WithOnError(func(err error) {
		e.mu.Lock()
		e.errors = append(e.errors, err.Error())
		e.mu.Unlock()
		if onErrorThrows {
			panic("onError threw, as the test asked")
		}
	})}
	if stream, ok := body.Get("stream").AsBool(); ok {
		opts = append(opts, toggleslash.WithStream(stream))
	}
	durationOption(body, "pollIntervalMs", toggleslash.WithPollInterval, &opts)
	durationOption(body, "flushIntervalMs", toggleslash.WithFlushInterval, &opts)
	durationOption(body, "requestTimeoutMs", toggleslash.WithRequestTimeout, &opts)
	if hook || hookThrows {
		opts = append(opts, toggleslash.WithAfterEvaluation(func(ev toggleslash.Event) {
			e.mu.Lock()
			e.events = append(e.events, ev)
			e.mu.Unlock()
			if hookThrows {
				panic("the hook threw, as the test asked")
			}
		}))
	}
	e.client = toggleslash.New(url, key, opts...)

	mu.Lock()
	created++
	id := strconv.Itoa(created)
	clients[id] = e
	mu.Unlock()
	return []byte(`{"id":` + strconv.Quote(id) + `}`), nil
}

func (e *entry) route(action string, r *http.Request) ([]byte, error) {
	switch r.Method + " " + action {
	case "POST ready":
		return []byte(`{"ready":` + strconv.FormatBool(e.client.Ready()) + `}`), nil
	case "POST evaluate":
		return e.evaluate(r)
	case "GET status":
		return statusJSON(e.client.Status()), nil
	case "GET hook-events":
		return e.hookEvents(), nil
	case "GET errors":
		return e.errorList(), nil
	case "POST close":
		e.client.Close()
		return []byte(`{}`), nil
	}
	return nil, notFound("no such route")
}

func jsonString(s string) []byte { return jsonv.AppendString(nil, s) }

func nullableString(s string) []byte {
	if s == "" {
		return []byte("null")
	}
	return jsonString(s)
}

func statusJSON(s toggleslash.Status) []byte {
	out := []byte(`{"etag":`)
	out = append(out, nullableString(s.ETag)...)
	out = append(out, `,"lastSyncAt":`...)
	if s.LastSyncAt.IsZero() {
		out = append(out, "null"...)
	} else {
		out = strconv.AppendInt(out, s.LastSyncAt.UnixMilli(), 10)
	}
	out = append(out, `,"stream":`...)
	out = append(out, jsonString(string(s.Stream))...)
	out = append(out, `,"lastError":`...)
	out = append(out, nullableString(s.LastError)...)
	return append(out, '}')
}

func reasonJSON(r toggleslash.Reason) []byte {
	out := []byte(`{"kind":`)
	out = append(out, jsonString(string(r.Kind))...)
	switch r.Kind {
	case toggleslash.ReasonRuleMatch:
		out = append(out, `,"ruleIndex":`...)
		out = strconv.AppendInt(out, int64(r.RuleIndex), 10)
	case toggleslash.ReasonPrerequisiteFailed:
		out = append(out, `,"flagKey":`...)
		out = append(out, jsonString(r.FlagKey)...)
	case toggleslash.ReasonError:
		out = append(out, `,"code":`...)
		out = append(out, jsonString(string(r.Code))...)
	}
	return append(out, '}')
}

func indexJSON(i *int) []byte {
	if i == nil {
		return []byte("null")
	}
	return strconv.AppendInt(nil, int64(*i), 10)
}

// valueJSON writes what the SDK returned. A number that is not finite is written as null, as JSON.stringify
// does.
func valueJSON(v any) []byte {
	switch x := v.(type) {
	case bool:
		return strconv.AppendBool(nil, x)
	case string:
		return jsonString(x)
	case int64:
		return strconv.AppendInt(nil, x, 10)
	case float64:
		return jsonv.Marshal(jsonv.Number(x))
	case toggleslash.Value:
		return jsonv.Marshal(x)
	}
	return []byte("null")
}

func answer(value any, index *int, reason toggleslash.Reason) []byte {
	out := []byte(`{"value":`)
	out = append(out, valueJSON(value)...)
	out = append(out, `,"variationIndex":`...)
	out = append(out, indexJSON(index)...)
	out = append(out, `,"reason":`...)
	out = append(out, reasonJSON(reason)...)
	return append(out, '}')
}

func detail[T any](d toggleslash.Detail[T]) []byte {
	return answer(d.Value, d.VariationIndex, d.Reason)
}

// evaluate maps the harness's accessor onto the SDK's typed methods. The context goes through as the parsed
// Value, valid or not: an invalid one is a case the SDK must answer itself (ERROR INVALID_CONTEXT).
func (e *entry) evaluate(r *http.Request) ([]byte, error) {
	body, err := readBody(r)
	if err != nil {
		return nil, err
	}
	flag, _ := body.Get("flagKey").AsString()
	context := body.Get("context")
	fallback := body.Get("fallback")
	accessor, _ := body.Get("accessor").AsString()
	if accessor == "" {
		accessor = "any"
	}
	switch accessor {
	case "bool":
		b, ok := fallback.AsBool()
		if !ok {
			return nil, badRequest("the bool accessor needs a boolean fallback")
		}
		return detail(e.client.BoolVariationDetail(flag, context, b)), nil
	case "string":
		s, ok := fallback.AsString()
		if !ok {
			return nil, badRequest("the string accessor needs a string fallback")
		}
		return detail(e.client.StringVariationDetail(flag, context, s)), nil
	case "number":
		n, ok := fallback.AsNumber()
		if !ok {
			return nil, badRequest("the number accessor needs a number fallback")
		}
		return detail(e.client.FloatVariationDetail(flag, context, n)), nil
	case "int":
		n, ok := fallback.AsNumber()
		if !ok || n != float64(int64(n)) {
			return nil, badRequest("the int accessor needs an integer fallback")
		}
		return detail(e.client.IntVariationDetail(flag, context, int64(n))), nil
	case "json", "any":
		return detail(e.client.JSONVariationDetail(flag, context, fallback)), nil
	}
	return nil, badRequest("no such accessor: " + accessor)
}

func (e *entry) hookEvents() []byte {
	e.mu.Lock()
	events := append([]toggleslash.Event(nil), e.events...)
	e.mu.Unlock()
	out := []byte(`{"events":[`)
	for i, ev := range events {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, `{"flagKey":`...)
		out = append(out, jsonString(ev.FlagKey)...)
		out = append(out, `,"context":`...)
		out = append(out, contextJSON(ev.Context)...)
		out = append(out, `,"value":`...)
		out = append(out, valueJSON(ev.Value)...)
		out = append(out, `,"variationIndex":`...)
		out = append(out, indexJSON(ev.VariationIndex)...)
		out = append(out, `,"reason":`...)
		out = append(out, reasonJSON(ev.Reason)...)
		out = append(out, `,"etag":`...)
		out = append(out, nullableString(ev.ETag)...)
		out = append(out, `,"flagVersion":`...)
		if ev.FlagVersion == nil {
			out = append(out, "null"...)
		} else {
			out = strconv.AppendInt(out, *ev.FlagVersion, 10)
		}
		out = append(out, '}')
	}
	return append(out, "]}"...)
}

// contextJSON writes the context the hook received; the service passed in a Value, so it is one.
func contextJSON(c any) []byte {
	if v, ok := c.(toggleslash.Value); ok {
		return jsonv.Marshal(v)
	}
	return []byte("null")
}

func (e *entry) errorList() []byte {
	e.mu.Lock()
	messages := append([]string(nil), e.errors...)
	e.mu.Unlock()
	out := []byte(`{"count":`)
	out = strconv.AppendInt(out, int64(len(messages)), 10)
	out = append(out, `,"messages":[`...)
	for i, m := range messages {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, jsonString(m)...)
	}
	return append(out, "]}"...)
}
