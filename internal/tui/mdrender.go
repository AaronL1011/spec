package tui

import "strings"

// stripHTMLComments removes <!-- ... --> markers from content.
func stripHTMLComments(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for {
		start := strings.Index(s, "<!--")
		if start < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:start])
		end := strings.Index(s[start:], "-->")
		if end < 0 {
			break // unterminated comment — stop
		}
		s = s[start+end+3:]
	}
	return b.String()
}
