package tui

import (
	"context"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

const sampleTable = `| # | Question | Decision |
|---|---|:---:|
| 1 | Approach for v1? | **Option B** |
| 2 | Host containers where? | ECS RunTask |`

func TestSplitTableSegments_MixedContent(t *testing.T) {
	md := "Intro text.\n\n" + sampleTable + "\n\nTrailing text."
	segs := splitTableSegments(md)
	if len(segs) != 3 {
		t.Fatalf("got %d segments, want 3: %#v", len(segs), segs)
	}
	if segs[0].table || !segs[1].table || segs[2].table {
		t.Fatalf("segment table flags = %v %v %v, want false true false",
			segs[0].table, segs[1].table, segs[2].table)
	}
	if !strings.HasPrefix(segs[1].text, "| # |") {
		t.Errorf("table segment text = %q", segs[1].text)
	}
}

func TestSplitTableSegments_NoTable(t *testing.T) {
	segs := splitTableSegments("Just a paragraph.\n\nAnother one.")
	if len(segs) != 1 || segs[0].table {
		t.Fatalf("got %#v, want one non-table segment", segs)
	}
}

func TestSplitTableSegments_IgnoresFencedCode(t *testing.T) {
	md := "```\n| a | b |\n|---|---|\n| 1 | 2 |\n```"
	for _, seg := range splitTableSegments(md) {
		if seg.table {
			t.Fatalf("table detected inside code fence: %#v", seg)
		}
	}
}

func TestIsTableDelimiter(t *testing.T) {
	cases := map[string]bool{
		"|---|---|":      true,
		"| :--- | --: |": true,
		"|:-:|":          true,
		"| a | b |":      false,
		"|   |   |":      false,
		"plain text":     false,
	}
	for in, want := range cases {
		if got := isTableDelimiter(in); got != want {
			t.Errorf("isTableDelimiter(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseAlignments(t *testing.T) {
	got := parseAlignments("| :--- | :---: | ---: | --- |")
	want := []cellAlign{alignLeft, alignCenter, alignRight, alignNone}
	if len(got) != len(want) {
		t.Fatalf("got %d alignments, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("alignment[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestStyleCellInline(t *testing.T) {
	md := goldmark.New(goldmark.WithExtensions(extension.Strikethrough))
	st := cellStyles{
		code:   lipgloss.NewStyle().Bold(true),
		bold:   lipgloss.NewStyle().Bold(true),
		italic: lipgloss.NewStyle().Italic(true),
		strike: lipgloss.NewStyle().Strikethrough(true),
		link:   lipgloss.NewStyle().Underline(true),
		url:    lipgloss.NewStyle().Faint(true),
	}
	cases := map[string]string{
		"plain text":                   "plain text",
		"**Option B**":                 "Option B",
		"a `code span` b":              "a code span b",
		"**bold** and `code`":          "bold and code",
		"unbalanced `tick":             "unbalanced `tick",
		"unbalanced **stars":           "unbalanced **stars",
		"*italic text*":                "italic text",
		"_italic text_":                "italic text",
		"~~strike~~":                   "strike",
		"**bold _italic_**":            "bold italic",
		"[label](https://example.com)": "label (https://example.com)",
		"[example.com](example.com)":   "example.com",
		"<https://example.com>":        "https://example.com",
		"decision_log":                 "decision_log",
		"user_id and tenant_id":        "user_id and tenant_id",
		"#":                            "#",
	}
	for in, want := range cases {
		if got := xansi.Strip(styleCellInline(in, md, st)); got != want {
			t.Errorf("styleCellInline(%q) = %q, want %q", in, got, want)
		}
	}

	// Text following a nested span must keep the outer style: leaves are
	// styled individually, so the inner span's ANSI reset cannot bleed.
	raw := styleCellInline("**bold _x_ tail**", md, st)
	if got := xansi.Strip(raw); got != "bold x tail" {
		t.Fatalf("nested with tail = %q, want %q", got, "bold x tail")
	}
	if !strings.Contains(raw, "\x1b[1m tail") {
		t.Errorf("trailing text lost bold styling: %q", raw)
	}
}

func TestGlamourRenderer_TableHasRowSeparators(t *testing.T) {
	r := NewGlamourRenderer(catppuccinMocha())
	out, err := r.Render(context.Background(), sampleTable, 80)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// One separator under the header plus one between the two data rows.
	if got := strings.Count(out, "┼"); got < 2 {
		t.Errorf("got %d horizontal separator junctions, want >= 2:\n%s", got, out)
	}
	plain := xansi.Strip(out)
	if !strings.Contains(plain, "Option B") || strings.Contains(plain, "**") {
		t.Errorf("bold cell not styled correctly:\n%s", plain)
	}
}

func TestGlamourRenderer_TextAroundTableStillRendered(t *testing.T) {
	md := "Intro paragraph.\n\n" + sampleTable + "\n\nTrailing paragraph."
	r := NewGlamourRenderer(catppuccinMocha())
	out, err := r.Render(context.Background(), md, 80)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	plain := xansi.Strip(out)
	for _, want := range []string{"Intro paragraph.", "Trailing paragraph.", "┼"} {
		if !strings.Contains(plain, want) {
			t.Errorf("output missing %q:\n%s", want, plain)
		}
	}
	if strings.Index(plain, "Intro") > strings.Index(plain, "┼") {
		t.Error("intro paragraph rendered after the table")
	}
}
