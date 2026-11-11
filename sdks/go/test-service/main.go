// Command test-service wraps the Go SDK in the protocol of docs/sdk-test-service.md, so the conformance
// harness (packages/sdk-conformance) can drive it from outside. The accessor mapping is in README.md.
// It catches nothing the SDK raises: a call that panics answers 500 with the text, and the harness
// fails the scenario.
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
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// entry is one client and what the service recorded about it.
type entry struct {
	client *toggleslash.Client

	mu         sync.Mutex
	hookEvents []jsonv.Value
	errors     []string
}

func (e *entry) addError(msg string) {
	e.mu.Lock()
	e.errors = append(e.errors, msg)
	e.mu.Unlock()
}

func (e *entry) addEvent(ev jsonv.Value) {
	e.mu.Lock()
	e.hookEvents = append(e.hookEvents, ev)
	e.mu.Unlock()
}

type service struct {
	mu      sync.Mutex
	clients map[string]*entry
	created int
}

var clientRoute = regexp.MustCompile(`^/clients/([^/]+)/([a-z-]+)$`)

func main() {
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, "listen:", err)
		os.Exit(1)
	}
	s := &service{clients: make(map[string]*entry)}
	_, _ = fmt.Fprintf(os.Stdout, "listening http://127.0.0.1:%d\n", ln.Addr().(*net.TCPAddr).Port)
	_ = os.Stdout.Sync()
	srv := &http.Server{Handler: http.HandlerFunc(s.serve)}
	if err := srv.Serve(ln); err != nil {
		fmt.Fprintln(os.Stderr, "serve:", err)
		os.Exit(1)
	}
}

// httpError is an answer other than 200: a request the service refuses, or an error it reports.
type httpError struct {
	status int
	text   string
}

func (e httpError) Error() string { return e.text }

func (s *service) serve(w http.ResponseWriter, r *http.Request) {
	body, err := s.handle(r)
	if err != nil {
		var he httpError
		status := http.StatusInternalServerError
		if errors.As(err, &he) {
			status = he.status
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(body.status)
	_, _ = w.Write(body.data)
}

type answer struct {
	status int
	data   []byte
}

func ok(v jsonv.Value) (answer, error) { return answer{http.StatusOK, jsonv.Marshal(v)}, nil }

// handle answers one request. A panic anywhere in it, the SDK's included, becomes a 500 with the text.
func (s *service) handle(r *http.Request) (a answer, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("the SDK raised: %v", p)
		}
	}()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/":
		return ok(object(
			"name", jsonv.NewString("toggleslash-sdk (Go)"),
			"capabilities", jsonv.NewArray([]jsonv.Value{jsonv.NewString("integer-accessor")}),
		))
	case r.Method == http.MethodPost && r.URL.Path == "/clients":
		return s.create(r)
	}
	m := clientRoute.FindStringSubmatch(r.URL.Path)
	if m == nil {
		return answer{}, httpError{http.StatusNotFound, "no such route"}
	}
	s.mu.Lock()
	e := s.clients[m[1]]
	s.mu.Unlock()
	if e == nil {
		return answer{}, httpError{http.StatusNotFound, "no such client"}
	}
	switch r.Method + " " + m[2] {
	case "POST ready":
		return ok(object("ready", jsonv.NewBool(e.client.Ready(r.Context()))))
	case "POST evaluate":
		return evaluate(e, r)
	case "GET status":
		return ok(statusValue(e.client.Status()))
	case "GET hook-events":
		e.mu.Lock()
		events := append([]jsonv.Value(nil), e.hookEvents...)
		e.mu.Unlock()
		return ok(object("events", jsonv.NewArray(events)))
	case "GET errors":
		e.mu.Lock()
		msgs := make([]jsonv.Value, len(e.errors))
		for i, m := range e.errors {
			msgs[i] = jsonv.NewString(m)
		}
		e.mu.Unlock()
		return ok(object("count", jsonv.NewNumber(float64(len(msgs))), "messages", jsonv.NewArray(msgs)))
	case "POST close":
		e.client.Close()
		return ok(object())
	}
	return answer{}, httpError{http.StatusNotFound, "no such route"}
}

func object(kv ...any) jsonv.Value {
	m := make(map[string]jsonv.Value, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1].(jsonv.Value)
	}
	return jsonv.NewObject(m)
}

func readBody(r *http.Request) (jsonv.Value, error) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return jsonv.Value{}, httpError{http.StatusBadRequest, "reading the body: " + err.Error()}
	}
	if len(raw) == 0 {
		return jsonv.NewObject(nil), nil
	}
	v, err := jsonv.Parse(raw)
	if err != nil {
		return jsonv.Value{}, httpError{http.StatusBadRequest, "the body is not JSON"}
	}
	if _, isObj := v.Object(); !isObj {
		return jsonv.Value{}, httpError{http.StatusBadRequest, "the body is not a JSON object"}
	}
	return v, nil
}

func stringField(v jsonv.Value, name string) string {
	f, _ := v.Get(name)
	s, _ := f.Str()
	return s
}

func flagField(v jsonv.Value, name string) bool {
	f, _ := v.Get(name)
	b, _ := f.Bool()
	return b
}

func msField(v jsonv.Value, name string) (time.Duration, bool) {
	f, found := v.Get(name)
	if !found {
		return 0, false
	}
	n, isNum := f.Number()
	if !isNum {
		return 0, false
	}
	return time.Duration(n) * time.Millisecond, true
}

func (s *service) create(r *http.Request) (answer, error) {
	body, err := readBody(r)
	if err != nil {
		return answer{}, err
	}
	e := &entry{}
	onErrorThrows := flagField(body, "onErrorThrows")
	opts := []toggleslash.Option{toggleslash.WithOnError(func(err error) {
		e.addError(err.Error())
		if onErrorThrows {
			panic("onError threw, as the test asked")
		}
	})}
	if f, found := body.Get("stream"); found {
		if on, isBool := f.Bool(); isBool {
			opts = append(opts, toggleslash.WithStream(on))
		}
	}
	if d, found := msField(body, "pollIntervalMs"); found {
		opts = append(opts, toggleslash.WithPollInterval(d))
	}
	if d, found := msField(body, "flushIntervalMs"); found {
		opts = append(opts, toggleslash.WithFlushInterval(d))
	}
	if d, found := msField(body, "requestTimeoutMs"); found {
		opts = append(opts, toggleslash.WithRequestTimeout(d))
	}
	hookThrows := flagField(body, "hookThrows")
	if flagField(body, "hook") || hookThrows {
		opts = append(opts, toggleslash.WithAfterEvaluation(func(ev toggleslash.EvaluationEvent) {
			e.addEvent(eventValue(ev))
			if hookThrows {
				panic("the hook threw, as the test asked")
			}
		}))
	}
	e.client = toggleslash.New(stringField(body, "url"), stringField(body, "sdkKey"), opts...)

	s.mu.Lock()
	s.created++
	id := strconv.Itoa(s.created)
	s.clients[id] = e
	s.mu.Unlock()
	return answer{http.StatusCreated, jsonv.Marshal(object("id", jsonv.NewString(id)))}, nil
}

func nullable(s string, has bool) jsonv.Value {
	if !has {
		return jsonv.Value{}
	}
	return jsonv.NewString(s)
}

func indexValue(idx *int) jsonv.Value {
	if idx == nil {
		return jsonv.Value{}
	}
	return jsonv.NewNumber(float64(*idx))
}

func reasonValue(r toggleslash.Reason) jsonv.Value {
	m := map[string]jsonv.Value{"kind": jsonv.NewString(string(r.Kind))}
	switch r.Kind {
	case toggleslash.ReasonError:
		m["code"] = jsonv.NewString(string(r.Code))
	case toggleslash.ReasonRuleMatch:
		m["ruleIndex"] = jsonv.NewNumber(float64(r.RuleIndex))
	case toggleslash.ReasonPrerequisiteFailed:
		m["flagKey"] = jsonv.NewString(r.FlagKey)
	}
	return jsonv.NewObject(m)
}

func statusValue(st toggleslash.Status) jsonv.Value {
	sync := jsonv.Value{}
	if !st.LastSyncAt.IsZero() {
		sync = jsonv.NewNumber(float64(st.LastSyncAt.UnixMilli()))
	}
	lastError := jsonv.Value{}
	if st.LastError != toggleslash.LastErrorNone {
		lastError = jsonv.NewString(string(st.LastError))
	}
	return object(
		"etag", nullable(st.ETag, st.HasETag),
		"lastSyncAt", sync,
		"stream", jsonv.NewString(string(st.Stream)),
		"lastError", lastError,
	)
}

func eventValue(ev toggleslash.EvaluationEvent) jsonv.Value {
	version := jsonv.Value{}
	if ev.FlagVersion != nil {
		version = jsonv.NewNumber(float64(*ev.FlagVersion))
	}
	return object(
		"flagKey", jsonv.NewString(ev.FlagKey),
		"context", ev.Context,
		"value", ev.Value,
		"variationIndex", indexValue(ev.VariationIndex),
		"reason", reasonValue(ev.Reason),
		"etag", nullable(ev.ETag, ev.HasETag),
		"flagVersion", version,
	)
}

// evaluate maps each accessor onto the SDK's method for it (README.md). The value is written as the
// accessor holds it, so an int64 keeps every digit.
func evaluate(e *entry, r *http.Request) (answer, error) {
	body, err := readBody(r)
	if err != nil {
		return answer{}, err
	}
	flagKey := stringField(body, "flagKey")
	ctx, _ := body.Get("context")
	fallback, _ := body.Get("fallback")
	accessor := stringField(body, "accessor")
	if accessor == "" {
		accessor = "any"
	}
	c := e.client

	var value []byte
	var index *int
	var reason toggleslash.Reason
	switch accessor {
	case "bool":
		fb, isBool := fallback.Bool()
		if !isBool {
			return answer{}, httpError{http.StatusBadRequest, "the bool accessor needs a boolean fallback"}
		}
		d := c.BoolVariationDetail(flagKey, ctx, fb)
		value, index, reason = jsonv.Marshal(jsonv.NewBool(d.Value)), d.VariationIndex, d.Reason
	case "string":
		fb, isStr := fallback.Str()
		if !isStr {
			return answer{}, httpError{http.StatusBadRequest, "the string accessor needs a string fallback"}
		}
		d := c.StringVariationDetail(flagKey, ctx, fb)
		value, index, reason = jsonv.Marshal(jsonv.NewString(d.Value)), d.VariationIndex, d.Reason
	case "number":
		fb, isNum := fallback.Number()
		if !isNum {
			return answer{}, httpError{http.StatusBadRequest, "the number accessor needs a number fallback"}
		}
		d := c.FloatVariationDetail(flagKey, ctx, fb)
		value, index, reason = jsonv.Marshal(jsonv.NewNumber(d.Value)), d.VariationIndex, d.Reason
	case "int":
		fb, isNum := fallback.Number()
		if !isNum || fb != float64(int64(fb)) {
			return answer{}, httpError{http.StatusBadRequest, "the int accessor needs an integer fallback"}
		}
		d := c.IntVariationDetail(flagKey, ctx, int64(fb))
		value, index, reason = strconv.AppendInt(nil, d.Value, 10), d.VariationIndex, d.Reason
	case "json", "any":
		d := c.JSONVariationDetail(flagKey, ctx, fallback)
		value, index, reason = jsonv.Marshal(d.Value), d.VariationIndex, d.Reason
	default:
		return answer{}, httpError{http.StatusBadRequest, "no such accessor: " + accessor}
	}
	out := []byte(`{"value":`)
	out = append(out, value...)
	out = append(out, `,"variationIndex":`...)
	out = append(out, jsonv.Marshal(indexValue(index))...)
	out = append(out, `,"reason":`...)
	out = append(out, jsonv.Marshal(reasonValue(reason))...)
	out = append(out, '}')
	return answer{http.StatusOK, out}, nil
}
