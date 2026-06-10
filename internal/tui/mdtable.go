package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
)

// Markdown tables are rendered here with lipgloss directly rather than by
// glamour: glamour never enables BorderRow on its underlying lipgloss table
// and exposes no style option for it, so multi-line rows run together with
// no separator. Inline cell content (code spans, bold) is styled with a
// plain string scan — running each cell through glamour is far too slow.

// docMargin mirrors glamour's standard-style document margin so tables align
// and wrap identically to glamour-rendered blocks around them.
const docMargin = 2

// mdSegment is a run of markdown lines, either one table block or the
// content between table blocks.
type mdSegment struct {
	table bool
	text  string
}

// splitTableSegments splits markdown into table and non-table segments. A
// table block is a `|` row followed by a delimiter row plus any further `|`
// rows. Lines inside code fences are never treated as table rows.
func splitTableSegments(md string) []mdSegment {
	lines := strings.Split(md, "\n")
	var segs []mdSegment
	plainStart := 0
	inFence := false

	i := 0
	for i < len(lines) {
		t := strings.TrimSpace(lines[i])
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
			i++
			continue
		}
		if inFence || !isTableRow(lines[i]) || i+1 >= len(lines) || !isTableDelimiter(lines[i+1]) {
			i++
			continue
		}
		if i > plainStart {
			segs = append(segs, mdSegment{text: strings.Join(lines[plainStart:i], "\n")})
		}
		end := i + 2
		for end < len(lines) && isTableRow(lines[end]) {
			end++
		}
		segs = append(segs, mdSegment{table: true, text: strings.Join(lines[i:end], "\n")})
		i = end
		plainStart = end
	}
	if plainStart < len(lines) {
		segs = append(segs, mdSegment{text: strings.Join(lines[plainStart:], "\n")})
	}
	return segs
}

func isTableRow(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), "|")
}

func isTableDelimiter(s string) bool {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "|") || !strings.Contains(t, "-") {
		return false
	}
	for _, r := range t {
		switch r {
		case '|', '-', ':', ' ', '\t':
		default:
			return false
		}
	}
	return true
}

func splitCells(line string) []string {
	t := strings.TrimSpace(line)
	t = strings.TrimPrefix(t, "|")
	t = strings.TrimSuffix(t, "|")
	cells := strings.Split(t, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

type cellAlign int

const (
	alignNone cellAlign = iota
	alignLeft
	alignCenter
	alignRight
)

func parseAlignments(delim string) []cellAlign {
	cells := splitCells(delim)
	aligns := make([]cellAlign, len(cells))
	for i, c := range cells {
		left := strings.HasPrefix(c, ":")
		right := strings.HasSuffix(c, ":")
		switch {
		case left && right:
			aligns[i] = alignCenter
		case left:
			aligns[i] = alignLeft
		case right:
			aligns[i] = alignRight
		}
	}
	return aligns
}

// renderTableBlock renders one markdown table block as terminal output with
// separator lines between rows, indented to match glamour's document margin.
func (g *GlamourRenderer) renderTableBlock(block string, width int) string {
	var lines []string
	for _, l := range strings.Split(block, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) < 2 {
		return block
	}

	aligns := parseAlignments(lines[1])
	tbl := table.New().
		Width(width - 2*docMargin).
		BorderStyle(g.border).
		BorderTop(false).
		BorderLeft(false).
		BorderRight(false).
		BorderBottom(false).
		BorderRow(true).
		StyleFunc(func(_, col int) lipgloss.Style {
			st := lipgloss.NewStyle().Inline(false).Margin(0, 1)
			if col < len(aligns) {
				switch aligns[col] {
				case alignLeft:
					st = st.Align(lipgloss.Left).PaddingRight(0)
				case alignCenter:
					st = st.Align(lipgloss.Center)
				case alignRight:
					st = st.Align(lipgloss.Right).PaddingLeft(0)
				case alignNone:
				}
			}
			return st
		}).
		Headers(g.styleCells(splitCells(lines[0]))...)

	for _, row := range lines[2:] {
		tbl.Row(g.styleCells(splitCells(row))...)
	}

	indent := strings.Repeat(" ", docMargin)
	var b strings.Builder
	for _, line := range strings.Split(tbl.String(), "\n") {
		b.WriteString(indent)
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func (g *GlamourRenderer) styleCells(cells []string) []string {
	out := make([]string, len(cells))
	for i, c := range cells {
		out[i] = styleCellInline(c, g.cellCode, g.cellBold)
	}
	return out
}

// styleCellInline styles `code` spans and **bold** runs in a cell. Unbalanced
// markers are left as literal text.
func styleCellInline(s string, code, bold lipgloss.Style) string {
	if !strings.ContainsAny(s, "`*") {
		return s
	}
	segs := strings.Split(s, "`")
	if len(segs)%2 == 0 {
		return styleBold(s, bold)
	}
	var b strings.Builder
	for i, seg := range segs {
		if i%2 == 1 {
			b.WriteString(code.Render(seg))
		} else {
			b.WriteString(styleBold(seg, bold))
		}
	}
	return b.String()
}

func styleBold(s string, bold lipgloss.Style) string {
	segs := strings.Split(s, "**")
	if len(segs)%2 == 0 {
		return s
	}
	var b strings.Builder
	for i, seg := range segs {
		if i%2 == 1 {
			b.WriteString(bold.Render(seg))
		} else {
			b.WriteString(seg)
		}
	}
	return b.String()
}
