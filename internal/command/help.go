package command

import (
	"strings"
)

// PlainHelp formats explicit command examples in plain text, escaping all other
// content. Command arguments stay in the same code entity for one-tap copying.
func PlainHelp(text, prefix string, names ...string) string {
	var out strings.Builder
	for len(text) > 0 {
		start, token := -1, ""
		for _, name := range names {
			candidate := prefix + name
			if i := strings.Index(text, candidate); i >= 0 && (start < 0 || i < start || i == start && len(candidate) > len(token)) {
				start, token = i, candidate
			}
		}
		if start < 0 {
			out.WriteString(Escape(text))
			break
		}
		out.WriteString(Escape(text[:start]))
		text = text[start:]
		end := len(text)
		for _, separator := range []string{"\n", "：", "；", "。", "（", " — ", " / "} {
			if i := strings.Index(text, separator); i >= len(token) && i < end {
				end = i
			}
		}
		// A second command on the same line must be independently copyable.
		for _, name := range names {
			if i := strings.Index(text[len(token):], prefix+name); i >= 0 && i+len(token) < end {
				end = i + len(token)
			}
		}
		value := strings.TrimRight(text[:end], " \t")
		out.WriteString(Code(value))
		out.WriteString(Escape(text[len(value):end]))
		text = text[end:]
	}
	return out.String()
}
