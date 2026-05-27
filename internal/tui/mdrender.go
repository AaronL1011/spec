package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// mdRenderer applies lightweight ANSI styling to markdown content.
// It processes line-by-line without a full AST parser — intentionally
// simple and fast, avoiding the ~180ms glamour overhead on large specs.
type mdRenderer struct {
	width  int
	styles mdStyles
}

type mdStyles struct {
	heading  lipgloss.Style
	bold     lipgloss.Style
	italic   lipgloss.Style
	code     lipgloss.Style
	listDot  lipgloss.Style
	quote    lipgloss.Style
	hrule    lipgloss.Style
	link     lipgloss.Style
	text     lipgloss.Style
	taskDone lipgloss.Style
	taskOpen lipgloss.Style
}

// newMDRenderer creates a markdown renderer configured for the given
// theme and terminal width.
func newMDRenderer(theme Theme, width int) *mdRenderer {
	contentWidth := width - 6
	if contentWidth < 30 {
		contentWidth = 30
	}

	return &mdRenderer{
		width: contentWidth,
		styles: mdStyles{
			heading:  lipgloss.NewStyle().Foreground(theme.Accent).Bold(true),
			bold:     lipgloss.NewStyle().Foreground(theme.Text).Bold(true),
			italic:   lipgloss.NewStyle().Foreground(theme.SubText).Italic(true),
			code:     lipgloss.NewStyle().Foreground(theme.Accent).Background(theme.Surface),
			listDot:  lipgloss.NewStyle().Foreground(theme.Accent),
			quote:    lipgloss.NewStyle().Foreground(theme.Muted),
			hrule:    lipgloss.NewStyle().Foreground(theme.Muted),
			link:     lipgloss.NewStyle().Foreground(theme.Accent).Underline(true),
			text:     lipgloss.NewStyle().Foreground(theme.Text),
			taskDone: lipgloss.NewStyle().Foreground(theme.Success),
			taskOpen: lipgloss.NewStyle().Foreground(theme.Muted),
		},
	}
}

// render converts markdown content to styled terminal output lines.
func (r *mdRenderer) render(content string) []string {
	if content == "" {
		return nil
	}

	content = stripHTMLComments(content)
	raw := strings.Split(content, "\n")

	var out []string
	inCodeBlock := false
	inTable := false

	for _, line := range raw {
		trimmed := strings.TrimSpace(line)

		// Fenced code blocks.
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			if inCodeBlock {
				out = append(out, "")
			} else {
				out = append(out, "")
			}
			continue
		}
		if inCodeBlock {
			out = append(out, r.styles.code.Render(line))
			continue
		}

		// Blank lines.
		if trimmed == "" {
			if inTable {
				inTable = false
			}
			out = append(out, "")
			continue
		}

		// Headings.
		if strings.HasPrefix(trimmed, "#") {
			text := strings.TrimLeft(trimmed, "# ")
			out = append(out, "")
			out = append(out, r.styles.heading.Render(text))
			continue
		}

		// Horizontal rules.
		if isHRule(trimmed) {
			out = append(out, r.styles.hrule.Render(strings.Repeat("─", min(r.width, 40))))
			continue
		}

		// Blockquotes.
		if strings.HasPrefix(trimmed, ">") {
			text := strings.TrimPrefix(trimmed, ">")
			text = strings.TrimPrefix(text, " ")
			out = append(out, r.styles.quote.Render("│ "+r.applyInline(text)))
			continue
		}

		// Task lists.
		if strings.HasPrefix(trimmed, "- [x]") || strings.HasPrefix(trimmed, "- [X]") {
			text := strings.TrimSpace(trimmed[5:])
			out = append(out, r.styles.taskDone.Render("  ✓ "+r.applyInline(text)))
			continue
		}
		if strings.HasPrefix(trimmed, "- [ ]") {
			text := strings.TrimSpace(trimmed[5:])
			out = append(out, r.styles.taskOpen.Render("  ○ "+r.applyInline(text)))
			continue
		}

		// Unordered lists.
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			indent := leadingSpaces(line)
			pad := strings.Repeat(" ", indent)
			text := strings.TrimSpace(trimmed[2:])
			out = append(out, pad+r.styles.listDot.Render("• ")+r.applyInline(text))
			continue
		}

		// Ordered lists.
		if idx := orderedListPrefix(trimmed); idx > 0 {
			indent := leadingSpaces(line)
			pad := strings.Repeat(" ", indent)
			rest := trimmed[idx:]
			numStr := trimmed[:idx-2] // digits before ". "
			out = append(out, pad+r.styles.listDot.Render(numStr+". ")+r.applyInline(rest))
			continue
		}

		// Table rows.
		if strings.HasPrefix(trimmed, "|") {
			inTable = true
			// Skip separator rows (|---|---|).
			if isTableSeparator(trimmed) {
				continue
			}
			out = append(out, r.renderTableRow(trimmed))
			continue
		}

		// Indented continuation (sub-list content, etc.).
		if leadingSpaces(line) >= 2 {
			pad := strings.Repeat(" ", leadingSpaces(line))
			out = append(out, pad+r.applyInline(trimmed))
			continue
		}

		// Regular paragraph text — apply inline formatting.
		out = append(out, r.applyInline(trimmed))
	}

	return out
}

// applyInline handles **bold**, *italic*, `code`, and [links](url) within a line.
func (r *mdRenderer) applyInline(s string) string {
	var b strings.Builder
	b.Grow(len(s) * 2)
	i := 0
	n := len(s)

	for i < n {
		// Bold: **text**
		if i+1 < n && s[i] == '*' && s[i+1] == '*' {
			if end := strings.Index(s[i+2:], "**"); end >= 0 {
				b.WriteString(r.styles.bold.Render(s[i+2 : i+2+end]))
				i = i + 2 + end + 2
				continue
			}
		}

		// Italic: *text*
		if s[i] == '*' && (i == 0 || s[i-1] == ' ') {
			if end := strings.IndexByte(s[i+1:], '*'); end >= 0 && end > 0 {
				b.WriteString(r.styles.italic.Render(s[i+1 : i+1+end]))
				i = i + 1 + end + 1
				continue
			}
		}

		// Inline code: `text`
		if s[i] == '`' {
			if end := strings.IndexByte(s[i+1:], '`'); end >= 0 {
				b.WriteString(r.styles.code.Render(s[i+1 : i+1+end]))
				i = i + 1 + end + 1
				continue
			}
		}

		// Links: [text](url)
		if s[i] == '[' {
			if closing := strings.IndexByte(s[i:], ']'); closing > 1 {
				after := i + closing + 1
				if after < n && s[after] == '(' {
					if pclose := strings.IndexByte(s[after:], ')'); pclose > 1 {
						linkText := s[i+1 : i+closing]
						b.WriteString(r.styles.link.Render(linkText))
						i = after + pclose + 1
						continue
					}
				}
			}
		}

		b.WriteByte(s[i])
		i++
	}

	return r.styles.text.Render(b.String())
}

// renderTableRow renders a pipe-delimited table row.
func (r *mdRenderer) renderTableRow(line string) string {
	cells := strings.Split(strings.Trim(line, "|"), "|")
	var parts []string
	for _, cell := range cells {
		parts = append(parts, strings.TrimSpace(cell))
	}
	return r.styles.text.Render("  " + strings.Join(parts, "  │  "))
}

// isHRule returns true for ---, ***, ___ (3+ chars).
func isHRule(s string) bool {
	if len(s) < 3 {
		return false
	}
	c := s[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	for _, r := range s {
		if r != rune(c) && r != ' ' {
			return false
		}
	}
	return true
}

// isTableSeparator returns true for rows like |---|---|.
func isTableSeparator(s string) bool {
	for _, c := range s {
		if c != '|' && c != '-' && c != ':' && c != ' ' {
			return false
		}
	}
	return true
}

// orderedListPrefix returns the length of "N. " prefix, or 0 if not a list item.
func orderedListPrefix(s string) int {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 || i >= len(s)-1 {
		return 0
	}
	if s[i] == '.' && i+1 < len(s) && s[i+1] == ' ' {
		return i + 2
	}
	return 0
}

// leadingSpaces counts the number of leading spaces.
func leadingSpaces(s string) int {
	n := 0
	for _, c := range s {
		if c == ' ' {
			n++
		} else if c == '\t' {
			n += 4
		} else {
			break
		}
	}
	return n
}

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
