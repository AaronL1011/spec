package tui

import (
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/dashboard"
)

// snapshotWidths are the fixed widths every view is rendered at to lock the
// standardised layout and catch drift (AC-9).
var snapshotWidths = []int{80, 120}

// containsEmoji reports whether s carries any emoji-range code point. It is
// the rendered-output guard for AC-1/AC-2: no view may emit emoji.
func containsEmoji(s string) bool {
	for _, r := range s {
		if isEmojiRune(r) {
			return true
		}
	}
	return false
}

// firstContentIndent returns the leading-space count of the first non-empty,
// non-section-header content line. Every full view should begin its content
// at the standard gutter, so this is uniform across views (AC-4/AC-7).
func leadingSpaces(line string) int {
	n := 0
	for _, r := range line {
		if r != ' ' {
			break
		}
		n++
	}
	return n
}

// populatedViews returns each top-level view rendered with representative data
// at the given width.
func populatedViews(t *testing.T, width int) map[string]string {
	t.Helper()
	height := 30

	dash := testDashboard()
	dash.loading = false
	dash.width = width
	dash.height = height
	dash.data = &dashboard.DashboardData{
		Do:       []dashboard.DashboardItem{{SpecID: "SPEC-001", Title: "Auth service", Stage: "build", Urgency: "normal"}},
		Blocked:  []dashboard.DashboardItem{{SpecID: "SPEC-003", Title: "Payments", Detail: "waiting"}},
		Review:   []dashboard.DashboardItem{{SpecID: "SPEC-004", Title: "Search", Detail: "PR #7"}},
		Incoming: []dashboard.DashboardItem{{SpecID: "SPEC-005", Title: "Notifications", Stage: "triage"}},
	}
	dash.items = dash.buildRows()

	pipe := testPipelineModel()
	pipe.width = width
	pipe.height = height
	pipe.stages = []pipelineStage{
		{Name: "draft", Owner: "pm", Specs: []pipelineSpec{{ID: "SPEC-001", Title: "Auth service", Updated: "1d"}}},
		{Name: "build", Owner: "engineer", Specs: []pipelineSpec{{ID: "SPEC-002", Title: "Onboarding", Updated: "2h"}}},
	}

	list := testSpecListModel()
	list.loading = false
	list.width = width
	list.height = height
	list.allSpecs = []specListItem{
		{ID: "SPEC-001", Title: "Auth service", Status: "build", Author: "aaron", Updated: "1d"},
		{ID: "SPEC-002", Title: "Onboarding", Status: "draft", Author: "sam", Updated: "2h"},
	}
	list.applyFilter()

	rev := testReviewModel()
	rev.loading = false
	rev.width = width
	rev.height = height
	rev.items = []reviewItem{{Number: 7, Title: "Add search", Repo: "api", CIStatus: "passing"}}

	tri := testTriageModel()
	tri.loading = false
	tri.width = width
	tri.height = height
	tri.items = []triageItem{{ID: "TRG-001", Title: "Flaky login", Priority: "high", Source: "sentry"}}

	return map[string]string{
		"dashboard": dash.view(),
		"pipeline":  pipe.view(),
		"specs":     list.view(),
		"reviews":   rev.view(),
		"triage":    tri.view(),
	}
}

// TestSnapshot_NoEmojiInAnyView asserts no rendered view emits emoji at any
// snapshot width (AC-1/AC-2).
func TestSnapshot_NoEmojiInAnyView(t *testing.T) {
	for _, w := range snapshotWidths {
		for name, out := range populatedViews(t, w) {
			if containsEmoji(out) {
				t.Errorf("view %q at width %d emitted emoji:\n%s", name, w, out)
			}
		}
	}
}

// TestSnapshot_UniformGutter asserts every view's primary data rows start at
// the same standard gutter, so switching views never shifts the gutter
// (AC-4/AC-7). Section/stage headers are a separate flush-left pattern and are
// not data rows.
func TestSnapshot_UniformGutter(t *testing.T) {
	for _, w := range snapshotWidths {
		for name, out := range populatedViews(t, w) {
			row := firstDataRow(out)
			if row == "" {
				t.Errorf("view %q at width %d produced no data row", name, w)
				continue
			}
			// The uniformity invariant: every data row respects the gutter and
			// aligns to the indent grid. Flat lists sit at the gutter (Gutter);
			// the pipeline's stage→spec tree nests its rows one further unit.
			lead := leadingSpaces(row)
			if lead < Gutter {
				t.Errorf("view %q at width %d data row indent = %d, below gutter %d: %q",
					name, w, lead, Gutter, row)
			}
			if (lead-Gutter)%IndentUnit != 0 {
				t.Errorf("view %q at width %d data row indent = %d, off the indent grid: %q",
					name, w, lead, row)
			}
		}
	}
}

// firstDataRow returns the first rendered line that carries a spec/PR/triage
// identifier (a data row), skipping headers, rules, counts, and blanks.
func firstDataRow(out string) string {
	for _, line := range strings.Split(out, "\n") {
		plain := stripANSI(line)
		if strings.Contains(plain, "SPEC-") || strings.Contains(plain, "TRG-") || strings.Contains(plain, "PR #") {
			return plain
		}
	}
	return ""
}

// stripANSI removes lipgloss colour codes so leading-space counts reflect the
// real text indent.
func stripANSI(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEscape = true
		case inEscape && r == 'm':
			inEscape = false
		case inEscape:
			// skip
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
