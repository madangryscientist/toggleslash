// Command test-service is the Go SDK's conformance test service: an HTTP server that wraps toggleslash.Client so the
// harness in packages/sdk-conformance can drive it from outside. The protocol is docs/sdk-test-service.md; the
// accessor mapping is in README.md next to this file. It absorbs nothing the SDK does: a call that fails answers 500
// with the error text, and the harness fails the scenario.
package main

import (
	"context"
	"errors"
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

func (e *entry) recordEvent(ev toggleslash.EvaluationEvent) {
	e.mu.Lock()
	e.events = append(e.events, hookEvent(ev))
	e.mu.Unlock()
}

func (e *entry) recordError(err error) {
	e.mu.Lock()
	e.errors = append(e.errors, err.Error())
	e.mu.Unlock()
}

var (
	mu      sync.Mutex
	clients = map[string]*entry{}
	created int
)

var clientPath = regexp.MustCompile(`^/clients/([^/]+)/([a-z-]+)$`)

// badRequest is a request the service cannot read, as opposed to an SDK call that failed.
type badRequest string

func (b badRequest) Error() string { return string(b) }

func obj(members ...jsonvalue.Member) jsonvalue.Value { return jsonvalue.FromObject(members...) }

func member(key string, v jsonvalue.Value) jsonvalue.Member {
	return jsonvalue.Member{Key: key, Value: v}
}

func writeJSON(w http.ResponseWriter, status int, v jsonvalue.Value) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(v.JSON())
}

func readBody(r *http.Request) (jsonvalue.Value, error) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return jsonvalue.Value{}, err
	}
	if len(raw) == 0 {
		return obj(), nil
	}
	v, err := jsonvalue.Parse(raw)
	if err != nil {
		return jsonvalue.Value{}, badRequest("the body is not JSON: " + err.Error())
	}
	if v.Kind() != jsonvalue.Object {
		return jsonvalue.Value{}, badRequest("the body is not a JSON object")
	}
	return v, nil
}

func str(body jsonvalue.Value, key string) string {
	v, _ := body.Get(key)
	if s, ok := v.AsString(); ok {
		return s
	}
	return v.String()
}

func boolean(body jsonvalue.Value, key string) (value, present bool) {
	v, ok := body.Get(key)
	if !ok {
		return false, false
	}
	b, isBool := v.AsBool()
	return b, isBool
}

func millis(body jsonvalue.Value, key string) (time.Duration, bool) {
	v, ok := body.Get(key)
	if !ok {
		return 0, false
	}
	n, isNum := v.AsNumber()
	if !isNum {
		return 0, false
	}
	return time.Duration(n * float64(time.Millisecond)), true
}

func reasonJSON(r toggleslash.Reason) jsonvalue.Value {
	members := []jsonvalue.Member{member("kind", jsonvalue.FromString(r.Kind))}
	if r.Kind == toggleslash.ReasonRuleMatch {
		members = append(members, member("ruleIndex", jsonvalue.FromInt(int64(r.RuleIndex))))
	}
	if r.FlagKey != "" {
		members = append(members, member("flagKey", jsonvalue.FromString(r.FlagKey)))
	}
	if r.Code != "" {
		members = append(members, member("code", jsonvalue.FromString(r.Code)))
	}
	return obj(members...)
}

func indexJSON(i int) jsonvalue.Value {
	if i < 0 {
		return jsonvalue.NullValue()
	}
	return jsonvalue.FromInt(int64(i))
}

func nonEmpty(s string) jsonvalue.Value {
	if s == "" {
		return jsonvalue.NullValue()
	}
	return jsonvalue.FromString(s)
}

func hookEvent(ev toggleslash.EvaluationEvent) jsonvalue.Value {
	version := jsonvalue.NullValue()
	if ev.FlagVersion >= 0 {
		version = jsonvalue.FromFloat(ev.FlagVersion)
	}
	return obj(
		member("flagKey", jsonvalue.FromString(ev.FlagKey)),
		member("context", ev.Context),
		member("value", ev.Value),
		member("variationIndex", indexJSON(ev.VariationIndex)),
		member("reason", reasonJSON(ev.Reason)),
		member("etag", nonEmpty(ev.ETag)),
		member("flagVersion", version),
	)
}

func create(w http.ResponseWriter, r *http.Request) error {
	body, err := readBody(r)
	if err != nil {
		return err
	}
	e := &entry{}
	opts := []toggleslash.Option{toggleslash.WithOnError(func(err error) {
		e.recordError(err)
		if throws, _ := boolean(body, "onErrorThrows"); throws {
			panic("onError threw, as the test asked")
		}
	})}
	if on, ok := boolean(body, "stream"); ok {
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
	hook, _ := boolean(body, "hook")
	hookThrows, _ := boolean(body, "hookThrows")
	if hook || hookThrows {
		opts = append(opts, toggleslash.WithAfterEvaluation(func(ev toggleslash.EvaluationEvent) {
			e.recordEvent(ev)
			if hookThrows {
				panic("the hook threw, as the test asked")
			}
		}))
	}
	e.client = toggleslash.New(str(body, "url"), str(body, "sdkKey"), opts...)
	mu.Lock()
	created++
	id := strconv.Itoa(created)
	clients[id] = e
	mu.Unlock()
	writeJSON(w, http.StatusCreated, obj(member("id", jsonvalue.FromString(id))))
	return nil
}

// intFallback reads the fallback of the int accessor.
func intFallback(v jsonvalue.Value) (int64, error) {
	f, ok := v.AsNumber()
	if !ok || f != math.Trunc(f) || f < -(1<<63) || f >= 1<<63 {
		return 0, badRequest("the fallback of the int accessor is not an integer")
	}
	return int64(f), nil
}

func detailJSON[T any](d toggleslash.Detail[T], value jsonvalue.Value) jsonvalue.Value {
	return obj(
		member("value", value),
		member("variationIndex", indexJSON(d.VariationIndex)),
		member("reason", reasonJSON(d.Reason)),
	)
}

// evaluate maps each accessor onto the SDK method of its type; see README.md.
func evaluate(w http.ResponseWriter, r *http.Request, e *entry) error {
	body, err := readBody(r)
	if err != nil {
		return err
	}
	flag := str(body, "flagKey")
	ctx, _ := body.Get("context")
	fallback, _ := body.Get("fallback")
	c := e.client
	var answer jsonvalue.Value
	switch accessor := str(body, "accessor"); accessor {
	case "bool":
		b, ok := fallback.AsBool()
		if !ok {
			return badRequest("the fallback of the bool accessor is not a boolean")
		}
		d := c.BoolVariationDetail(flag, ctx, b)
		answer = detailJSON(d, jsonvalue.FromBool(d.Value))
	case "string":
		s, ok := fallback.AsString()
		if !ok {
			return badRequest("the fallback of the string accessor is not a string")
		}
		d := c.StringVariationDetail(flag, ctx, s)
		answer = detailJSON(d, jsonvalue.FromString(d.Value))
	case "number":
		n, ok := fallback.AsNumber()
		if !ok {
			return badRequest("the fallback of the number accessor is not a number")
		}
		d := c.FloatVariationDetail(flag, ctx, n)
		answer = detailJSON(d, jsonvalue.FromFloat(d.Value))
	case "int":
		n, err := intFallback(fallback)
		if err != nil {
			return err
		}
		d := c.IntVariationDetail(flag, ctx, n)
		answer = detailJSON(d, jsonvalue.FromInt(d.Value))
	case "json":
		d := c.JSONVariationDetail(flag, ctx, fallback)
		answer = detailJSON(d, d.Value)
	case "any", "":
		d := c.VariationDetail(flag, ctx, fallback)
		answer = detailJSON(d, d.Value)
	default:
		return badRequest("no such accessor: " + accessor)
	}
	writeJSON(w, http.StatusOK, answer)
	return nil
}

func status(w http.ResponseWriter, e *entry) {
	s := e.client.Status()
	syncAt := jsonvalue.NullValue()
	if s.LastSyncAt >= 0 {
		syncAt = jsonvalue.FromInt(s.LastSyncAt)
	}
	writeJSON(w, http.StatusOK, obj(
		member("etag", nonEmpty(s.ETag)),
		member("lastSyncAt", syncAt),
		member("stream", jsonvalue.FromString(s.Stream)),
		member("lastError", nonEmpty(s.LastError)),
	))
}

func clientRoute(w http.ResponseWriter, r *http.Request, e *entry, action string) error {
	switch r.Method + " " + action {
	case "POST ready":
		writeJSON(w, http.StatusOK, obj(member("ready", jsonvalue.FromBool(e.client.Ready()))))
	case "POST evaluate":
		return evaluate(w, r, e)
	case "GET status":
		status(w, e)
	case "GET hook-events":
		e.mu.Lock()
		events := append([]jsonvalue.Value(nil), e.events...)
		e.mu.Unlock()
		writeJSON(w, http.StatusOK, obj(member("events", jsonvalue.FromArray(events...))))
	case "GET errors":
		e.mu.Lock()
		messages := make([]jsonvalue.Value, len(e.errors))
		for i, m := range e.errors {
			messages[i] = jsonvalue.FromString(m)
		}
		e.mu.Unlock()
		writeJSON(w, http.StatusOK, obj(
			member("count", jsonvalue.FromInt(int64(len(messages)))),
			member("messages", jsonvalue.FromArray(messages...)),
		))
	case "POST close":
		e.client.Close()
		writeJSON(w, http.StatusOK, obj())
	default:
		http.Error(w, "no such route", http.StatusNotFound)
	}
	return nil
}

func route(w http.ResponseWriter, r *http.Request) error {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/":
		writeJSON(w, http.StatusOK, obj(
			member("name", jsonvalue.FromString("toggleslash-sdk (Go)")),
			member("capabilities", jsonvalue.FromArray(jsonvalue.FromString("integer-accessor"))),
		))
		return nil
	case r.Method == http.MethodPost && r.URL.Path == "/clients":
		return create(w, r)
	}
	m := clientPath.FindStringSubmatch(r.URL.Path)
	if m == nil {
		http.Error(w, "no such client", http.StatusNotFound)
		return nil
	}
	mu.Lock()
	e := clients[m[1]]
	mu.Unlock()
	if e == nil {
		http.Error(w, "no such client", http.StatusNotFound)
		return nil
	}
	return clientRoute(w, r, e, m[2])
}

func handler(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if p := recover(); p != nil {
			http.Error(w, fmt.Sprint("panic: ", p), http.StatusInternalServerError)
		}
	}()
	if err := route(w, r); err != nil {
		code := http.StatusInternalServerError
		var bad badRequest
		if errors.As(err, &bad) {
			code = http.StatusBadRequest
		}
		http.Error(w, err.Error(), code)
	}
}

func main() {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("listening http://%s\n", listener.Addr())
	if err := http.Serve(listener, http.HandlerFunc(handler)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
