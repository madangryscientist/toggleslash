package toggleslash

import "github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"

// sseEvent is one dispatched server-sent event. Only the event and data fields are read.
type sseEvent struct {
	name string
	data string
}

// sseParser reads a server-sent event stream as the WHATWG HTML standard describes: LF, CRLF and a lone CR end
// a line; a line starting with a colon is a comment; a field is split at the first colon and one leading space of
// its value is dropped; several data lines are joined with LF; an event with no data line is dropped; an event with
// no name is "message"; an event still being read when the stream ends is discarded. A leading byte order mark is
// skipped. Bytes may arrive split anywhere, including inside a CRLF or a BOM.
type sseParser struct {
	bomPos    int // how many bytes of the BOM have matched; 3 means the start of the stream is behind us
	pendingCR bool
	line      []byte
	name      string
	data      []byte
	hasData   bool
}

var utf8BOM = [3]byte{0xEF, 0xBB, 0xBF}

// feed adds bytes and returns the events they completed.
func (p *sseParser) feed(chunk []byte) []sseEvent {
	var out []sseEvent
	for _, b := range chunk {
		if p.bomPos < 3 {
			if b == utf8BOM[p.bomPos] {
				p.bomPos++
				continue
			}
			p.line = append(p.line, utf8BOM[:p.bomPos]...)
			p.bomPos = 3
		}
		if p.pendingCR {
			p.pendingCR = false
			if b == '\n' {
				continue
			}
		}
		switch b {
		case '\r':
			p.pendingCR = true
			out = p.endLine(out)
		case '\n':
			out = p.endLine(out)
		default:
			p.line = append(p.line, b)
		}
	}
	return out
}

func (p *sseParser) endLine(out []sseEvent) []sseEvent {
	line := p.line
	p.line = p.line[:0]
	if len(line) == 0 {
		if p.hasData {
			name := p.name
			if name == "" {
				name = "message"
			}
			data := p.data
			if n := len(data); n > 0 {
				data = data[:n-1] // the LF every data line added
			}
			out = append(out, sseEvent{name: name, data: jsonvalue.LossyUTF8(data)})
		}
		p.name = ""
		p.data = p.data[:0]
		p.hasData = false
		return out
	}
	if line[0] == ':' {
		return out
	}
	field, value := line, []byte(nil)
	for i, b := range line {
		if b == ':' {
			field, value = line[:i], line[i+1:]
			if len(value) > 0 && value[0] == ' ' {
				value = value[1:]
			}
			break
		}
	}
	switch string(field) {
	case "event":
		p.name = jsonvalue.LossyUTF8(value)
	case "data":
		p.data = append(p.data, value...)
		p.data = append(p.data, '\n')
		p.hasData = true
	}
	return out
}
