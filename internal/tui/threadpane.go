package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/thread"
)

// inputBodyHeight bounds the comment/reply textarea so the pinned-bottom pane
// stays within its height budget while still allowing multi-line entry.
const inputBodyHeight = 4

// threadInput tracks an in-progress ask or reply within the reader. kind is ""
// when no input is active. The body is a bubbles textarea so the user gets full
// cursor navigation and multi-line editing (Enter inserts a newline; ctrl+s
// submits) rather than an append-only single-line buffer.
type threadInput struct {
	kind     string // "ask" | "reply" | ""
	area     textarea.Model
	threadID string // target thread for a reply
	section  string // anchor section for an ask
}

func (i threadInput) active() bool { return i.kind != "" }

// body returns the trimmed text currently in the input area.
func (i threadInput) body() string { return strings.TrimSpace(i.area.Value()) }

// newThreadArea builds a focused, multi-line textarea for comment/reply entry,
// themed so the cursor-line fill matches the app palette rather than the
// library's near-white default.
func newThreadArea(theme Theme) textarea.Model {
	ta := textarea.New()
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.SetStyles(textAreaStyles(theme))
	ta.SetHeight(inputBodyHeight)
	// Focus drives the active styling; the blink command is intentionally
	// discarded so the caret renders statically within the pane.
	_ = ta.Focus()
	return ta
}

// ── Thread queries ──────────────────────────────────────────────────────────

// currentSectionSlug returns the slug of the section under the reader cursor.
func (m specDetailModel) currentSectionSlug() string {
	sections := m.readableSections()
	if m.sectionIdx >= 0 && m.sectionIdx < len(sections) {
		return sections[m.sectionIdx].Slug
	}
	return ""
}

// threadsForSection returns the threads anchored to a section slug.
func (m specDetailModel) threadsForSection(slug string) []thread.Thread {
	var out []thread.Thread
	for _, t := range m.threads {
		if t.Section == slug {
			out = append(out, t)
		}
	}
	return out
}

// openCountForSection counts open threads anchored to a section slug.
func (m specDetailModel) openCountForSection(slug string) int {
	n := 0
	for _, t := range m.threads {
		if t.Section == slug && t.IsOpen() {
			n++
		}
	}
	return n
}

// totalOpenThreads counts all open threads on the spec.
func (m specDetailModel) totalOpenThreads() int {
	n := 0
	for _, t := range m.threads {
		if t.IsOpen() {
			n++
		}
	}
	return n
}

// paneActiveForCurrentSection reports whether the thread pane should render:
// visible, plus the focused section has threads (or input is active).
func (m specDetailModel) paneActiveForCurrentSection() bool {
	if !m.paneVisible {
		return false
	}
	if m.input.active() {
		return true
	}
	return len(m.threadsForSection(m.currentSectionSlug())) > 0
}

// ── Key handling ────────────────────────────────────────────────────────────

// handleThreadInputKey processes a keystroke while an ask/reply prompt is open.
// It returns handled=false when the key is not consumed by the input.
func (m specDetailModel) handleThreadInputKey(msg tea.KeyPressMsg) (specDetailModel, tea.Cmd, bool) {
	if !m.input.active() {
		return m, nil, false
	}
	switch msg.String() {
	case "esc":
		m.input = threadInput{}
		return m, nil, true
	case "ctrl+s":
		return m.submitInput()
	default:
		// Delegate to the textarea: arrows, home/end, backspace, word jumps,
		// rune entry, and Enter (newline) are all handled natively. Absorbing
		// every key here keeps reader hotkeys from firing while typing.
		var cmd tea.Cmd
		m.input.area, cmd = m.input.area.Update(msg)
		return m, cmd, true
	}
}

// handleThreadActionKey processes thread action keys (a/r/x/t/tab) in reader
// mode. Returns handled=false when the key is not a thread action.
func (m specDetailModel) handleThreadActionKey(msg tea.KeyPressMsg) (specDetailModel, tea.Cmd, bool) {
	if msg.Text == "" && msg.Code != tea.KeyTab {
		return m, nil, false
	}
	if msg.String() == "tab" {
		// Focus toggles only when the pane is showing something to focus.
		if m.paneActiveForCurrentSection() {
			m.paneFocused = !m.paneFocused
			m.threadIdx = 0
			m.threadScroll = 0
		}
		return m, nil, true
	}

	switch msg.Text {
	case "t":
		m.paneVisible = !m.paneVisible
		if !m.paneVisible {
			m.paneFocused = false
			m.threadScroll = 0
		}
		return m, nil, true
	case "a":
		// Ask re-shows the pane so an action is never silently lost.
		m.paneVisible = true
		m.input = threadInput{kind: "ask", section: m.currentSectionSlug(), area: newThreadArea(m.theme)}
		m.sizeInputArea()
		return m, nil, true
	case "r":
		if t, ok := m.selectedThread(); ok {
			m.paneVisible = true
			m.input = threadInput{kind: "reply", threadID: t.ID, area: newThreadArea(m.theme)}
			m.sizeInputArea()
			return m, nil, true
		}
		return m, nil, true
	case "x":
		if t, ok := m.selectedThread(); ok && t.IsOpen() {
			return m, m.resolveThreadCmd(t.ID), true
		}
		return m, nil, true
	}
	return m, nil, false
}

// selectedThread returns the currently selected thread in the focused section.
func (m specDetailModel) selectedThread() (thread.Thread, bool) {
	ts := m.threadsForSection(m.currentSectionSlug())
	if len(ts) == 0 {
		return thread.Thread{}, false
	}
	idx := m.threadIdx
	if idx < 0 || idx >= len(ts) {
		idx = 0
	}
	return ts[idx], true
}

// selectThread moves the selection within the focused section's threads and
// resets the body scroll so the newly-selected thread starts from its top.
func (m specDetailModel) selectThread(delta int) specDetailModel {
	n := len(m.threadsForSection(m.currentSectionSlug()))
	if n == 0 {
		return m
	}
	m.threadIdx += delta
	if m.threadIdx < 0 {
		m.threadIdx = 0
	}
	if m.threadIdx >= n {
		m.threadIdx = n - 1
	}
	m.threadScroll = 0
	return m
}

// submitInput commits the active ask/reply prompt.
func (m specDetailModel) submitInput() (specDetailModel, tea.Cmd, bool) {
	in := m.input
	body := in.body()
	m.input = threadInput{}
	if body == "" {
		return m, nil, true // empty submit is ignored, not an error
	}
	switch in.kind {
	case "ask":
		return m, m.createThreadCmd(in.section, body), true
	case "reply":
		return m, m.replyThreadCmd(in.threadID, body), true
	}
	return m, nil, true
}

// ── Mutation commands ───────────────────────────────────────────────────────
//
// Mutations write to the local sidecar next to the spec in the specs-repo
// clone. They sync outward on the next `spec push`/`sync`, consistent with how
// the reader already operates on the local clone. Each returns a
// threadsChangedMsg carrying the refreshed set plus a toast string.

func (m specDetailModel) store() *thread.SidecarStore {
	dir := m.rc.SpecsRepoDir
	if p := m.specFilePath(); p != "" {
		dir = filepath.Dir(p)
	}
	return thread.NewSidecarStore(dir)
}

func (m specDetailModel) author() string {
	if h := m.rc.UserHandle(); h != "" {
		return h
	}
	return m.rc.UserName()
}

func (m specDetailModel) createThreadCmd(section, question string) tea.Cmd {
	store, specID, author := m.store(), m.specID, m.author()
	return func() tea.Msg {
		if _, err := store.Create(specID, section, author, question, nil); err != nil {
			return threadsChangedMsg{Err: err}
		}
		threads, err := store.List(specID)
		return threadsChangedMsg{Threads: threads, Err: err, Toast: "Question added"}
	}
}

func (m specDetailModel) replyThreadCmd(threadID, body string) tea.Cmd {
	store, specID, author := m.store(), m.specID, m.author()
	return func() tea.Msg {
		if _, err := store.Reply(specID, threadID, author, body, nil); err != nil {
			return threadsChangedMsg{Err: err}
		}
		threads, err := store.List(specID)
		return threadsChangedMsg{Threads: threads, Err: err, Toast: "Reply added"}
	}
}

func (m specDetailModel) resolveThreadCmd(threadID string) tea.Cmd {
	store, specID, author := m.store(), m.specID, m.author()
	return func() tea.Msg {
		if _, err := store.Resolve(specID, threadID, author); err != nil {
			return threadsChangedMsg{Err: err}
		}
		threads, err := store.List(specID)
		return threadsChangedMsg{Threads: threads, Err: err, Toast: "Thread resolved"}
	}
}

// ── Rendering ───────────────────────────────────────────────────────────────

// renderThreadPane returns the thread pane lines for the focused section,
// constrained to width w and at most maxHeight rows. Returns nil when the pane
// is not active.
//
// Layout: a fixed separator + header at the top, a fixed footer (hint or input)
// at the bottom, and a scrollable body between them. When the selected thread's
// full text exceeds the body budget, the body is windowed using threadScroll so
// the user can read everything by scrolling (↑/↓ while the pane is focused).
func (m specDetailModel) renderThreadPane(w, maxHeight int) []string {
	if !m.paneActiveForCurrentSection() {
		return nil
	}
	slug := m.currentSectionSlug()
	threads := m.threadsForSection(slug)
	open := m.openCountForSection(slug)

	sep := m.styles.Separator.Render(strings.Repeat("─", max(w, 8)))
	// Style the header inline with the accent colour rather than the
	// SectionTitle style: SectionTitle carries MarginTop(1), which injects a
	// leading newline and silently makes the pane one row taller than its line
	// count — overflowing the viewport and pushing the input off-screen.
	header := m.styles.Accent.Bold(true).Render(fmt.Sprintf(" %s Threads (%d open)", GlyphSection, open))

	var footer []string
	if m.input.active() {
		// Fit the textarea height to the budget: reserve the separator, header,
		// the prompt/hint chrome, and at least one body row, then cap at the
		// preferred multi-line height. Sizing here (value receiver) mutates only
		// the render-local copy.
		areaHeight := maxHeight - 2 - 1 - 1
		if areaHeight > inputBodyHeight {
			areaHeight = inputBodyHeight
		}
		if areaHeight < 1 {
			areaHeight = 1
		}
		m.input.area.SetHeight(areaHeight)
		footer = m.renderInputBlock()
	} else {
		footer = []string{m.styles.Muted.Render(threadHintLine(m.paneFocused))}
	}

	body := flattenLines(m.threadBodyLines(threads, w))

	// Budget for the scrollable body = maxHeight minus the fixed chrome rows
	// (separator, header, and the footer block which may span several rows for
	// the multi-line input). Keep at least one body row.
	bodyBudget := maxHeight - 2 - len(footer)
	if bodyBudget < 1 {
		bodyBudget = 1
	}

	start := 0
	if len(body) > bodyBudget {
		start = clampScroll(m.threadScroll, len(body)-bodyBudget)
	}
	window := body
	moreUp, moreDown := false, false
	if len(body) > bodyBudget {
		end := start + bodyBudget
		if end > len(body) {
			end = len(body)
		}
		window = body[start:end]
		moreUp = start > 0
		moreDown = end < len(body)
	}

	// Scroll affordances replace an edge row when there is hidden content.
	if moreUp && len(window) > 0 {
		window[0] = m.styles.Muted.Render("   ↑ more")
	}
	if moreDown && len(window) > 0 {
		window[len(window)-1] = m.styles.Muted.Render("   ↓ more")
	}

	out := append([]string{sep, header}, window...)
	out = append(out, footer...)
	return flattenLines(out)
}

// sizeInputArea fits the active input textarea to the current pane geometry so
// its wrapping matches the rendered width and its height stays within budget.
func (m *specDetailModel) sizeInputArea() {
	if !m.input.active() {
		return
	}
	w := max(m.width, 20)
	if m.width >= readerSidebarMinWidth {
		w = max(m.width-readerSidebarWidth-1, 20)
	}
	label := inputLabel(m.input)
	prefix := " " + label + " › "
	areaWidth := w - len([]rune(prefix))
	if areaWidth < 8 {
		areaWidth = 8
	}
	m.input.area.SetWidth(areaWidth)
}

// threadBodyLines renders the scrollable body of the pane: collapsed one-line
// previews for unselected threads, and the full word-wrapped question and
// replies for the selected thread.
func (m specDetailModel) threadBodyLines(threads []thread.Thread, w int) []string {
	selected, hasSel := m.selectedThread()
	var lines []string
	for _, t := range threads {
		isSel := hasSel && t.ID == selected.ID
		marker := m.styles.Accent.Render("●")
		if !t.IsOpen() {
			marker = m.styles.Muted.Render("✓")
		}
		caret := " "
		if m.paneFocused && isSel {
			caret = m.styles.Accent.Bold(true).Render("›")
		}

		if isSel {
			// The selected thread is shown in full: the question and every
			// reply body are word-wrapped so nothing is truncated.
			meta := m.styles.Muted.Render(fmt.Sprintf("%s · %s", t.Author, relTime(t.Created)))
			lines = append(lines, fmt.Sprintf("%s%s %s", caret, marker, meta))
			for _, ql := range wrapPlain(t.Question, max(w-3, 8)) {
				lines = append(lines, "   "+ql)
			}
			for _, r := range t.Replies {
				lines = append(lines, m.styles.Muted.Render("   └ "+r.Author))
				for _, bl := range wrapPlain(r.Body, max(w-5, 8)) {
					lines = append(lines, "     "+bl)
				}
			}
			continue
		}

		// Unselected threads are one-line collapsed previews; select one
		// (tab to focus, then ↑/↓) to read it in full.
		lines = append(lines, fmt.Sprintf("%s%s %s  %s  %s", caret, marker,
			m.styles.Muted.Render(t.Author), truncate(t.Question, max(w-26, 12)),
			m.styles.Muted.Render(relTime(t.Created))))
	}
	return lines
}

// maxThreadScroll returns the largest valid pane-body scroll offset for the
// current section, width, and height budget. Mirrors the geometry used by
// renderThreadPane so key handling and rendering agree.
func (m specDetailModel) maxThreadScroll() int {
	threads := m.threadsForSection(m.currentSectionSlug())
	if len(threads) == 0 {
		return 0
	}
	w := max(m.width-27, 20)
	if m.width < 100 {
		w = max(m.width, 20)
	}
	budget := max(m.height/2, 6) - 3
	if budget < 1 {
		budget = 1
	}
	body := flattenLines(m.threadBodyLines(threads, w))
	if len(body) <= budget {
		return 0
	}
	return len(body) - budget
}

// clampScroll bounds a scroll offset to [0, maxStart].
func clampScroll(v, maxStart int) int {
	if v < 0 {
		return 0
	}
	if v > maxStart {
		return maxStart
	}
	return v
}

// wrapPlain word-wraps plain (un-styled) text to at most width columns,
// returning one string per visual row. Words longer than the width are hard
// split so a single long token never overflows. Returns at least one row.
func wrapPlain(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	var rows []string
	for _, word := range strings.Fields(s) {
		// Hard-split an over-long word across rows.
		for len([]rune(word)) > width {
			r := []rune(word)
			rows = append(rows, string(r[:width]))
			word = string(r[width:])
		}
		if len(rows) == 0 {
			rows = append(rows, word)
			continue
		}
		last := rows[len(rows)-1]
		switch {
		case last == "":
			rows[len(rows)-1] = word
		case len([]rune(last))+1+len([]rune(word)) <= width:
			rows[len(rows)-1] = last + " " + word
		default:
			rows = append(rows, word)
		}
	}
	if len(rows) == 0 {
		return []string{""}
	}
	return rows
}

// flattenLines splits any entries that contain embedded newlines so the slice
// length always equals the number of visual rows.
func flattenLines(in []string) []string {
	out := make([]string, 0, len(in))
	for _, l := range in {
		if strings.IndexByte(l, '\n') >= 0 {
			out = append(out, strings.Split(l, "\n")...)
			continue
		}
		out = append(out, l)
	}
	return out
}

// inputLabel returns the prompt label for the active ask/reply input.
func inputLabel(in threadInput) string {
	label := "ask"
	if in.kind == "reply" {
		label = "reply"
	}
	if in.kind == "ask" && in.section != "" {
		label = "ask §" + in.section
	}
	return label
}

// renderInputBlock renders the active ask/reply prompt as a labelled,
// multi-line textarea block. The first row carries the prompt label; the
// textarea body (with full cursor navigation) follows, then a one-line hint.
// Returns one string per visual row so the pane can budget its height. The
// textarea is pre-sized by the caller, so width is not needed here.
func (m specDetailModel) renderInputBlock() []string {
	prefix := " " + inputLabel(m.input) + " › "
	areaView := strings.Split(m.input.area.View(), "\n")

	rows := make([]string, 0, len(areaView)+1)
	for i, line := range areaView {
		if i == 0 {
			rows = append(rows, m.styles.Accent.Render(prefix)+line)
			continue
		}
		// Indent continuation rows under the textarea body.
		rows = append(rows, strings.Repeat(" ", len([]rune(prefix)))+line)
	}
	rows = append(rows, m.styles.Muted.Render(" enter newline · ctrl+s send · esc cancel"))
	return rows
}

func threadHintLine(focused bool) string {
	if focused {
		return " [r]eply [x]resolve  ·  ↑/↓ scroll · n/p thread · tab text · [t] hide"
	}
	return " [a]sk  ·  tab focus threads  ·  [t] hide"
}

// relTime formats a timestamp as a short relative string (e.g. "2h", "3d").
func relTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
