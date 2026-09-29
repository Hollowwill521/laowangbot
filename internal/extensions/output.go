package extensions

import "context"

// writePluginText preserves plain text while delivering long plugin responses.
func writePluginText(ctx context.Context, text string, edit, reply func(string) error) error {
	start, units, first := 0, 0, true
	send := func(page string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if first {
			first = false
			return edit(page)
		}
		return reply(page)
	}
	for offset, r := range text {
		size := 1
		if r > 0xffff {
			size = 2
		}
		if units+size > 4096 {
			if err := send(text[start:offset]); err != nil {
				return err
			}
			start, units = offset, 0
		}
		units += size
	}
	return send(text[start:])
}
