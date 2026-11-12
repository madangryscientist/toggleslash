package evaluator

import "strings"

// parseReference reads an attribute reference, a name that starts with '/': the leading '/' is dropped, the rest
// is split on '/', and in each component "~1" is '/' and "~0" is '~', in one left-to-right pass. It reports false
// when a component is empty or a '~' is followed by anything but '0' or '1', or ends the component.
func parseReference(text string) ([]string, bool) {
	var components []string
	for _, part := range strings.Split(text[1:], "/") {
		if part == "" {
			return nil, false
		}
		var b strings.Builder
		for i := 0; i < len(part); i++ {
			c := part[i]
			if c != '~' {
				b.WriteByte(c)
				continue
			}
			if i+1 >= len(part) {
				return nil, false
			}
			switch part[i+1] {
			case '0':
				b.WriteByte('~')
			case '1':
				b.WriteByte('/')
			default:
				return nil, false
			}
			i++
		}
		components = append(components, b.String())
	}
	return components, true
}
