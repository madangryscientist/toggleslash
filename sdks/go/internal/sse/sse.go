// Package sse reads Server-Sent Events the way the WHATWG standard says to.
//
// It works on bytes, so a UTF-8 sequence or a CR LF pair split across two
// reads is safe. Only the event and data fields are kept; every other field
// and every comment is dropped.
package sse

import "github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"

// Event is one dispatched event. Name is "message" when the stream named none.
type Event struct {
	Name string
	Data string
}

var bom = []byte{0xEF, 0xBB, 0xBF}

// Parser turns chunks of a stream into events. The zero value is ready.
type Parser struct {
	lead     []byte // the first bytes, held back while they may still be a byte order mark
	leadDone bool
	line     []byte
	crSeen   bool // the last byte was CR, so an LF next belongs to it
	name     string
	data     []byte
	hasData  bool
}

// Feed reads chunk and calls emit for each complete event. It stops early, and
// returns false, when emit returns false.
func (p *Parser) Feed(chunk []byte, emit func(Event) bool) bool {
	for _, b := range chunk {
		if !p.leadDone {
			if p.lead == nil {
				p.lead = make([]byte, 0, len(bom))
			}
			if b == bom[len(p.lead)] {
				p.lead = append(p.lead, b)
				if len(p.lead) == len(bom) {
					p.lead, p.leadDone = nil, true
				}
				continue
			}
			// Not a byte order mark after all: the held bytes are ordinary input.
			held := p.lead
			p.lead, p.leadDone = nil, true
			for _, h := range held {
				if !p.byte(h, emit) {
					return false
				}
			}
		}
		if !p.byte(b, emit) {
			return false
		}
	}
	return true
}

func (p *Parser) byte(b byte, emit func(Event) bool) bool {
	if p.crSeen {
		p.crSeen = false
		if b == '\n' {
			return true
		}
	}
	switch b {
	case '\r':
		p.crSeen = true
		return p.endLine(emit)
	case '\n':
		return p.endLine(emit)
	}
	p.line = append(p.line, b)
	return true
}

func (p *Parser) endLine(emit func(Event) bool) bool {
	line := p.line
	p.line = p.line[:0]
	if len(line) == 0 {
		return p.dispatch(emit)
	}
	if line[0] == ':' {
		return true
	}
	field, value := line, []byte(nil)
	for i, c := range line {
		if c == ':' {
			field, value = line[:i], line[i+1:]
			if len(value) > 0 && value[0] == ' ' {
				value = value[1:]
			}
			break
		}
	}
	switch string(field) {
	case "event":
		p.name = wtf8.DecodeUTF8(value)
	case "data":
		if p.hasData {
			p.data = append(p.data, '\n')
		}
		p.data = append(p.data, value...)
		p.hasData = true
	}
	return true
}

func (p *Parser) dispatch(emit func(Event) bool) bool {
	name, data, has := p.name, p.data, p.hasData
	p.name, p.hasData = "", false
	p.data = p.data[:0]
	if !has {
		return true
	}
	if name == "" {
		name = "message"
	}
	// Decode once over the whole data, so a sequence cut by a newline cannot join.
	return emit(Event{Name: name, Data: wtf8.DecodeUTF8(data)})
}
