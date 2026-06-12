package components

import (
	"strings"
	"testing"
)

// wideBar returns a status bar sized wide enough to show every slot at full
// verbosity, for asserting content rather than layout.
func wideBar() StatusBar {
	s := testStatusBar()
	s.SetWidth(120)
	return s
}

// TestUpdateNotice_HiddenByDefault asserts no update affordance renders until a
// newer version is recorded.
func TestUpdateNotice_HiddenByDefault(t *testing.T) {
	s := wideBar()
	if strings.Contains(s.View(), "spec update") {
		t.Error("update affordance shown before a notice was set")
	}
}

// TestUpdateNotice_RendersVersion asserts the recorded version and the call to
// action appear at full width once an update is available.
func TestUpdateNotice_RendersVersion(t *testing.T) {
	s := wideBar()
	s.SetUpdateAvailable("v0.4.0")

	got := s.View()
	if !strings.Contains(got, "v0.4.0") || !strings.Contains(got, "spec update") {
		t.Errorf("status bar = %q, want version and 'spec update'", got)
	}
}

// TestUpdateNotice_ClearedByEmpty asserts an empty version clears the notice.
func TestUpdateNotice_ClearedByEmpty(t *testing.T) {
	s := wideBar()
	s.SetUpdateAvailable("v0.4.0")
	s.SetUpdateAvailable("")
	if strings.Contains(s.View(), "spec update") {
		t.Error("update affordance still shown after being cleared")
	}
}
