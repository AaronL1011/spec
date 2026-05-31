package components

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/aaronl1011/spec/internal/tui/glyph"
)

func testStatusStyles() StatusStyles {
	return StatusStyles{
		Idle:    lipgloss.NewStyle(),
		Pending: lipgloss.NewStyle(),
		Success: lipgloss.NewStyle(),
		Error:   lipgloss.NewStyle(),
	}
}

// AC-4: the idle state is non-empty so the slot never collapses, and it
// reports the standing pending-spec signal folded in from the old badge.
func TestStatus_IdleReportsPendingCount(t *testing.T) {
	s := NewStatus(testStatusStyles())
	if s.Kind() != StatusIdle {
		t.Errorf("default kind = %v, want StatusIdle", s.Kind())
	}

	// Zero pending: still non-empty, shows the clear-state label.
	if got := s.View(); strings.TrimSpace(got) == "" {
		t.Fatal("idle status must render a non-empty slot")
	}
	if got := s.View(); !strings.Contains(got, idleClearLabel) {
		t.Errorf("empty idle status should show %q, got %q", idleClearLabel, got)
	}

	// Non-zero pending: the resting state surfaces the count.
	s.SetRestingCount(4)
	if got := s.View(); !strings.Contains(got, "4 pending") {
		t.Errorf("idle status should report pending count, got %q", got)
	}
}

// A transient outcome overrides the resting count, then decays back to it.
func TestStatus_OutcomeDecaysBackToPendingCount(t *testing.T) {
	s := NewStatus(testStatusStyles())
	s.SetRestingCount(3)
	s.SetSuccess("Saved", time.Millisecond)
	if got := s.View(); !strings.Contains(got, "Saved") {
		t.Fatalf("outcome should override resting count, got %q", got)
	}
	time.Sleep(2 * time.Millisecond)
	if got := s.View(); !strings.Contains(got, "3 pending") {
		t.Errorf("decayed status should return to pending count, got %q", got)
	}
}

// US-2 / AC-2: pending shows an animated icon and the present-tense label.
func TestStatus_PendingShowsLabelAndAnimates(t *testing.T) {
	s := NewStatus(testStatusStyles())
	s.SetPending("Syncing…")

	if !s.Animating() {
		t.Fatal("pending status must report Animating")
	}
	first := s.View()
	if !strings.Contains(first, "Syncing…") {
		t.Errorf("pending view should contain label, got %q", first)
	}

	s.NextFrame()
	second := s.View()
	if first == second {
		t.Fatal("advancing a frame while pending must change the render")
	}
}

// §7.1: non-pending states consume no animation frames.
func TestStatus_NonPendingDoesNotAnimate(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(s *Status)
	}{
		{"idle", func(s *Status) { s.SetIdle() }},
		{"success", func(s *Status) { s.SetSuccess("Saved", time.Minute) }},
		{"error", func(s *Status) { s.SetError("Failed", time.Minute) }},
	} {
		s := NewStatus(testStatusStyles())
		tc.set(&s)
		if s.Animating() {
			t.Errorf("%s state must not animate", tc.name)
		}
		before := s.View()
		s.NextFrame()
		if after := s.View(); before != after {
			t.Errorf("%s: NextFrame must be a no-op, render changed", tc.name)
		}
	}
}

// AC-5: success and error render distinct icons in the same element.
func TestStatus_SuccessAndErrorGlyphs(t *testing.T) {
	s := NewStatus(testStatusStyles())

	s.SetSuccess("Saved", time.Minute)
	if got := s.View(); !strings.Contains(got, glyph.Done) {
		t.Errorf("success should use the done glyph, got %q", got)
	}

	s.SetError("Push failed", time.Minute)
	if got := s.View(); !strings.Contains(got, glyph.Rejected) {
		t.Errorf("error should use the rejected glyph, got %q", got)
	}
}

// Transient outcomes decay back to idle once expired, with no extra timer.
func TestStatus_TransientDecaysToIdle(t *testing.T) {
	s := NewStatus(testStatusStyles())
	s.SetSuccess("Saved", time.Millisecond)
	if s.Kind() != StatusSuccess {
		t.Fatalf("immediately after set, kind = %v, want success", s.Kind())
	}
	time.Sleep(2 * time.Millisecond)
	if s.Kind() != StatusIdle {
		t.Errorf("after expiry, kind = %v, want idle", s.Kind())
	}
	if !strings.Contains(s.View(), idleClearLabel) {
		t.Errorf("decayed status should show idle label, got %q", s.View())
	}
}

// AC-3: the slot is fixed-footprint — switching kind never changes its width.
func TestStatusBar_FixedFootprintAcrossKinds(t *testing.T) {
	styles := StatusBarStyles{
		Bar:     lipgloss.NewStyle(),
		Label:   lipgloss.NewStyle(),
		Pending: lipgloss.NewStyle(),
		Hint:    lipgloss.NewStyle(),
		Clock:   lipgloss.NewStyle(),
		Stale:   lipgloss.NewStyle(),
		Status:  testStatusStyles(),
	}
	sb := NewStatusBar(styles)
	sb.SetWidth(120)

	widthOf := func() int {
		// Width of just the status slot, computed the same way the bar does.
		return lipgloss.Width(sb.renderStatusSlot())
	}

	sb.SetStatusIdle()
	idleW := widthOf()

	sb.SetStatusPending("Syncing…")
	if w := widthOf(); w != idleW {
		t.Errorf("pending slot width = %d, want %d (idle)", w, idleW)
	}

	sb.SetStatusSuccess("Saved", time.Minute)
	if w := widthOf(); w != idleW {
		t.Errorf("success slot width = %d, want %d (idle)", w, idleW)
	}

	// A very long label must truncate to the fixed footprint, not widen it.
	sb.SetStatusError(strings.Repeat("x", 200), time.Minute)
	if w := widthOf(); w != idleW {
		t.Errorf("over-long error slot width = %d, want %d (idle)", w, idleW)
	}
	if !strings.Contains(ansi.Strip(sb.renderStatusSlot()), "…") {
		t.Error("over-long label should be truncated with an ellipsis")
	}
}
