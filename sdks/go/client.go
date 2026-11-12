package toggleslash

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonx"
)

// The client: options, evaluation, the sync loop of contract 7.3, usage and close.

const (
	defaultPollInterval   = 30 * time.Second
	defaultFlushInterval  = 30 * time.Second
	defaultRequestTimeout = 10 * time.Second
	// defaultStreamIdle is the 60 s watchdog on stream reads (contract 7.2).
	defaultStreamIdle = 60 * time.Second
	// maxDuration bounds a caller's interval, so an absurd one cannot make a timer misbehave.
	maxDuration = 365 * 24 * time.Hour
	// intLimit is 2^63 as a float64: the first number that does not fit a signed 64-bit integer.
	intLimit = 9223372036854775808.0
	intFloor = -9223372036854775808.0
)

// config is everything a client is built from. The fields after requestTimeout are test seams: New leaves them
// at their defaults, and a test in this package sets them.
type config struct {
	url            string
	sdkKey         string
	stream         bool
	pollInterval   time.Duration
	flushInterval  time.Duration
	requestTimeout time.Duration
	afterEvaluate  func(EvaluationEvent)
	onError        func(error)

	now             func() time.Time
	monotonic       func() time.Time
	jitter          func() float64
	streamIdle      time.Duration
	roundTripper    http.RoundTripper
	raiseEvaluation bool
}

// heldRuleset is a ruleset and the ETag that names it. It is never changed once built, so one pointer store
// swaps both.
type heldRuleset struct {
	ruleset *ruleset
	etag    string
}

// clientSnapshot is everything Status reads and every loop writes, kept in one value so a reader sees one
// moment: a new snapshot is built and stored as one atomic pointer, and no evaluation ever takes a lock.
type clientSnapshot struct {
	held      *heldRuleset
	lastSync  time.Time
	hasSync   bool
	stream    StreamState
	lastError LastError
	closing   bool
}

// Client is a ToggleSlash server SDK client. Every method is safe to call from many goroutines at once, and
// none panics.
type Client struct {
	cfg config

	state   atomic.Pointer[clientSnapshot]
	stateMu sync.Mutex

	readyCh    chan struct{}
	readyMu    sync.Mutex
	readyDone  bool
	readyValue bool

	stop     chan struct{}
	stopOnce sync.Once
	closed   chan struct{}

	closeMu      sync.Mutex
	closeStarted bool

	syncCtx     context.Context
	cancelSync  context.CancelFunc
	flushCtx    context.Context
	cancelFlush context.CancelFunc

	syncDone  chan struct{}
	flushDone chan struct{}

	callbacks atomic.Int32

	hold          *retryHold
	usage         *usageReporter
	syncTransport *httpTransport
}

// New builds a client and starts its loops. It returns at once: it does not wait for the network, and it never
// fails. A URL or key it cannot use makes each request a network error.
func New(options Options) *Client {
	cfg := config{
		url:            options.URL,
		sdkKey:         options.SDKKey,
		stream:         options.Stream == nil || *options.Stream,
		pollInterval:   durationOr(options.PollInterval, defaultPollInterval),
		flushInterval:  durationOr(options.FlushInterval, defaultFlushInterval),
		requestTimeout: durationOr(options.RequestTimeout, defaultRequestTimeout),
		afterEvaluate:  options.Hooks.AfterEvaluation,
		onError:        options.OnError,
		now:            time.Now,
		monotonic:      time.Now,
		jitter:         rand.Float64,
		streamIdle:     defaultStreamIdle,
	}
	return newClient(cfg)
}

func durationOr(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	if value > maxDuration {
		return maxDuration
	}
	return value
}

func newClient(cfg config) *Client {
	syncCtx, cancelSync := context.WithCancel(context.Background())
	flushCtx, cancelFlush := context.WithCancel(context.Background())
	start := StreamOff
	if cfg.stream {
		start = StreamConnecting
	}
	client := &Client{
		cfg:         cfg,
		readyCh:     make(chan struct{}),
		stop:        make(chan struct{}),
		closed:      make(chan struct{}),
		syncCtx:     syncCtx,
		cancelSync:  cancelSync,
		flushCtx:    flushCtx,
		cancelFlush: cancelFlush,
		syncDone:    make(chan struct{}),
		flushDone:   make(chan struct{}),
	}
	client.state.Store(&clientSnapshot{stream: start})
	client.hold = newRetryHold(cfg.monotonic)
	client.syncTransport = newHTTPTransport(syncCtx, cfg.roundTripper)
	client.usage = newUsageReporter(
		newHTTPTransport(flushCtx, cfg.roundTripper),
		cfg.url, cfg.sdkKey, cfg.requestTimeout, client.reportFromLoop, cfg.now,
		newRetryHold(cfg.monotonic),
	)
	go client.runSync()
	go client.runFlush()
	return client
}

// Ready waits for the first ruleset attempt to settle, bounded by the request timeout plus a second of grace,
// and answers whether a ruleset is held. It may be called any number of times, and the answer is fixed when
// the first attempt settles.
func (c *Client) Ready() (answer bool) {
	defer func() {
		if recover() != nil {
			answer = false
		}
	}()
	select {
	case <-c.readyCh:
		return c.readyValue
	default:
	}
	if c.inCallback() {
		return c.state.Load().held != nil
	}
	timer := time.NewTimer(c.cfg.requestTimeout + time.Second)
	defer timer.Stop()
	select {
	case <-c.readyCh:
		return c.readyValue
	case <-timer.C:
		c.settleReady(c.state.Load().held != nil)
		return c.readyValue
	}
}

// Variation is the flag's value for a context, or the caller's fallback.
func (c *Client) Variation(flagKey string, context Context, fallback any) any {
	return c.VariationDetail(flagKey, context, fallback).Value
}

// VariationDetail is Variation with the variation index and the reason. It is the generic accessor: the
// expected kind is read off the fallback's type, and a nil fallback accepts any kind.
func (c *Client) VariationDetail(flagKey string, context Context, fallback any) Detail {
	return c.answer(flagKey, context, fallback, accessorJSON)
}

// BoolVariation is the boolean accessor.
func (c *Client) BoolVariation(flagKey string, context Context, fallback bool) bool {
	return c.BoolVariationDetail(flagKey, context, fallback).Value.(bool)
}

// BoolVariationDetail is the boolean accessor with the variation index and the reason.
func (c *Client) BoolVariationDetail(flagKey string, context Context, fallback bool) Detail {
	return c.answer(flagKey, context, fallback, accessorBool)
}

// StringVariation is the string accessor.
func (c *Client) StringVariation(flagKey string, context Context, fallback string) string {
	return c.StringVariationDetail(flagKey, context, fallback).Value.(string)
}

// StringVariationDetail is the string accessor with the variation index and the reason.
func (c *Client) StringVariationDetail(flagKey string, context Context, fallback string) Detail {
	return c.answer(flagKey, context, fallback, accessorString)
}

// Float64Variation is the number accessor: any number matches, with no integer rule.
func (c *Client) Float64Variation(flagKey string, context Context, fallback float64) float64 {
	return c.Float64VariationDetail(flagKey, context, fallback).Value.(float64)
}

// Float64VariationDetail is the number accessor with the variation index and the reason.
func (c *Client) Float64VariationDetail(flagKey string, context Context, fallback float64) Detail {
	return c.answer(flagKey, context, fallback, accessorNumber)
}

// Int64Variation is the integer accessor: a served number with a fractional part, or outside signed 64 bits,
// is the fallback with TYPE_MISMATCH, and a served 2.0 is the integer 2.
func (c *Client) Int64Variation(flagKey string, context Context, fallback int64) int64 {
	return c.Int64VariationDetail(flagKey, context, fallback).Value.(int64)
}

// Int64VariationDetail is the integer accessor with the variation index and the reason.
func (c *Client) Int64VariationDetail(flagKey string, context Context, fallback int64) Detail {
	return c.answer(flagKey, context, fallback, accessorInt)
}

// Status is a snapshot of what the SDK has loaded. It reads memory, makes no request and never fails.
func (c *Client) Status() (status Status) {
	defer func() {
		if recover() != nil {
			status = Status{Stream: StreamOff}
		}
	}()
	snapshot := c.state.Load()
	if snapshot == nil {
		return Status{Stream: StreamOff}
	}
	status = Status{Stream: snapshot.stream, LastError: snapshot.lastError}
	if snapshot.held != nil {
		status.ETag = snapshot.held.etag
	}
	if snapshot.hasSync {
		status.LastSyncAt = snapshot.lastSync
	}
	return status
}

// Close stops following the server, stops the flush loop, waits for the loops and sends the final usage
// flush. It never fails, and it is safe to call twice or from many goroutines.
func (c *Client) Close() {
	defer func() { _ = recover() }()
	c.close()
}

// Close bounds its waits by about twice the request timeout, and states that in README.md.

type accessorKind uint8

const (
	accessorJSON accessorKind = iota
	accessorBool
	accessorString
	accessorNumber
	accessorInt
)

// answer builds one result and runs the hook. It never panics: a failure becomes the fallback with
// EVALUATION_FAILED and one OnError call.
func (c *Client) answer(flagKey string, context Context, fallback any, kind accessorKind) (detail Detail) {
	snapshot := c.state.Load()
	defer func() {
		if recovered := recover(); recovered != nil {
			c.report(&Error{Message: "the evaluation failed"})
			detail = Detail{Value: fallback, Reason: Reason{Kind: ReasonError, Code: CodeEvaluationFailed}}
		}
		c.runHook(snapshot, flagKey, context, detail)
	}()
	return c.detailOf(snapshot, flagKey, context, fallback, kind)
}

func (c *Client) detailOf(snapshot *clientSnapshot, flagKey string, context Context, fallback any, kind accessorKind) Detail {
	if snapshot.held == nil {
		return Detail{Value: fallback, Reason: Reason{Kind: ReasonError, Code: CodeNotReady}}
	}
	if c.cfg.raiseEvaluation {
		panic("the evaluator raised, as the test asked")
	}
	result := evaluateOne(snapshot.held.ruleset, flagKey, context)
	if !result.hasIndex {
		return Detail{Value: fallback, Reason: reasonOf(result)}
	}
	value := result.value
	var out any
	matched := false
	switch kind {
	case accessorBool:
		out, matched = value.(bool)
	case accessorString:
		out, matched = value.(string)
	case accessorNumber:
		var number float64
		if number, matched = numericValue(value); matched {
			out = number
		}
	case accessorInt:
		var number int64
		if number, matched = wholeInt64(value); matched {
			out = number
		}
	default:
		wanted := jsonKind(fallback)
		if fallback == nil {
			matched, out = true, value
		} else if wanted != "" && wanted == jsonKind(value) {
			matched, out = true, value
		}
	}
	if !matched {
		return Detail{Value: fallback, Reason: Reason{Kind: ReasonError, Code: CodeTypeMismatch}}
	}
	c.usage.record(flagKey, result.index)
	if kind == accessorJSON {
		out = copyJSON(out)
	}
	index := result.index
	return Detail{Value: out, VariationIndex: &index, Reason: reasonOf(result)}
}

// wholeInt64 reads a served value as a signed 64-bit integer, which is contract 4's integer rule: a fraction,
// an infinity or a number outside that range does not match.
func wholeInt64(value any) (int64, bool) {
	number, ok := numericValue(value)
	if !ok || !isFiniteNumber(number) || number != math.Trunc(number) {
		return 0, false
	}
	if number < intFloor || number >= intLimit {
		return 0, false
	}
	return int64(number), true
}

func reasonOf(result outcome) Reason {
	return Reason{Kind: result.kind, Code: result.code, RuleIndex: result.ruleIndex, FlagKey: result.flagKey}
}

// copyJSON is a deep copy of JSON data, so a caller cannot change the ruleset it was served from.
func copyJSON(value any) any {
	switch v := value.(type) {
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = copyJSON(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = copyJSON(item)
		}
		return out
	}
	return value
}

// runHook calls afterEvaluation on the caller's goroutine. A panic in it goes to OnError and changes nothing
// for the caller.
func (c *Client) runHook(snapshot *clientSnapshot, flagKey string, context Context, detail Detail) {
	hook := c.cfg.afterEvaluate
	if hook == nil {
		return
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			c.report(&Error{Message: fmt.Sprintf("the afterEvaluation hook panicked: %v", recovered)})
		}
	}()
	etag := ""
	var version *int64
	if snapshot != nil && snapshot.held != nil {
		etag = snapshot.held.etag
		if entry, ok := snapshot.held.ruleset.flags[flagKey]; ok {
			version = entry.versionOf()
		}
	}
	hook(EvaluationEvent{
		FlagKey:        flagKey,
		Context:        context,
		Value:          detail.Value,
		VariationIndex: detail.VariationIndex,
		Reason:         detail.Reason,
		ETag:           etag,
		FlagVersion:    version,
	})
}

// report tells OnError, with no lock held. A panic in OnError is ignored.
func (c *Client) report(err error) {
	callback := c.cfg.onError
	if callback == nil {
		return
	}
	defer func() { _ = recover() }()
	callback(err)
}

// reportFromLoop is report from one of the SDK's own goroutines, marked so that Ready and Close called from
// inside the callback do not wait for the work that goroutine is doing.
func (c *Client) reportFromLoop(err error) {
	if c.cfg.onError == nil {
		return
	}
	c.callbacks.Add(1)
	defer c.callbacks.Add(-1)
	c.report(err)
}

func (c *Client) inCallback() bool { return c.callbacks.Load() > 0 }

// safely runs one step of a loop, sending a panic to OnError rather than letting it kill the process.
func (c *Client) safely(step func()) {
	defer func() {
		if recovered := recover(); recovered != nil {
			c.reportFromLoop(&Error{Message: fmt.Sprintf("the SDK recovered from a panic: %v", recovered)})
		}
	}()
	step()
}

func (c *Client) settleReady(value bool) {
	c.readyMu.Lock()
	if !c.readyDone {
		c.readyValue = value
		c.readyDone = true
		close(c.readyCh)
	}
	c.readyMu.Unlock()
}

// update builds a new snapshot from the current one under the writers' lock. No I/O and no callback runs
// inside it.
func (c *Client) update(change func(*clientSnapshot)) {
	c.stateMu.Lock()
	snapshot := *c.state.Load()
	change(&snapshot)
	c.state.Store(&snapshot)
	c.stateMu.Unlock()
}

// swap takes a new ruleset: the pair, the sync time and a cleared error, together.
func (c *Client) swap(held *heldRuleset) {
	c.update(func(snapshot *clientSnapshot) {
		snapshot.held = held
		snapshot.lastSync = c.cfg.now()
		snapshot.hasSync = true
		snapshot.lastError = ""
	})
}

// touch is a sync that changes nothing held: a 304, or a stream tag equal to the one held.
func (c *Client) touch() {
	c.update(func(snapshot *clientSnapshot) {
		snapshot.lastSync = c.cfg.now()
		snapshot.hasSync = true
		snapshot.lastError = ""
	})
}

// fail records a failed ruleset or stream attempt and tells OnError. Nothing is recorded once close has begun,
// so the requests close abandoned reach no one.
func (c *Client) fail(code LastError, message string, first, disconnect bool) {
	suppressed := false
	c.update(func(snapshot *clientSnapshot) {
		if snapshot.closing {
			suppressed = true
			return
		}
		snapshot.lastError = code
		if disconnect {
			snapshot.stream = StreamDisconnected
		}
	})
	if suppressed {
		return
	}
	if first {
		c.settleReady(c.state.Load().held != nil)
	}
	c.reportFromLoop(&Error{Message: message, Code: code})
}

// ---- the sync goroutine ------------------------------------------------------------------------------

func (c *Client) runSync() {
	defer close(c.syncDone)
	c.safely(func() { c.fetchRuleset(true, false) })
	c.settleReady(c.state.Load().held != nil)
	c.loop()
}

func (c *Client) loop() {
	failures := 0
	for {
		if c.stopped() {
			return
		}
		connected := false
		if c.cfg.stream {
			c.waitForHold()
			if c.stopped() {
				return
			}
			c.safely(func() { connected = c.follow() })
		}
		if c.stopped() {
			return
		}
		if connected {
			failures = 0
		} else {
			failures++
		}
		c.wait(c.longer(c.delay(failures), c.hold.remaining()))
		if c.stopped() {
			return
		}
		if !connected {
			c.safely(func() { c.fetchRuleset(false, false) })
		}
	}
}

func (c *Client) stopped() bool {
	select {
	case <-c.stop:
		return true
	default:
		return false
	}
}

// longer is the longer of two waits: contract 7.4 makes each wait of the loop last at least until a
// Retry-After hold has passed.
func (c *Client) longer(first, second time.Duration) time.Duration {
	if second > first {
		return second
	}
	return first
}

func (c *Client) wait(duration time.Duration) {
	if duration <= 0 {
		return
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-c.stop:
	}
}

func (c *Client) waitForHold() {
	for !c.stopped() {
		remaining := c.hold.remaining()
		if remaining <= 0 {
			return
		}
		c.wait(remaining)
	}
}

// delay is contract 7.3's backoff: the poll interval when the stream is off, and otherwise the smaller of the
// poll interval and a doubling ceiling, multiplied by a jitter drawn from [0.5, 1).
func (c *Client) delay(failures int) time.Duration {
	if !c.cfg.stream {
		return c.cfg.pollInterval
	}
	shift := failures
	if shift > 16 {
		shift = 16
	}
	ceiling := time.Second * time.Duration(1<<shift)
	if ceiling > c.cfg.pollInterval {
		ceiling = c.cfg.pollInterval
	}
	return time.Duration(float64(ceiling) * (0.5 + c.cfg.jitter()/2))
}

func (c *Client) headers(extra map[string]string) map[string]string {
	headers := map[string]string{"Authorization": "Bearer " + c.cfg.sdkKey}
	for name, value := range extra {
		headers[name] = value
	}
	return headers
}

func codeOf(err error) LastError {
	if errors.Is(err, errTimeout) {
		return LastErrorTimeout
	}
	return LastErrorNetwork
}

// fetchRuleset is the ruleset request of contract 7.1, and reports whether it was a sync.
func (c *Client) fetchRuleset(first, disconnect bool) bool {
	held := c.state.Load().held
	extra := map[string]string{}
	if held != nil && held.etag != "" {
		extra["If-None-Match"] = held.etag
	}
	response, err := c.syncTransport.do("GET", c.cfg.url, "/sdk/v1/ruleset", c.headers(extra), nil, c.cfg.requestTimeout)
	if err != nil {
		if errors.Is(err, errAborted) {
			return false
		}
		c.fail(codeOf(err), describeError(err, "the ruleset request"), first, disconnect)
		return false
	}
	switch response.status {
	case 200:
		next, ok := readRuleset(response.body, response.header.Get("ETag"))
		if !ok {
			c.fail(LastErrorBadRuleset, "the ruleset is not valid", first, disconnect)
			return false
		}
		c.swap(next)
		return true
	case 304:
		if held != nil {
			c.touch()
			return true
		}
		c.fail("http_304", "GET /sdk/v1/ruleset answered 304", first, disconnect)
		return false
	default:
		if wait, ok := retryAfter(response.status, response.header.Get("Retry-After")); ok {
			c.hold.raiseTo(wait)
		}
		code := LastError(fmt.Sprintf("http_%d", response.status))
		c.fail(code, fmt.Sprintf("GET /sdk/v1/ruleset answered %d", response.status), first, disconnect)
		return false
	}
}

// readRuleset reads a 200 body as a ruleset: a JSON object whose flags and segments are both arrays.
func readRuleset(body []byte, etag string) (*heldRuleset, bool) {
	value, err := jsonx.Parse(body)
	if err != nil {
		return nil, false
	}
	record, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	if _, ok := record["flags"].([]any); !ok {
		return nil, false
	}
	if _, ok := record["segments"].([]any); !ok {
		return nil, false
	}
	return &heldRuleset{ruleset: createRuleset(record), etag: etag}, true
}

// follow is one stream attempt (contract 7.2). It reports whether the stream connected and was not ended by a
// failed fetch.
func (c *Client) follow() bool {
	timeout := c.cfg.requestTimeout
	c.update(func(snapshot *clientSnapshot) {
		if !snapshot.closing {
			snapshot.stream = StreamConnecting
		}
	})
	headers := c.headers(map[string]string{"Accept": "text/event-stream"})
	opened, err := c.syncTransport.openStream(c.cfg.url, "/sdk/v1/stream", headers, timeout)
	if err != nil {
		if errors.Is(err, errAborted) {
			return false
		}
		c.fail(codeOf(err), describeError(err, "the stream request"), false, true)
		return false
	}
	defer opened.close()
	if opened.status != 200 {
		if wait, ok := retryAfter(opened.status, opened.header.Get("Retry-After")); ok {
			c.hold.raiseTo(wait)
		}
		code := LastError(fmt.Sprintf("http_%d", opened.status))
		c.fail(code, fmt.Sprintf("GET /sdk/v1/stream answered %d", opened.status), false, true)
		return false
	}
	parser := &sseParser{}
	chunks := make(chan []byte)
	readErr := make(chan error, 1)
	done := make(chan struct{})
	defer close(done)
	go pumpStream(opened.body, chunks, readErr, done)
	connected := false
	idle := time.NewTimer(c.cfg.streamIdle)
	defer idle.Stop()
	for {
		select {
		case <-c.stop:
			return connected
		case data, ok := <-chunks:
			if !ok {
				readErr := <-readErr
				if readErr == nil {
					c.update(func(snapshot *clientSnapshot) {
						if !snapshot.closing {
							snapshot.stream = StreamDisconnected
						}
					})
					return connected
				}
				if errors.Is(readErr, errAborted) || c.stopped() {
					return connected
				}
				c.fail(LastErrorNetwork, "the stream ended abruptly", false, true)
				return connected
			}
			idle.Reset(c.cfg.streamIdle)
			for _, event := range parser.feed(data) {
				switch event.name {
				case "connected":
					connected = true
					c.update(func(snapshot *clientSnapshot) {
						if !snapshot.closing {
							snapshot.stream = StreamConnected
							snapshot.lastError = ""
						}
					})
				case "changed":
				default:
					continue
				}
				// The fetch a tag asks for happens with the watchdog paused: the stream is quiet while the SDK
				// is not reading it, which is not the server's fault.
				idle.Stop()
				keep := c.onTag(event.data)
				idle.Reset(c.cfg.streamIdle)
				if !keep {
					return false
				}
			}
		case <-idle.C:
			c.fail(LastErrorTimeout, "the stream was quiet for too long", false, true)
			return connected
		}
	}
}

// pumpStream reads the stream body and hands each piece to the reader. A clean end sends a nil error and
// closes the channel; anything else is that error.
func pumpStream(body io.ReadCloser, chunks chan<- []byte, readErr chan<- error, done <-chan struct{}) {
	buffer := make([]byte, 32*1024)
	for {
		count, err := body.Read(buffer)
		if count > 0 {
			data := make([]byte, count)
			copy(data, buffer[:count])
			select {
			case chunks <- data:
			case <-done:
				readErr <- nil
				close(chunks)
				return
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				readErr <- nil
			} else {
				readErr <- err
			}
			close(chunks)
			return
		}
	}
}

// onTag acts on a connected or changed event, answering false when the fetch it asked for failed (the stream
// then ends at once, so the loop retries it).
func (c *Client) onTag(tag string) bool {
	held := c.state.Load().held
	if held != nil && held.etag != "" && tag == held.etag {
		c.touch()
		return true
	}
	return c.fetchRuleset(false, true)
}

// ---- the flush goroutine -----------------------------------------------------------------------------

func (c *Client) runFlush() {
	defer close(c.flushDone)
	for {
		timer := time.NewTimer(c.cfg.flushInterval)
		select {
		case <-c.stop:
			timer.Stop()
			return
		case <-timer.C:
		}
		c.safely(func() { c.usage.flush() })
	}
}

// ---- close -------------------------------------------------------------------------------------------

func (c *Client) close() {
	timeout := c.cfg.requestTimeout
	c.closeMu.Lock()
	performer := !c.closeStarted
	c.closeStarted = true
	c.closeMu.Unlock()

	c.update(func(snapshot *clientSnapshot) {
		snapshot.closing = true
		snapshot.stream = StreamClosed
	})
	c.usage.stopCounting()
	c.stopOnce.Do(func() { close(c.stop) })
	c.cancelSync()

	if !performer {
		if !c.inCallback() {
			timer := time.NewTimer(4 * timeout)
			defer timer.Stop()
			select {
			case <-c.closed:
			case <-timer.C:
			}
		}
		return
	}
	defer close(c.closed)
	defer c.cancelFlush()
	if !c.inCallback() {
		c.waitForLoops(timeout)
	}
	c.usage.finish(timeout)
	c.settleReady(false)
	// Nothing else will use them, so no pooled connection outlives the client.
	c.syncTransport.client.CloseIdleConnections()
	c.usage.transport.client.CloseIdleConnections()
}

// waitForLoops waits for the sync and flush goroutines, bounded: a request close abandoned may take a moment to
// come back.
func (c *Client) waitForLoops(timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for _, done := range []chan struct{}{c.syncDone, c.flushDone} {
		select {
		case <-done:
		case <-timer.C:
			return
		}
	}
}
