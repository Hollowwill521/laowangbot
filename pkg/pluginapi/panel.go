package pluginapi

import (
	"html"
	"strings"
)

// PanelHTML formats only whole command lines. Descriptions are always escaped
// separately so punctuation, bot names and regex arguments remain copyable.
func PanelHTML(text, name, prefix string) string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "."+name+" ") {
			lines = append(lines, "<code>"+html.EscapeString(prefix+strings.TrimPrefix(line, "."))+"</code>")
		} else if strings.HasPrefix(line, "【") && strings.HasSuffix(line, "】") {
			lines = append(lines, "<b>"+html.EscapeString(line)+"</b>")
		} else {
			lines = append(lines, html.EscapeString(line))
		}
	}
	return strings.Join(lines, "\n")
}

// PanelPages packs complete HTML lines without splitting a copyable command.
// Panel generators keep commands short; oversized description lines are escaped
// text and can be split at rune/entity boundaries.
func PanelPages(text string) []string {
	const limit = 3500
	var pages []string
	page := ""
	for _, line := range strings.Split(text, "\n") {
		for len(line) > limit {
			if page != "" {
				pages = append(pages, page)
				page = ""
			}
			// Oversized plain descriptions are split before escaping again.
			raw := html.UnescapeString(line)
			var chunk strings.Builder
			consumed := 0
			for offset, r := range raw {
				token := html.EscapeString(string(r))
				if chunk.Len()+len(token) > limit {
					break
				}
				chunk.WriteString(token)
				consumed = offset + len(string(r))
			}
			pages = append(pages, chunk.String())
			line = html.EscapeString(raw[consumed:])
		}
		if len(page)+len(line)+1 > limit {
			pages = append(pages, page)
			page = ""
		}
		if page != "" {
			page += "\n"
		}
		page += line
	}
	if page != "" {
		pages = append(pages, page)
	}
	return pages
}
