package toggleslash

import "strings"

// sseParser reads Server-Sent Events the way the WHATWG standard does: a line ends on LF, CRLF or a lone
// CR (and a CR at the end of one read waits for the next byte), a line starting with ":" is a comment, an
// event with no data line is dropped, and an event still incomplete when the stream ends is discarded.
// Only the "event" and "data" fields are read.
type sseParser struct {
	started bool
	head    []byte // the first bytes, held back until a byte order mark is ruled in or out
	line    []byte
	sawCR   bool
	name    string
	data    strings.Builder
	hasData bool
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// feed adds a chunk and calls emit for each complete event.
func (p *sseParser) feed(chunk []byte, emit func(name, data string)) {
	if !p.started {
		p.head = append(p.head, chunk...)
		if len(p.head) < len(utf8BOM) && string(p.head) == string(utf8BOM[:len(p.head)]) {
			return // could still be a byte order mark
		}
		p.started = true
		chunk = p.head
		p.head = nil
		if len(chunk) >= len(utf8BOM) && string(chunk[:len(utf8BOM)]) == string(utf8BOM) {
			chunk = chunk[len(utf8BOM):]
		}
	}
	for _, b := range chunk {
		if p.sawCR {
			p.sawCR = false
			if b == '\n' {
				continue
			}
		}
		switch b {
		case '\r':
			p.sawCR = true
			p.endLine(emit)
		case '\n':
			p.endLine(emit)
		default:
			p.line = append(p.line, b)
		}
	}
}

func (p *sseParser) endLine(emit func(name, data string)) {
	line := p.line
	p.line = p.line[:0]
	if len(line) == 0 {
		if p.hasData {
			name := p.name
			if name == "" {
				name = "message"
			}
			data := p.data.String()
			data = strings.TrimSuffix(data, "\n")
			p.reset()
			emit(name, data)
			return
		}
		p.reset()
		return
	}
	if line[0] == ':' {
		return
	}
	field, value := string(line), ""
	if i := strings.IndexByte(field, ':'); i >= 0 {
		field, value = field[:i], field[i+1:]
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
}

func (p *sseParser) reset() {
	p.name = ""
	p.data.Reset()
	p.hasData = false
}
