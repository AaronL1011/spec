// Package components provides reusable TUI building blocks.
package components

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// Header renders the top bar: greeting, role, cycle, breadcrumb.
type Header struct {
	userName string
	role     string
	cycle    string
	width    int
	styles   HeaderStyles
}

// HeaderStyles holds the styles for the header component.
type HeaderStyles struct {
	Bar        lipgloss.Style
	Greeting   lipgloss.Style
	Meta       lipgloss.Style
	Breadcrumb lipgloss.Style
}

// NewHeader creates a new header component.
func NewHeader(userName, role, cycle string, styles HeaderStyles) Header {
	return Header{
		userName: userName,
		role:     role,
		cycle:    cycle,
		styles:   styles,
	}
}

// SetWidth updates the header width.
func (h *Header) SetWidth(w int) { h.width = w }

// View renders the header.
func (h Header) View() string {
	greeting := h.greeting()

	var meta []string
	if h.role != "" {
		meta = append(meta, h.role)
	}
	if h.cycle != "" {
		meta = append(meta, h.cycle)
	}

	left := h.styles.Greeting.Render(greeting)
	right := ""
	if len(meta) > 0 {
		right = h.styles.Meta.Render(strings.Join(meta, " · "))
	}

	// The bar is laid out inside the style's content box, which is narrower
	// than h.width by the style's horizontal padding/border. Sizing against
	// the full width would overflow and wrap the bar onto a second line —
	// diverging from Height() and breaking the app's row budget.
	inner := h.innerWidth()

	// At narrow widths, stack vertically instead of colliding.
	minGap := 2
	available := inner - lipgloss.Width(left) - lipgloss.Width(right)
	if available < minGap && right != "" {
		// Stack: greeting on top, meta below right-aligned. MaxHeight(1)
		// guarantees each bar stays a single line even when the text is
		// wider than the content box, keeping View() == Height() (2).
		topBar := h.styles.Bar.Width(h.width).MaxHeight(1).Render(left)
		botBar := h.styles.Bar.Width(h.width).Align(lipgloss.Right).MaxHeight(1).Render(right)
		return topBar + "\n" + botBar
	}

	gap := available
	if gap < 0 {
		gap = 0
	}

	// MaxHeight(1) is a safety net: Height() promises a single row here, so
	// the rendered bar must never wrap regardless of content width.
	bar := left + strings.Repeat(" ", gap) + right
	return h.styles.Bar.Width(h.width).MaxHeight(1).Render(bar)
}

// Height returns how many lines the header occupies at the current width.
// It must stay in lockstep with View(): the app reserves exactly this many
// rows for the header, so any divergence overflows the terminal and corrupts
// the layout.
func (h Header) Height() int {
	greeting := h.greeting()
	left := h.styles.Greeting.Render(greeting)

	var meta []string
	if h.role != "" {
		meta = append(meta, h.role)
	}
	if h.cycle != "" {
		meta = append(meta, h.cycle)
	}

	right := ""
	if len(meta) > 0 {
		right = h.styles.Meta.Render(strings.Join(meta, " · "))
	}

	available := h.innerWidth() - lipgloss.Width(left) - lipgloss.Width(right)
	if available < 2 && right != "" {
		return 2
	}
	return 1
}

// innerWidth is the content width available inside the Bar style after its
// horizontal padding/border is subtracted from the header width.
func (h Header) innerWidth() int {
	inner := h.width - h.styles.Bar.GetHorizontalFrameSize()
	if inner < 0 {
		return 0
	}
	return inner
}

func (h Header) greeting() string {
	hour := time.Now().Hour()
	name := h.userName
	if name == "" || name == "unknown" {
		name = "there"
	}
	switch {
	case hour >= 4 && hour < 6:
		return fmt.Sprintf("Catching the early worm, %s?", name)
	case hour >= 6 && hour < 12:
		return fmt.Sprintf("Good morning, %s", name)
	case hour >= 12 && hour < 17:
		return fmt.Sprintf("Good afternoon, %s", name)
	case hour >= 17 && hour < 21:
		return fmt.Sprintf("Good evening, %s", name)
	default:
		return fmt.Sprintf("Burning the midnight oil, %s?", name)
	}
}
