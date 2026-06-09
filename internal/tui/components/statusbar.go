package components

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/aaronl1011/spec/internal/tui/glyph"
)

// StatusBar renders the bottom bar: view name, the canonical status element,
// help hint, and time. The status element is the single authoritative place
// to learn "what's going on right now" (SPEC-016); it occupies the fixed slot
// formerly held by the scattered pending/spinner/banner/toast surfaces.
type StatusBar struct {
	viewLabel   string
	scrollPos   string // e.g. "3/12" — set by the active view
	lastRefresh time.Time
	width       int
	exitArmed   bool // true while the first esc has been pressed at the top level
	status      Status
	styles      StatusBarStyles
}

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
		lastRefresh: time.Now(),
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

// SetRefresh updates the last refresh time.
func (s *StatusBar) SetRefresh(t time.Time) { s.lastRefresh = t }

// SetScroll updates the scroll position indicator (e.g. "3/12").
func (s *StatusBar) SetScroll(pos string) { s.scrollPos = pos }

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

// View renders the status bar. The canonical status element sits immediately
// after the view label and sizes to its content; the freshness ("Ns ago")
// indicator lives in the right-hand cluster, only appearing once data is stale.
func (s StatusBar) View() string {
	viewPart := s.styles.Label.Render(" " + s.viewLabel + " ")

	// The single canonical status element — always present, content-width.
	// In its resting state it reports pending-spec count; in-flight operations
	// override it and decay back.
	statusPart := s.status.View()

	// Freshness indicator — show age if last refresh was >60s ago. Lives on the
	// right so it reads as ambient metadata, not part of the live status.
	var stalePart string
	if age := time.Since(s.lastRefresh); age > 60*time.Second {
		staleLabel := fmt.Sprintf("%ds ago", int(age.Seconds()))
		if age > 120*time.Second {
			staleLabel = fmt.Sprintf("%dm ago", int(age.Minutes()))
		}
		stalePart = s.styles.Stale.Render(" " + glyph.Clock + " " + staleLabel + " ")
	}

	var scrollPart string
	if s.scrollPos != "" {
		scrollPart = s.styles.Hint.Render(" " + s.scrollPos + " ")
	}
	var hint string
	if s.exitArmed {
		hint = s.styles.Pending.Render(" esc again to quit ")
	} else {
		hint = s.styles.Hint.Render(" ? help · esc/esc exit ")
	}
	clock := s.styles.Clock.Render(time.Now().Format(" 15:04 "))

	left := viewPart + " " + statusPart
	right := stalePart + scrollPart + hint + clock

	// Lay the bar out inside the style's content box. The Bar style adds
	// horizontal padding, so the usable width is narrower than s.width;
	// filling to the full width would overflow and wrap onto a second line,
	// which the app's single-row status budget cannot absorb.
	inner := s.width - s.styles.Bar.GetHorizontalFrameSize()
	if inner < 0 {
		inner = 0
	}

	gap := inner - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}

	// MaxHeight(1) keeps the status bar a single row even if content is
	// wider than the box; the app reserves exactly one row for it.
	bar := left + strings.Repeat(" ", gap) + right
	return s.styles.Bar.Width(s.width).MaxHeight(1).Render(bar)
}
