package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/thread"
)

func readerWithThreads(threads []thread.Thread) specDetailModel {
	m := testSpecDetailModel()
	m.meta = &markdown.SpecMeta{ID: "SPEC-012", Title: "Discussion", Status: "review"}
	m.sections = []markdown.Section{
		{Slug: "problem_statement", Level: 2, Content: "Some problem text that is long enough."},
		{Slug: "technical_implementation", Level: 2, Content: "Some technical text that is long enough."},
	}
	m.readerMode = true
	m.sectionIdx = 1 // technical_implementation
	m.readerContent = "content"
	m.threads = threads
	m.applyReaderContent("rendered content")
	return m
}

func openThread(id, q string) thread.Thread {
	return thread.Thread{ID: id, Section: "technical_implementation", Status: thread.StatusOpen, Author: "@mike", Question: q, Created: time.Now()}
}

func TestThreadPane_SidebarShowsOpenBadge(t *testing.T) {
	m := readerWithThreads([]thread.Thread{
		openThread("T-1", "Why Redis?"),
		openThread("T-2", "Burst allowance?"),
	})
	out := m.viewReaderWithSidebar()
	if !strings.Contains(out, "●2") {
		t.Errorf("sidebar should show open badge ●2, got:\n%s", out)
	}
}

func TestThreadPane_ResolvedThreadsNotInBadge(t *testing.T) {
	resolved := openThread("T-3", "Naming?")
	resolved.Status = thread.StatusResolved
	m := readerWithThreads([]thread.Thread{resolved})
	if got := m.openCountForSection("technical_implementation"); got != 0 {
		t.Errorf("open count = %d, want 0 (resolved excluded)", got)
	}
}

func TestThreadPane_RendersWhenSectionHasThreads(t *testing.T) {
	m := readerWithThreads([]thread.Thread{openThread("T-1", "Why Redis?")})
	pane := m.renderThreadPane(60, 12)
	if len(pane) == 0 {
		t.Fatal("expected thread pane to render for a section with threads")
	}
	joined := strings.Join(pane, "\n")
	if !strings.Contains(joined, "Threads (1 open)") || !strings.Contains(joined, "Why Redis?") {
		t.Errorf("pane missing header/question:\n%s", joined)
	}
}

func TestThreadPane_HiddenWhenToggledOff(t *testing.T) {
	m := readerWithThreads([]thread.Thread{openThread("T-1", "Why Redis?")})
	m, _, handled := m.handleThreadActionKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if !handled {
		t.Fatal("'t' should be handled")
	}
	if m.paneVisible {
		t.Error("pane should be hidden after 't'")
	}
	if pane := m.renderThreadPane(60, 12); len(pane) != 0 {
		t.Error("hidden pane should render nothing")
	}
}

func TestThreadPane_AskOpensInputAndShowsPane(t *testing.T) {
	m := readerWithThreads(nil)
	m.paneVisible = false
	m, _, handled := m.handleThreadActionKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if !handled || !m.input.active() {
		t.Fatal("'a' should open the ask input")
	}
	if !m.paneVisible {
		t.Error("'a' should re-show the pane")
	}
	if m.input.section != "technical_implementation" {
		t.Errorf("ask anchored to %q, want technical_implementation", m.input.section)
	}
}

func TestThreadPane_InputCapturesTypingAndEscCancels(t *testing.T) {
	m := readerWithThreads(nil)
	m, _, _ = m.handleThreadActionKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	for _, r := range "hi" {
		m, _, _ = m.handleThreadInputKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if m.input.buffer != "hi" {
		t.Errorf("buffer = %q, want 'hi'", m.input.buffer)
	}
	m, _, handled := m.handleThreadInputKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !handled || m.input.active() {
		t.Error("esc should cancel the input")
	}
}

func TestThreadPane_TabTogglesFocus(t *testing.T) {
	m := readerWithThreads([]thread.Thread{openThread("T-1", "Why Redis?")})
	if m.paneFocused {
		t.Fatal("pane should start unfocused")
	}
	m, _, handled := m.handleThreadActionKey(tea.KeyMsg{Type: tea.KeyTab})
	if !handled || !m.paneFocused {
		t.Error("tab should focus the pane when it is active")
	}
}

func TestThreadPane_ArrowMovesSelectionWhenFocused(t *testing.T) {
	m := readerWithThreads([]thread.Thread{
		openThread("T-1", "q1"),
		openThread("T-2", "q2"),
	})
	m.paneFocused = true
	m = m.selectThread(1)
	if m.threadIdx != 1 {
		t.Errorf("threadIdx = %d, want 1", m.threadIdx)
	}
	// Clamped at the end.
	m = m.selectThread(5)
	if m.threadIdx != 1 {
		t.Errorf("threadIdx = %d, want clamped to 1", m.threadIdx)
	}
}

func TestThreadPane_ResolveKeyIgnoredWithoutThread(t *testing.T) {
	m := readerWithThreads(nil)
	_, cmd, handled := m.handleThreadActionKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if !handled {
		t.Error("'x' should be handled (no-op) even with no thread")
	}
	if cmd != nil {
		t.Error("'x' with no thread should not produce a command")
	}
}

func TestRelTime(t *testing.T) {
	now := time.Now()
	cases := map[string]time.Time{
		"now": now,
		"5m":  now.Add(-5 * time.Minute),
		"3h":  now.Add(-3 * time.Hour),
		"2d":  now.Add(-48 * time.Hour),
	}
	for want, ts := range cases {
		if got := relTime(ts); got != want {
			t.Errorf("relTime(%v) = %q, want %q", ts, got, want)
		}
	}
}

// TestThreadPane_InputAlwaysVisible guards against the regression where the
// ask/reply input was pushed off-screen (and the pane overflowed the viewport)
// because a margin-bearing style embedded a newline that desynced the row
// count from the reserved height.
func TestThreadPane_InputAlwaysVisible(t *testing.T) {
	cases := []struct {
		name          string
		width, height int
		proseLines    int
	}{
		{"wide short prose", 120, 20, 2},
		{"wide long prose", 120, 20, 60},
		{"narrow long prose", 80, 20, 60},
		{"tiny height", 120, 6, 60},
		{"tall", 120, 30, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := testSpecDetailModel()
			m.meta = &markdown.SpecMeta{ID: "SPEC-012", Title: "D", Status: "review"}
			m.sections = []markdown.Section{{Slug: "problem_statement", Level: 2, Content: "x"}}
			m.readerMode = true
			m.sectionIdx = 0
			m.setSize(tc.width, tc.height)
			m.applyReaderContent(strings.TrimRight(strings.Repeat("prose line\n", tc.proseLines), "\n"))

			m, _, _ = m.handleThreadActionKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
			for _, r := range "why redis" {
				m, _, _ = m.handleThreadInputKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
			}

			out := m.view()
			lines := strings.Split(out, "\n")
			if len(lines) > tc.height {
				t.Fatalf("view overflows: %d lines > height %d", len(lines), tc.height)
			}
			last := lines[len(lines)-1]
			if !strings.Contains(last, "why redis") {
				t.Errorf("input not on the last visible row; got %q", last)
			}
		})
	}
}

// TestThreadPane_SelectedThreadShowsFullText guards against truncation: the
// selected thread's question and reply bodies must be readable in full (wrapped
// across rows), and a thread taller than the pane budget must be fully
// reachable by scrolling.
func TestThreadPane_SelectedThreadShowsFullText(t *testing.T) {
	q := "Why Redis over an in-memory bucket given multiple instances need shared state and consistent eviction semantics across the whole fleet under sustained load"
	th := thread.Thread{ID: "T-1", Section: "technical_implementation", Status: thread.StatusOpen, Author: "@mike", Question: q}
	th.Replies = []thread.Reply{{Author: "@aaron", Body: "Because each node would otherwise keep its own private bucket and the limits would not hold globally."}}

	m := testSpecDetailModel()
	m.meta = &markdown.SpecMeta{ID: "SPEC-012", Title: "D", Status: "review"}
	m.sections = []markdown.Section{{Slug: "technical_implementation", Level: 2, Content: "x"}}
	m.readerMode = true
	m.sectionIdx = 0
	m.setSize(120, 30)
	m.applyReaderContent("Prose.")
	m.threads = []thread.Thread{th}
	m, _, _ = m.handleThreadActionKey(tea.KeyMsg{Type: tea.KeyTab}) // focus + expand

	// Collect all rows reachable by scrolling from top to bottom.
	seen := map[string]bool{}
	collect := func() {
		for _, l := range strings.Split(m.view(), "\n") {
			seen[stripANSI(l)] = true
		}
	}
	collect()
	for i := 0; i < m.maxThreadScroll(); i++ {
		m, _ = m.updateReader(tea.KeyMsg{Type: tea.KeyDown})
		collect()
	}
	joined := strings.Join(keysOf(seen), "\n")

	// Every word of the question and reply must appear somewhere — nothing
	// is permanently truncated.
	for _, word := range strings.Fields(q + " " + th.Replies[0].Body) {
		if !strings.Contains(joined, word) {
			t.Errorf("word %q from thread text is not readable anywhere in the pane", word)
		}
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
