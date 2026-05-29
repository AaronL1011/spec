package components

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/aaronl1011/spec/internal/tui/glyph"
)

func testToastStyles() ToastStyles {
	return ToastStyles{
		Success: lipgloss.NewStyle(),
		Error:   lipgloss.NewStyle(),
		Info:    lipgloss.NewStyle(),
	}
}

func TestToast_NotVisibleByDefault(t *testing.T) {
	toast := NewToast(testToastStyles())
	if toast.Visible() {
		t.Error("toast should not be visible by default")
	}
	if got := toast.View(); got != "" {
		t.Errorf("hidden toast should render empty, got: %q", got)
	}
}

func TestToast_ShowSuccess(t *testing.T) {
	toast := NewToast(testToastStyles())
	toast.Show("Spec advanced", ToastSuccess, 5*time.Second)

	if !toast.Visible() {
		t.Error("toast should be visible after Show")
	}

	got := toast.View()
	if !strings.Contains(got, "Spec advanced") {
		t.Errorf("toast should contain message, got: %q", got)
	}
	if !strings.Contains(got, glyph.ToastOK) {
		t.Error("success toast should contain success glyph")
	}
}

func TestToast_ShowError(t *testing.T) {
	toast := NewToast(testToastStyles())
	toast.Show("something broke", ToastError, 5*time.Second)

	got := toast.View()
	if !strings.Contains(got, glyph.ToastErr) {
		t.Error("error toast should contain error glyph")
	}
}

func TestToast_ExpiresAfterDuration(t *testing.T) {
	toast := NewToast(testToastStyles())
	toast.Show("flash", ToastInfo, 1*time.Millisecond)

	// Sleep just past expiry.
	time.Sleep(5 * time.Millisecond)

	if toast.Visible() {
		t.Error("toast should have expired")
	}
	if got := toast.View(); got != "" {
		t.Error("expired toast should render empty")
	}
}
