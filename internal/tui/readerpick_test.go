package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/thread"
)

// pickerModel builds a reader whose content went through the real renderer so
// the anchor map has genuine rendered lines to pick from.
func pickerModel(t *testing.T) specDetailModel {
	t.Helper()
	body := "First paragraph here.\n\nSecond paragraph is the pick target.\n\nThird paragraph."
	m := testSpecDetailModel()
	m.meta = &markdown.SpecMeta{ID: "SPEC-012", Title: "D", Status: "review"}
	m.sections = []markdown.Section{
		{Slug: "problem_statement", Heading: "## Problem Statement", Level: 2, Content: body},
	}
	m.readerMode = true
	m.sectionIdx = 0
	m.setSize(120, 30)

	r := NewGlamourRenderer(darkTestTheme())
	rendered, err := r.Render(context.Background(), body, 76)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	m.applyReaderContent(rendered)
	return m
}

func TestPickMode_EnterCapturesBlockQuote(t *testing.T) {
	m := pickerModel(t)
	m = m.enterPickMode()
	if !m.pickMode {
		t.Fatal("A should enter pick mode")
	}

	// Move the cursor onto the second paragraph's rendered line.
	target := -1
	for i, l := range splitLines(m.readerContent) {
		if strings.Contains(stripANSI(l), "pick target") {
			target = i
			break
		}
	}
	if target < 0 {
		t.Fatal("rendered content missing pick target")
	}
	for m.pickLine < target {
		m, _ = m.updatePickMode(tea.KeyPressMsg{Code: tea.KeyDown})
	}

	m, _ = m.updatePickMode(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.pickMode {
		t.Error("enter should leave pick mode")
	}
	if !m.input.active() || m.input.kind != "ask" {
		t.Fatal("enter should open the ask input")
	}
	if m.input.quote != "Second paragraph is the pick target." {
		t.Errorf("quote = %q, want the source block", m.input.quote)
	}
	// The prompt label must show what the thread will pin to.
	if !strings.Contains(inputLabel(m.input), "Second paragraph") {
		t.Errorf("label = %q, want it to surface the quote", inputLabel(m.input))
	}
}

func TestPickMode_EscFallsBackToSectionAsk(t *testing.T) {
	m := pickerModel(t)
	m = m.enterPickMode()
	m, _ = m.updatePickMode(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.pickMode {
		t.Error("esc should leave pick mode")
	}
	if !m.input.active() || m.input.quote != "" {
		t.Errorf("esc should fall back to a section-level ask, got quote=%q active=%v",
			m.input.quote, m.input.active())
	}
	if m.input.section != "problem_statement" {
		t.Errorf("section = %q, want problem_statement", m.input.section)
	}
}

func TestPickMode_AbsorbsReaderHotkeys(t *testing.T) {
	m := pickerModel(t)
	m = m.enterPickMode()
	before := m.sectionIdx
	// ']' would switch sections outside pick mode; inside it must be inert.
	m, _ = m.updatePickMode(tea.KeyPressMsg{Text: "]"})
	if m.sectionIdx != before || !m.pickMode {
		t.Error("pick mode must absorb reader hotkeys")
	}
}

func TestGutterOverlay_MarksAnchoredLine(t *testing.T) {
	m := pickerModel(t)
	th := quotedThread("T-1", "Second paragraph is the pick target.")
	th.Section = "problem_statement"
	m.threads = []thread.Thread{th}
	m.rebuildAnchors()

	line, ok := m.anchors.renderedLineFor("T-1")
	if !ok {
		t.Fatal("quote should resolve for the gutter")
	}
	view := splitLines(m.readerBodyView())
	if line >= len(view) {
		t.Skipf("anchor line %d beyond viewport window (%d rows)", line, len(view))
	}
	if !strings.Contains(view[line], IconGutter) {
		t.Errorf("line %d missing gutter glyph:\n%q", line, view[line])
	}
}
