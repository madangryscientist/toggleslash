package toggleslash

import "github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"

// sseEvent is one dispatched Server-Sent Event. Only the event name and the data are kept.
type sseEvent struct {
	name string
	data string
}

// sseParser reads Server-Sent Events as the WHATWG standard does, from raw bytes in whatever pieces they arrive.
// LF, CRLF and a lone CR end a line; a line that starts with ':' is a comment; `event` and `data` are the fields
// read, several `data` lines are joined with LF, an event with no `data` line is dropped, an event with no name
// is named "message", and an event still incomplete when the bytes stop is never dispatched. A leading UTF-8 byte
// order mark is skipped. It works on bytes and decodes a finished field once, so a UTF-8 sequence cut between
// two reads is whole by then.
type sseParser struct {
	line    []byte
	skipLF  bool // the last byte was a CR, so an LF straight after it belongs to the same line end
	bomSeen int  // how much of the byte order mark has matched; 3 once the start of the stream is behind us
	held    []byte

	name    []byte
	data    []byte
	hasData bool
}

var utf8BOM = [3]byte{0xEF, 0xBB, 0xBF}

// feed adds bytes and returns the events they completed.
func (p *sseParser) feed(chunk []byte) []sseEvent {
	var events []sseEvent
	for _, b := range chunk {
		if p.bomSeen < len(utf8BOM) {
			if b == utf8BOM[p.bomSeen] {
				p.held = append(p.held, b)
				p.bomSeen++
				if p.bomSeen == len(utf8BOM) {
					p.held = nil
				}
				continue
			}
			held := p.held
			p.held = nil
			p.bomSeen = len(utf8BOM)
			for _, h := range held {
				events = p.byte(events, h)
			}
		}
		events = p.byte(events, b)
	}
	return events
}

func (p *sseParser) byte(events []sseEvent, b byte) []sseEvent {
	if p.skipLF {
		p.skipLF = false
		if b == '\n' {
			return events
		}
	}
	switch b {
	case '\r':
		p.skipLF = true
		return p.endLine(events)
	case '\n':
		return p.endLine(events)
	}
	p.line = append(p.line, b)
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
	for i, c := range line {
		if c == ':' {
			field, value = line[:i], line[i+1:]
			break
		}
	}
	if len(value) > 0 && value[0] == ' ' {
		value = value[1:]
	}
	switch string(field) {
	case "event":
		p.name = append(p.name[:0], value...)
	case "data":
		p.data = append(p.data, value...)
		p.data = append(p.data, '\n')
		p.hasData = true
	}
	return events
}

func (p *sseParser) dispatch(events []sseEvent) []sseEvent {
	defer func() {
		p.name = p.name[:0]
		p.data = p.data[:0]
		p.hasData = false
	}()
	if !p.hasData {
		return events
	}
	data := p.data[:len(p.data)-1] // the LF the last data line added
	name := "message"
	if len(p.name) > 0 {
		name = wtf8.DecodeWHATWG(p.name)
	}
	return append(events, sseEvent{name: name, data: wtf8.DecodeWHATWG(data)})
}
