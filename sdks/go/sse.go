package toggleslash

import (
	"errors"
	"strings"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonv"
)

// maxEventBytes bounds what the parser holds for one event, so a server that never ends a line cannot
// grow the SDK's memory without limit. The events the server sends are a tag long.
const maxEventBytes = 1 << 20

var errEventTooLarge = errors.New("the stream sent an event that is too large")

// sseEvent is one dispatched server-sent event.
type sseEvent struct {
	name string
	data string
}

// sseParser reads the WHATWG server-sent events format from bytes, in any chunking. Bytes are
// assembled into lines first and a line is decoded as a whole (one U+FFFD for each maximal invalid
// subpart), so a character split across two reads is whole when it is decoded.
type sseParser struct {
	line    []byte
	skipLF  bool // the last byte was a CR, so an LF right after it is the same line ending
	started bool // the first line has been read, so a byte order mark is no longer skipped
	name    string
	data    []string
	size    int
}

// feed adds a chunk and returns the events it completed.
func (p *sseParser) feed(chunk []byte) ([]sseEvent, error) {
	var out []sseEvent
	for _, b := range chunk {
		if p.skipLF {
			p.skipLF = false
			if b == '\n' {
				continue
			}
		}
		switch b {
		case '\n':
			if ev, ok := p.endLine(); ok {
				out = append(out, ev)
			}
		case '\r':
			p.skipLF = true
			if ev, ok := p.endLine(); ok {
				out = append(out, ev)
			}
		default:
			p.line = append(p.line, b)
			p.size++
			if p.size > maxEventBytes {
				return out, errEventTooLarge
			}
		}
	}
	return out, nil
}

// endLine handles one complete line and reports the event a blank line dispatches.
func (p *sseParser) endLine() (sseEvent, bool) {
	raw := p.line
	p.line = p.line[:0]
	if !p.started {
		p.started = true
		if len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF {
			raw = raw[3:]
		}
	}
	line := string(jsonv.DecodeUTF8(raw))
	if line == "" {
		ev := sseEvent{name: p.name, data: strings.Join(p.data, "\n")}
		dispatch := len(p.data) > 0
		p.name, p.data, p.size = "", nil, 0
		if !dispatch {
			return sseEvent{}, false
		}
		if ev.name == "" {
			ev.name = "message"
		}
		return ev, true
	}
	if line[0] == ':' {
		return sseEvent{}, false
	}
	field, value, found := strings.Cut(line, ":")
	if found {
		value = strings.TrimPrefix(value, " ")
	}
	switch field {
	case "event":
		p.name = value
	case "data":
		p.data = append(p.data, value)
	}
	return sseEvent{}, false
}
