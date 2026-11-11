package toggleslash

import "github.com/madangryscientist/toggleslash/sdks/go/internal/wtf8"

// sseEvent is one dispatched Server-Sent Event: the name (message when none was given) and the data, which
// is the lines of its data fields joined with LF, scrubbed of invalid UTF-8.
type sseEvent struct {
	name string
	data string
}

// sseParser reads a Server-Sent Events stream as the WHATWG standard does, from byte chunks of any size:
// LF, CRLF and a lone CR end a line (a CR at the end of a chunk waits for a possible LF in the next), a line
// that starts with ":" is a comment, a field is split at the first ":" and one space after it is dropped,
// only `event` and `data` are read, an event with no data field is dropped, and one still incomplete when
// the stream ends is never dispatched (the caller simply stops feeding). A leading byte order mark is
// skipped, even when its bytes arrive in separate chunks.
type sseParser struct {
	head    []byte // the first bytes, held until they can be told from a byte order mark
	started bool
	skipLF  bool // the last chunk ended in CR
	line    []byte
	name    string
	data    []byte
	hasData bool
}

var byteOrderMark = []byte{0xEF, 0xBB, 0xBF}

// feed reads a chunk and calls emit for each event it completes. If emit returns false, feed stops at once
// and returns false.
func (p *sseParser) feed(chunk []byte, emit func(sseEvent) bool) bool {
	if !p.started {
		p.head = append(p.head, chunk...)
		n := min(len(p.head), len(byteOrderMark))
		if string(p.head[:n]) == string(byteOrderMark[:n]) && len(p.head) < len(byteOrderMark) {
			return true // could still become a byte order mark
		}
		p.started = true
		chunk = p.head
		if string(chunk[:n]) == string(byteOrderMark) {
			chunk = chunk[n:]
		}
		p.head = nil
	}
	for _, b := range chunk {
		if p.skipLF {
			p.skipLF = false
			if b == '\n' {
				continue
			}
		}
		switch b {
		case '\n':
			if !p.endLine(emit) {
				return false
			}
		case '\r':
			p.skipLF = true
			if !p.endLine(emit) {
				return false
			}
		default:
			p.line = append(p.line, b)
		}
	}
	return true
}

func (p *sseParser) endLine(emit func(sseEvent) bool) bool {
	line := p.line
	p.line = p.line[:0]
	if len(line) == 0 {
		return p.dispatch(emit)
	}
	if line[0] == ':' {
		return true
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
		p.name = string(value)
	case "data":
		if p.hasData {
			p.data = append(p.data, '\n')
		}
		p.data = append(p.data, value...)
		p.hasData = true
	}
	return true
}

func (p *sseParser) dispatch(emit func(sseEvent) bool) bool {
	name, data, hasData := p.name, p.data, p.hasData
	p.name, p.data, p.hasData = "", nil, false
	if !hasData {
		return true
	}
	if name == "" {
		name = "message"
	}
	return emit(sseEvent{name: name, data: string(wtf8.Scrub(data))})
}
