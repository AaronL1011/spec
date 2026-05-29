package components

import (
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/aaronl1011/spec/internal/tui/glyph"
)

// ToastKind distinguishes toast types.
type ToastKind int

const (
	ToastSuccess ToastKind = iota
	ToastError
	ToastInfo
)

// Toast renders an ephemeral notification.
type Toast struct {
	Message   string
	Kind      ToastKind
	ExpiresAt time.Time
	styles    ToastStyles
}

// ToastStyles holds the styles for toast notifications.
type ToastStyles struct {
	Success lipgloss.Style
	Error   lipgloss.Style
	Info    lipgloss.Style
}

// NewToast creates a new toast component.
func NewToast(styles ToastStyles) Toast {
	return Toast{styles: styles}
}

// Show displays a toast for the given duration.
func (t *Toast) Show(msg string, kind ToastKind, duration time.Duration) {
	t.Message = msg
	t.Kind = kind
	t.ExpiresAt = time.Now().Add(duration)
}

// Visible returns true if the toast has not expired.
func (t Toast) Visible() bool {
	return t.Message != "" && time.Now().Before(t.ExpiresAt)
}

// View renders the toast. Returns empty string if not visible.
func (t Toast) View() string {
	if !t.Visible() {
		return ""
	}

	switch t.Kind {
	case ToastSuccess:
		return t.styles.Success.Render(" " + glyph.ToastOK + " " + t.Message + " ")
	case ToastError:
		return t.styles.Error.Render(" " + glyph.ToastErr + " " + t.Message + " ")
	default:
		return t.styles.Info.Render(" " + glyph.ToastInfo + " " + t.Message + " ")
	}
}
