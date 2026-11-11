package toggleslash

import (
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/evaluator"
	"github.com/madangryscientist/toggleslash/sdks/go/internal/wjson"
)

// failure is why one ruleset or stream attempt failed.
type failure struct {
	code string
	// retryAfter is set for a 429 that carried a valid Retry-After.
	retryAfter    time.Duration
	hasRetryAfter bool
}

// syncLoop is the sync goroutine: the first load, then the loop of contract 7.3.
func (c *Client) syncLoop() {
	defer c.loops.Done()
	defer c.own.enter()()

	// The first load runs at once, with no timer and no hold.
	safely(func() { c.refresh() })
	c.settleFirst(c.snap.Load() != nil)

	failures := 0
	for c.ctx.Err() == nil {
		if c.stream && !c.wait(0) {
			return
		}
		connected := false
		if c.stream {
			safely(func() { connected = c.follow() })
		}
		if c.ctx.Err() != nil {
			return
		}
		if connected {
			failures = 0
		} else if failures < 1<<20 {
			failures++
		}
		if !c.wait(c.delay(failures)) {
			return
		}
		if !connected {
			safely(func() { c.refresh() })
		}
	}
}

// delay is the wait after failures attempts that did not stay connected. With
// no stream it is the poll interval. With one it is the poll interval or one
// second doubled per failure (16 doublings at most), whichever is shorter,
// times a jitter in [0.5, 1).
func (c *Client) delay(failures int) time.Duration {
	if !c.stream {
		return c.poll
	}
	ceiling := min(c.poll, time.Second<<min(failures, 16))
	j := c.jitter()
	if !(j >= 0) { // also catches NaN
		j = 0
	}
	if j >= 1 {
		j = 0.999999
	}
	return time.Duration(float64(ceiling) * (0.5 + j/2))
}

// wait sleeps d, or until the hold of a Retry-After has passed if that is
// later. It reports false when Close ended the wait.
func (c *Client) wait(d time.Duration) bool {
	if h := c.holdLeft(); h > d {
		d = h
	}
	return c.clock.Sleep(c.ctx, d)
}

func (c *Client) holdLeft() time.Duration {
	c.holdMu.Lock()
	defer c.holdMu.Unlock()
	return c.hold.Sub(c.clock.Now())
}

// extendHold remembers what a 429 asked for. A shorter answer never shortens a
// longer wait.
func (c *Client) extendHold(d time.Duration) {
	until := c.clock.Now().Add(d)
	c.holdMu.Lock()
	if until.After(c.hold) {
		c.hold = until
	}
	c.holdMu.Unlock()
}

// refresh does one ruleset request (7.1). It reports whether it was a sync; a
// failure is recorded and reported here.
func (c *Client) refresh() bool {
	f, ok := c.fetchRuleset()
	if ok {
		return true
	}
	c.fail("ruleset", f)
	return false
}

// fail records a failed ruleset or stream attempt: the hold, lastError and
// OnError. Close abandons requests, and a failure it caused is not recorded.
func (c *Client) fail(route string, f failure) {
	if f.code == codeAborted || c.ctx.Err() != nil {
		return
	}
	if f.hasRetryAfter {
		c.extendHold(f.retryAfter)
	}
	c.stMu.Lock()
	c.lastError = f.code
	c.stMu.Unlock()
	c.reportError(&RequestError{Route: route, Code: f.code})
}

// synced says the ruleset held is confirmed current as of now.
func (c *Client) synced() {
	now := c.clock.Now()
	c.stMu.Lock()
	c.lastSyncAt = now
	c.lastError = ""
	c.stMu.Unlock()
}

// install swaps in a new ruleset and its ETag together, and that is a sync.
func (c *Client) install(rs *evaluator.Ruleset, etag string) {
	now := c.clock.Now()
	c.stMu.Lock()
	c.snap.Store(&snapshot{ruleset: rs, etag: etag})
	c.lastSyncAt = now
	c.lastError = ""
	c.stMu.Unlock()
}

func (c *Client) setStream(s StreamState) {
	c.stMu.Lock()
	c.streamState = s
	c.stMu.Unlock()
}

// fetchRuleset is GET /sdk/v1/ruleset: conditional when a ruleset and an ETag
// are held. It returns true when the ruleset is now confirmed current.
func (c *Client) fetchRuleset() (failure, bool) {
	headers := [][2]string{
		{"Authorization", "Bearer " + c.key},
		{"Accept", "application/json"},
	}
	held := c.snap.Load()
	if held != nil && held.etag != "" {
		headers = append(headers, [2]string{"If-None-Match", held.etag})
	}
	r := c.request(c.ctx, "GET", "/sdk/v1/ruleset", headers, nil, func(status int) bool { return status == 200 })
	if r.code != "" {
		return failure{code: r.code}, false
	}
	switch r.status {
	case 200:
		rs, ok := buildRuleset(r.body)
		if !ok {
			return failure{code: "bad_ruleset"}, false
		}
		c.install(rs, r.header.Get("ETag"))
		return failure{}, true
	case 304:
		if c.snap.Load() == nil {
			return failure{code: statusCode(304)}, false
		}
		c.synced()
		return failure{}, true
	case 429:
		d, ok := retryAfter(r.header)
		return failure{code: statusCode(429), retryAfter: d, hasRetryAfter: ok}, false
	}
	return failure{code: statusCode(r.status)}, false
}

// buildRuleset reads a 200 body. It is a ruleset only if it is a JSON object
// whose flags and segments are both arrays; what is inside them is the
// evaluator's business.
func buildRuleset(body []byte) (rs *evaluator.Ruleset, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			rs, ok = nil, false
		}
	}()
	v, err := wjson.Parse(body)
	if err != nil {
		return nil, false
	}
	o := v.Object()
	if o == nil {
		return nil, false
	}
	flags, _ := o.Get("flags")
	segments, _ := o.Get("segments")
	if flags.Kind() != wjson.KindArray || segments.Kind() != wjson.KindArray {
		return nil, false
	}
	return evaluator.NewRuleset(v), true
}
