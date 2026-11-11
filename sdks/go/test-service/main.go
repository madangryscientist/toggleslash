// Command test-service wraps the Go SDK in the HTTP protocol of docs/sdk-test-service.md, so the conformance
// harness (packages/sdk-conformance) can drive it from outside. It catches nothing the SDK raises: a call that
// fails answers 500 with the error text, and the harness fails the scenario. README.md has the accessor mapping.
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	toggleslash "github.com/madangryscientist/toggleslash/sdks/go"
)

type entry struct {
	client *toggleslash.Client

	mu     sync.Mutex
	events []toggleslash.EvaluationEvent
	errors []string
}

type service struct {
	mu      sync.Mutex
	clients map[string]*entry
	created int
}

var routePattern = regexp.MustCompile(`^/clients/([^/]+)/([a-z-]+)$`)

func main() {
	s := &service{clients: map[string]*entry{}}
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, "test-service:", err)
		os.Exit(1)
	}
	_, _ = fmt.Fprintf(os.Stdout, "listening http://127.0.0.1:%d\n", listener.Addr().(*net.TCPAddr).Port)
	_ = os.Stdout.Sync()
	server := &http.Server{Handler: http.HandlerFunc(s.serve), ReadHeaderTimeout: 30 * time.Second}
	if err := server.Serve(listener); err != nil {
		fmt.Fprintln(os.Stderr, "test-service:", err)
		os.Exit(1)
	}
}

func (s *service) serve(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if p := recover(); p != nil {
			fail(w, fmt.Sprint("panic: ", p))
		}
	}()
	path := r.URL.Path
	switch {
	case r.Method == http.MethodGet && path == "/":
		reply(w, 200, `{"name":"toggleslash-sdk (Go)","capabilities":["integer-accessor"]}`)
	case r.Method == http.MethodPost && path == "/clients":
		s.create(w, r)
	default:
		m := routePattern.FindStringSubmatch(path)
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
		s.clientRoute(w, r, e, m[2])
	}
}

func fail(w http.ResponseWriter, text string) { http.Error(w, text, http.StatusInternalServerError) }

func reply(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func readBody(r *http.Request) (toggleslash.Value, error) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return toggleslash.Value{}, err
	}
	if len(raw) == 0 {
		return toggleslash.ObjectValue(nil, nil), nil
	}
	v, err := toggleslash.ParseValue(string(raw))
	if err != nil {
		return toggleslash.Value{}, err
	}
	if v.Kind() != toggleslash.KindObject {
		return toggleslash.Value{}, fmt.Errorf("the body is not a JSON object")
	}
	return v, nil
}

func millis(body toggleslash.Value, name string) (time.Duration, bool) {
	v, ok := body.Get(name)
	if !ok {
		return 0, false
	}
	n, isNum := v.Float()
	if !isNum {
		return 0, false
	}
	return time.Duration(n * float64(time.Millisecond)), true
}

func flag(body toggleslash.Value, name string) bool {
	v, ok := body.Get(name)
	if !ok {
		return false
	}
	b, _ := v.Bool()
	return b
}

func str(body toggleslash.Value, name string) string {
	v, _ := body.Get(name)
	s, _ := v.String()
	return s
}

func (s *service) create(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		fail(w, err.Error())
		return
	}
	e := &entry{}
	var opts []toggleslash.Option
	onErrorThrows := flag(body, "onErrorThrows")
	opts = append(opts, toggleslash.WithOnError(func(err error) {
		e.mu.Lock()
		e.errors = append(e.errors, err.Error())
		e.mu.Unlock()
		if onErrorThrows {
			panic("onError threw, as the test asked")
		}
	}))
	if v, ok := body.Get("stream"); ok && v.Kind() == toggleslash.KindBoolean {
		on, _ := v.Bool()
		opts = append(opts, toggleslash.WithStream(on))
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
	hookThrows := flag(body, "hookThrows")
	if flag(body, "hook") || hookThrows {
		opts = append(opts, toggleslash.WithAfterEvaluation(func(ev toggleslash.EvaluationEvent) {
			e.mu.Lock()
			e.events = append(e.events, ev)
			e.mu.Unlock()
			if hookThrows {
				panic("the hook threw, as the test asked")
			}
		}))
	}
	e.client = toggleslash.NewClient(str(body, "url"), str(body, "sdkKey"), opts...)
	s.mu.Lock()
	s.created++
	id := strconv.Itoa(s.created)
	s.clients[id] = e
	s.mu.Unlock()
	reply(w, 201, `{"id":`+quote(id)+`}`)
}

func (s *service) clientRoute(w http.ResponseWriter, r *http.Request, e *entry, action string) {
	switch r.Method + " " + action {
	case "POST ready":
		reply(w, 200, `{"ready":`+strconv.FormatBool(e.client.Ready())+`}`)
	case "POST evaluate":
		evaluate(w, r, e)
	case "GET status":
		reply(w, 200, statusJSON(e.client.Status()))
	case "GET hook-events":
		e.mu.Lock()
		defer e.mu.Unlock()
		parts := make([]string, len(e.events))
		for i, ev := range e.events {
			parts[i] = hookEventJSON(ev)
		}
		reply(w, 200, `{"events":[`+strings.Join(parts, ",")+`]}`)
	case "GET errors":
		e.mu.Lock()
		defer e.mu.Unlock()
		parts := make([]string, len(e.errors))
		for i, m := range e.errors {
			parts[i] = quote(m)
		}
		reply(w, 200, `{"count":`+strconv.Itoa(len(e.errors))+`,"messages":[`+strings.Join(parts, ",")+`]}`)
	case "POST close":
		e.client.Close()
		reply(w, 200, `{}`)
	default:
		http.Error(w, "no such route", http.StatusNotFound)
	}
}

func quote(s string) string { return toggleslash.StringValue(s).JSON() }

func statusJSON(st toggleslash.Status) string {
	etag, last, lastErr := "null", "null", "null"
	if st.ETag != "" {
		etag = quote(st.ETag)
	}
	if !st.LastSyncAt.IsZero() {
		last = strconv.FormatInt(st.LastSyncAt.UnixMilli(), 10)
	}
	if st.LastError != "" {
		lastErr = quote(st.LastError)
	}
	return `{"etag":` + etag + `,"lastSyncAt":` + last + `,"stream":` + quote(string(st.Stream)) + `,"lastError":` + lastErr + `}`
}

func index(i *int) string {
	if i == nil {
		return "null"
	}
	return strconv.Itoa(*i)
}

func reasonJSON(r toggleslash.Reason) string {
	b := `{"kind":` + quote(string(r.Kind))
	switch r.Kind {
	case toggleslash.ReasonRuleMatch:
		b += `,"ruleIndex":` + index(r.RuleIndex)
	case toggleslash.ReasonPrerequisiteFailed:
		b += `,"flagKey":` + quote(r.PrerequisiteKey)
	case toggleslash.ReasonError:
		b += `,"code":` + quote(string(r.Code))
	}
	return b + `}`
}

// valueJSON writes a Value as JSON. A number that is not finite is written as null, as JSON.stringify does.
func valueJSON(v toggleslash.Value) string {
	switch v.Kind() {
	case toggleslash.KindNumber:
		n, _ := v.Float()
		if n-n != 0 { // +Inf, -Inf and NaN
			return "null"
		}
		return v.JSON()
	case toggleslash.KindArray:
		parts := make([]string, v.Len())
		for i := range parts {
			item, _ := v.Index(i)
			parts[i] = valueJSON(item)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case toggleslash.KindObject:
		var parts []string
		for _, k := range v.Keys() {
			member, _ := v.Get(k)
			parts = append(parts, quote(k)+":"+valueJSON(member))
		}
		return "{" + strings.Join(parts, ",") + "}"
	}
	return v.JSON()
}

func hookEventJSON(ev toggleslash.EvaluationEvent) string {
	etag, version := "null", "null"
	if ev.ETag != "" {
		etag = quote(ev.ETag)
	}
	if ev.FlagVersion != nil {
		version = strconv.FormatInt(*ev.FlagVersion, 10)
	}
	return `{"flagKey":` + quote(ev.FlagKey) + `,"context":` + valueJSON(ev.Context.ToValue()) +
		`,"value":` + valueJSON(ev.Value) + `,"variationIndex":` + index(ev.VariationIndex) +
		`,"reason":` + reasonJSON(ev.Reason) + `,"etag":` + etag + `,"flagVersion":` + version + `}`
}

func answer[T any](d toggleslash.Detail[T], value func(T) string) string {
	return `{"value":` + value(d.Value) + `,"variationIndex":` + index(d.VariationIndex) + `,"reason":` + reasonJSON(d.Reason) + `}`
}

// evaluate maps the protocol's accessors onto the SDK's methods: bool, string, number, int, json and any are
// BoolVariationDetail, StringVariationDetail, FloatVariationDetail, IntVariationDetail, JSONVariationDetail
// and AnyVariationDetail.
func evaluate(w http.ResponseWriter, r *http.Request, e *entry) {
	body, err := readBody(r)
	if err != nil {
		fail(w, err.Error())
		return
	}
	flagKey := str(body, "flagKey")
	ctxValue, _ := body.Get("context")
	ctx := toggleslash.ContextFromValue(ctxValue)
	fallback, _ := body.Get("fallback")
	accessor := str(body, "accessor")
	if accessor == "" {
		accessor = "any"
	}
	c := e.client
	switch accessor {
	case "bool":
		b, ok := fallback.Bool()
		if !ok {
			fail(w, "the fallback is not a boolean")
			return
		}
		reply(w, 200, answer(c.BoolVariationDetail(flagKey, ctx, b), strconv.FormatBool))
	case "string":
		sv, ok := fallback.String()
		if !ok {
			fail(w, "the fallback is not a string")
			return
		}
		reply(w, 200, answer(c.StringVariationDetail(flagKey, ctx, sv), quote))
	case "number":
		n, ok := fallback.Float()
		if !ok {
			fail(w, "the fallback is not a number")
			return
		}
		reply(w, 200, answer(c.FloatVariationDetail(flagKey, ctx, n), func(f float64) string {
			return valueJSON(toggleslash.NumberValue(f))
		}))
	case "int":
		n, ok := fallback.Float()
		if !ok || n != float64(int64(n)) {
			fail(w, "the fallback is not a whole number")
			return
		}
		reply(w, 200, answer(c.IntVariationDetail(flagKey, ctx, int64(n)), func(i int64) string {
			return strconv.FormatInt(i, 10)
		}))
	case "json":
		reply(w, 200, answer(c.JSONVariationDetail(flagKey, ctx, fallback), valueJSON))
	case "any":
		reply(w, 200, answer(c.AnyVariationDetail(flagKey, ctx, fallback), valueJSON))
	default:
		fail(w, "unknown accessor "+accessor)
	}
}
