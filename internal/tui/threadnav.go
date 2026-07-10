package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/thread"
)

// Thread pane filter values (discussion-03 §4.1). The default is open: the
// reader is a review cockpit, and resolved threads are one keypress away.
// A blocking filter joins the cycle once thread kinds (discussion-02) ship.
const (
	threadFilterOpen   = "open"
	threadFilterAll    = "all"
	threadFilterMine   = "mine"
	threadFilterUnread = "unread"
)

// filterCycle is the order the f key steps through.
var filterCycle = []string{threadFilterOpen, threadFilterAll, threadFilterMine, threadFilterUnread}

// unanchoredSlug is the synthetic section slug that collects threads whose
// section heading no longer exists. No real heading ever slugifies to a
// leading underscore, so it can never collide.
const unanchoredSlug = "_unanchored"

// unanchoredSection is the synthetic reader entry that keeps severed threads
// reachable — a thread is never simply absent from a surface that claims to
// show all threads (discussion-03 §2.4).
func unanchoredSection() markdown.Section {
	return markdown.Section{
		Slug:    unanchoredSlug,
		Heading: "## Unanchored threads",
		Level:   2,
		Content: "_These threads reference sections that no longer exist in this document " +
			"(e.g. a reworded heading). Focus the pane to review them; when a thread's " +
			"quote matches exactly one live section, press enter to re-anchor it._",
	}
}

// ── Filters ─────────────────────────────────────────────────────────────────

// matchesFilter reports whether a thread passes the active pane filter.
func (m specDetailModel) matchesFilter(t thread.Thread) bool {
	switch m.threadFilter {
	case threadFilterAll:
		return true
	case threadFilterMine:
		viewer := normalizeHandle(m.author())
		if viewer == "" {
			return false
		}
		for _, p := range t.Participants() {
			if normalizeHandle(p) == viewer {
				return true
			}
		}
		return false
	case threadFilterUnread:
		return m.unreadSnapshot[t.ID]
	default: // threadFilterOpen
		return t.IsOpen()
	}
}

// normalizeHandle lowercases a handle and strips a leading '@' so "@Bob" and
// "bob" compare equal, mirroring thread.Participants' own dedupe key.
func normalizeHandle(h string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(h), "@"))
}

// cycleFilter advances the pane filter. Entering the unread filter snapshots
// the unread set so items never vanish from under the cursor as they are read
// — they leave the traversal only when the filter is re-entered.
func (m *specDetailModel) cycleFilter() {
	idx := 0
	for i, f := range filterCycle {
		if f == m.threadFilter {
			idx = i
			break
		}
	}
	m.threadFilter = filterCycle[(idx+1)%len(filterCycle)]
	if m.threadFilter == threadFilterUnread {
		snap := make(map[string]bool)
		for _, t := range m.threads {
			if t.IsOpen() && m.isUnread(t) {
				snap[t.ID] = true
			}
		}
		m.unreadSnapshot = snap
	}
	m.reconcileThreadSelection()
	m.rebuildAnchors()
	m.syncViewportHeight()
}

// reconcileThreadSelection keeps the selected ID honest after filters or a
// live sidecar refresh. Rendering never invents a fallback that navigation
// does not know about.
func (m *specDetailModel) reconcileThreadSelection() {
	ordered := m.orderedThreads()
	if len(ordered) == 0 {
		m.selectedThreadID = ""
		return
	}
	for _, t := range ordered {
		if t.ID == m.selectedThreadID {
			return
		}
	}
	m.selectedThreadID = ordered[0].ID
}

// ── Read-state ──────────────────────────────────────────────────────────────

// isUnread reports whether the thread has activity the user hasn't viewed.
// With no read-state row (or no DB at all) every thread reads as unread —
// the degrade direction that never hides work.
func (m specDetailModel) isUnread(t thread.Thread) bool {
	seen, ok := m.seen[t.ID]
	return !ok || t.LastActivity().After(seen)
}

// unreadCount counts unread open threads across the document.
func (m specDetailModel) unreadCount() int {
	n := 0
	for _, t := range m.threads {
		if t.IsOpen() && m.isUnread(t) {
			n++
		}
	}
	return n
}

// markSeen records a thread as viewed up to its latest activity: in memory
// immediately, and in the local store as a fire-and-forget command so the
// n-keypress hot path never blocks on SQLite.
func (m *specDetailModel) markSeen(t thread.Thread) tea.Cmd {
	la := t.LastActivity()
	if prev, ok := m.seen[t.ID]; ok && !la.After(prev) {
		return nil
	}
	if m.seen == nil {
		m.seen = make(map[string]time.Time)
	}
	m.seen[t.ID] = la
	db, specID, threadID, viewer := m.db, m.specID, t.ID, m.author()
	if db == nil {
		return nil
	}
	return func() tea.Msg {
		// Best-effort: read-state is a progressive enhancement; a failed
		// write just leaves the thread unread on the next load.
		_ = db.MarkThreadSeen(specID, threadID, viewer, la)
		return nil
	}
}

// toggleRead flips the selected thread's read-state (the u key). Auto-mark on
// select without an undo would make fast n-n-n scanning destructive; this is
// the "keep this for later" escape hatch.
func (m specDetailModel) toggleRead() (specDetailModel, tea.Cmd) {
	t, ok := m.selectedThread()
	if !ok {
		return m, nil
	}
	if m.isUnread(t) {
		cmd := m.markSeen(t)
		return m, cmd
	}
	delete(m.seen, t.ID)
	if m.unreadSnapshot != nil {
		m.unreadSnapshot[t.ID] = true
	}
	db, specID, threadID, viewer := m.db, m.specID, t.ID, m.author()
	if db == nil {
		return m, nil
	}
	return m, func() tea.Msg {
		// Best-effort, mirroring markSeen.
		_ = db.MarkThreadUnseen(specID, threadID, viewer)
		return nil
	}
}

// ── Document-wide navigation ────────────────────────────────────────────────

// orderedThreads returns the active filter's threads across the whole
// document in reading order: by section position, then anchor line, then
// creation time. Unanchored threads (§2.4) traverse last, matching their
// synthetic trailing section entry.
func (m specDetailModel) orderedThreads() []thread.Thread {
	var out []thread.Thread
	for _, sec := range m.readableSections() {
		out = append(out, m.threadsForSection(sec.Slug)...)
	}
	return out
}

// stepThread moves to the next/prev thread document-wide, switching the
// focused section, scrolling the reader to the thread's anchor, focusing the
// pane and selecting the thread. It wraps at either end with a status flash —
// a review pass never dead-ends.
func (m specDetailModel) stepThread(delta int) (specDetailModel, tea.Cmd) {
	ordered := m.orderedThreads()
	if len(ordered) == 0 {
		return m, func() tea.Msg {
			return readerFlashMsg{Text: "no threads match filter: " + m.threadFilter + " · f to change"}
		}
	}
	if m.reviewPassComplete {
		m.reviewPassComplete = false
		if delta > 0 {
			m.selectedThreadID = ""
		}
	}
	cur := -1
	for i, t := range ordered {
		if t.ID == m.selectedThreadID {
			cur = i
			break
		}
	}
	next := cur + delta
	if cur < 0 {
		// Nothing selected yet: enter the traversal at the nearest end.
		if delta < 0 {
			next = len(ordered) - 1
		} else {
			next = 0
		}
	}
	wrapped := false
	if next >= len(ordered) {
		next, wrapped = 0, true
	}
	if next < 0 {
		next, wrapped = len(ordered)-1, true
	}

	t := ordered[next]
	m.selectedThreadID = t.ID
	if m.reviewVisited == nil {
		m.reviewVisited = make(map[string]bool)
	}
	m.reviewVisited[t.ID] = true
	m.paneVisible = true
	m.paneFocused = true
	m.threadScroll = 0

	var cmds []tea.Cmd
	if cmd := m.markSeen(t); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if wrapped && delta > 0 {
		m.reviewPassComplete = true
		open, unread := 0, 0
		for _, candidate := range ordered {
			if candidate.IsOpen() {
				open++
			}
			if m.isUnread(candidate) {
				unread++
			}
		}
		text := fmt.Sprintf("review pass complete · %d visited · %d open · %d unread · n continue", len(m.reviewVisited), open, unread)
		return m, func() tea.Msg { return readerFlashMsg{Text: text} }
	}

	targetSlug := t.Section
	if !m.sectionExists(targetSlug) {
		targetSlug = unanchoredSlug
	}
	idx := m.sectionIdx
	for i, sec := range m.readableSections() {
		if sec.Slug == targetSlug {
			idx = i
			break
		}
	}
	if idx != m.sectionIdx {
		// Cross-section step: the render is async (or a cache hit), so the
		// anchor scroll is deferred until the content lands.
		m.sectionIdx = idx
		m.pendingAnchorThreadID = t.ID
		var cmd tea.Cmd
		m, cmd = m.requestCurrentSectionRender()
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	} else {
		m.scrollToAnchor(t.ID)
	}
	return m, tea.Batch(cmds...)
}

func (m specDetailModel) threadByID(id string) (thread.Thread, bool) {
	for _, t := range m.threads {
		if t.ID == id {
			return t, true
		}
	}
	return thread.Thread{}, false
}

func (m specDetailModel) followThread(t thread.Thread) (specDetailModel, tea.Cmd) {
	target := t.Section
	if !m.sectionExists(target) {
		target = unanchoredSlug
	}
	for i, section := range m.readableSections() {
		if section.Slug == target {
			if i != m.sectionIdx {
				m.sectionIdx = i
				m.pendingAnchorThreadID = t.ID
				return m.requestCurrentSectionRender()
			}
			m.scrollToAnchor(t.ID)
			break
		}
	}
	return m, nil
}

// sectionExists reports whether a slug resolves against any live section.
func (m specDetailModel) sectionExists(slug string) bool {
	for _, sec := range m.sections {
		if sec.Slug == slug {
			return true
		}
	}
	return false
}

// unanchoredThreads returns threads whose section slug no longer resolves
// against any live section heading.
func (m specDetailModel) unanchoredThreads() []thread.Thread {
	var out []thread.Thread
	for _, t := range m.threads {
		if !m.sectionExists(t.Section) {
			out = append(out, t)
		}
	}
	return out
}

// reanchorTarget returns the single live section a thread's quote resolves
// in; ok=false when the thread has no quote or the match is absent or
// ambiguous. Ambiguity never guesses — the human decides.
func (m specDetailModel) reanchorTarget(t thread.Thread) (string, bool) {
	if t.Quote == "" {
		return "", false
	}
	var target string
	for _, sec := range m.sections {
		if sec.Level != 2 {
			continue
		}
		if markdown.ResolveAnchor(sec.Content, t.Quote, t.QuotePrefix).Found {
			if target != "" {
				return "", false // ambiguous — matches more than one section
			}
			target = sec.Slug
		}
	}
	return target, target != ""
}

// ── Anchor map lifecycle ────────────────────────────────────────────────────

// rebuildAnchors recomputes the rendered-line anchor map for the focused
// section. Called whenever the rendered content or the thread set changes —
// never inside view().
func (m *specDetailModel) rebuildAnchors() {
	m.anchors = anchorMap{}
	if m.readerContent == "" {
		return
	}
	sections := m.readableSections()
	if m.sectionIdx < 0 || m.sectionIdx >= len(sections) {
		return
	}
	sec := sections[m.sectionIdx]
	m.anchors = buildAnchorMapState(sec.Content, m.readerContent,
		m.threadsForSection(sec.Slug), m.selectedThreadID, m.isUnread)
}

// scrollToAnchor scrolls the reader so the thread's anchor line is visible
// with a little context above; section-level threads go to the section top.
func (m *specDetailModel) scrollToAnchor(threadID string) {
	line, ok := m.anchors.renderedLineFor(threadID)
	if !ok {
		m.readerViewport.GotoTop()
		return
	}
	offset := line - 2
	if offset < 0 {
		offset = 0
	}
	if mx := m.maxScroll(); offset > mx {
		offset = mx
	}
	m.readerViewport.SetYOffset(offset)
}
