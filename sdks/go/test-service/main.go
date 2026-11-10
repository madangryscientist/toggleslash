// Command test-service wraps the Go SDK in the HTTP protocol of docs/sdk-test-service.md, so the conformance
// harness (packages/sdk-conformance) can drive it from outside. It catches nothing the SDK does wrong: a
// handler that fails answers 500 with the error text, and the harness fails the scenario.
package main

import (
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
)

// entry is one client and what the service recorded about it. The client stays here after close.
type entry struct {
	client *toggleslash.Client

	mu     sync.Mutex
	events []toggleslash.EvaluationEvent
	errors []string
}

var (
	mu      sync.Mutex
	clients = map[string]*entry{}
	created int
)

func main() {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	// Stdout is a pipe to the harness; os.Stdout is unbuffered, and Sync is for a platform that buffers anyway.
	_, _ = fmt.Fprintf(os.Stdout, "listening http://127.0.0.1:%d\n", port)
	_ = os.Stdout.Sync()
	server := &http.Server{Handler: http.HandlerFunc(serve), ReadHeaderTimeout: 10 * time.Second}
	if err := server.Serve(listener); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// serve turns a failure or a panic into a 500 with its text, so the harness sees it.
func serve(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if recovered := recover(); recovered != nil {
			http.Error(w, fmt.Sprint("panic: ", recovered), http.StatusInternalServerError)
		}
	}()
	status, body, err := route(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func route(r *http.Request) (int, []byte, error) {
	path := strings.Trim(r.URL.Path, "/")
	switch {
	case r.Method == http.MethodGet && path == "":
		return 200, []byte(`{"name":"toggleslash-sdk (Go)","capabilities":["integer-accessor"]}`), nil
	case r.Method == http.MethodPost && path == "clients":
		return create(r)
	}
	parts := strings.Split(path, "/")
	if len(parts) != 3 || parts[0] != "clients" {
		return 404, []byte(`"no such route"`), nil
	}
	mu.Lock()
	e := clients[parts[1]]
	mu.Unlock()
	if e == nil {
		return 404, []byte(`"no such client"`), nil
	}
	switch r.Method + " " + parts[2] {
	case "POST ready":
		return 200, []byte(`{"ready":` + strconv.FormatBool(e.client.Ready()) + `}`), nil
	case "POST evaluate":
		return evaluate(e, r)
	case "GET status":
		return 200, statusJSON(e.client.Status()), nil
	case "GET hook-events":
		return 200, hookEventsJSON(e), nil
	case "GET errors":
		return 200, errorsJSON(e), nil
	case "POST close":
		e.client.Close()
		return 200, []byte(`{}`), nil
	}
	return 404, []byte(`"no such route"`), nil
}

func readBody(r *http.Request) (toggleslash.Value, error) {
	text, err := io.ReadAll(r.Body)
	if err != nil {
		return toggleslash.Null(), err
	}
	if len(strings.TrimSpace(string(text))) == 0 {
		return toggleslash.Object(), nil
	}
	body, err := toggleslash.ParseJSON(text)
	if err != nil {
		return toggleslash.Null(), err
	}
	if !body.IsObject() {
		return toggleslash.Null(), errors.New("the body is not a JSON object")
	}
	return body, nil
}

func milliseconds(body toggleslash.Value, name string) (time.Duration, bool) {
	n, ok := body.Field(name).Number()
	return time.Duration(n * float64(time.Millisecond)), ok
}

func create(r *http.Request) (int, []byte, error) {
	body, err := readBody(r)
	if err != nil {
		return 0, nil, err
	}
	e := &entry{}
	url, _ := body.Field("url").Text()
	key, _ := body.Field("sdkKey").Text()
	opts := []toggleslash.Option{toggleslash.WithOnError(func(err error) {
		e.mu.Lock()
		e.errors = append(e.errors, err.Error())
		e.mu.Unlock()
		if on, _ := body.Field("onErrorThrows").Bool(); on {
			panic("onError threw, as the test asked")
		}
	})}
	if on, ok := body.Field("stream").Bool(); ok {
		opts = append(opts, toggleslash.WithStream(on))
	}
	if d, ok := milliseconds(body, "pollIntervalMs"); ok {
		opts = append(opts, toggleslash.WithPollInterval(d))
	}
	if d, ok := milliseconds(body, "flushIntervalMs"); ok {
		opts = append(opts, toggleslash.WithFlushInterval(d))
	}
	if d, ok := milliseconds(body, "requestTimeoutMs"); ok {
		opts = append(opts, toggleslash.WithRequestTimeout(d))
	}
	hook, _ := body.Field("hook").Bool()
	throws, _ := body.Field("hookThrows").Bool()
	if hook || throws {
		opts = append(opts, toggleslash.WithAfterEvaluation(func(event toggleslash.EvaluationEvent) {
			e.mu.Lock()
			e.events = append(e.events, event)
			e.mu.Unlock()
			if throws {
				panic("the hook panicked, as the test asked")
			}
		}))
	}
	e.client = toggleslash.New(url, key, opts...)
	mu.Lock()
	created++
	id := strconv.Itoa(created)
	clients[id] = e
	mu.Unlock()
	return 201, []byte(`{"id":` + strconv.Quote(id) + `}`), nil
}

// evaluate maps each accessor of the protocol onto the SDK's typed methods; see README.md.
func evaluate(e *entry, r *http.Request) (int, []byte, error) {
	body, err := readBody(r)
	if err != nil {
		return 0, nil, err
	}
	flagKey, _ := body.Field("flagKey").Text()
	ctx := toggleslash.ContextFromValue(body.Field("context"))
	fallback := body.Field("fallback")
	accessor, _ := body.Field("accessor").Text()

	var value toggleslash.Value
	var index int
	var reason toggleslash.Reason
	switch accessor {
	case "bool":
		b, _ := fallback.Bool()
		d := e.client.BoolVariationDetail(flagKey, ctx, b)
		value, index, reason = toggleslash.Bool(d.Value), d.VariationIndex, d.Reason
	case "string":
		s, _ := fallback.Text()
		d := e.client.StringVariationDetail(flagKey, ctx, s)
		value, index, reason = toggleslash.String(d.Value), d.VariationIndex, d.Reason
	case "number":
		n, _ := fallback.Number()
		d := e.client.Float64VariationDetail(flagKey, ctx, n)
		value, index, reason = toggleslash.Number(d.Value), d.VariationIndex, d.Reason
	case "int":
		n, _ := fallback.Number()
		d := e.client.IntVariationDetail(flagKey, ctx, int64(n))
		value, index, reason = toggleslash.Number(float64(d.Value)), d.VariationIndex, d.Reason
	case "json", "any":
		d := e.client.JSONVariationDetail(flagKey, ctx, fallback)
		value, index, reason = d.Value, d.VariationIndex, d.Reason
	default:
		return 0, nil, fmt.Errorf("unknown accessor %q", accessor)
	}
	out := []byte(`{"value":`)
	out = value.AppendJSON(out)
	out = append(out, `,"variationIndex":`...)
	out = appendIndex(out, index)
	out = append(out, `,"reason":`...)
	out = appendReason(out, reason)
	return 200, append(out, '}'), nil
}

func appendIndex(out []byte, index int) []byte {
	if index == toggleslash.NoVariation {
		return append(out, "null"...)
	}
	return strconv.AppendInt(out, int64(index), 10)
}

func appendString(out []byte, s string) []byte { return toggleslash.String(s).AppendJSON(out) }

func appendReason(out []byte, r toggleslash.Reason) []byte {
	out = append(out, `{"kind":`...)
	out = appendString(out, string(r.Kind))
	switch r.Kind {
	case toggleslash.ReasonRuleMatch:
		out = append(out, `,"ruleIndex":`...)
		out = strconv.AppendInt(out, int64(r.RuleIndex), 10)
	case toggleslash.ReasonPrerequisiteFailed:
		out = append(out, `,"flagKey":`...)
		out = appendString(out, r.FlagKey)
	case toggleslash.ReasonError:
		out = append(out, `,"code":`...)
		out = appendString(out, string(r.Code))
	}
	return append(out, '}')
}

func statusJSON(s toggleslash.Status) []byte {
	out := []byte(`{"etag":`)
	if s.ETag == nil {
		out = append(out, "null"...)
	} else {
		out = appendString(out, *s.ETag)
	}
	out = append(out, `,"lastSyncAt":`...)
	if s.LastSyncAt.IsZero() {
		out = append(out, "null"...)
	} else {
		out = strconv.AppendInt(out, s.LastSyncAt.UnixMilli(), 10)
	}
	out = append(out, `,"stream":`...)
	out = appendString(out, string(s.Stream))
	out = append(out, `,"lastError":`...)
	if s.LastError == "" {
		out = append(out, "null"...)
	} else {
		out = appendString(out, string(s.LastError))
	}
	return append(out, '}')
}

func hookEventsJSON(e *entry) []byte {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []byte(`{"events":[`)
	for i, ev := range e.events {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, `{"flagKey":`...)
		out = appendString(out, ev.FlagKey)
		out = append(out, `,"context":`...)
		out = ev.Context.Value().AppendJSON(out)
		out = append(out, `,"value":`...)
		out = ev.Value.AppendJSON(out)
		out = append(out, `,"variationIndex":`...)
		out = appendIndex(out, ev.VariationIndex)
		out = append(out, `,"reason":`...)
		out = appendReason(out, ev.Reason)
		out = append(out, `,"etag":`...)
		if ev.ETag == nil {
			out = append(out, "null"...)
		} else {
			out = appendString(out, *ev.ETag)
		}
		out = append(out, `,"flagVersion":`...)
		if ev.FlagVersion == nil {
			out = append(out, "null"...)
		} else {
			out = toggleslash.Number(*ev.FlagVersion).AppendJSON(out)
		}
		out = append(out, '}')
	}
	return append(out, `]}`...)
}

func errorsJSON(e *entry) []byte {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []byte(`{"count":` + strconv.Itoa(len(e.errors)) + `,"messages":[`)
	for i, m := range e.errors {
		if i > 0 {
			out = append(out, ',')
		}
		out = appendString(out, m)
	}
	return append(out, `]}`...)
}
