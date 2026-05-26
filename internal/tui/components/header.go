// Package components provides reusable TUI building blocks.
package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
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

	// Fill the bar to full width
	gap := h.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}

	bar := left + strings.Repeat(" ", gap) + right
	return h.styles.Bar.Width(h.width).Render(bar)
}

func (h Header) greeting() string {
	hour := time.Now().Hour()
	name := h.userName
	if name == "" || name == "unknown" {
		name = "there"
	}
	switch {
	case hour >= 5 && hour < 12:
		return fmt.Sprintf("Good morning, %s", name)
	case hour >= 12 && hour < 17:
		return fmt.Sprintf("Afternoon, %s", name)
	case hour >= 17 && hour < 21:
		return fmt.Sprintf("Good evening, %s", name)
	default:
		return fmt.Sprintf("Late night, %s", name)
	}
}
