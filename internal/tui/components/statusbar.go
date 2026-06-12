package components

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/aaronl1011/spec/internal/tui/glyph"
)

// StatusBar renders the bottom bar: view name, the canonical status element,
// help hint, and time. The status element is the single authoritative place
// to learn "what's going on right now" (SPEC-016); it occupies the fixed slot
// formerly held by the scattered pending/spinner/banner/toast surfaces.
type StatusBar struct {
	viewLabel string
	scrollPos string // e.g. "3/12" — set by the active view
	// refreshedAt records the last successful data load per tab (keyed by the
	// app's refresh key). Each tab carries its own data-age clock so the
	// staleness indicator reflects the tab the user is actually looking at,
	// not a global last-refresh.
	refreshedAt map[string]time.Time
	activeKey   string // refresh key of the active tab, drives the indicator
	staleAfter  time.Duration
	offline     bool
	// updateNotice holds the latest available version when a newer release
	// exists, surfaced as an ambient affordance in the right-hand cluster. It is
	// persistent metadata (the "you have mail" model), distinct from the
	// canonical status element which owns transient operation outcomes.
	updateNotice string
	width        int
	exitArmed    bool // true while the first esc has been pressed at the top level
	status       Status
	styles       StatusBarStyles
}

// defaultStaleAfter is the age past which a tab's data is flagged "stale · r to
// refresh". It is a fallback used until the app sets its own threshold from the
// configured refresh interval.
const defaultStaleAfter = 60 * time.Second

// minLabelWidth is the fewest text cells a truncated view label may occupy
// before it is dropped entirely. Below this a label is illegible noise, so the
// space is better spent on the status element.
const minLabelWidth = 4

// StatusBarStyles holds the styles for the status bar.
type StatusBarStyles struct {
	Bar     lipgloss.Style
	Label   lipgloss.Style
	Pending lipgloss.Style
	Hint    lipgloss.Style
	Clock   lipgloss.Style
	Stale   lipgloss.Style
	Status  StatusStyles
}

// NewStatusBar creates a new status bar with the canonical status element in
// its idle state.
func NewStatusBar(styles StatusBarStyles) StatusBar {
	return StatusBar{
		styles:      styles,
		refreshedAt: make(map[string]time.Time),
		staleAfter:  defaultStaleAfter,
		status:      NewStatus(styles.Status),
	}
}

// SetView updates the displayed view label.
func (s *StatusBar) SetView(label string) { s.viewLabel = label }

// SetPending records the number of pending specs. This is the standing work
// signal shown by the canonical status element in its resting (idle) state —
// the former "N pending" badge, now folded into the single status element so
// the bar carries one status surface instead of two.
func (s *StatusBar) SetPending(count int) { s.status.SetRestingCount(count) }

// SetRefresh records a successful data load for the given tab's refresh key.
// An empty key is ignored so callers without a key never reset every tab.
func (s *StatusBar) SetRefresh(key string, t time.Time) {
	if key == "" {
		return
	}
	if s.refreshedAt == nil {
		s.refreshedAt = make(map[string]time.Time)
	}
	s.refreshedAt[key] = t
}

// SetActiveRefreshKey selects which tab's data-age clock the indicator shows.
func (s *StatusBar) SetActiveRefreshKey(key string) { s.activeKey = key }

// SetStaleAfter sets the age past which the active tab is flagged stale.
func (s *StatusBar) SetStaleAfter(d time.Duration) {
	if d > 0 {
		s.staleAfter = d
	}
}

// SetOffline toggles the offline affordance, which renders the data-age
// indicator in a muted "cached · offline" form.
func (s *StatusBar) SetOffline(offline bool) { s.offline = offline }

// SetScroll updates the scroll position indicator (e.g. "3/12").
func (s *StatusBar) SetScroll(pos string) { s.scrollPos = pos }

// SetUpdateAvailable records that a newer release (latest) is available, which
// renders a persistent "update" affordance in the right-hand cluster. An empty
// latest clears the notice.
func (s *StatusBar) SetUpdateAvailable(latest string) { s.updateNotice = latest }

// SetWidth updates the status bar width.
func (s *StatusBar) SetWidth(w int) { s.width = w }

// SetStatusPending puts the canonical status element into the animated pending
// state with a present-tense label. This replaces the retired spinner +
// pending notice; callers set the status model rather than toggling widgets.
func (s *StatusBar) SetStatusPending(label string) { s.status.SetPending(label) }

// SetStatusSuccess shows a success outcome in the status element; it decays
// back to idle after duration. Replaces the success toast path.
func (s *StatusBar) SetStatusSuccess(label string, duration time.Duration) {
	s.status.SetSuccess(label, duration)
}

// SetStatusError shows a sticky error in the status element. It stays until the
// next operation supersedes it or the user dismisses it; the full message is
// reachable via ErrorDetail (shown in a modal on demand). Replaces the error
// toast/banner path. summary is the short slot-sized headline; detail is the
// full untruncated message.
func (s *StatusBar) SetStatusError(summary, detail string) {
	s.status.SetError(summary, detail)
}

// HasError reports whether a sticky error is currently shown.
func (s StatusBar) HasError() bool { return s.status.HasError() }

// ErrorDetail returns the full untruncated text of the current error, or empty.
func (s StatusBar) ErrorDetail() string { return s.status.Detail() }

// SetStatusIdle returns the canonical status element to its resting state.
func (s *StatusBar) SetStatusIdle() { s.status.SetIdle() }

// SetExitArmed sets whether the status bar should show the double-esc-to-quit hint.
func (s *StatusBar) SetExitArmed(armed bool) { s.exitArmed = armed }

// NextSpinner advances the pending-status animation frame. It is a no-op
// unless the status element is pending, so idle/success/error consume no
// animation frames.
func (s *StatusBar) NextSpinner() { s.status.NextFrame() }

// Animating reports whether the status element needs animation ticks.
func (s StatusBar) Animating() bool { return s.status.Animating() }

// StatusKind returns the canonical status element's current (decay-resolved)
// kind. Intended for assertions about which status is showing.
func (s StatusBar) StatusKind() StatusKind { return s.status.Kind() }

// StatusLabel returns the canonical status element's current label.
func (s StatusBar) StatusLabel() string { return s.status.Label() }

// ShowingOutcome reports whether a live (non-decayed) success or error outcome
// is currently displayed, so callers can avoid stomping it with lower-salience
// pending cues such as a background refresh.
func (s StatusBar) ShowingOutcome() bool { return s.status.ShowingOutcome() }

// freshnessIndicator renders the active tab's data-age affordance (§5.2) at
// full verbosity:
//   - offline:        "cached · offline" in the muted style
//   - within staleAfter: "fresh"
//   - past staleAfter:   "stale · r to refresh"
//   - otherwise:         "Ns ago" / "Nm ago"
//
// It returns the empty string when the active tab has never loaded, so a
// just-opened tab shows nothing until its first fetch lands. The richest form
// is forms[0] of freshnessForms; narrower terminals fall back to sparser forms.
func (s StatusBar) freshnessIndicator() string {
	return firstForm(s.freshnessForms())
}

// freshnessForms returns the data-age affordance from richest to sparsest, so
// the layout can trade detail for width: full label → short label → glyph only.
func (s StatusBar) freshnessForms() []string {
	st := s.styles.Stale
	glyphOnly := st.Render(" " + glyph.Clock + " ")

	if s.offline {
		return []string{
			st.Render(" " + glyph.Clock + " cached · offline "),
			st.Render(" " + glyph.Clock + " offline "),
			glyphOnly,
		}
	}

	t, ok := s.refreshedAt[s.activeKey]
	if !ok || t.IsZero() {
		return nil
	}

	age := time.Since(t)
	switch {
	case age >= s.staleAfter:
		return []string{
			st.Render(" " + glyph.Clock + " stale · r to refresh "),
			st.Render(" " + glyph.Clock + " stale "),
			glyphOnly,
		}
	case age < 5*time.Second:
		return []string{st.Render(" " + glyph.Clock + " fresh "), glyphOnly}
	case age < time.Minute:
		return []string{st.Render(fmt.Sprintf(" %s %ds ago ", glyph.Clock, int(age.Seconds()))), glyphOnly}
	default:
		return []string{st.Render(fmt.Sprintf(" %s %dm ago ", glyph.Clock, int(age.Minutes()))), glyphOnly}
	}
}

// updateForms returns the "newer version available" affordance from richest to
// sparsest: "↑ v0.4.0 · spec update" → "↑ v0.4.0" → "↑". Styled with the pending
// (amber) tone so it reads as gently actionable, not as an error.
func (s StatusBar) updateForms() []string {
	if s.updateNotice == "" {
		return nil
	}
	st := s.styles.Pending
	return []string{
		st.Render(fmt.Sprintf(" %s %s · spec update ", glyph.Upgrade, s.updateNotice)),
		st.Render(fmt.Sprintf(" %s %s ", glyph.Upgrade, s.updateNotice)),
		st.Render(" " + glyph.Upgrade + " "),
	}
}

// scrollForms returns the scroll-position indicator. It has a single form.
func (s StatusBar) scrollForms() []string {
	if s.scrollPos == "" {
		return nil
	}
	return []string{s.styles.Hint.Render(" " + s.scrollPos + " ")}
}

// hintForms returns the help/exit hint from richest to sparsest. While exit is
// armed it shows the double-esc safety prompt (kept legible, so only lightly
// abbreviated); otherwise it advertises help and exit.
func (s StatusBar) hintForms() []string {
	if s.exitArmed {
		st := s.styles.Pending
		return []string{st.Render(" esc again to quit "), st.Render(" esc to quit ")}
	}
	st := s.styles.Hint
	return []string{
		st.Render(" ? help · esc/esc exit "),
		st.Render(" ? help "),
		st.Render(" ? "),
	}
}

// clockForms returns the wall clock. It has a single form.
func (s StatusBar) clockForms() []string {
	return []string{s.styles.Clock.Render(time.Now().Format(" 15:04 "))}
}

// fit returns the richest form that fits within budget and the budget left
// after reserving it. When no form fits (or the slot is absent) it returns ""
// and leaves the budget untouched, dropping the slot.
func fit(forms []string, budget int) (string, int) {
	for _, f := range forms {
		if w := lipgloss.Width(f); w <= budget {
			return f, budget - w
		}
	}
	return "", budget
}

// firstForm returns the richest (first) form, or empty when there are none.
func firstForm(forms []string) string {
	if len(forms) == 0 {
		return ""
	}
	return forms[0]
}

// fitLabel renders the view label, truncating it to fit the budget rather than
// dropping it outright — the active view's name is high-value context. It
// vanishes only when the budget cannot hold a legible stub (minLabelWidth).
func (s StatusBar) fitLabel(budget int) (string, int) {
	if s.viewLabel == "" || budget <= 0 {
		return "", budget
	}
	full := " " + s.viewLabel + " "
	if w := lipgloss.Width(full); w <= budget {
		return s.styles.Label.Render(full), budget - w
	}
	// Reserve two cells for the surrounding padding before truncating the text.
	textBudget := budget - 2
	if textBudget < minLabelWidth {
		return "", budget
	}
	rendered := " " + ansi.Truncate(s.viewLabel, textBudget, "…") + " "
	return s.styles.Label.Render(rendered), budget - lipgloss.Width(rendered)
}

// View renders the status bar within a single row. The canonical status element
// is the mandatory anchor; every other slot is fitted by descending importance
// into the remaining width, taking the richest form that fits and vanishing
// before any higher-priority slot does (measure-and-fit). This keeps the wide
// view fully detailed while small windows degrade to a tidy, clipped-free bar.
func (s StatusBar) View() string {
	// The Bar style adds horizontal padding, so the usable width is narrower
	// than s.width; filling the full width would overflow and wrap onto a
	// second line, which the app's single-row status budget cannot absorb.
	inner := s.width - s.styles.Bar.GetHorizontalFrameSize()
	if inner < 0 {
		inner = 0
	}

	// The status element is always present and content-sized. In its resting
	// state it reports pending-spec count; in-flight operations override it and
	// decay back. Reserve its width plus one cell for the gap before it.
	statusPart := s.status.View()
	remaining := inner - lipgloss.Width(statusPart) - 1

	// Allocate the optional slots by descending importance. Each consumes the
	// richest form that still fits; lower-priority slots vanish first. The hint
	// occupies one visual position but two priorities: while exit is armed it is
	// a safety prompt allocated ahead of everything else, otherwise it is a
	// low-value help reminder allocated after the ambient signals.
	var hintPart string
	if s.exitArmed {
		hintPart, remaining = fit(s.hintForms(), remaining)
	}
	viewPart, remaining := s.fitLabel(remaining)
	scrollPart, remaining := fit(s.scrollForms(), remaining)
	freshPart, remaining := fit(s.freshnessForms(), remaining)
	updatePart, remaining := fit(s.updateForms(), remaining)
	if !s.exitArmed {
		hintPart, remaining = fit(s.hintForms(), remaining)
	}
	clockPart, _ := fit(s.clockForms(), remaining)

	// Assemble in fixed left-to-right visual order, independent of the
	// importance order used for allocation above.
	leftCluster := statusPart
	if viewPart != "" {
		leftCluster = viewPart + " " + statusPart
	}
	right := updatePart + freshPart + scrollPart + hintPart + clockPart

	gap := inner - lipgloss.Width(leftCluster) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}

	// MaxHeight(1) keeps the status bar a single row even if content is
	// wider than the box; the app reserves exactly one row for it.
	bar := leftCluster + strings.Repeat(" ", gap) + right
	return s.styles.Bar.Width(s.width).MaxHeight(1).Render(bar)
}
