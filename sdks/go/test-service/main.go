// Command test-service is the conformance harness's test service for the Go
// SDK: a small HTTP server that wraps a toggleslash.Client so the harness in
// packages/sdk-conformance can drive the SDK from outside.
//
// The protocol is docs/sdk-test-service.md and the accessor mapping is in this
// directory's README. It absorbs nothing the SDK raises: a call that panics is
// answered 500 with the panic's text, and the harness fails the scenario.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go"
	"github.com/madangryscientist/toggleslash/sdks/go/jsonval"
)

// maxBodyDepth is how deep a request body may nest. The harness sends flat
// options and contexts, and the SDK's own reader bounds a context by its own
// rule; this only stops a hostile body from recursing without limit.
const maxBodyDepth = 200

// entry is one client, with what the service records about it. It is kept after
// close, because the harness reads a closed client's status and errors.
type entry struct {
	sdk *toggleslash.Client

	mu         sync.Mutex
	hookEvents []jsonval.Object
	errors     []string

	// closeInHook / closeInOnError: stored is the client as a callback sees
	// it (nil until New returns), closing is set when the callback's Close
	// starts, and closeCalls, closeThrew and closeMs once it has returned.
	stored     *toggleslash.Client
	closing    bool
	closeCalls int
	closeThrew int
	closeMs    int64
}

// closeFromCallback is the first callback that finds the client stored: it
// calls Close, times how long the call took to return, and recovers a panic.
func (e *entry) closeFromCallback() {
	e.mu.Lock()
	client := e.stored
	if client == nil || e.closing {
		e.mu.Unlock()
		return
	}
	e.closing = true
	e.mu.Unlock()
	started := time.Now()
	threw := 0
	func() {
		defer func() {
			if recover() != nil {
				threw = 1
			}
		}()
		client.Close()
	}()
	elapsed := time.Since(started).Milliseconds()
	e.mu.Lock()
	e.closeCalls, e.closeThrew, e.closeMs = 1, threw, elapsed
	e.mu.Unlock()
}

func (e *entry) recordEvent(event jsonval.Object) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.hookEvents = append(e.hookEvents, event)
}

func (e *entry) recordError(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.errors = append(e.errors, err.Error())
}

func (e *entry) events() []jsonval.Object {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]jsonval.Object, len(e.hookEvents))
	copy(out, e.hookEvents)
	return out
}

func (e *entry) errorCount() (int, []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.errors))
	copy(out, e.errors)
	return len(e.errors), out
}

var (
	registryMu sync.Mutex
	registry   = map[string]*entry{}
	created    int
)

func addEntry(e *entry) string {
	registryMu.Lock()
	defer registryMu.Unlock()
	created++
	id := strconv.Itoa(created)
	registry[id] = e
	return id
}

func lookup(id string) (*entry, bool) {
	registryMu.Lock()
	defer registryMu.Unlock()
	e, ok := registry[id]
	return e, ok
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", root)
	mux.HandleFunc("POST /clients", createClient)
	mux.HandleFunc("POST /clients/{id}/ready", ready)
	mux.HandleFunc("POST /clients/{id}/evaluate", evaluate)
	mux.HandleFunc("GET /clients/{id}/status", status)
	mux.HandleFunc("GET /clients/{id}/hook-events", hookEvents)
	mux.HandleFunc("GET /clients/{id}/errors", errorList)
	mux.HandleFunc("POST /clients/{id}/close", closeClient)
	mux.HandleFunc("GET /clients/{id}/callback-close", callbackClose)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "test-service: listen: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("listening http://%s\n", listener.Addr())
	// The harness reads a pipe, not a terminal; make sure the line is out
	// before the first request is served. os.Stdout is unbuffered already, and
	// the Sync covers a pipe that would rather be flushed.
	_ = os.Stdout.Sync()

	server := &http.Server{Handler: recover500(mux), ReadHeaderTimeout: 0}
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(os.Stderr, "test-service: serve: %v\n", err)
		os.Exit(1)
	}
}

// recover500 catches a panic at the handler, which net/http would otherwise
// answer by dropping the connection, and sends the 500 the protocol wants.
func recover500(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if raised := recover(); raised != nil {
				http.Error(w, fmt.Sprintf("%v", raised), http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func root(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, jsonval.Object{
		"name":         jsonval.String("toggleslash-sdk (Go)"),
		"capabilities": jsonval.Array{jsonval.String("integer-accessor")},
	})
}

func createClient(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	e, err := newEntry(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, jsonval.Object{"id": jsonval.String(addEntry(e))})
}

// newEntry builds the SDK client the options ask for. Every option is passed to
// the SDK when present, and the three switches are the service's own.
func newEntry(body jsonval.Object) (*entry, error) {
	url, ok := body["url"].(jsonval.String)
	if !ok {
		return nil, errors.New("url is required")
	}
	key, ok := body["sdkKey"].(jsonval.String)
	if !ok {
		return nil, errors.New("sdkKey is required")
	}
	e := &entry{}
	opts := toggleslash.Options{
		URL:    string(url),
		SDKKey: string(key),
		OnError: func(err error) {
			if err == nil {
				return
			}
			e.recordError(err)
			if on(body["closeInOnError"]) {
				e.closeFromCallback()
			}
			if on(body["onErrorThrows"]) {
				panic("onError threw, as the test asked")
			}
		},
	}
	if stream, ok := body["stream"].(jsonval.Bool); ok {
		follow := bool(stream)
		opts.Stream = &follow
	}
	if interval, ok := milliseconds(body["pollIntervalMs"]); ok {
		opts.PollInterval = interval
	}
	if interval, ok := milliseconds(body["flushIntervalMs"]); ok {
		opts.FlushInterval = interval
	}
	if interval, ok := milliseconds(body["requestTimeoutMs"]); ok {
		opts.RequestTimeout = interval
	}
	if on(body["hook"]) || on(body["hookThrows"]) || on(body["closeInHook"]) {
		throws := on(body["hookThrows"])
		closes := on(body["closeInHook"])
		opts.Hooks = toggleslash.Hooks{AfterEvaluation: func(event toggleslash.Event) {
			e.recordEvent(eventJSON(event))
			if closes {
				e.closeFromCallback()
			}
			if throws {
				panic("the hook threw, as the test asked")
			}
		}}
	}
	sdk, err := toggleslash.New(opts)
	if err != nil {
		return nil, err
	}
	e.sdk = sdk
	e.mu.Lock()
	e.stored = sdk
	e.mu.Unlock()
	return e, nil
}

func ready(w http.ResponseWriter, r *http.Request) {
	e, ok := entryFor(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, jsonval.Object{"ready": jsonval.Bool(e.sdk.Ready(context.Background()))})
}

func evaluate(w http.ResponseWriter, r *http.Request) {
	e, ok := entryFor(w, r)
	if !ok {
		return
	}
	body, err := readBody(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	flagKey, ok := body["flagKey"].(jsonval.String)
	if !ok {
		http.Error(w, "flagKey is required", http.StatusBadRequest)
		return
	}
	contextValue := body["context"]
	if contextValue == nil {
		contextValue = jsonval.Null{}
	}
	accessor := "any"
	if name, ok := body["accessor"].(jsonval.String); ok {
		accessor = string(name)
	}
	value, index, reason, err := e.vary(string(flagKey), contextValue, body["fallback"], accessor)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, jsonval.Object{
		"value":          answerValue(value),
		"variationIndex": indexJSON(index),
		"reason":         reasonJSON(reason),
	})
}

// vary maps one protocol accessor onto the SDK's matching method. The fallback
// arrives as JSON, so a typed accessor takes the fallback of its own kind and
// answers 400 for any other; the harness never sends one (docs/sdk-test-service.md,
// "Accessors").
func (e *entry) vary(flagKey string, contextValue, fallback jsonval.Value, accessor string) (jsonval.Value, *int, toggleslash.Reason, error) {
	switch accessor {
	case "bool":
		typed, ok := fallback.(jsonval.Bool)
		if !ok {
			return nil, nil, toggleslash.Reason{}, fmt.Errorf("the bool accessor needs a boolean fallback")
		}
		detail := e.sdk.BoolVariationDetail(flagKey, contextValue, bool(typed))
		return jsonval.Bool(detail.Value), detail.VariationIndex, detail.Reason, nil
	case "string":
		typed, ok := fallback.(jsonval.String)
		if !ok {
			return nil, nil, toggleslash.Reason{}, fmt.Errorf("the string accessor needs a string fallback")
		}
		detail := e.sdk.StringVariationDetail(flagKey, contextValue, string(typed))
		return jsonval.String(detail.Value), detail.VariationIndex, detail.Reason, nil
	case "number":
		typed, ok := fallback.(jsonval.Number)
		if !ok {
			return nil, nil, toggleslash.Reason{}, fmt.Errorf("the number accessor needs a number fallback")
		}
		detail := e.sdk.FloatVariationDetail(flagKey, contextValue, float64(typed))
		return jsonval.Number(detail.Value), detail.VariationIndex, detail.Reason, nil
	case "int":
		typed, ok := fallback.(jsonval.Number)
		if !ok {
			return nil, nil, toggleslash.Reason{}, fmt.Errorf("the int accessor needs a number fallback")
		}
		detail := e.sdk.IntVariationDetail(flagKey, contextValue, int64(float64(typed)))
		// The answer goes back through a double: no scenario or vector serves
		// the int accessor an integer a double cannot hold exactly.
		return jsonval.Number(float64(detail.Value)), detail.VariationIndex, detail.Reason, nil
	case "json", "any":
		typed := fallback
		if typed == nil {
			typed = jsonval.Null{}
		}
		detail := e.sdk.JSONVariationDetail(flagKey, contextValue, typed)
		return detail.Value, detail.VariationIndex, detail.Reason, nil
	}
	return nil, nil, toggleslash.Reason{}, fmt.Errorf("no such accessor: %s", accessor)
}

func status(w http.ResponseWriter, r *http.Request) {
	e, ok := entryFor(w, r)
	if !ok {
		return
	}
	snapshot := e.sdk.Status()
	etag := jsonval.Value(jsonval.Null{})
	if snapshot.ETag != "" {
		etag = jsonval.String(snapshot.ETag)
	}
	lastSyncAt := jsonval.Value(jsonval.Null{})
	if snapshot.LastSyncAt != nil {
		lastSyncAt = jsonval.Number(float64(snapshot.LastSyncAt.UnixMilli()))
	}
	lastError := jsonval.Value(jsonval.Null{})
	if snapshot.LastError != nil {
		lastError = jsonval.String(string(*snapshot.LastError))
	}
	writeJSON(w, http.StatusOK, jsonval.Object{
		"etag":       etag,
		"lastSyncAt": lastSyncAt,
		"stream":     jsonval.String(string(snapshot.Stream)),
		"lastError":  lastError,
	})
}

func callbackClose(w http.ResponseWriter, r *http.Request) {
	e, ok := entryFor(w, r)
	if !ok {
		return
	}
	e.mu.Lock()
	answer := jsonval.Object{
		"calls": jsonval.Number(float64(e.closeCalls)),
		"threw": jsonval.Number(float64(e.closeThrew)),
		"maxMs": jsonval.Number(float64(e.closeMs)),
	}
	e.mu.Unlock()
	writeJSON(w, http.StatusOK, answer)
}

func hookEvents(w http.ResponseWriter, r *http.Request) {
	e, ok := entryFor(w, r)
	if !ok {
		return
	}
	events := e.events()
	out := make(jsonval.Array, 0, len(events))
	for _, event := range events {
		out = append(out, event)
	}
	writeJSON(w, http.StatusOK, jsonval.Object{"events": out})
}

func errorList(w http.ResponseWriter, r *http.Request) {
	e, ok := entryFor(w, r)
	if !ok {
		return
	}
	count, messages := e.errorCount()
	out := make(jsonval.Array, 0, len(messages))
	for _, message := range messages {
		out = append(out, jsonval.String(message))
	}
	writeJSON(w, http.StatusOK, jsonval.Object{
		"count":    jsonval.Number(float64(count)),
		"messages": out,
	})
}

func closeClient(w http.ResponseWriter, r *http.Request) {
	e, ok := entryFor(w, r)
	if !ok {
		return
	}
	e.sdk.Close()
	writeJSON(w, http.StatusOK, jsonval.Object{})
}

func entryFor(w http.ResponseWriter, r *http.Request) (*entry, bool) {
	e, ok := lookup(r.PathValue("id"))
	if !ok {
		http.Error(w, "no such client", http.StatusNotFound)
	}
	return e, ok
}

func readBody(r *http.Request) (jsonval.Object, error) {
	value, err := jsonval.Read(r.Body, maxBodyDepth)
	if err != nil {
		return nil, fmt.Errorf("the body is not JSON: %w", err)
	}
	body, ok := value.(jsonval.Object)
	if !ok {
		return nil, errors.New("the body is not a JSON object")
	}
	return body, nil
}

func writeJSON(w http.ResponseWriter, status int, value jsonval.Value) {
	var body bytes.Buffer
	if err := jsonval.Write(&body, value); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	body.WriteByte('\n')
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body.Bytes())
}

func answerValue(value jsonval.Value) jsonval.Value {
	if value == nil {
		return jsonval.Null{}
	}
	if number, ok := value.(jsonval.Number); ok {
		f := float64(number)
		if math.IsInf(f, 0) || math.IsNaN(f) {
			// A non-finite answer is JSON null, as JSON.stringify writes it.
			return jsonval.Null{}
		}
	}
	return value
}

func indexJSON(index *int) jsonval.Value {
	if index == nil {
		return jsonval.Null{}
	}
	return jsonval.Number(float64(*index))
}

func versionJSON(version *int64) jsonval.Value {
	if version == nil {
		return jsonval.Null{}
	}
	return jsonval.Number(float64(*version))
}

func reasonJSON(reason toggleslash.Reason) jsonval.Object {
	out := jsonval.Object{"kind": jsonval.String(string(reason.Kind))}
	if reason.RuleIndex != nil {
		out["ruleIndex"] = jsonval.Number(float64(*reason.RuleIndex))
	}
	if reason.FlagKey != "" {
		out["flagKey"] = jsonval.String(reason.FlagKey)
	}
	if reason.Code != "" {
		out["code"] = jsonval.String(string(reason.Code))
	}
	return out
}

func eventJSON(event toggleslash.Event) jsonval.Object {
	contextValue := event.Context
	if contextValue == nil {
		contextValue = jsonval.Null{}
	}
	etag := jsonval.Value(jsonval.Null{})
	if event.ETag != "" {
		etag = jsonval.String(event.ETag)
	}
	return jsonval.Object{
		"flagKey":        jsonval.String(event.FlagKey),
		"context":        contextValue,
		"value":          answerValue(event.Value),
		"variationIndex": indexJSON(event.VariationIndex),
		"reason":         reasonJSON(event.Reason),
		"etag":           etag,
		"flagVersion":    versionJSON(event.FlagVersion),
	}
}

func on(value jsonval.Value) bool {
	flag, ok := value.(jsonval.Bool)
	return ok && bool(flag)
}

func milliseconds(value jsonval.Value) (time.Duration, bool) {
	number, ok := value.(jsonval.Number)
	if !ok {
		return 0, false
	}
	return time.Duration(float64(number)) * time.Millisecond, true
}
