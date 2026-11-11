package evaluator

import "strings"

// parseReference reads an attribute reference, to "Reading the attribute" in docs/evaluation.md: a name that
// starts with "/". The components are what is left after dropping the leading "/", splitting on "/" and
// decoding "~1" as "/" and "~0" as "~" in one left-to-right pass. ok is false when a component is empty or a
// "~" is followed by anything but 0 or 1, or ends the component. Every character involved is ASCII, so the
// bytes of a WTF-8 string can be walked directly.
func parseReference(text string) (components []string, ok bool) {
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
