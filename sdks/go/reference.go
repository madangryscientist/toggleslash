package toggleslash

import "strings"

// An attribute reference: a name that starts with "/" (docs/evaluation.md, "Reading the attribute").

// parseReference returns the components of a "/" reference, or false when a component is empty or a "~" is
// not "~0" or "~1".
func parseReference(text string) ([]string, bool) {
	parts := strings.Split(text[1:], "/")
	components := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			return nil, false
		}
		var decoded strings.Builder
		for i := 0; i < len(part); {
			c := part[i]
			if c != '~' {
				decoded.WriteByte(c)
				i++
				continue
			}
			if i+1 >= len(part) {
				return nil, false
			}
			switch part[i+1] {
			case '0':
				decoded.WriteByte('~')
			case '1':
				decoded.WriteByte('/')
			default:
				return nil, false
			}
			i += 2
		}
		components = append(components, decoded.String())
	}
	return components, true
}
