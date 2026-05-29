package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/aaronl1011/spec/internal/tui/glyph"
)

// StatusBar renders the bottom bar: view name, pending count, help hint, time.
type StatusBar struct {
	viewLabel    string
	pendingCount int
	scrollPos    string // e.g. "3/12" — set by the active view
	lastRefresh  time.Time
	width        int
	busy         bool
	busyLabel    string
	spinnerFrame int
	styles       StatusBarStyles
}

// StatusBarStyles holds the styles for the status bar.
type StatusBarStyles struct {
	Bar     lipgloss.Style
	Label   lipgloss.Style
	Pending lipgloss.Style
	Hint    lipgloss.Style
	Clock   lipgloss.Style
	Stale   lipgloss.Style
}

// NewStatusBar creates a new status bar.
func NewStatusBar(styles StatusBarStyles) StatusBar {
	return StatusBar{styles: styles, lastRefresh: time.Now()}
}

// SetView updates the displayed view label.
func (s *StatusBar) SetView(label string) { s.viewLabel = label }

// SetPending updates the pending count.
func (s *StatusBar) SetPending(count int) { s.pendingCount = count }

// SetRefresh updates the last refresh time.
func (s *StatusBar) SetRefresh(t time.Time) { s.lastRefresh = t }

// SetScroll updates the scroll position indicator (e.g. "3/12").
func (s *StatusBar) SetScroll(pos string) { s.scrollPos = pos }

// SetWidth updates the status bar width.
func (s *StatusBar) SetWidth(w int) { s.width = w }

// SetBusy updates whether the status bar should show an inline busy indicator.
func (s *StatusBar) SetBusy(active bool, label string) {
	s.busy = active
	s.busyLabel = strings.TrimSpace(label)
}

// NextSpinner advances the spinner animation frame.
func (s *StatusBar) NextSpinner() {
	s.spinnerFrame = (s.spinnerFrame + 1) % len(spinnerFrames)
}

// View renders the status bar.
func (s StatusBar) View() string {
	viewPart := s.styles.Label.Render(" " + s.viewLabel + " ")

	var parts []string
	if s.pendingCount > 0 {
		parts = append(parts, s.styles.Pending.Render(
			fmt.Sprintf(" %s %d pending ", glyph.Active, s.pendingCount),
		))
	}

	// Stale indicator — show age if last refresh was >60s ago.
	age := time.Since(s.lastRefresh)
	if age > 60*time.Second {
		staleLabel := fmt.Sprintf("%ds ago", int(age.Seconds()))
		if age > 120*time.Second {
			staleLabel = fmt.Sprintf("%dm ago", int(age.Minutes()))
		}
		parts = append(parts, s.styles.Stale.Render(" "+glyph.Clock+" "+staleLabel+" "))
	}
	if s.busy {
		label := s.busyLabel
		if label == "" {
			label = "working"
		}
		spinner := spinnerFrames[s.spinnerFrame%len(spinnerFrames)]
		parts = append(parts, s.styles.Pending.Render(fmt.Sprintf(" %s %s ", spinner, label)))
	}

	var scrollPart string
	if s.scrollPos != "" {
		scrollPart = s.styles.Hint.Render(" " + s.scrollPos + " ")
	}
	hint := s.styles.Hint.Render(" ? help · q quit ")
	clock := s.styles.Clock.Render(time.Now().Format(" 15:04 "))

	left := viewPart
	if len(parts) > 0 {
		left += " " + strings.Join(parts, " ")
	}
	right := scrollPart + hint + clock

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

var spinnerFrames = glyph.SpinnerFrames
