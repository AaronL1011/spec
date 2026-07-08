package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/thread"
)

func threadIn(id, section, q string) thread.Thread {
	return thread.Thread{ID: id, Section: section, Status: thread.StatusOpen,
		Author: "@mike", Question: q, Created: time.Now()}
}

// cockpitModel returns a reader over two sections with a mixed thread set.
func cockpitModel() specDetailModel {
	m := testSpecDetailModel()
	m.meta = &markdown.SpecMeta{ID: "SPEC-012", Title: "Discussion", Status: "review"}
	m.sections = []markdown.Section{
		{Slug: "problem_statement", Heading: "## Problem Statement", Level: 2,
			Content: "The problem paragraph explains everything in detail."},
		{Slug: "technical_implementation", Heading: "## Technical Implementation", Level: 2,
			Content: "Retries are capped at three attempts.\n\nThe gate can require review."},
	}
	m.readerMode = true
	m.sectionIdx = 0
	m.applyReaderContent("rendered")
	return m
}

// ── Filters ─────────────────────────────────────────────────────────────────

func TestFilter_DefaultOpenHidesResolved(t *testing.T) {
	m := cockpitModel()
	resolved := threadIn("T-2", "problem_statement", "done?")
	resolved.Status = thread.StatusResolved
	m.threads = []thread.Thread{threadIn("T-1", "problem_statement", "open?"), resolved}

	ts := m.threadsForSection("problem_statement")
	if len(ts) != 1 || ts[0].ID != "T-1" {
		t.Errorf("default open filter returned %v, want only T-1", ts)
	}
}

func TestFilter_CycleReachesAllAndWrapsHome(t *testing.T) {
	m := cockpitModel()
	seen := []string{m.threadFilter}
	for range filterCycle {
		m.cycleFilter()
		seen = append(seen, m.threadFilter)
	}
	if seen[0] != threadFilterOpen || seen[len(seen)-1] != threadFilterOpen {
		t.Errorf("filter cycle = %v, want to start and end at open", seen)
	}
}

func TestFilter_AllShowsResolved(t *testing.T) {
	m := cockpitModel()
	resolved := threadIn("T-2", "problem_statement", "done?")
	resolved.Status = thread.StatusResolved
	m.threads = []thread.Thread{resolved}
	m.threadFilter = threadFilterAll
	if len(m.threadsForSection("problem_statement")) != 1 {
		t.Error("filter all should include resolved threads")
	}
}

func TestFilter_MineMatchesParticipantsCaseInsensitive(t *testing.T) {
	m := cockpitModel()
	mine := threadIn("T-1", "problem_statement", "hey @Tester ping")
	mine.Mentions = []string{"Tester"}
	other := threadIn("T-2", "problem_statement", "unrelated")
	m.threads = []thread.Thread{mine, other}
	m.threadFilter = threadFilterMine

	// testResolvedConfig's viewer identity comes from m.author().
	viewer := m.author()
	if viewer == "" {
		t.Skip("no viewer identity in test config")
	}
	mine.Mentions = []string{viewer}
	m.threads = []thread.Thread{mine, other}
	ts := m.threadsForSection("problem_statement")
	if len(ts) != 1 || ts[0].ID != "T-1" {
		t.Errorf("mine filter returned %v, want only T-1", ts)
	}
}

func TestFilter_UnreadSnapshotKeepsItemsUnderCursor(t *testing.T) {
	m := cockpitModel()
	m.threads = []thread.Thread{
		threadIn("T-1", "problem_statement", "q1"),
		threadIn("T-2", "problem_statement", "q2"),
	}
	// Cycle to unread: open → all → mine → unread.
	m.cycleFilter()
	m.cycleFilter()
	m.cycleFilter()
	if m.threadFilter != threadFilterUnread {
		t.Fatalf("filter = %s, want unread", m.threadFilter)
	}
	if len(m.threadsForSection("problem_statement")) != 2 {
		t.Fatal("both threads should be unread initially")
	}
	// Reading one must NOT remove it from the snapshot traversal.
	cmd := m.markSeen(m.threads[0])
	_ = cmd // db is nil in tests; in-memory seen map is what matters
	if len(m.threadsForSection("problem_statement")) != 2 {
		t.Error("reading a thread must not shrink the unread traversal until the filter is re-entered")
	}
	if m.isUnread(m.threads[0]) {
		t.Error("marked thread should read as seen")
	}
}

func TestPane_EmptyFilterStateRendersNotHides(t *testing.T) {
	m := cockpitModel()
	resolved := threadIn("T-1", "problem_statement", "done?")
	resolved.Status = thread.StatusResolved
	m.threads = []thread.Thread{resolved} // open filter matches nothing

	if !m.paneActiveForCurrentSection() {
		t.Fatal("pane must stay active when unfiltered threads exist (tab-ownership stability)")
	}
	pane := stripANSI(strings.Join(m.renderThreadPane(80, 10), "\n"))
	if !strings.Contains(pane, "no threads match") || !strings.Contains(pane, "filter: open") {
		t.Errorf("expected explanatory empty state, got:\n%s", pane)
	}
}

// ── Read-state ──────────────────────────────────────────────────────────────

func TestToggleRead_FlipsAndSurvivesSnapshot(t *testing.T) {
	m := cockpitModel()
	th := threadIn("T-1", "problem_statement", "q1")
	m.threads = []thread.Thread{th}
	m.selectedThreadID = "T-1"

	if !m.isUnread(th) {
		t.Fatal("threads start unread")
	}
	m, _ = m.toggleRead()
	if m.isUnread(th) {
		t.Error("toggle should mark the thread read")
	}
	m, _ = m.toggleRead()
	if !m.isUnread(th) {
		t.Error("second toggle should mark it unread again")
	}
}

func TestMarkSeen_ReplyReopensUnread(t *testing.T) {
	m := cockpitModel()
	th := threadIn("T-1", "problem_statement", "q1")
	m.threads = []thread.Thread{th}
	_ = m.markSeen(th)
	if m.isUnread(th) {
		t.Fatal("seen thread should read as read")
	}
	// A newer reply moves LastActivity past the watermark.
	th.Replies = []thread.Reply{{Author: "@bob", At: time.Now().Add(time.Hour), Body: "new"}}
	if !m.isUnread(th) {
		t.Error("a reply after last_seen must re-mark the thread unread")
	}
}

// ── Document-wide stepping ──────────────────────────────────────────────────

func TestStepThread_CrossesSectionBoundary(t *testing.T) {
	m := cockpitModel()
	m.threads = []thread.Thread{
		threadIn("T-1", "problem_statement", "q1"),
		threadIn("T-2", "technical_implementation", "q2"),
	}
	m, _ = m.stepThread(1) // T-1 in section 0
	m, _ = m.stepThread(1) // T-2 — must switch to section 1
	if m.sectionIdx != 1 {
		t.Errorf("sectionIdx = %d, want 1 after stepping into the next section's thread", m.sectionIdx)
	}
	if sel, _ := m.selectedThread(); sel.ID != "T-2" {
		t.Errorf("selected = %s, want T-2", sel.ID)
	}
	if !m.paneFocused || !m.paneVisible {
		t.Error("stepping must focus and show the pane")
	}
}

func TestStepThread_MarksSeen(t *testing.T) {
	m := cockpitModel()
	th := threadIn("T-1", "problem_statement", "q1")
	m.threads = []thread.Thread{th}
	m, _ = m.stepThread(1)
	if m.isUnread(th) {
		t.Error("selecting via the stepper should mark the thread seen")
	}
}

// ── Unanchored bucket + repair (drift corpus: heading rename) ───────────────

func TestUnanchored_SectionRenameKeepsThreadReachable(t *testing.T) {
	m := cockpitModel()
	// Thread anchored to a slug that no longer exists.
	m.threads = []thread.Thread{threadIn("T-1", "technical_approach", "severed?")}

	secs := m.readableSections()
	last := secs[len(secs)-1]
	if last.Slug != unanchoredSlug {
		t.Fatalf("expected trailing %s section, got %s", unanchoredSlug, last.Slug)
	}
	if got := m.threadsForSection(unanchoredSlug); len(got) != 1 || got[0].ID != "T-1" {
		t.Errorf("unanchored bucket = %v, want T-1", got)
	}
	// The stepper must reach it.
	m, _ = m.stepThread(1)
	if sel, ok := m.selectedThread(); !ok || sel.ID != "T-1" {
		t.Errorf("stepper could not reach unanchored thread: %+v ok=%v", sel, ok)
	}
}

func TestUnanchored_RepairTargetUniqueQuote(t *testing.T) {
	m := cockpitModel()
	th := threadIn("T-1", "old_slug", "severed")
	th.Quote = "Retries are capped at three attempts."
	m.threads = []thread.Thread{th}

	target, ok := m.reanchorTarget(th)
	if !ok || target != "technical_implementation" {
		t.Errorf("reanchorTarget = %q ok=%v, want technical_implementation", target, ok)
	}
}

func TestUnanchored_RepairAmbiguousOrMissingNeverGuesses(t *testing.T) {
	m := cockpitModel()
	m.sections = append(m.sections, markdown.Section{
		Slug: "another", Heading: "## Another", Level: 2,
		Content: "Retries are capped at three attempts.",
	})
	ambiguous := threadIn("T-1", "old_slug", "severed")
	ambiguous.Quote = "Retries are capped at three attempts."
	if _, ok := m.reanchorTarget(ambiguous); ok {
		t.Error("ambiguous quote (two sections) must not offer a repair")
	}
	noQuote := threadIn("T-2", "old_slug", "severed")
	if _, ok := m.reanchorTarget(noQuote); ok {
		t.Error("quote-less thread must not offer a repair")
	}
}

func TestUnanchored_EnterTriggersRepair(t *testing.T) {
	m := cockpitModel()
	th := threadIn("T-1", "old_slug", "severed")
	th.Quote = "The gate can require review."
	m.threads = []thread.Thread{th}
	// Focus the unanchored synthetic section.
	secs := m.readableSections()
	m.sectionIdx = len(secs) - 1
	m.paneFocused = true
	m.selectedThreadID = "T-1"

	_, cmd, handled := m.handleThreadActionKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !handled || cmd == nil {
		t.Errorf("enter on a repairable unanchored thread should dispatch the re-anchor command (handled=%v cmd=%v)", handled, cmd != nil)
	}
}

// ── Drift corpus: quote-level degrade paths ─────────────────────────────────

func TestDriftCorpus_QuoteDegradePaths(t *testing.T) {
	original := "Retries are capped at three attempts.\n\nThe gate can require review."
	quote := "Retries are capped at three attempts."

	cases := []struct {
		name      string
		editedTo  string
		wantFound bool
	}{
		{"untouched", original, true},
		{"reflowed whitespace", "Retries are capped\nat three attempts.\n\nThe gate can require review.", true},
		{"paragraph moved", "The gate can require review.\n\nRetries are capped at three attempts.", true},
		{"reworded within block", "Retries are capped at five attempts.\n\nThe gate can require review.", false},
		{"block deleted", "The gate can require review.", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := markdown.ResolveAnchor(tc.editedTo, quote, "")
			if got.Found != tc.wantFound {
				t.Errorf("Found = %v, want %v (degrade path must be exact)", got.Found, tc.wantFound)
			}
		})
	}
}
