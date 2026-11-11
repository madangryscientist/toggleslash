package toggleslash

// sseParser reads Server-Sent Events as the WHATWG standard does. It is fed whatever chunks the network
// gives, and a chunk can end anywhere, inside a line, a code point or between a CR and its LF. It works on
// bytes: the only fields it reads are ASCII, and the data is handed on as bytes for the JSON reader, so a
// split UTF-8 sequence is never broken.
type sseParser struct {
	emit func(event string, data []byte)

	line    []byte
	sawCR   bool
	started bool

	name    []byte
	data    []byte
	hasData bool
}

func (p *sseParser) feed(chunk []byte) {
	for _, c := range chunk {
		if p.sawCR {
			p.sawCR = false
			if c == '\n' {
				continue
			}
		}
		switch c {
		case '\r':
			p.sawCR = true
			p.endLine()
		case '\n':
			p.endLine()
		default:
			p.line = append(p.line, c)
		}
	}
}

func (p *sseParser) endLine() {
	line := p.line
	p.line = p.line[:0]
	if !p.started {
		p.started = true
		if len(line) >= 3 && line[0] == 0xEF && line[1] == 0xBB && line[2] == 0xBF {
			line = line[3:]
		}
	}
	if len(line) == 0 {
		p.dispatch()
		return
	}
	if line[0] == ':' {
		return
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
		p.name = append(p.name[:0], value...)
	case "data":
		p.data = append(p.data, value...)
		p.data = append(p.data, '\n')
		p.hasData = true
	}
}

func (p *sseParser) dispatch() {
	name, data, has := string(p.name), p.data, p.hasData
	if has && len(data) > 0 {
		data = data[:len(data)-1]
	}
	p.name, p.data, p.hasData = p.name[:0], p.data[:0], false
	if !has {
		return
	}
	if name == "" {
		name = "message"
	}
	p.emit(name, data)
}
