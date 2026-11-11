package toggleslash

import (
	"bytes"
	"unicode/utf8"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// sseEvent is one dispatched Server-Sent Event.
type sseEvent struct {
	name string
	data string
}

// sseParser reads Server-Sent Events as the WHATWG standard does. It works on bytes, so UTF-8 and line
// endings split across reads are kept, and it only decodes a whole line.
type sseParser struct {
	line    []byte
	skipLF  bool // the last byte was CR, so an LF straight after it is part of the same line ending
	started bool
	event   string
	data    []byte
	hasData bool
}

func (p *sseParser) feed(chunk []byte) []sseEvent {
	var out []sseEvent
	for _, b := range chunk {
		if p.skipLF {
			p.skipLF = false
			if b == '\n' {
				continue
			}
		}
		if b == '\n' || b == '\r' {
			p.skipLF = b == '\r'
			if ev, ok := p.endLine(); ok {
				out = append(out, ev)
			}
			p.line = p.line[:0]
			continue
		}
		p.line = append(p.line, b)
	}
	return out
}

var bom = []byte{0xEF, 0xBB, 0xBF}

func (p *sseParser) endLine() (sseEvent, bool) {
	line := p.line
	if !p.started {
		p.started = true
		line = bytes.TrimPrefix(line, bom)
	}
	if len(line) == 0 {
		return p.dispatch()
	}
	if line[0] == ':' {
		return sseEvent{}, false
	}
	field, value := line, []byte(nil)
	if i := bytes.IndexByte(line, ':'); i >= 0 {
		field, value = line[:i], line[i+1:]
		value = bytes.TrimPrefix(value, []byte{' '})
	}
	switch string(field) {
	case "event":
		p.event = decodeLine(value)
	case "data":
		p.data = append(p.data, value...)
		p.data = append(p.data, '\n')
		p.hasData = true
	}
	return sseEvent{}, false
}

func (p *sseParser) dispatch() (sseEvent, bool) {
	name, data, has := p.event, p.data, p.hasData
	p.event, p.data, p.hasData = "", nil, false
	if !has {
		return sseEvent{}, false
	}
	data = bytes.TrimSuffix(data, []byte{'\n'})
	if name == "" {
		name = "message"
	}
	return sseEvent{name: name, data: decodeLine(data)}, true
}

// decodeLine turns bytes into a string, replacing invalid UTF-8 with U+FFFD per maximal invalid subpart.
func decodeLine(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	return wtf8.Repair(b)
}
