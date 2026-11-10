package toggleslash

import "bytes"

// sseEvent is one dispatched Server-Sent Event.
type sseEvent struct {
	name string
	data string
}

// sseParser reads a Server-Sent Events byte stream to the WHATWG rules the contract names. It works on
// bytes: a UTF-8 sequence never holds a CR or LF byte, so a character split across two reads is whole by the
// time its line is complete. LF, CRLF and a lone CR end a line (a CR at the end of one read is remembered, so
// the LF that starts the next read is not a second line end); a line that starts with ":" is a comment; a
// field with no ":" has an empty value; one leading space of a value is dropped; a leading BOM is dropped;
// several "data" lines join with LF; an event with no "data" line is dropped; the name is "message" when none
// was given; an event still open when the stream ends is never delivered.
type sseParser struct {
	line      []byte
	afterCR   bool
	started   bool
	name      string
	data      []byte
	hasData   bool
	bomPrefix []byte
}

var bom = []byte{0xEF, 0xBB, 0xBF}

// feed adds bytes and returns the events they complete.
func (p *sseParser) feed(chunk []byte) []sseEvent {
	if !p.started {
		// A BOM may arrive split; hold back a prefix of it until it is known to be one.
		chunk = append(p.bomPrefix, chunk...)
		p.bomPrefix = nil
		if len(chunk) < len(bom) && bytes.HasPrefix(bom, chunk) {
			p.bomPrefix = append([]byte(nil), chunk...)
			return nil
		}
		p.started = true
		chunk = bytes.TrimPrefix(chunk, bom)
	}
	var events []sseEvent
	for _, b := range chunk {
		switch {
		case b == '\n' && p.afterCR:
			p.afterCR = false
		case b == '\n' || b == '\r':
			p.afterCR = b == '\r'
			if event, ok := p.endLine(); ok {
				events = append(events, event)
			}
		default:
			p.afterCR = false
			p.line = append(p.line, b)
		}
	}
	return events
}

func (p *sseParser) endLine() (sseEvent, bool) {
	line := p.line
	p.line = p.line[:0]
	if len(line) == 0 {
		return p.dispatch()
	}
	if line[0] == ':' {
		return sseEvent{}, false
	}
	field, value, _ := bytes.Cut(line, []byte{':'})
	value = bytes.TrimPrefix(value, []byte{' '})
	switch string(field) {
	case "event":
		p.name = string(value)
	case "data":
		if p.hasData {
			p.data = append(p.data, '\n')
		}
		p.data = append(p.data, value...)
		p.hasData = true
	}
	return sseEvent{}, false
}

func (p *sseParser) dispatch() (sseEvent, bool) {
	name, data, hasData := p.name, p.data, p.hasData
	p.name, p.data, p.hasData = "", nil, false
	if !hasData {
		return sseEvent{}, false
	}
	if name == "" {
		name = "message"
	}
	return sseEvent{name: name, data: string(data)}, true
}
