// Package sse reads a Server-Sent Events stream as the WHATWG HTML standard (9.2) describes it. It does no
// I/O: bytes go in through Write, in pieces of any size, and the events they complete come out.
package sse

import (
	"bytes"
	"strings"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

// Event is one dispatched event. Name is "message" when the stream gave none.
type Event struct {
	Name string
	Data string
}

// Parser holds what a stream has sent that is not yet an event. The zero value is ready.
type Parser struct {
	buf     []byte
	started bool
	name    string
	data    strings.Builder
	hasData bool
}

var bom = []byte{0xEF, 0xBB, 0xBF}

// Write adds bytes and returns the events they complete. A line may end at LF, CRLF or a lone CR; a CR at the
// end of the bytes so far waits for the next byte, in case an LF follows. A UTF-8 sequence split across
// writes is whole by the time its line is read. An event that is not complete when the stream ends is never
// dispatched, because Write is the only way out.
func (p *Parser) Write(b []byte) []Event {
	p.buf = append(p.buf, b...)
	if !p.started {
		if len(p.buf) < len(bom) && bytes.HasPrefix(bom, p.buf) {
			return nil
		}
		p.buf = bytes.TrimPrefix(p.buf, bom)
		p.started = true
	}
	var out []Event
	for {
		end := bytes.IndexAny(p.buf, "\r\n")
		if end < 0 {
			break
		}
		next := end + 1
		if p.buf[end] == '\r' {
			if next == len(p.buf) {
				break
			}
			if p.buf[next] == '\n' {
				next++
			}
		}
		line := p.buf[:end]
		if e, ok := p.line(string(jsonvalue.DecodeLossy(line))); ok {
			out = append(out, e)
		}
		p.buf = p.buf[next:]
	}
	if len(p.buf) == 0 {
		p.buf = nil
	}
	return out
}

func (p *Parser) line(l string) (Event, bool) {
	if l == "" {
		return p.dispatch()
	}
	if l[0] == ':' {
		return Event{}, false
	}
	field, value, found := strings.Cut(l, ":")
	if found {
		value = strings.TrimPrefix(value, " ")
	}
	switch field {
	case "event":
		p.name = value
	case "data":
		p.data.WriteString(value)
		p.data.WriteByte('\n')
		p.hasData = true
	}
	return Event{}, false
}

func (p *Parser) dispatch() (Event, bool) {
	name, data, has := p.name, p.data.String(), p.hasData
	p.name, p.hasData = "", false
	p.data.Reset()
	if !has {
		return Event{}, false
	}
	if name == "" {
		name = "message"
	}
	return Event{Name: name, Data: strings.TrimSuffix(data, "\n")}, true
}
