package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aaronl1011/spec/internal/thread"
)

// renderForAnchorTest renders a section body through the real Glamour
// renderer at a fixed width, mirroring what the reader shows. Token matching
// against real renderer output is the load-bearing assumption of the anchor
// map, so these tests must not use a fake renderer.
func renderForAnchorTest(t *testing.T, body string) string {
	t.Helper()
	r := NewGlamourRenderer(darkTestTheme())
	out, err := r.Render(context.Background(), body, 76)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return out
}

func darkTestTheme() Theme {
	return ResolveTheme("catppuccin-mocha")
}

func quotedThread(id, quote string) thread.Thread {
	return thread.Thread{
		ID: id, Section: "technical_implementation", Status: thread.StatusOpen,
		Author: "@mike", Question: "why?", Created: time.Now(),
		Quote: quote,
	}
}

func TestAnchorMap_PlainParagraph(t *testing.T) {
	body := "First paragraph about the gate.\n\nRetries are capped at three attempts.\n\nA closing paragraph."
	rendered := renderForAnchorTest(t, body)

	am := buildAnchorMap(body, rendered, []thread.Thread{
		quotedThread("T-1", "Retries are capped at three attempts."),
	})
	line, ok := am.renderedLineFor("T-1")
	if !ok {
		t.Fatal("quote should resolve in rendered output")
	}
	if !strings.Contains(stripANSI(splitLines(rendered)[line]), "Retries") {
		t.Errorf("anchored line %d does not carry the quote text: %q", line, splitLines(rendered)[line])
	}
	if am.countAt(line) != 1 {
		t.Errorf("countAt = %d, want 1", am.countAt(line))
	}
}

func TestAnchorMap_ListItemAndEmphasis(t *testing.T) {
	body := "Intro line.\n\n- item one is plain\n- item two has **bold** emphasis inside\n- item three"
	rendered := renderForAnchorTest(t, body)

	am := buildAnchorMap(body, rendered, []thread.Thread{
		quotedThread("T-1", "item two has bold emphasis inside"),
	})
	line, ok := am.renderedLineFor("T-1")
	if !ok {
		t.Fatal("list-item quote should resolve through the renderer's bullet rewriting")
	}
	if !strings.Contains(stripANSI(splitLines(rendered)[line]), "item two") {
		t.Errorf("anchored to wrong line: %q", splitLines(rendered)[line])
	}
}

func TestAnchorMap_TableRow(t *testing.T) {
	body := "| PR | Scope |\n|----|-------|\n| 1 | Span anchoring model |\n| 2 | Document navigation |\n"
	rendered := renderForAnchorTest(t, body)

	am := buildAnchorMap(body, rendered, []thread.Thread{
		quotedThread("T-1", "Span anchoring model"),
	})
	if _, ok := am.renderedLineFor("T-1"); !ok {
		t.Fatal("table-cell quote should resolve through the custom table path")
	}
}

func TestAnchorMap_MissDegradesToSection(t *testing.T) {
	body := "Some prose that exists."
	rendered := renderForAnchorTest(t, body)

	am := buildAnchorMap(body, rendered, []thread.Thread{
		quotedThread("T-1", "text that was edited away entirely"),
	})
	if _, ok := am.renderedLineFor("T-1"); ok {
		t.Error("a drifted quote must miss (degrade to section), not match")
	}
}

func TestAnchorMap_SectionLevelThreadHasNoEntry(t *testing.T) {
	body := "Some prose."
	rendered := renderForAnchorTest(t, body)
	tt := quotedThread("T-1", "")
	am := buildAnchorMap(body, rendered, []thread.Thread{tt})
	if _, ok := am.renderedLineFor("T-1"); ok {
		t.Error("section-level threads carry no rendered anchor")
	}
}

func TestAnchorMap_CoAnchoredThreadsCollapseToCount(t *testing.T) {
	body := "Shared target paragraph for two threads.\n\nOther prose."
	rendered := renderForAnchorTest(t, body)
	am := buildAnchorMap(body, rendered, []thread.Thread{
		quotedThread("T-1", "Shared target paragraph for two threads."),
		quotedThread("T-2", "Shared target paragraph for two threads."),
	})
	line, ok := am.renderedLineFor("T-1")
	if !ok {
		t.Fatal("quote should resolve")
	}
	if am.countAt(line) != 2 {
		t.Errorf("countAt = %d, want 2 (badge collapse)", am.countAt(line))
	}
}

func TestAnchorMap_SourceBlockAtRoundTrip(t *testing.T) {
	body := "First paragraph here.\n\nSecond paragraph spans a sentence with several words to pick.\n\nThird paragraph."
	rendered := renderForAnchorTest(t, body)

	am := buildAnchorMap(body, rendered, nil)
	// Find the rendered line carrying the second paragraph.
	var target int
	found := false
	for i, l := range splitLines(rendered) {
		if strings.Contains(stripANSI(l), "Second paragraph") {
			target, found = i, true
			break
		}
	}
	if !found {
		t.Fatal("rendered output missing second paragraph")
	}

	quote, _, ok := am.sourceBlockAt(target)
	if !ok {
		t.Fatal("sourceBlockAt should map a prose line back to its block")
	}
	if quote != "Second paragraph spans a sentence with several words to pick." {
		t.Errorf("quote = %q, want the full source block", quote)
	}

	// The captured quote must round-trip through buildAnchorMap.
	am2 := buildAnchorMap(body, rendered, []thread.Thread{quotedThread("T-9", quote)})
	if _, ok := am2.renderedLineFor("T-9"); !ok {
		t.Error("picker-captured quote failed to resolve on the next render")
	}
}

func TestAnchorMap_SourceBlockAtChromeLineMisses(t *testing.T) {
	body := "Only paragraph."
	rendered := renderForAnchorTest(t, body)
	am := buildAnchorMap(body, rendered, nil)
	// A blank rendered line has no tokens — must miss, not panic.
	lines := splitLines(rendered)
	for i, l := range lines {
		if strings.TrimSpace(stripANSI(l)) == "" {
			if _, _, ok := am.sourceBlockAt(i); ok {
				t.Errorf("blank line %d should not map to a block", i)
			}
			return
		}
	}
}
