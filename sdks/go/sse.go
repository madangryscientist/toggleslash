package toggleslash

import (
	"bytes"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// sseParser reads Server-Sent Events the way the WHATWG standard does, on bytes, so a read may end anywhere:
// inside a UTF-8 sequence, between a CR and its LF, or inside the byte order mark. Only the "event" and
// "data" fields are read.
type sseParser struct {
	head      []byte // the first bytes, until the byte order mark question is settled
	headDone  bool
	line      []byte
	pendingCR bool // the last byte was CR: an LF next belongs to it

	name    string
	data    []byte
	hasData bool

	emit func(name, data string)
}

func newSSEParser(emit func(name, data string)) *sseParser { return &sseParser{emit: emit} }

// feed passes one read to the parser. emit is called for each complete event, in order.
func (p *sseParser) feed(chunk []byte) {
	for _, b := range chunk {
		if !p.headDone {
			p.head = append(p.head, b)
			if !bytes.HasPrefix(utf8BOM, p.head) {
				p.headDone = true
				buffered := p.head
				p.head = nil
				for _, h := range buffered {
					p.byte(h)
				}
			} else if len(p.head) == len(utf8BOM) {
				p.headDone = true
				p.head = nil
			}
			continue
		}
		p.byte(b)
	}
}

func (p *sseParser) byte(b byte) {
	if p.pendingCR {
		p.pendingCR = false
		if b == '\n' {
			return
		}
	}
	switch b {
	case '\r':
		p.endLine()
		p.pendingCR = true
	case '\n':
		p.endLine()
	default:
		p.line = append(p.line, b)
	}
}

func (p *sseParser) endLine() {
	line := p.line
	p.line = p.line[:0]
	if len(line) == 0 {
		p.dispatch()
		return
	}
	if line[0] == ':' {
		return
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
		p.name = wtf8.DecodeLossy(value)
	case "data":
		if p.hasData {
			p.data = append(p.data, '\n')
		}
		p.data = append(p.data, wtf8.DecodeLossy(value)...)
		p.hasData = true
	}
}

func (p *sseParser) dispatch() {
	name, data, ok := p.name, string(p.data), p.hasData
	p.name, p.data, p.hasData = "", p.data[:0], false
	if !ok {
		return
	}
	if name == "" {
		name = "message"
	}
	p.emit(name, data)
}
