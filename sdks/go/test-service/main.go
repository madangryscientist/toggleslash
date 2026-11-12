// The Go SDK's conformance test service: an HTTP server that wraps toggleslash.New, so the harness in
// packages/sdk-conformance can drive the SDK. The protocol is docs/sdk-test-service.md; the accessor mapping is
// in this directory's README. It absorbs nothing the SDK returns: a panic answers 500 with its text, and the
// harness fails the scenario.
package main

import (
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonx"
)

// entry is one client the harness created, with the hook events and errors it asked to record.
type entry struct {
	mu         sync.Mutex
	client     *toggleslash.Client
	hookEvents []any
	errors     []string
}

// service holds every client the harness created.
type service struct {
	mu      sync.Mutex
	clients map[string]*entry
	counter int
}

type reply struct {
	status  int
	payload any
}

// route splits "/clients/<id>/<action>" into the action path and the id. A request for "/" or "/clients" has no
// id.
func route(urlPath string) (path, id string) {
	trimmed := strings.Trim(urlPath, "/")
	switch {
	case trimmed == "":
		return "/", ""
	case trimmed == "clients":
		return "/clients", ""
	case !strings.HasPrefix(trimmed, "clients/"):
		return "/" + trimmed, ""
	}
	id, action, _ := strings.Cut(strings.TrimPrefix(trimmed, "clients/"), "/")
	return "/" + action, id
}

// handle answers one request, with a panic turned into the protocol's 500 rather than a dropped connection.
func (s *service) handle(method, path, id string, body []byte) (answer reply) {
	defer func() {
		if recovered := recover(); recovered != nil {
			answer = reply{status: 500, payload: fmt.Sprintf("panic: %v", recovered)}
		}
	}()
	switch method + " " + path {
	case "GET /":
		return reply{status: 200, payload: map[string]any{
			"name":         "toggleslash-sdk (Go)",
			"capabilities": []any{"integer-accessor"},
		}}
	case "POST /clients":
		return s.create(body)
	}
	client := s.lookup(id)
	if client == nil {
		return reply{status: 404, payload: "no such client"}
	}
	switch method + " " + path {
	case "POST /ready":
		return reply{status: 200, payload: map[string]any{"ready": client.client.Ready()}}
	case "POST /evaluate":
		return reply{status: 200, payload: evaluate(client.client, parseBody(body))}
	case "GET /status":
		return reply{status: 200, payload: statusOf(client.client.Status())}
	case "GET /hook-events":
		client.mu.Lock()
		events := append([]any{}, client.hookEvents...)
		client.mu.Unlock()
		return reply{status: 200, payload: map[string]any{"events": events}}
	case "GET /errors":
		client.mu.Lock()
		messages := append([]string{}, client.errors...)
		client.mu.Unlock()
		entries := make([]any, 0, len(messages))
		for _, message := range messages {
			entries = append(entries, message)
		}
		return reply{status: 200, payload: map[string]any{"count": len(messages), "messages": entries}}
	case "POST /close":
		client.client.Close()
		return reply{status: 200, payload: map[string]any{}}
	}
	return reply{status: 404, payload: "no such route"}
}

func (s *service) lookup(id string) *entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clients[id]
}

func parseBody(body []byte) map[string]any {
	if len(body) == 0 {
		return map[string]any{}
	}
	value, err := jsonx.Parse(body)
	if err != nil {
		return map[string]any{}
	}
	record, _ := value.(map[string]any)
	if record == nil {
		return map[string]any{}
	}
	return record
}

func (s *service) create(body []byte) reply {
	options := parseBody(body)
	current := &entry{}
	client := toggleslash.New(toggleslash.Options{
		URL:            stringOf(options["url"]),
		SDKKey:         stringOf(options["sdkKey"]),
		Stream:         boolPointer(options["stream"]),
		PollInterval:   millisDuration(options["pollIntervalMs"]),
		FlushInterval:  millisDuration(options["flushIntervalMs"]),
		RequestTimeout: millisDuration(options["requestTimeoutMs"]),
		Hooks:          toggleslash.Hooks{AfterEvaluation: hookRecorder(current, options)},
		OnError:        errorRecorder(current, options),
	})
	current.client = client
	s.mu.Lock()
	s.counter++
	id := fmt.Sprintf("%d", s.counter)
	s.clients[id] = current
	s.mu.Unlock()
	return reply{status: 201, payload: map[string]any{"id": id}}
}

func stringOf(value any) string {
	text, _ := value.(string)
	return text
}

func boolPointer(value any) *bool {
	flag, ok := value.(bool)
	if !ok {
		return nil
	}
	return &flag
}

// millisDuration reads a protocol duration: whole milliseconds, or nothing when the field was absent.
func millisDuration(value any) time.Duration {
	number, ok := value.(float64)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) || number <= 0 {
		return 0
	}
	return time.Duration(number * float64(time.Millisecond))
}

func errorRecorder(current *entry, options map[string]any) func(error) {
	return func(err error) {
		current.mu.Lock()
		current.errors = append(current.errors, err.Error())
		current.mu.Unlock()
		if throws, _ := options["onErrorThrows"].(bool); throws {
			panic("onError panicked, as the test asked")
		}
	}
}

// hookRecorder installs the recording hook for hook or hookThrows, and none otherwise.
func hookRecorder(current *entry, options map[string]any) func(toggleslash.EvaluationEvent) {
	records, _ := options["hook"].(bool)
	throws, _ := options["hookThrows"].(bool)
	if !records && !throws {
		return nil
	}
	return func(event toggleslash.EvaluationEvent) {
		current.mu.Lock()
		current.hookEvents = append(current.hookEvents, eventJSON(event))
		current.mu.Unlock()
		if throws {
			panic("the hook panicked, as the test asked")
		}
	}
}

func eventJSON(event toggleslash.EvaluationEvent) map[string]any {
	out := map[string]any{
		"flagKey":        event.FlagKey,
		"context":        event.Context,
		"value":          event.Value,
		"variationIndex": indexJSON(event.VariationIndex),
		"reason":         reasonJSON(event.Reason),
		"etag":           etagJSON(event.ETag),
		"flagVersion":    nil,
	}
	if event.FlagVersion != nil {
		out["flagVersion"] = *event.FlagVersion
	}
	return out
}

func indexJSON(index *int) any {
	if index == nil {
		return nil
	}
	return *index
}

func etagJSON(tag string) any {
	if tag == "" {
		return nil
	}
	return tag
}

func reasonJSON(reason toggleslash.Reason) map[string]any {
	out := map[string]any{"kind": string(reason.Kind)}
	switch reason.Kind {
	case toggleslash.ReasonError:
		out["code"] = string(reason.Code)
	case toggleslash.ReasonRuleMatch:
		out["ruleIndex"] = reason.RuleIndex
	case toggleslash.ReasonPrerequisiteFailed:
		out["flagKey"] = reason.FlagKey
	}
	return out
}

func statusOf(status toggleslash.Status) map[string]any {
	var synced any
	if !status.LastSyncAt.IsZero() {
		synced = status.LastSyncAt.UnixMilli()
	}
	var lastError any
	if status.LastError != "" {
		lastError = string(status.LastError)
	}
	return map[string]any{
		"etag":       etagJSON(status.ETag),
		"lastSyncAt": synced,
		"stream":     string(status.Stream),
		"lastError":  lastError,
	}
}

// evaluate maps the protocol's accessor onto the SDK's method. The context is the JSON it arrived as, so the
// vectors' 1e999 and lone surrogates reach the SDK as they were written.
func evaluate(client *toggleslash.Client, body map[string]any) map[string]any {
	flagKey := stringOf(body["flagKey"])
	fallback := body["fallback"]
	context, _ := body["context"].(map[string]any)
	var detail toggleslash.Detail
	switch stringOf(body["accessor"]) {
	case "int":
		number, ok := fallback.(float64)
		if !ok || number != math.Trunc(number) {
			panic("the int accessor needs a whole number fallback")
		}
		detail = client.Int64VariationDetail(flagKey, context, int64(number))
	case "bool":
		flag, ok := fallback.(bool)
		if !ok {
			panic("the bool accessor needs a boolean fallback")
		}
		detail = client.BoolVariationDetail(flagKey, context, flag)
	case "string":
		text, ok := fallback.(string)
		if !ok {
			panic("the string accessor needs a string fallback")
		}
		detail = client.StringVariationDetail(flagKey, context, text)
	case "number":
		number, ok := fallback.(float64)
		if !ok {
			panic("the number accessor needs a number fallback")
		}
		detail = client.Float64VariationDetail(flagKey, context, number)
	default:
		detail = client.VariationDetail(flagKey, context, fallback)
	}
	return map[string]any{
		"value":          detail.Value,
		"variationIndex": indexJSON(detail.VariationIndex),
		"reason":         reasonJSON(detail.Reason),
	}
}

func main() {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "the test service cannot listen: %v\n", err)
		os.Exit(1)
	}
	current := &service{clients: map[string]*entry{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		path, id := route(request.URL.Path)
		answer := current.handle(request.Method, path, id, body)
		if text, ok := answer.payload.(string); ok {
			writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
			writer.WriteHeader(answer.status)
			_, _ = writer.Write([]byte(text))
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(answer.status)
		_, _ = writer.Write(jsonx.Encode(answer.payload))
	})
	server := &http.Server{Handler: mux, ErrorLog: log.New(io.Discard, "", 0)}
	fmt.Printf("listening http://127.0.0.1:%d\n", listener.Addr().(*net.TCPAddr).Port)
	if err := server.Serve(listener); err != nil {
		fmt.Fprintf(os.Stderr, "the test service stopped: %v\n", err)
		os.Exit(1)
	}
}
