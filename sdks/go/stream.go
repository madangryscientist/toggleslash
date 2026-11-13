package toggleslash

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
)

// followStream is one stream attempt (contract 7.2). It answers whether the
// stream counts as connected for the backoff of contract 7.3: the `connected`
// event arrived and no ruleset fetch the stream asked for failed.
func (c *Client) followStream() bool {
	c.setStream(StreamConnecting)

	attempt, cancel := context.WithCancel(c.ctx)
	defer cancel()

	// The open is bounded by requestTimeout, from the request until the
	// response headers arrive; the reads after it are not.
	var openTimedOut atomic.Bool
	openTimer := c.opts.clock.AfterFunc(c.opts.RequestTimeout, func() {
		openTimedOut.Store(true)
		cancel()
	})
	req, err := http.NewRequestWithContext(attempt, http.MethodGet, c.opts.URL+"/sdk/v1/stream", nil)
	if err != nil {
		openTimer.Stop()
		c.failStream(LastErrorNetwork, networkError{cause: err})
		return false
	}
	req.Header.Set("Authorization", "Bearer "+c.opts.SDKKey)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.http.Do(req)
	openTimer.Stop()
	if err != nil {
		if c.closed.Load() {
			return false
		}
		code := LastErrorNetwork
		if openTimedOut.Load() || isTimeout(err) {
			code = LastErrorTimeout
		}
		c.failStream(code, failureError{code: code, cause: err})
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTooManyRequests {
			c.readRetryAfter(&c.requestHold, resp.Header.Get("Retry-After"))
		}
		code := HTTPLastError(resp.StatusCode)
		c.failStream(code, failureError{code: code})
		return false
	}

	// The watchdog is armed on the same cancel: a stream that goes quiet for
	// the whole length makes the next read fail, which ends the attempt.
	var watchTimedOut atomic.Bool
	watchdog := c.opts.clock.AfterFunc(c.opts.watchdog, func() {
		watchTimedOut.Store(true)
		cancel()
	})
	defer watchdog.Stop()

	reader := newSSEReader(bufio.NewReaderSize(resp.Body, sseBufferSize), func() {
		watchdog.Reset(c.opts.watchdog)
	})
	sawConnected := false
	fetchFailed := false
	var readErr error
	for {
		name, data, _, err := reader.next()
		if err != nil {
			readErr = err
			break
		}
		switch name {
		case "connected":
			sawConnected = true
			c.streamConnected()
			if code, err := c.onStreamTag(data); code != "" {
				// One store sets the stream's end and this failure's
				// lastError together (contract 6.5).
				c.streamFailed(code)
				c.report(err)
				fetchFailed = true
			}
		case "changed":
			if code, err := c.onStreamTag(data); code != "" {
				c.streamFailed(code)
				c.report(err)
				fetchFailed = true
			}
		}
		if fetchFailed {
			break
		}
	}

	watchdog.Stop()
	if fetchFailed {
		// The stream is not connected for the backoff: a stream left up would
		// never retry the fetch.
		return false
	}
	connected := sawConnected
	if c.closed.Load() {
		return connected
	}
	switch {
	case watchTimedOut.Load() || openTimedOut.Load():
		// The open timer can fire in the moment between the headers arriving
		// and its Stop, which cancels the body read: that is the open's bound,
		// not a network failure.
		c.failStream(LastErrorTimeout, failureError{code: LastErrorTimeout, cause: readErr})
	case readErr != nil && !errors.Is(readErr, io.EOF) && c.ctx.Err() == nil:
		c.failStream(LastErrorNetwork, failureError{code: LastErrorNetwork, cause: readErr})
	default:
		// A stream the server ends, or one Close ended: a clean end is not an
		// error and leaves lastError alone.
		c.setStream(StreamDisconnected)
	}
	return connected
}

// onStreamTag is contract 7.2's answer to a `connected` or `changed` data
// line: a tag equal to the one held is a sync, anything else is a ruleset
// fetch. It answers the failure code of that fetch, empty when there was none.
func (c *Client) onStreamTag(tag string) (LastError, error) {
	held := c.loaded.Load()
	if held != nil && held.etag != "" && tag == held.etag {
		c.markSynced()
		return "", nil
	}
	_, code, err := c.load()
	return code, err
}

// failStream sets the last error and the disconnected state together and
// reports the failure, unless close caused it.
func (c *Client) failStream(code LastError, err error) {
	if c.closed.Load() {
		return
	}
	c.streamFailed(code)
	c.report(err)
}

const sseBufferSize = 4096

// sseReader parses Server-Sent Events the way the WHATWG standard does, one
// event at a time (contract 7.2). It reads bytes, not lines, so a line ending
// split across a read boundary is still one ending.
type sseReader struct {
	reader  *bufio.Reader
	onByte  func()
	pending []byte

	line    []byte
	skipLF  bool
	name    string
	data    []byte
	hasData bool
	started bool
}

func newSSEReader(reader *bufio.Reader, onByte func()) *sseReader {
	return &sseReader{reader: reader, onByte: onByte, line: make([]byte, 0, 64)}
}

// next answers the next complete event, skipping the events the standard
// drops (one with no data line). It returns io.EOF at the end of the stream,
// where an incomplete event is discarded.
func (s *sseReader) next() (node, data string, ok bool, err error) {
	if !s.started {
		s.skipBOM()
		s.started = true
	}
	for {
		b, err := s.readByte()
		if err != nil {
			return "", "", false, err
		}
		s.onByte()
		switch b {
		case '\n':
			if s.skipLF {
				s.skipLF = false
				continue
			}
			if name, data, ok, done := s.endLine(); done {
				if ok {
					return name, data, true, nil
				}
			}
		case '\r':
			s.skipLF = true
			if name, data, ok, done := s.endLine(); done {
				if ok {
					return name, data, true, nil
				}
			}
		default:
			s.skipLF = false
			s.line = append(s.line, b)
		}
	}
}

// readByte answers the next byte, from the bytes a non-BOM head left over
// first: skipBOM consumes up to three before it knows what they are.
func (s *sseReader) readByte() (byte, error) {
	if len(s.pending) > 0 {
		b := s.pending[0]
		s.pending = s.pending[1:]
		return b, nil
	}
	return s.reader.ReadByte()
}

// skipBOM drops one leading UTF-8 byte order mark, as the standard does. It
// reads a byte at a time and restarts the watchdog on each: the watchdog's rule
// is about bytes arriving, so a BOM spread over three slow reads must not be
// dropped as a timeout.
func (s *sseReader) skipBOM() {
	head := make([]byte, 0, 3)
	for len(head) < 3 {
		b, err := s.reader.ReadByte()
		if err != nil {
			break
		}
		s.onByte()
		head = append(head, b)
	}
	if len(head) == 3 && head[0] == 0xEF && head[1] == 0xBB && head[2] == 0xBF {
		return
	}
	s.pending = head
}

// endLine handles one complete line and reports a dispatched event.
func (s *sseReader) endLine() (name, data string, ok bool, done bool) {
	line := s.line
	s.line = s.line[:0]
	if len(line) == 0 {
		name, data, ok = s.dispatch()
		s.name = ""
		s.data = s.data[:0]
		s.hasData = false
		return name, data, ok, true
	}
	if line[0] == ':' {
		// A comment, including `: keep-alive`; it still counted as a byte for
		// the watchdog.
		return "", "", false, false
	}
	field, value := splitField(line)
	switch string(field) {
	case "event":
		s.name = string(value)
	case "data":
		if s.hasData {
			s.data = append(s.data, '\n')
		}
		s.data = append(s.data, value...)
		s.hasData = true
	}
	return "", "", false, false
}

// dispatch answers the event a blank line completes.
func (s *sseReader) dispatch() (name, data string, ok bool) {
	if !s.hasData {
		return "", "", false
	}
	name = s.name
	if name == "" {
		name = "message"
	}
	return name, string(s.data), true
}

// splitField splits a field line at its first colon and drops one leading
// space from the value.
func splitField(line []byte) (field, value []byte) {
	for i, b := range line {
		if b == ':' {
			field = line[:i]
			value = line[i+1:]
			if len(value) > 0 && value[0] == ' ' {
				value = value[1:]
			}
			return field, value
		}
	}
	return line, nil
}
