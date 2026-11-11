package toggleslash

import (
	"bufio"
	"io"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"
)

// sseEvent is one dispatched Server-Sent Event: only the fields the SDK reads.
type sseEvent struct {
	name string
	data string
}

// sseParser reads Server-Sent Events as the WHATWG standard does. Lines end in LF, CR or CRLF; a
// line starting with a colon is a comment; one leading space of a value is removed; an event with
// no data line is dropped; an event still incomplete at the end of the input is discarded. It reads
// byte by byte from the reader it is given, so an event is dispatched when its blank line is read,
// not when the next read returns.
type sseParser struct {
	r       *bufio.Reader
	line    []byte
	skipLF  bool
	first   bool
	name    string
	data    []byte
	hasData bool
}

func newSSEParser(r io.Reader) *sseParser {
	return &sseParser{r: bufio.NewReader(r), first: true}
}

// next returns the next dispatched event, or the error that ended the input (io.EOF for a clean
// end).
func (p *sseParser) next() (sseEvent, error) {
	for {
		b, err := p.r.ReadByte()
		if err != nil {
			return sseEvent{}, err
		}
		if p.skipLF {
			p.skipLF = false
			if b == '\n' {
				continue
			}
		}
		switch b {
		case '\n':
		case '\r':
			p.skipLF = true
		default:
			p.line = append(p.line, b)
			continue
		}
		line := p.line
		p.line = p.line[:0]
		if p.first {
			p.first = false
			line = wtf8.StripBOM(line)
		}
		if ev, ok := p.handle(line); ok {
			return ev, nil
		}
	}
}

func (p *sseParser) handle(line []byte) (sseEvent, bool) {
	if len(line) == 0 {
		name, data, has := p.name, p.data, p.hasData
		p.name, p.data, p.hasData = "", p.data[:0], false
		if !has {
			return sseEvent{}, false
		}
		if name == "" {
			name = "message"
		}
		return sseEvent{name: name, data: string(wtf8.DecodeLossy(data))}, true
	}
	if line[0] == ':' {
		return sseEvent{}, false
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
		p.name = string(wtf8.DecodeLossy(value))
	case "data":
		if p.hasData {
			p.data = append(p.data, '\n')
		}
		p.data = append(p.data, value...)
		p.hasData = true
	}
	return sseEvent{}, false
}
