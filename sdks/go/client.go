package toggleslash

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/eval"
	"github.com/madangryscientist/toggleslash/sdks/go/jsonval"
)

// Client is one ToggleSlash server SDK client. Every operation is safe to call
// from many goroutines at once (docs/sdk-contract.md section 6).
type Client struct {
	opts   Options
	stream bool
	http   *http.Client

	// loaded is the ruleset held, with the ETag that names it, swapped
	// together in one store (contract 6.3).
	loaded atomic.Pointer[snapshot]
	// status is one consistent snapshot of what the client has loaded and
	// how its stream is doing (contract 6.5). Writers hold statusMu; readers
	// do not.
	status   atomic.Pointer[statusSnapshot]
	statusMu sync.Mutex

	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeOnce sync.Once
	closed    atomic.Bool
	// inOnError counts the onError calls the notify goroutine is making. Go
	// has no goroutine identity, so a Close that sees it above zero takes
	// itself to be on the SDK's own goroutine (contract 6.4) and returns at
	// once, with shutdown finishing on a goroutine of its own. A Close from
	// another goroutine during that window returns early too (DESIGN.md).
	inOnError atomic.Int32

	readyCh   chan struct{}
	readyOK   atomic.Bool
	readyOnce sync.Once

	// requestHold holds the ruleset and stream routes; eventsHold holds the
	// usage reports. They are separate (contract 9).
	requestHold hold
	eventsHold  hold

	usageMu sync.Mutex
	usage   map[usageKey]int64

	queueMu     sync.Mutex
	queueCond   *sync.Cond
	queue       []error
	queueClosed bool
}

// statusSnapshot is the storage behind Status. Empty lastError and etag mean
// none, as does a zero lastSyncAt with hasLastSync false.
type statusSnapshot struct {
	etag        string
	lastSyncAt  time.Time
	hasLastSync bool
	stream      StreamState
	lastError   LastError
}

func newHTTPClient() *http.Client {
	// Redirects are not followed: Go re-sends the request, and a 301/302/303
	// turns the usage POST into a bodyless GET whose 2xx would read as sent and
	// lose the counts (contract 9). A 3xx is therefore http_<status>, as
	// decision 72 records.
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	// An application may have replaced http.DefaultTransport with its own
	// RoundTripper. New must not panic on that (contract 5.1), so only a
	// *http.Transport is cloned for the raised idle limit; anything else is
	// used as it stands.
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		transport := base.Clone()
		transport.MaxIdleConnsPerHost = 4
		client.Transport = transport
		return client
	}
	client.Transport = http.DefaultTransport
	return client
}

func newCond(mu *sync.Mutex) *sync.Cond { return sync.NewCond(mu) }

func defaultJitter() float64 { return rand.Float64() }

// updateStatus changes the snapshot under the writers' lock, and keeps the
// stream state closed once Close has begun.
func (c *Client) updateStatus(change func(*statusSnapshot)) {
	c.statusMu.Lock()
	next := *c.status.Load()
	change(&next)
	if c.closed.Load() {
		next.stream = StreamClosed
	}
	c.status.Store(&next)
	c.statusMu.Unlock()
}

func (c *Client) setStream(state StreamState) {
	c.updateStatus(func(s *statusSnapshot) { s.stream = state })
}

// streamFailed sets lastError and the disconnected state together, so no
// reader sees one without the other (contract 6.5).
func (c *Client) streamFailed(code LastError) {
	c.updateStatus(func(s *statusSnapshot) {
		s.lastError = code
		s.stream = StreamDisconnected
	})
}

func (c *Client) loadFailed(code LastError) {
	c.updateStatus(func(s *statusSnapshot) { s.lastError = code })
}

// markSynced moves lastSyncAt and clears lastError: a 200, a 304, or a stream
// event naming the tag held (contract 8).
func (c *Client) markSynced() { c.markSyncedWith("", false) }

// markSyncedWith is markSynced, and replaces the reported ETag when a 200
// brought a new ruleset.
func (c *Client) markSyncedWith(etag string, replaceETag bool) {
	now := c.opts.clock.Now()
	c.updateStatus(func(s *statusSnapshot) {
		if replaceETag {
			s.etag = etag
		}
		s.lastSyncAt = now
		s.hasLastSync = true
		s.lastError = ""
	})
}

// streamConnected is a `connected` event: the stream state and the cleared
// last error in one store, so a reader never sees a failure's error beside
// connected (contract 6.5).
func (c *Client) streamConnected() {
	c.updateStatus(func(s *statusSnapshot) {
		s.stream = StreamConnected
		s.lastError = ""
	})
}

// settleReady fixes the answer Ready gives, once.
func (c *Client) settleReady(ok bool) {
	c.readyOnce.Do(func() {
		c.readyOK.Store(ok)
		close(c.readyCh)
	})
}

// report queues a failure for onError, which runs on the notify goroutine.
func (c *Client) report(err error) {
	if err == nil {
		return
	}
	c.queueMu.Lock()
	if !c.queueClosed {
		c.queue = append(c.queue, err)
		c.queueCond.Signal()
		c.queueMu.Unlock()
		return
	}
	c.queueMu.Unlock()
	// After close the notify goroutine is gone, but evaluation still answers
	// and a hook still runs on a closed client: a failure it raises is still a
	// failure the SDK absorbed (contract 10), so deliver it on this caller's
	// goroutine rather than nowhere.
	c.callOnError(err)
}

func (c *Client) notifyLoop() {
	for {
		c.queueMu.Lock()
		for len(c.queue) == 0 && !c.queueClosed {
			c.queueCond.Wait()
		}
		if len(c.queue) == 0 {
			c.queueMu.Unlock()
			return
		}
		err := c.queue[0]
		c.queue = c.queue[1:]
		c.queueMu.Unlock()
		c.inOnError.Add(1)
		c.callOnError(err)
		c.inOnError.Add(-1)
	}
}

// callOnError runs the application's callback, and ignores anything it raises
// (contract 10).
func (c *Client) callOnError(err error) {
	if c.opts.OnError == nil {
		return
	}
	defer func() { _ = recover() }()
	c.opts.OnError(err)
}

// syncLoop runs syncRun and keeps it running: a panic that reaches it is
// reported and the loop, unless the client is closed, waits a backoff that
// grows with the panics so far and goes on, because a panic on an SDK goroutine
// would end the host process (contract 5, guarantee 1; FS-178). The wait is
// inside the guard, so a panic in it is caught too.
func (c *Client) syncLoop() {
	defer c.wg.Done()
	firstLoadDone := false
	for panics := 0; ; panics++ {
		stop := false
		raised, panicked := catch(func() {
			if panics > 0 && (!c.waitFor(c.delay(panics)) || c.closed.Load()) {
				stop = true
				return
			}
			c.syncRun(&firstLoadDone)
		})
		if !panicked || stop {
			return
		}
		// lastError is set and Ready settles before the report, as for a failed
		// first load; a panic after the first load also ends the stream attempt.
		if c.stream && firstLoadDone {
			c.streamFailed(LastErrorNetwork)
		} else {
			c.loadFailed(LastErrorNetwork)
		}
		c.settleReady(c.loaded.Load() != nil)
		c.reportPanic("sync", raised)
		if c.closed.Load() {
			return
		}
	}
}

// syncRun owns the sync state machine: the first load, the polls, the stream
// and the ruleset fetch a stream event asks for. One goroutine owns it, so a
// fetch and a stream attempt cannot overtake each other.
func (c *Client) syncRun(firstLoadDone *bool) {
	if !*firstLoadDone {
		// The first load sets lastError and settles Ready before its failure is
		// reported, so no callback on an SDK goroutine ever runs while Ready is
		// unsettled (contract 5.3, 6.4).
		ok, code, err := c.load()
		if code != "" {
			c.loadFailed(code)
		}
		c.settleReady(ok)
		*firstLoadDone = true
		c.report(err)
		if c.closed.Load() {
			return
		}
	}

	failures := 0
	for {
		connected := false
		if c.stream {
			if !c.waitFor(0) {
				return
			}
			if c.closed.Load() {
				// Close began while this wait ended: no stream attempt may
				// start after it (contract 11).
				return
			}
			connected = c.followStream()
			if c.closed.Load() {
				return
			}
		}
		if connected {
			failures = 0
		} else {
			failures++
		}
		if !c.waitFor(c.delay(failures)) {
			return
		}
		if c.closed.Load() {
			// The same guard for the poll below: close stops new ruleset
			// requests, and only one already in flight may finish.
			return
		}
		if connected {
			continue
		}
		// One poll: the stream never connected, so catch up once.
		_, err := c.loadAndReport()
		c.report(err)
		if c.closed.Load() {
			return
		}
	}
}

// catch runs fn and answers the value it panicked with. It tells a panic from a
// normal return by a flag, so a panic(nil) is one even under GODEBUG=panicnil=1.
func catch(fn func()) (raised any, panicked bool) {
	returned := false
	defer func() {
		if !returned {
			raised, panicked = recover(), true
		}
	}()
	fn()
	returned = true
	return nil, false
}

// loopPanicError is a panic an SDK goroutine recovered, which goes to onError
// and changes nothing else.
type loopPanicError struct {
	loop  string
	cause any
}

func (e loopPanicError) Error() string {
	return fmt.Sprintf("toggleslash: the %s loop panicked: %v", e.loop, e.cause)
}

func (c *Client) reportPanic(loop string, raised any) {
	c.report(loopPanicError{loop: loop, cause: raised})
}

// delay is contract 7.3's backoff: pollInterval with the stream off, and
// min(pollInterval, 1s * 2^min(failures, 16)) scaled by a jitter in [0.5, 1)
// with it on.
func (c *Client) delay(failures int) time.Duration {
	if !c.stream {
		return c.opts.PollInterval
	}
	ceiling := time.Second
	for i := 0; i < failures && ceiling < c.opts.PollInterval; i++ {
		ceiling *= 2
	}
	if ceiling > c.opts.PollInterval {
		ceiling = c.opts.PollInterval
	}
	jitter := c.opts.jitter()
	if jitter < 0 {
		jitter = 0
	}
	if jitter >= 1 {
		jitter = 0.999999
	}
	return time.Duration(float64(ceiling) * (0.5 + 0.5*jitter))
}

// load does one ruleset request (contract 7.1). It answers whether a ruleset
// is held afterwards, the lastError code the attempt failed with (empty when it
// worked), and the failure to report. It leaves lastError to its caller, because
// a fetch a stream asked for must set it in the same status store as the
// stream's end (contract 6.5).
func (c *Client) load() (bool, LastError, error) {
	held := c.loaded.Load()
	req, err := http.NewRequestWithContext(c.ctx, http.MethodGet, c.opts.URL+"/sdk/v1/ruleset", nil)
	if err != nil {
		return held != nil, LastErrorNetwork, networkError{cause: err}
	}
	req.Header.Set("Authorization", "Bearer "+c.opts.SDKKey)
	if held != nil && held.etag != "" {
		req.Header.Set("If-None-Match", held.etag)
	}
	ctx, cancel := context.WithTimeout(c.ctx, c.opts.RequestTimeout)
	defer cancel()
	resp, err := c.http.Do(req.WithContext(ctx))
	if err != nil {
		if c.closed.Load() {
			return held != nil, "", nil
		}
		code := LastErrorNetwork
		if isTimeout(err) {
			code = LastErrorTimeout
		}
		return held != nil, code, failureError{code: code, cause: err}
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		return c.readRuleset(resp, held)
	case http.StatusNotModified:
		if held == nil {
			code := HTTPLastError(http.StatusNotModified)
			return false, code, failureError{code: code}
		}
		if c.closed.Load() {
			return true, "", nil
		}
		c.markSynced()
		return true, "", nil
	case http.StatusTooManyRequests:
		c.readRetryAfter(&c.requestHold, resp.Header.Get("Retry-After"))
		code := HTTPLastError(http.StatusTooManyRequests)
		return held != nil, code, failureError{code: code}
	default:
		code := HTTPLastError(resp.StatusCode)
		return held != nil, code, failureError{code: code}
	}
}

// loadAndReport is load for the first load and the polls: it records the
// failure's lastError itself, because nothing else is changing the stream state
// at that moment.
func (c *Client) loadAndReport() (bool, error) {
	held, code, err := c.load()
	if code != "" {
		c.loadFailed(code)
	}
	return held, err
}

// readRuleset reads a 200 answer. Only a real ruleset replaces the one held
// (contract 5, item 5); anything else is bad_ruleset.
func (c *Client) readRuleset(resp *http.Response, held *snapshot) (bool, LastError, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if c.closed.Load() {
			return held != nil, "", nil
		}
		code := LastErrorNetwork
		if isTimeout(err) {
			code = LastErrorTimeout
		}
		return held != nil, code, failureError{code: code, cause: err}
	}
	next, err := parseRuleset(body, resp.Header.Get("ETag"))
	if err != nil {
		return held != nil, LastErrorBadRuleset, failureError{code: LastErrorBadRuleset, cause: err}
	}
	if c.closed.Load() {
		return held != nil, "", nil
	}
	c.loaded.Store(next)
	c.markSyncedWith(next.etag, true)
	return true, "", nil
}

// readRetryAfter raises a hold from a 429's Retry-After, if the header is
// whole decimal seconds, capped at 300 s (contract 7.4).
func (c *Client) readRetryAfter(target *hold, header string) {
	if wait, ok := parseRetryAfter(header); ok {
		target.raise(c.opts.clock.Now(), wait)
	}
}

// parseRetryAfter reads Retry-After as contract 7.4 does: whole decimal
// seconds and nothing else, surrounding spaces ignored, capped at 300 s. A
// date, a fraction or a negative number is not a hold.
func parseRetryAfter(header string) (time.Duration, bool) {
	// Contract 7.4 ignores the header's "surrounding spaces", which is HTTP's
	// optional whitespace (space or tab), not every Unicode space: a value
	// padded with a no-break space is not whole decimal seconds.
	text := strings.Trim(header, " \t")
	if text == "" {
		return 0, false
	}
	for i := 0; i < len(text); i++ {
		if text[i] < '0' || text[i] > '9' {
			return 0, false
		}
	}
	seconds, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		if errors.Is(err, strconv.ErrRange) {
			return holdCap, true
		}
		return 0, false
	}
	if seconds > int64(holdCap/time.Second) {
		return holdCap, true
	}
	return time.Duration(seconds) * time.Second, true
}

// isTimeout tells a deadline from a connection failure.
func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}

// failureError is the value onError receives for a failed request. It carries
// the lastError code and never the URL or the key (contract 12), and its text
// is fixed for the same reason: a transport cause is a *url.Error, whose text
// carries the whole URL (language notes, "A network error's text carries the
// URL"). The cause stays available through Unwrap, for errors.Is.
type failureError struct {
	code  LastError
	cause error
}

func (e failureError) Error() string { return "toggleslash: " + string(e.code) }

func (e failureError) Unwrap() error { return e.cause }

// networkError is a request that could not be built at all.
type networkError struct{ cause error }

func (e networkError) Error() string { return "toggleslash: " + string(LastErrorNetwork) }

func (e networkError) Unwrap() error { return e.cause }

// copyInt answers a new pointer to the same value, so a hook cannot change the
// ruleset or the caller's result through the pointer it was given.
func copyInt(v *int) *int {
	if v == nil {
		return nil
	}
	n := *v
	return &n
}

// copyInt64 is copyInt for the flag version, which is a 64 bit integer.
func copyInt64(v *int64) *int64 {
	if v == nil {
		return nil
	}
	n := *v
	return &n
}

// after counts a served evaluation and runs the hook, when one is set.
func (c *Client) after(e evaluation, flagKey string, context Context, fallback jsonval.Value) {
	value := fallback
	if e.served {
		// The event is the hook's to keep: a copy keeps a mutation from
		// changing the ruleset, as the JSON accessor's copy does.
		value = jsonval.Clone(e.value)
		c.record(flagKey, *e.index)
	}
	if value == nil {
		value = jsonval.Null{}
	}
	hook := c.opts.Hooks.AfterEvaluation
	if hook == nil {
		return
	}
	reason := e.reason
	reason.RuleIndex = copyInt(e.reason.RuleIndex)
	event := Event{
		FlagKey:        flagKey,
		Context:        context,
		Value:          value,
		VariationIndex: copyInt(e.index),
		Reason:         reason,
		ETag:           e.etag,
		FlagVersion:    copyInt64(e.flagVersion),
	}
	defer func() {
		if raised := recover(); raised != nil {
			c.report(hookError{cause: raised})
		}
	}()
	hook(event)
}

// hookError is a hook that raised, which goes to onError and changes nothing
// else (contract 10).
type hookError struct{ cause any }

func (e hookError) Error() string { return "toggleslash: afterEvaluation hook raised" }

// evaluationError is an evaluator that raised, which is EVALUATION_FAILED and
// one onError call (contract 4, step 5).
type evaluationError struct{ cause any }

func (e evaluationError) Error() string { return "toggleslash: the evaluator raised" }

// evaluation is one accessor's answer before it is put in the caller's type.
type evaluation struct {
	value       jsonval.Value
	index       *int
	reason      Reason
	served      bool
	etag        string
	flagVersion *int64
}

// evaluateFlag runs one evaluation to contract 4, recovering anything the
// evaluator raises as EVALUATION_FAILED.
func (c *Client) evaluateFlag(flagKey string, context Context, accept func(jsonval.Value) bool) (result evaluation) {
	snap := c.loaded.Load()
	if snap == nil {
		return evaluation{reason: Reason{Kind: ReasonError, Code: CodeNotReady}}
	}
	result.etag = snap.etag
	result.flagVersion = snap.ruleset.FlagVersion(flagKey)

	var evaluated eval.Result
	raised := c.runEvaluator(snap.ruleset, flagKey, context, &evaluated)
	if raised != nil {
		// The failure goes to onError, and the caller gets EVALUATION_FAILED
		// (contract 4, step 5).
		c.report(evaluationError{cause: raised})
		return evaluation{
			reason:      Reason{Kind: ReasonError, Code: CodeEvaluationFailed},
			etag:        snap.etag,
			flagVersion: result.flagVersion,
		}
	}
	if evaluated.VariationIndex == nil {
		return evaluation{
			reason:      reasonOf(evaluated.Reason),
			etag:        snap.etag,
			flagVersion: result.flagVersion,
		}
	}
	if !accept(evaluated.Value) {
		return evaluation{
			reason:      Reason{Kind: ReasonError, Code: CodeTypeMismatch},
			etag:        snap.etag,
			flagVersion: result.flagVersion,
		}
	}
	return evaluation{
		value:       evaluated.Value,
		index:       evaluated.VariationIndex,
		reason:      reasonOf(evaluated.Reason),
		served:      true,
		etag:        snap.etag,
		flagVersion: result.flagVersion,
	}
}

// runEvaluator calls the evaluator and turns a panic into a reported error.
func (c *Client) runEvaluator(ruleset *eval.Ruleset, flagKey string, context Context, out *eval.Result) (raised any) {
	defer func() { raised = recover() }()
	*out = c.opts.evaluate(ruleset, flagKey, context)
	return nil
}

func reasonOf(reason eval.Reason) Reason {
	return Reason{
		Kind:      ReasonKind(reason.Kind),
		RuleIndex: reason.RuleIndex,
		FlagKey:   reason.FlagKey,
		Code:      ErrorCode(reason.Code),
	}
}

func kindOf(value jsonval.Value) string {
	switch value.(type) {
	case jsonval.Null, nil:
		return "null"
	case jsonval.Bool:
		return "boolean"
	case jsonval.Number:
		return "number"
	case jsonval.String:
		return "string"
	case jsonval.Array:
		return "array"
	case jsonval.Object:
		return "object"
	}
	return ""
}

func isNull(value jsonval.Value) bool {
	if value == nil {
		return true
	}
	_, ok := value.(jsonval.Null)
	return ok
}
