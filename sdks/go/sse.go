package toggleslash

import "github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"

// sseEvent is one dispatched server-sent event.
type sseEvent struct {
	name string
	data string
}

// sseParser reads a server-sent event stream as the WHATWG HTML standard does, a byte at a time: LF, CRLF and
// a lone CR end a line; a line starting with ":" is a comment; several data lines are joined with LF; an
// event with no data line is dropped; the name defaults to "message"; and an event still incomplete when the
// stream ends is never dispatched (the caller simply stops feeding). A line is decoded as UTF-8 once it is
// whole, so a character split across two reads is whole by then. There is no cap on a line or an event.
type sseParser struct {
	line    []byte
	skipLF  bool // the last byte was CR, so an LF right after it is the same line end
	started bool
	name    string
	data    []byte
	hasData bool
}

// feed adds bytes and calls emit for each event they complete.
func (p *sseParser) feed(chunk []byte, emit func(sseEvent)) {
	for _, b := range chunk {
		switch {
		case b == '\n' && p.skipLF:
			p.skipLF = false
		case b == '\n' || b == '\r':
			p.skipLF = b == '\r'
			p.endLine(emit)
		default:
			p.skipLF = false
			p.line = append(p.line, b)
		}
	}
}

func (p *sseParser) endLine(emit func(sseEvent)) {
	raw := p.line
	p.line = p.line[:0:0]
	if !p.started {
		p.started = true
		raw = jsonv.StripBOM(raw)
	}
	if len(raw) == 0 {
		p.dispatch(emit)
		return
	}
	if raw[0] == ':' {
		return
	}
	text := string(jsonv.DecodeUTF8(raw))
	field, value := text, ""
	for i := range len(text) {
		if text[i] == ':' {
			field, value = text[:i], text[i+1:]
			if len(value) > 0 && value[0] == ' ' {
				value = value[1:]
			}
			break
		}
	}
	switch field {
	case "event":
		p.name = value
	case "data":
		if p.hasData {
			p.data = append(p.data, '\n')
		}
		p.data = append(p.data, value...)
		p.hasData = true
	}
}

func (p *sseParser) dispatch(emit func(sseEvent)) {
	if p.hasData {
		name := p.name
		if name == "" {
			name = "message"
		}
		emit(sseEvent{name: name, data: string(p.data)})
	}
	p.name, p.data, p.hasData = "", nil, false
}
