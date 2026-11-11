// Command test-service wraps the Go SDK in the HTTP protocol of docs/sdk-test-service.md, so the
// conformance harness (packages/sdk-conformance) can drive it. It listens on 127.0.0.1 and prints
// "listening http://127.0.0.1:<port>" once it does.
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
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
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

var clientRoute = regexp.MustCompile(`^/clients/([^/]+)/([a-z-]+)$`)

func main() {
	var lc net.ListenConfig
	listener, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot listen:", err)
		os.Exit(1)
	}
	s := &service{clients: map[string]*entry{}}
	fmt.Fprintf(os.Stdout, "listening http://%s\n", listener.Addr())
	server := &http.Server{Handler: http.HandlerFunc(s.serve)}
	if err := server.Serve(listener); err != nil {
		fmt.Fprintln(os.Stderr, "serve:", err)
		os.Exit(1)
	}
}

func (s *service) serve(w http.ResponseWriter, r *http.Request) {
	// The SDK must never throw at a caller: anything that escapes it is reported, not absorbed.
	defer func() {
		if recovered := recover(); recovered != nil {
			http.Error(w, fmt.Sprint("panic: ", recovered), http.StatusInternalServerError)
		}
	}()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/":
		write(w, 200, object(
			"name", jsonvalue.String("toggleslash-sdk (Go)"),
			"capabilities", jsonvalue.Array([]jsonvalue.Value{jsonvalue.String("integer-accessor")}),
		))
		return
	case r.Method == http.MethodPost && r.URL.Path == "/clients":
		s.create(w, r)
		return
	}
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
	switch r.Method + " " + m[2] {
	case "POST ready":
		write(w, 200, object("ready", jsonvalue.Bool(e.client.Ready())))
	case "POST evaluate":
		e.evaluate(w, r)
	case "GET status":
		write(w, 200, statusValue(e.client.Status()))
	case "GET hook-events":
		e.mu.Lock()
		events := append([]jsonvalue.Value(nil), e.events...)
		e.mu.Unlock()
		write(w, 200, object("events", jsonvalue.Array(events)))
	case "GET errors":
		e.mu.Lock()
		messages := make([]jsonvalue.Value, len(e.errors))
		for i, msg := range e.errors {
			messages[i] = toggleslash.String(msg)
		}
		count := len(e.errors)
		e.mu.Unlock()
		write(w, 200, object("count", jsonvalue.Number(float64(count)), "messages", jsonvalue.Array(messages)))
	case "POST close":
		_ = e.client.Close()
		write(w, 200, object())
	default:
		http.Error(w, "no such route", http.StatusNotFound)
	}
}

func readBody(r *http.Request) (jsonvalue.Value, error) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return jsonvalue.Null(), err
	}
	if len(raw) == 0 {
		return object(), nil
	}
	return toggleslash.ParseValue(raw)
}

func durationOption(body jsonvalue.Value, name string) (time.Duration, bool) {
	v, _ := body.Field(name)
	n, ok := v.AsNumber()
	if !ok {
		return 0, false
	}
	return time.Duration(n * float64(time.Millisecond)), true
}

func (s *service) create(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		http.Error(w, "the body is not JSON", http.StatusBadRequest)
		return
	}
	e := &entry{}
	str := func(name string) string {
		v, _ := body.Field(name)
		out, _ := v.AsString()
		return out
	}
	flag := func(name string) bool {
		v, _ := body.Field(name)
		out, _ := v.AsBool()
		return out
	}
	var opts []toggleslash.Option
	if v, ok := body.Field("stream"); ok {
		if on, isBool := v.AsBool(); isBool {
			opts = append(opts, toggleslash.WithStream(on))
		}
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
	hookThrows := flag("hookThrows")
	if flag("hook") || hookThrows {
		opts = append(opts, toggleslash.WithAfterEvaluation(func(ev toggleslash.Event) {
			e.mu.Lock()
			e.events = append(e.events, eventValue(ev))
			e.mu.Unlock()
			if hookThrows {
				panic("the hook threw, as the test asked")
			}
		}))
	}
	onErrorThrows := flag("onErrorThrows")
	opts = append(opts, toggleslash.WithOnError(func(err error) {
		e.mu.Lock()
		e.errors = append(e.errors, err.Error())
		e.mu.Unlock()
		if onErrorThrows {
			panic("onError threw, as the test asked")
		}
	}))
	e.client = toggleslash.New(str("url"), str("sdkKey"), opts...)

	s.mu.Lock()
	s.created++
	id := strconv.Itoa(s.created)
	s.clients[id] = e
	s.mu.Unlock()
	write(w, 201, object("id", toggleslash.String(id)))
}

func (e *entry) evaluate(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		http.Error(w, "the body is not JSON", http.StatusBadRequest)
		return
	}
	keyValue, _ := body.Field("flagKey")
	flagKey, _ := keyValue.AsString()
	contextValue, _ := body.Field("context")
	ctx := toggleslash.ContextOf(contextValue)
	fallback, _ := body.Field("fallback")
	accessor := "any"
	if v, ok := body.Field("accessor"); ok {
		if s, isString := v.AsString(); isString {
			accessor = s
		}
	}

	var (
		value jsonvalue.Value
		index int
		why   toggleslash.Reason
	)
	switch accessor {
	case "bool":
		b, ok := fallback.AsBool()
		if !ok {
			http.Error(w, "the bool accessor needs a boolean fallback", http.StatusBadRequest)
			return
		}
		d := e.client.BoolVariationDetail(flagKey, ctx, b)
		value, index, why = toggleslash.Bool(d.Value), d.VariationIndex, d.Reason
	case "string":
		s, ok := fallback.AsString()
		if !ok {
			http.Error(w, "the string accessor needs a string fallback", http.StatusBadRequest)
			return
		}
		d := e.client.StringVariationDetail(flagKey, ctx, s)
		value, index, why = jsonvalue.String(d.Value), d.VariationIndex, d.Reason
	case "number":
		n, ok := fallback.AsNumber()
		if !ok {
			http.Error(w, "the number accessor needs a number fallback", http.StatusBadRequest)
			return
		}
		d := e.client.FloatVariationDetail(flagKey, ctx, n)
		value, index, why = toggleslash.Number(d.Value), d.VariationIndex, d.Reason
	case "int":
		n, ok := fallback.AsNumber()
		if !ok || n != math.Trunc(n) || n < -(1<<63) || n >= 1<<63 {
			http.Error(w, "the int accessor needs an integer fallback", http.StatusBadRequest)
			return
		}
		d := e.client.IntVariationDetail(flagKey, ctx, int64(n))
		value, index, why = toggleslash.Number(float64(d.Value)), d.VariationIndex, d.Reason
	case "json", "any":
		d := e.client.JSONVariationDetail(flagKey, ctx, fallback)
		value, index, why = d.Value, d.VariationIndex, d.Reason
	default:
		http.Error(w, "no such accessor", http.StatusBadRequest)
		return
	}
	write(w, 200, object("value", value, "variationIndex", indexValue(index), "reason", reasonValue(why)))
}

func indexValue(i int) jsonvalue.Value {
	if i == toggleslash.NoVariation {
		return jsonvalue.Null()
	}
	return jsonvalue.Number(float64(i))
}

func reasonValue(r toggleslash.Reason) jsonvalue.Value {
	pairs := []any{"kind", toggleslash.String(string(r.Kind))}
	switch r.Kind {
	case toggleslash.ReasonRuleMatch:
		pairs = append(pairs, "ruleIndex", jsonvalue.Number(float64(r.RuleIndex)))
	case toggleslash.ReasonPrerequisiteFailed:
		pairs = append(pairs, "flagKey", toggleslash.String(r.FlagKey))
	case toggleslash.ReasonError:
		pairs = append(pairs, "code", toggleslash.String(string(r.Code)))
	}
	return object(pairs...)
}

func eventValue(ev toggleslash.Event) jsonvalue.Value {
	etag := jsonvalue.Null()
	if ev.ETag != "" {
		etag = toggleslash.String(ev.ETag)
	}
	version := jsonvalue.Null()
	if ev.FlagVersion != nil {
		version = jsonvalue.Number(float64(*ev.FlagVersion))
	}
	return object(
		"flagKey", toggleslash.String(ev.FlagKey),
		"context", ev.Context.Value(),
		"value", ev.Value,
		"variationIndex", indexValue(ev.VariationIndex),
		"reason", reasonValue(ev.Reason),
		"etag", etag,
		"flagVersion", version,
	)
}

func statusValue(st toggleslash.Status) jsonvalue.Value {
	etag, lastError, lastSync := jsonvalue.Null(), jsonvalue.Null(), jsonvalue.Null()
	if st.ETag != "" {
		etag = toggleslash.String(st.ETag)
	}
	if st.LastError != "" {
		lastError = toggleslash.String(string(st.LastError))
	}
	if !st.LastSyncAt.IsZero() {
		lastSync = jsonvalue.Number(float64(st.LastSyncAt.UnixMilli()))
	}
	return object("etag", etag, "lastSyncAt", lastSync, "stream", toggleslash.String(string(st.Stream)), "lastError", lastError)
}

// object builds a JSON object from alternating keys and values.
func object(pairs ...any) jsonvalue.Value {
	o := jsonvalue.NewObject()
	for i := 0; i+1 < len(pairs); i += 2 {
		o.Set(pairs[i].(string), pairs[i+1].(jsonvalue.Value))
	}
	return jsonvalue.ObjectValue(o)
}

func write(w http.ResponseWriter, status int, v jsonvalue.Value) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(jsonvalue.Marshal(v))
}
