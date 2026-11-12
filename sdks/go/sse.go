package toggleslash

import (
	"strings"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonx"
)

// A server-sent events parser, to the WHATWG rules the contract names: lines end on LF, CRLF or a lone CR
// (split across reads included), a line starting with ":" is a comment, one leading space of a value is
// dropped, several data lines are joined with LF, an event with no data line is dropped, and an event still
// incomplete when the stream ends is never returned.

type sseEvent struct {
	name string
	data string
}

type sseParser struct {
	buffer  []byte
	started bool
	skipLF  bool
	name    string
	data    []string
}

func (p *sseParser) feed(chunk []byte) []sseEvent {
	buffer := append(p.buffer, chunk...)
	if len(buffer) == 0 {
		p.buffer = buffer
		return nil
	}
	if p.skipLF {
		p.skipLF = false
		if buffer[0] == '\n' {
			buffer = buffer[1:]
		}
	}
	var events []sseEvent
	start := 0
	for start < len(buffer) {
		end := start
		for end < len(buffer) && buffer[end] != '\r' && buffer[end] != '\n' {
			end++
		}
		if end == len(buffer) {
			break
		}
		after := end + 1
		if buffer[end] == '\r' {
			switch {
			case end+1 == len(buffer):
				// The CR ended its line; an LF in the next read is the same line end.
				p.skipLF = true
			case buffer[end+1] == '\n':
				after = end + 2
			}
		}
		p.line(p.decode(buffer[start:end]), &events)
		start = after
	}
	p.buffer = append(p.buffer[:0], buffer[start:]...)
	return events
}

// decode reads one line's bytes. The stream's first line may start with a byte order mark, which is skipped.
func (p *sseParser) decode(line []byte) string {
	text := jsonx.DecodeStream(line)
	if !p.started && text != "" {
		p.started = true
		text = strings.TrimPrefix(text, "\uFEFF")
	}
	return text
}

func (p *sseParser) line(text string, events *[]sseEvent) {
	if text == "" {
		if len(p.data) > 0 {
			name := p.name
			if name == "" {
				name = "message"
			}
			*events = append(*events, sseEvent{name: name, data: strings.Join(p.data, "\n")})
		}
		p.name = ""
		p.data = nil
		return
	}
	if strings.HasPrefix(text, ":") {
		return
	}
	field, value, found := strings.Cut(text, ":")
	if found {
		value = strings.TrimPrefix(value, " ")
	}
	switch field {
	case "event":
		p.name = value
	case "data":
		p.data = append(p.data, value)
	}
}
