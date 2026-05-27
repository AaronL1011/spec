package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// StatusBar renders the bottom bar: view name, pending count, help hint, time.
type StatusBar struct {
	viewLabel    string
	pendingCount int
	lastRefresh  time.Time
	width        int
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

// SetWidth updates the status bar width.
func (s *StatusBar) SetWidth(w int) { s.width = w }

// View renders the status bar.
func (s StatusBar) View() string {
	viewPart := s.styles.Label.Render(" " + s.viewLabel + " ")

	var parts []string
	if s.pendingCount > 0 {
		parts = append(parts, s.styles.Pending.Render(
			fmt.Sprintf(" ⚡ %d pending ", s.pendingCount),
		))
	}

	// Stale indicator — show age if last refresh was >60s ago.
	age := time.Since(s.lastRefresh)
	if age > 60*time.Second {
		staleLabel := fmt.Sprintf("%ds ago", int(age.Seconds()))
		if age > 120*time.Second {
			staleLabel = fmt.Sprintf("%dm ago", int(age.Minutes()))
		}
		parts = append(parts, s.styles.Stale.Render(" ⏳ "+staleLabel+" "))
	}

	hint := s.styles.Hint.Render(" ? help · q quit ")
	clock := s.styles.Clock.Render(time.Now().Format(" 15:04 "))

	left := viewPart
	if len(parts) > 0 {
		left += " " + strings.Join(parts, " ")
	}
	right := hint + clock

	gap := s.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}

	bar := left + strings.Repeat(" ", gap) + right
	return s.styles.Bar.Width(s.width).Render(bar)
}
