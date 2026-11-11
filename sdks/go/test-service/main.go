// Command test-service is the Go SDK's conformance test service: an HTTP server that wraps the SDK so the
// harness in packages/sdk-conformance can drive it from outside. The protocol is docs/sdk-test-service.md;
// the accessor mapping is in README.md. It catches nothing the SDK raises except to answer 500 with the
// text, and the harness then fails the scenario.
package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
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

type obj = map[string]jsonvalue.Value

func str(s string) jsonvalue.Value { return jsonvalue.NewString(s) }

// optStr is a string, or null for none.
func optStr(s string) jsonvalue.Value {
	if s == "" {
		return jsonvalue.Value{}
	}
	return str(s)
}

func optInt(p *int) jsonvalue.Value {
	if p == nil {
		return jsonvalue.Value{}
	}
	return jsonvalue.NewNumber(float64(*p))
}

func reasonJSON(r toggleslash.Reason) jsonvalue.Value {
	m := obj{"kind": str(string(r.Kind))}
	if r.RuleIndex != nil {
		m["ruleIndex"] = jsonvalue.NewNumber(float64(*r.RuleIndex))
	}
	if r.Kind == toggleslash.ReasonKindPrerequisiteFailed {
		m["flagKey"] = str(r.FlagKey)
	}
	if r.Kind == toggleslash.ReasonKindError {
		m["code"] = str(string(r.ErrorCode))
	}
	return jsonvalue.NewObject(m)
}

// finite writes a number that is not finite as null, as JSON.stringify does, all the way down.
func finite(v jsonvalue.Value) jsonvalue.Value {
	switch v.Kind() {
	case jsonvalue.Number:
		if f, _ := v.Number(); math.IsInf(f, 0) || math.IsNaN(f) {
			return jsonvalue.Value{}
		}
	case jsonvalue.Array:
		items, _ := v.Array()
		out := make([]jsonvalue.Value, len(items))
		for i, item := range items {
			out[i] = finite(item)
		}
		return jsonvalue.NewArray(out)
	case jsonvalue.Object:
		members, _ := v.Object()
		out := make(obj, len(members))
		for k, item := range members {
			out[k] = finite(item)
		}
		return jsonvalue.NewObject(out)
	}
	return v
}

func writeJSON(w http.ResponseWriter, status int, v jsonvalue.Value) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(jsonvalue.Marshal(finite(v)))
}

func readBody(r *http.Request) (jsonvalue.Value, error) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return jsonvalue.Value{}, err
	}
	if len(raw) == 0 {
		return jsonvalue.NewObject(obj{}), nil
	}
	v, err := jsonvalue.Parse(raw)
	if err != nil {
		return jsonvalue.Value{}, err
	}
	if v.Kind() != jsonvalue.Object {
		return jsonvalue.Value{}, fmt.Errorf("the body is not a JSON object")
	}
	return v, nil
}

func durationOption(body jsonvalue.Value, name string) (time.Duration, bool) {
	f, ok := body.Get(name).Number()
	if !ok {
		return 0, false
	}
	return time.Duration(f * float64(time.Millisecond)), true
}

func (s *service) create(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	e := &entry{}
	url, _ := body.Get("url").String()
	key, _ := body.Get("sdkKey").String()
	onErrorThrows, _ := body.Get("onErrorThrows").Bool()
	opts := []toggleslash.Option{toggleslash.WithOnError(func(err error) {
		e.mu.Lock()
		e.errors = append(e.errors, err.Error())
		e.mu.Unlock()
		if onErrorThrows {
			panic("onError threw, as the test asked")
		}
	})}
	if on, ok := body.Get("stream").Bool(); ok {
		opts = append(opts, toggleslash.WithStream(on))
	}
	if d, ok := durationOption(body, "pollIntervalMs"); ok {
		opts = append(opts, toggleslash.WithPollInterval(d))
	}
	if d, ok := durationOption(body, "flushIntervalMs"); ok {
		opts = append(opts, toggleslash.WithFlushInterval(d))
	}
	if d, ok := durationOption(body, "requestTimeoutMs"); ok {
		opts = append(opts, toggleslash.WithRequestTimeout(d))
	}
	hook, _ := body.Get("hook").Bool()
	hookThrows, _ := body.Get("hookThrows").Bool()
	if hook || hookThrows {
		opts = append(opts, toggleslash.WithAfterEvaluation(func(ev toggleslash.EvaluationEvent) {
			var version jsonvalue.Value
			if ev.FlagVersion != nil {
				version = jsonvalue.NewNumber(float64(*ev.FlagVersion))
			}
			recorded := jsonvalue.NewObject(obj{
				"flagKey":        str(ev.FlagKey),
				"context":        ev.Context,
				"value":          ev.Value,
				"variationIndex": optInt(ev.VariationIndex),
				"reason":         reasonJSON(ev.Reason),
				"etag":           optStr(ev.ETag),
				"flagVersion":    version,
			})
			e.mu.Lock()
			e.events = append(e.events, recorded)
			e.mu.Unlock()
			if hookThrows {
				panic("the hook threw, as the test asked")
			}
		}))
	}
	e.client = toggleslash.New(url, key, opts...)
	s.mu.Lock()
	s.created++
	id := strconv.Itoa(s.created)
	s.clients[id] = e
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, jsonvalue.NewObject(obj{"id": str(id)}))
}

// evaluate maps the harness's accessor names onto the SDK's methods (README.md has the table).
func (e *entry) evaluate(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	flagKey, _ := body.Get("flagKey").String()
	ctx := body.Get("context")
	fallback := body.Get("fallback")
	accessor, _ := body.Get("accessor").String()
	if accessor == "" {
		accessor = "any"
	}

	var value jsonvalue.Value
	var index *int
	var reason toggleslash.Reason
	switch accessor {
	case "bool":
		fb, ok := fallback.Bool()
		if !ok {
			http.Error(w, "the bool accessor needs a boolean fallback", http.StatusBadRequest)
			return
		}
		d := e.client.BoolVariationDetail(flagKey, ctx, fb)
		value, index, reason = jsonvalue.NewBool(d.Value), d.VariationIndex, d.Reason
	case "string":
		fb, ok := fallback.String()
		if !ok {
			http.Error(w, "the string accessor needs a string fallback", http.StatusBadRequest)
			return
		}
		d := e.client.StringVariationDetail(flagKey, ctx, fb)
		value, index, reason = jsonvalue.NewString(d.Value), d.VariationIndex, d.Reason
	case "number":
		fb, ok := fallback.Number()
		if !ok {
			http.Error(w, "the number accessor needs a number fallback", http.StatusBadRequest)
			return
		}
		d := e.client.FloatVariationDetail(flagKey, ctx, fb)
		value, index, reason = jsonvalue.NewNumber(d.Value), d.VariationIndex, d.Reason
	case "int":
		f, ok := fallback.Number()
		if !ok || f != math.Trunc(f) || f < -(1<<63) || f >= 1<<63 {
			http.Error(w, "the int accessor needs an integer fallback", http.StatusBadRequest)
			return
		}
		d := e.client.IntVariationDetail(flagKey, ctx, int64(f))
		value, index, reason = jsonvalue.NewNumber(float64(d.Value)), d.VariationIndex, d.Reason
	case "json", "any":
		d := e.client.JSONVariationDetail(flagKey, ctx, fallback)
		value, index, reason = d.Value, d.VariationIndex, d.Reason
	default:
		http.Error(w, "unknown accessor", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, jsonvalue.NewObject(obj{
		"value":          value,
		"variationIndex": optInt(index),
		"reason":         reasonJSON(reason),
	}))
}

func (e *entry) status(w http.ResponseWriter) {
	st := e.client.Status()
	var synced jsonvalue.Value
	if !st.LastSyncAt.IsZero() {
		synced = jsonvalue.NewNumber(float64(st.LastSyncAt.UnixMilli()))
	}
	writeJSON(w, http.StatusOK, jsonvalue.NewObject(obj{
		"etag":       optStr(st.ETag),
		"lastSyncAt": synced,
		"stream":     str(string(st.Stream)),
		"lastError":  optStr(st.LastError),
	}))
}

func (e *entry) route(w http.ResponseWriter, r *http.Request, action string) {
	switch r.Method + " " + action {
	case "POST ready":
		writeJSON(w, http.StatusOK, jsonvalue.NewObject(obj{"ready": jsonvalue.NewBool(e.client.Ready())}))
	case "POST evaluate":
		e.evaluate(w, r)
	case "GET status":
		e.status(w)
	case "GET hook-events":
		e.mu.Lock()
		events := append([]jsonvalue.Value(nil), e.events...)
		e.mu.Unlock()
		writeJSON(w, http.StatusOK, jsonvalue.NewObject(obj{"events": jsonvalue.NewArray(events)}))
	case "GET errors":
		e.mu.Lock()
		messages := make([]jsonvalue.Value, len(e.errors))
		for i, m := range e.errors {
			messages[i] = str(m)
		}
		e.mu.Unlock()
		writeJSON(w, http.StatusOK, jsonvalue.NewObject(obj{
			"count":    jsonvalue.NewNumber(float64(len(messages))),
			"messages": jsonvalue.NewArray(messages),
		}))
	case "POST close":
		e.client.Close()
		writeJSON(w, http.StatusOK, jsonvalue.NewObject(obj{}))
	default:
		http.Error(w, "no such route", http.StatusNotFound)
	}
}

var clientRoute = regexp.MustCompile(`^/clients/([^/]+)/([a-z-]+)$`)

func (s *service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if p := recover(); p != nil {
			http.Error(w, fmt.Sprintf("%v", p), http.StatusInternalServerError)
		}
	}()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/":
		writeJSON(w, http.StatusOK, jsonvalue.NewObject(obj{
			"name":         str("toggleslash-sdk (Go)"),
			"capabilities": jsonvalue.NewArray([]jsonvalue.Value{str("integer-accessor")}),
		}))
	case r.Method == http.MethodPost && r.URL.Path == "/clients":
		s.create(w, r)
	default:
		m := clientRoute.FindStringSubmatch(r.URL.Path)
		if m == nil {
			http.Error(w, "no such client", http.StatusNotFound)
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

func main() {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("listening http://%s\n", listener.Addr())
	_ = os.Stdout.Sync()
	server := &http.Server{Handler: &service{clients: map[string]*entry{}}}
	if err := server.Serve(listener); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
