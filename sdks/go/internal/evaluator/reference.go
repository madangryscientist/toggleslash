package evaluator

import "strings"

// parseReference reads an attribute name that starts with "/" (to "Reading the attribute" in
// docs/evaluation.md): split on "/" after the first, decode "~1" as "/" and "~0" as "~" in one pass. It
// reports false when a component is empty or a "~" is followed by anything but 0 or 1, or ends the component.
func parseReference(text string) ([]string, bool) {
	parts := strings.Split(text[1:], "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			return nil, false
		}
		if strings.IndexByte(part, '~') < 0 {
			out = append(out, part)
			continue
		}
		var b strings.Builder
		for i := 0; i < len(part); i++ {
			if part[i] != '~' {
				b.WriteByte(part[i])
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
		out = append(out, b.String())
	}
	return out, true
}
