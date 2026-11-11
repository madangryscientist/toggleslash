package evaluator

import "strings"

// parseReference reads an attribute reference: a name that starts with "/".
// The components are what is left after dropping that "/", splitting on "/" and
// decoding "~1" as "/" and "~0" as "~" in one left-to-right pass. It reports
// false when a component is empty or a "~" is followed by anything but 0 or 1
// (or ends the component).
func parseReference(text string) ([]string, bool) {
	var components []string
	for _, part := range strings.Split(text[1:], "/") {
		if part == "" {
			return nil, false
		}
		var decoded strings.Builder
		for i := 0; i < len(part); i++ {
			if part[i] != '~' {
				decoded.WriteByte(part[i])
				continue
			}
			i++
			switch {
			case i < len(part) && part[i] == '0':
				decoded.WriteByte('~')
			case i < len(part) && part[i] == '1':
				decoded.WriteByte('/')
			default:
				return nil, false
			}
		}
		components = append(components, decoded.String())
	}
	return components, true
}
