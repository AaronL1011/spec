package tui

import (
	"testing"

	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/thread"
)

// loadedReader builds a detail model already in reader mode with two sections
// and a known content hash, as if an initial load had completed.
func loadedReader(threads []thread.Thread) specDetailModel {
	m := testSpecDetailModel()
	m.meta = &markdown.SpecMeta{ID: "SPEC-007", Title: "File Watcher", Status: "build"}
	m.sections = []markdown.Section{
		{Slug: "problem_statement", Level: 2, Owner: "pm", Content: "pain"},
		{Slug: "technical_implementation", Level: 2, Owner: "engineer", Content: "notes"},
	}
	m.threads = threads
	m.contentHash = "hash-v0"
	m.readerMode = true
	m.sectionIdx = 1 // technical_implementation
	m.applyReaderContent("rendered v0")
	return m
}

func refreshMsg(hash string, sections []markdown.Section, threads []thread.Thread) specDetailDataMsg {
	return specDetailDataMsg{
		Meta:     &markdown.SpecMeta{ID: "SPEC-007", Title: "File Watcher", Status: "build"},
		Sections: sections,
		Threads:  threads,
		Hash:     hash,
	}
}

func TestApplyRefresh_PreservesSection(t *testing.T) {
	m := loadedReader(nil)
	m.sectionIdx = 1
	out, _ := m.handleDataMsg(refreshMsg("hash-v1", m.sections, nil))
	if out.sectionIdx != 1 {
		t.Errorf("sectionIdx = %d, want 1 preserved across refresh", out.sectionIdx)
	}
	if out.contentHash != "hash-v1" {
		t.Errorf("contentHash = %q, want hash-v1", out.contentHash)
	}
}

func TestApplyRefresh_ClampsSectionWhenDocShrinks(t *testing.T) {
	m := loadedReader(nil)
	m.sectionIdx = 1
	// New content has only one readable section; index 1 is now out of range.
	shrunk := []markdown.Section{{Slug: "problem_statement", Level: 2, Owner: "pm", Content: "pain"}}
	out, _ := m.handleDataMsg(refreshMsg("hash-v1", shrunk, nil))
	if out.sectionIdx != 0 {
		t.Errorf("sectionIdx = %d, want clamped to 0", out.sectionIdx)
	}
}

func TestApplyRefresh_NoOpOnIdenticalHash(t *testing.T) {
	m := loadedReader(nil)
	m.sectionIdx = 1
	before := m.readerContent
	out, cmd := m.handleDataMsg(refreshMsg("hash-v0", m.sections, nil))
	if cmd != nil {
		t.Error("identical-hash refresh should issue no re-render command")
	}
	if out.readerContent != before {
		t.Error("identical-hash refresh should not change reader content")
	}
}

func TestApplyRefresh_PreservesThreadSelectionByID(t *testing.T) {
	threads := []thread.Thread{
		openThread("T-1", "Why Redis?"),
		openThread("T-2", "Burst allowance?"),
		openThread("T-3", "Backoff?"),
	}
	m := loadedReader(threads)
	m.threadIdx = 2 // T-3 selected

	// Refresh reorders threads; T-3 must stay selected by ID.
	reordered := []thread.Thread{
		openThread("T-3", "Backoff?"),
		openThread("T-1", "Why Redis?"),
		openThread("T-2", "Burst allowance?"),
	}
	out, _ := m.handleDataMsg(refreshMsg("hash-v1", m.sections, reordered))
	sel, ok := out.selectedThread()
	if !ok || sel.ID != "T-3" {
		t.Errorf("selected thread = %+v ok=%v, want T-3 preserved by ID", sel, ok)
	}
}

func TestApplyRefresh_ThreadSelectionFallbackWhenRemoved(t *testing.T) {
	threads := []thread.Thread{
		openThread("T-1", "Why Redis?"),
		openThread("T-2", "Burst allowance?"),
	}
	m := loadedReader(threads)
	m.threadIdx = 1 // T-2 selected

	// Refresh removes T-2 (e.g. resolved out of the open set). Selection must
	// fall back to a valid index without panicking.
	remaining := []thread.Thread{openThread("T-1", "Why Redis?")}
	out, _ := m.handleDataMsg(refreshMsg("hash-v1", m.sections, remaining))
	if out.threadIdx < 0 || out.threadIdx >= 1 {
		t.Errorf("threadIdx = %d, want clamped to valid range [0,1)", out.threadIdx)
	}
	if _, ok := out.selectedThread(); !ok {
		t.Error("expected a valid fallback selection after removal")
	}
}

func TestApplyRefresh_PreservesActiveInput(t *testing.T) {
	m := loadedReader([]thread.Thread{openThread("T-1", "Why Redis?")})
	m.input = threadInput{kind: "ask", section: "technical_implementation", area: newThreadArea(m.theme)}
	m.input.area.SetValue("half-typed question")

	out, _ := m.handleDataMsg(refreshMsg("hash-v1", m.sections, m.threads))
	if !out.input.active() {
		t.Fatal("active ask input should survive a refresh")
	}
	if out.input.body() != "half-typed question" {
		t.Errorf("input body = %q, want preserved", out.input.body())
	}
}

func TestApplyRefresh_OverviewClampsScroll(t *testing.T) {
	m := loadedReader(nil)
	m.readerMode = false
	m.scroll = 999 // beyond any plausible content height

	out, _ := m.handleDataMsg(refreshMsg("hash-v1", m.sections, nil))
	if out.scroll > out.maxScroll() {
		t.Errorf("scroll = %d exceeds maxScroll = %d after refresh", out.scroll, out.maxScroll())
	}
}

func TestHandleDataMsg_DeletedFileKeepsContentWithNotice(t *testing.T) {
	m := loadedReader(nil)
	out, _ := m.handleDataMsg(specDetailDataMsg{Err: errFileGone{}})
	if out.meta == nil {
		t.Error("a refresh error on a loaded spec must not drop the loaded meta")
	}
	if out.err != nil {
		t.Error("a refresh error on a loaded spec must not set a fatal err")
	}
	if out.notice == "" {
		t.Error("expected a calm inline notice when the file is gone")
	}
}

type errFileGone struct{}

func (errFileGone) Error() string { return "spec SPEC-007 not found" }

func TestRestoreThreadSelection_EmptySection(t *testing.T) {
	m := loadedReader(nil)
	m.sectionIdx = 0 // problem_statement has no threads
	m.restoreThreadSelection("T-9")
	if m.threadIdx != 0 {
		t.Errorf("threadIdx = %d, want 0 for an empty section", m.threadIdx)
	}
}

func TestClampInt(t *testing.T) {
	cases := []struct{ v, lo, hi, want int }{
		{5, 0, 10, 5},
		{-1, 0, 10, 0},
		{11, 0, 10, 10},
		{3, 0, -1, 0}, // empty slice: hi < lo
	}
	for _, c := range cases {
		if got := clampInt(c.v, c.lo, c.hi); got != c.want {
			t.Errorf("clampInt(%d,%d,%d) = %d, want %d", c.v, c.lo, c.hi, got, c.want)
		}
	}
}
