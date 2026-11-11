package toggleslash

import (
	"bytes"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// sseEvent is one dispatched Server-Sent Event; only the event name and the data are read.
type sseEvent struct {
	Name string
	Data string
}

// sseParser is an incremental Server-Sent Events parser to the WHATWG rules. Feed it bytes in any chunking
// and it returns the events completed so far. A line ends at LF, CRLF or a lone CR (a CR ends the line at
// once, and an LF straight after it is skipped), a leading byte order mark is skipped, a line that starts
// with ':' is a comment, the field is split at the first ':' with one leading space of the value removed,
// 'data' lines join with LF, the name defaults to "message", an event with no 'data' line is dropped, and
// an event still incomplete when the stream ends is never dispatched. Lines are assembled as bytes and
// decoded as UTF-8 only whole, so a character split across reads is safe.
type sseParser struct {
	head    []byte // the first bytes of the stream, held while they might be a byte order mark
	started bool
	line    []byte
	skipLF  bool // the previous line ended with CR
	name    string
	data    []byte
	hasData bool
}

var byteOrderMark = []byte{0xEF, 0xBB, 0xBF}

// Feed adds bytes and returns the events they completed.
func (p *sseParser) Feed(b []byte) []sseEvent {
	if !p.started {
		p.head = append(p.head, b...)
		if len(p.head) < len(byteOrderMark) && bytes.HasPrefix(byteOrderMark, p.head) {
			return nil
		}
		p.started = true
		b = bytes.TrimPrefix(p.head, byteOrderMark)
		p.head = nil
	}
	var events []sseEvent
	for _, ch := range b {
		if p.skipLF {
			p.skipLF = false
			if ch == '\n' {
				continue
			}
		}
		switch ch {
		case '\r':
			p.skipLF = true
			events = p.endLine(events)
		case '\n':
			events = p.endLine(events)
		default:
			p.line = append(p.line, ch)
		}
	}
	return events
}

func (p *sseParser) endLine(events []sseEvent) []sseEvent {
	line := p.line
	p.line = p.line[:0]
	if len(line) == 0 {
		return p.dispatch(events)
	}
	if line[0] == ':' {
		return events
	}
	field, value := line, []byte(nil)
	if i := bytes.IndexByte(line, ':'); i >= 0 {
		field, value = line[:i], line[i+1:]
		if len(value) > 0 && value[0] == ' ' {
			value = value[1:]
		}
	}
	switch string(field) {
	case "event":
		p.name = string(jsonv.DecodeUTF8(value))
	case "data":
		p.data = append(p.data, jsonv.DecodeUTF8(value)...)
		p.data = append(p.data, '\n')
		p.hasData = true
	}
	return events
}

func (p *sseParser) dispatch(events []sseEvent) []sseEvent {
	name, data, hasData := p.name, p.data, p.hasData
	p.name, p.data, p.hasData = "", nil, false
	if !hasData {
		return events
	}
	if name == "" {
		name = "message"
	}
	return append(events, sseEvent{Name: name, Data: string(bytes.TrimSuffix(data, []byte{'\n'}))})
}
