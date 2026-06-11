package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// Markdown tables are rendered here with lipgloss directly rather than by
// glamour: glamour never enables BorderRow on its underlying lipgloss table
// and exposes no style option for it, so multi-line rows run together with
// no separator. Inline cell content (code, emphasis, strikethrough, links)
// is styled by walking goldmark's inline AST — running each cell through
// glamour is far too slow.

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
		out[i] = styleCellInline(c, g.mdParser, g.cells)
	}
	return out
}

// inlineCtx carries the enclosing emphasis context down the AST walk so
// nested styles compose at the text leaves. Styling leaves (rather than
// wrapping container nodes) keeps an inner span's ANSI reset from
// terminating the outer style for text that follows it.
type inlineCtx struct {
	bold, italic, strike bool
}

// styleCellInline styles a cell's inline markdown (code spans, emphasis,
// strikethrough, links) by walking goldmark's inline AST. Cells whose parse
// yields no text (e.g. "#" parses as an empty heading) are shown verbatim.
func styleCellInline(s string, md goldmark.Markdown, st cellStyles) string {
	src := []byte(s)
	doc := md.Parser().Parse(text.NewReader(src))
	var b strings.Builder
	styleInlineAST(doc, src, &b, st, inlineCtx{})
	if b.Len() == 0 {
		return s
	}
	return b.String()
}

func styleInlineAST(n gast.Node, src []byte, b *strings.Builder, st cellStyles, ctx inlineCtx) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch c.Kind() {
		case gast.KindText:
			t := c.(*gast.Text)
			b.WriteString(styleLeaf(string(t.Value(src)), st, ctx))
			if t.SoftLineBreak() || t.HardLineBreak() {
				b.WriteByte(' ')
			}
		case gast.KindString:
			b.WriteString(styleLeaf(string(c.(*gast.String).Value), st, ctx))
		case gast.KindCodeSpan:
			b.WriteString(st.code.Render(plainText(c, src)))
		case gast.KindEmphasis:
			next := ctx
			if c.(*gast.Emphasis).Level == 2 {
				next.bold = true
			} else {
				next.italic = true
			}
			styleInlineAST(c, src, b, st, next)
		case east.KindStrikethrough:
			next := ctx
			next.strike = true
			styleInlineAST(c, src, b, st, next)
		case gast.KindLink:
			lnk := c.(*gast.Link)
			label := plainText(lnk, src)
			dest := string(lnk.Destination)
			if label == "" {
				label = dest
			}
			b.WriteString(st.link.Render(label))
			if dest != "" && dest != label {
				b.WriteString(" " + st.url.Render("("+dest+")"))
			}
		case gast.KindAutoLink:
			b.WriteString(st.link.Render(string(c.(*gast.AutoLink).Label(src))))
		default:
			styleInlineAST(c, src, b, st, ctx)
		}
	}
}

func styleLeaf(s string, st cellStyles, ctx inlineCtx) string {
	if !ctx.bold && !ctx.italic && !ctx.strike {
		return s
	}
	style := lipgloss.NewStyle()
	if ctx.bold {
		style = style.Inherit(st.bold)
	}
	if ctx.italic {
		style = style.Inherit(st.italic)
	}
	if ctx.strike {
		style = style.Inherit(st.strike)
	}
	return style.Render(s)
}

// plainText collects a node's text content with no styling, for spans that
// render as a single unit (code spans, link labels).
func plainText(n gast.Node, src []byte) string {
	var b strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch c.Kind() {
		case gast.KindText:
			b.Write(c.(*gast.Text).Value(src))
		case gast.KindString:
			b.Write(c.(*gast.String).Value)
		default:
			b.WriteString(plainText(c, src))
		}
	}
	return b.String()
}
