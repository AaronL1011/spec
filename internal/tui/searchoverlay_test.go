package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/search"
)

// TestSearchOverlayOpensFromEveryView asserts `/` opens the overlay from each
// top-level view (AC-1).
func TestSearchOverlayOpensFromEveryView(t *testing.T) {
	views := []View{
		ViewDashboard, ViewPipeline, ViewSpecs, ViewTriage, ViewReviews, ViewSecurity, ViewSettings,
	}
	for _, v := range views {
		t.Run(v.Label(), func(t *testing.T) {
			app := testApp()
			app.width = 100
			app.height = 30
			app.propagateSize()
			app.switchView(v)

			model, _ := app.Update(keyMsg("/"))
			a := model.(App)
			if !a.search.visible {
				t.Fatalf("overlay should open from %s", v.Label())
			}
			// The next keystroke lands in the query, not a hotkey.
			model, _ = a.Update(keyMsg("z"))
			a = model.(App)
			if got := a.search.input.Value(); got != "z" {
				t.Errorf("first keystroke after open = %q, want 'z'", got)
			}
		})
	}
}

// TestSearchOverlayEscClosesAndDoesNotArmExit pins AC-9/AC-10: a single Esc
// closes the overlay and must not arm the double-esc exit guard.
func TestSearchOverlayEscClosesAndDoesNotArmExit(t *testing.T) {
	app := testApp()
	app.width = 80
	app.height = 24
	app.propagateSize()
	app.switchView(ViewDashboard)

	model, _ := app.Update(keyMsg("/"))
	a := model.(App)
	if !a.search.visible {
		t.Fatal("overlay should be open")
	}
	model, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	a = model.(App)
	if a.search.visible {
		t.Error("esc should close the overlay")
	}
	if a.exitArmed {
		t.Error("esc closing the overlay must not arm exit")
	}
}

// TestSearchOverlayGenerationDiscardsStaleResults asserts AC-5: a stale
// searchResultsMsg (older generation) is dropped, so fast typing never renders
// out-of-order results.
func TestSearchOverlayGenerationDiscardsStaleResults(t *testing.T) {
	m := newSearchOverlay(NewStyles(ResolveTheme("auto")), ResolveTheme("auto"))
	m.visible = true
	m.input.Focus()

	// Type "ab": each keystroke bumps the generation.
	m, _ = m.update(keyMsg("a"))
	genAfterA := m.gen
	m, _ = m.update(keyMsg("b"))
	if m.gen <= genAfterA {
		t.Fatal("typing should advance the generation")
	}

	// A result tagged with the older generation must be discarded.
	stale := searchResultsMsg{Gen: genAfterA, Hits: []search.Hit{{SpecID: "SPEC-STALE"}}}
	m, _ = m.update(stale)
	if len(m.results) != 0 {
		t.Errorf("stale-generation results should be discarded, got %+v", m.results)
	}

	// A result tagged with the current generation is applied.
	current := searchResultsMsg{Gen: m.gen, Hits: []search.Hit{{SpecID: "SPEC-FRESH"}}}
	m, _ = m.update(current)
	if len(m.results) != 1 || m.results[0].SpecID != "SPEC-FRESH" {
		t.Errorf("current-generation results should be applied, got %+v", m.results)
	}
}

// TestSearchOverlayScopeCycle asserts tab cycles all → active → archived → all.
func TestSearchOverlayScopeCycle(t *testing.T) {
	m := newSearchOverlay(NewStyles(ResolveTheme("auto")), ResolveTheme("auto"))
	m.visible = true
	m.input.Focus()

	if m.scope != search.ScopeAll {
		t.Fatalf("initial scope = %v, want all", m.scope)
	}
	m, _ = m.update(keyMsg("tab"))
	if m.scope != search.ScopeActive {
		t.Errorf("after 1st tab = %v, want active", m.scope)
	}
	m, _ = m.update(keyMsg("tab"))
	if m.scope != search.ScopeArchived {
		t.Errorf("after 2nd tab = %v, want archived", m.scope)
	}
	m, _ = m.update(keyMsg("tab"))
	if m.scope != search.ScopeAll {
		t.Errorf("after 3rd tab = %v, want all", m.scope)
	}
}

// TestOpenDetailAtSectionSetsReaderMode asserts the deep-link opens the detail
// in reader mode on the requested section (AC-4), falling back to the first
// readable section when the slug is gone.
func TestOpenDetailAtSectionSetsReaderMode(t *testing.T) {
	app := testApp()
	app.width = 100
	app.height = 30
	app.propagateSize()

	app.openDetailAtSection("SPEC-001", "acceptance_criteria")
	if !app.detailFromSearch {
		t.Error("openDetailAtSection should set detailFromSearch")
	}
	if app.detail.pendingSectionSlug != "acceptance_criteria" {
		t.Errorf("pendingSectionSlug = %q, want acceptance_criteria", app.detail.pendingSectionSlug)
	}

	// Deliver a spec with that section; applyPendingSection should switch to
	// reader mode on it.
	sections := []markdown.Section{
		{Level: 2, Slug: "problem_statement", Heading: "## 1. Problem Statement"},
		{Level: 2, Slug: "acceptance_criteria", Heading: "## 6. Acceptance Criteria"},
	}
	model, _ := app.Update(specDetailDataMsg{
		Meta:     specMetaMinimal("SPEC-001", "draft"),
		Sections: sections,
	})
	a := model.(App)
	if !a.detail.readerMode {
		t.Error("deep-link should land in reader mode")
	}
	readable := a.detail.readableSections()
	if a.detail.sectionIdx >= len(readable) || readable[a.detail.sectionIdx].Slug != "acceptance_criteria" {
		t.Errorf("sectionIdx = %d, want the acceptance_criteria section", a.detail.sectionIdx)
	}
}

// TestOpenDetailAtSectionMissingSlugFallsBack asserts a gone slug lands on the
// first readable section with a soft notice (AC-4).
func TestOpenDetailAtSectionMissingSlugFallsBack(t *testing.T) {
	app := testApp()
	app.width = 100
	app.height = 30
	app.propagateSize()

	app.openDetailAtSection("SPEC-001", "gone_slug")
	model, _ := app.Update(specDetailDataMsg{
		Meta:     specMetaMinimal("SPEC-001", "draft"),
		Sections: []markdown.Section{{Level: 2, Slug: "problem_statement", Heading: "## 1. Problem Statement"}},
	})
	a := model.(App)
	if !a.detail.readerMode {
		t.Error("missing slug should still open in reader mode")
	}
	if a.detail.notice == "" {
		t.Error("missing slug should set a 'section moved' notice")
	}
}

// TestEscReturnsToOverlayAfterDeepLink asserts AC-9: Esc from a search-opened
// reader returns to the overlay, not the underlying view.
func TestEscReturnsToOverlayAfterDeepLink(t *testing.T) {
	app := testApp()
	app.width = 100
	app.height = 30
	app.propagateSize()

	app.openDetailAtSection("SPEC-001", "problem_statement")
	app.Update(specDetailDataMsg{
		Meta:     specMetaMinimal("SPEC-001", "draft"),
		Sections: []markdown.Section{{Level: 2, Slug: "problem_statement", Heading: "## 1. Problem Statement"}},
	})

	// In reader mode, Esc emits navigateBackMsg; with detailFromSearch set the
	// app returns to the overlay.
	model, _ := app.Update(navigateBackMsg{})
	a := model.(App)
	if a.showDetail {
		t.Error("Esc should close the detail")
	}
	if !a.search.visible {
		t.Error("Esc from a search-opened reader should reopen the overlay")
	}
}

// TestSearchOverlayRenderDoesNotPanic is a smoke test that the overlay renders
// in empty, no-results, and populated states without panicking.
func TestSearchOverlayRenderStates(t *testing.T) {
	m := newSearchOverlay(NewStyles(ResolveTheme("auto")), ResolveTheme("auto"))
	m.visible = true
	m.setSize(80, 24)

	_ = m.view() // empty query
	m.input.SetValue("zzz")
	_ = m.view() // no results
	m.results = []search.Hit{{SpecID: "SPEC-001", Title: "T", SectionHeading: "## TL;DR", Snippet: "match ⟨here⟩"}}
	m.input.SetValue("match")
	_ = m.view() // populated
}

// TestParseSnippetMultiByteMarkers pins the fix for the byte-math bug: the
// FTS5 highlight markers ⟨/⟩ are 3-byte runes, and splitting must produce
// clean UTF-8 segments with no leaked marker bytes.
func TestParseSnippetMultiByteMarkers(t *testing.T) {
	segs := parseSnippet("the ⟨sync⟩ engine handles ⟨conflict⟩ retries")
	want := []snippetSeg{
		{text: "the "},
		{text: "sync", term: true},
		{text: " engine handles "},
		{text: "conflict", term: true},
		{text: " retries"},
	}
	if len(segs) != len(want) {
		t.Fatalf("got %d segments %+v, want %d", len(segs), segs, len(want))
	}
	for i, s := range segs {
		if s != want[i] {
			t.Errorf("seg[%d] = %+v, want %+v", i, s, want[i])
		}
	}
}

// TestParseSnippetUnterminatedMarker asserts a dangling open marker degrades
// to plain text rather than corrupting output.
func TestParseSnippetUnterminatedMarker(t *testing.T) {
	segs := parseSnippet("tail ⟨dangling")
	if len(segs) != 2 || segs[0].text != "tail " || segs[1].text != "dangling" {
		t.Errorf("unexpected segments: %+v", segs)
	}
}

// TestGroupHitsClustersBySpecPreservingRank asserts hits group under their
// spec in first-appearance (best-rank) order, and the flattened visible list
// caps section rows per spec.
func TestGroupHitsClustersBySpecPreservingRank(t *testing.T) {
	hits := []search.Hit{
		{SpecID: "SPEC-013", SectionSlug: "a"},
		{SpecID: "SPEC-018", SectionSlug: "b"},
		{SpecID: "SPEC-013", SectionSlug: "c"},
		{SpecID: "SPEC-013", SectionSlug: "d"},
		{SpecID: "SPEC-013", SectionSlug: "e"},
	}
	groups := groupHits(hits)
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(groups))
	}
	if groups[0].specID != "SPEC-013" || groups[1].specID != "SPEC-018" {
		t.Errorf("group order = %s, %s; want SPEC-013 first", groups[0].specID, groups[1].specID)
	}
	if len(groups[0].hits) != 4 {
		t.Errorf("SPEC-013 group has %d hits, want 4", len(groups[0].hits))
	}

	m := newSearchOverlay(NewStyles(ResolveTheme("auto")), ResolveTheme("auto"))
	m.results = hits
	visible := m.visibleHitIndexes()
	// SPEC-013 capped at maxSectionRowsPerSpec (3) + SPEC-018's 1 = 4.
	if len(visible) != 4 {
		t.Fatalf("visible rows = %d, want 4", len(visible))
	}
	// Cursor row 3 (last) must resolve to SPEC-018's hit.
	if hits[visible[3]].SpecID != "SPEC-018" {
		t.Errorf("last visible row = %s, want SPEC-018", hits[visible[3]].SpecID)
	}
}

// TestSearchOverlayCursorBoundedByVisibleRows asserts down-arrow cannot move
// the cursor past the last visible (capped) row.
func TestSearchOverlayCursorBoundedByVisibleRows(t *testing.T) {
	m := newSearchOverlay(NewStyles(ResolveTheme("auto")), ResolveTheme("auto"))
	m.visible = true
	m.results = []search.Hit{
		{SpecID: "S1", SectionSlug: "a"},
		{SpecID: "S1", SectionSlug: "b"},
	}
	for range 5 {
		m, _ = m.update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1 (last visible row)", m.cursor)
	}
}

// TestWindowLinesKeepsCursorVisible asserts the viewport window always
// contains the cursor line and never exceeds the available height.
func TestWindowLinesKeepsCursorVisible(t *testing.T) {
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = fmt.Sprintf("line-%d", i)
	}
	cases := []struct{ cursor, avail int }{
		{0, 10}, {9, 10}, {10, 10}, {25, 10}, {39, 10}, {39, 5},
	}
	for _, tc := range cases {
		got := windowLines(lines, tc.cursor, tc.avail)
		if len(got) > tc.avail {
			t.Errorf("cursor=%d avail=%d: window %d lines, want <= %d", tc.cursor, tc.avail, len(got), tc.avail)
		}
		found := false
		want := fmt.Sprintf("line-%d", tc.cursor)
		for _, l := range got {
			if l == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("cursor=%d avail=%d: cursor line not in window", tc.cursor, tc.avail)
		}
	}
}

// TestSearchOverlayFooterAlwaysRendered asserts the footer hint strip survives
// a result set far taller than the overlay height.
func TestSearchOverlayFooterAlwaysRendered(t *testing.T) {
	m := newSearchOverlay(NewStyles(ResolveTheme("auto")), ResolveTheme("auto"))
	m.visible = true
	m.setSize(80, 20)
	m.input.SetValue("x")
	for i := range 50 {
		m.results = append(m.results, search.Hit{
			SpecID:         fmt.Sprintf("SPEC-%03d", i),
			Title:          "T",
			SectionHeading: "## S",
			Snippet:        "⟨x⟩",
		})
	}
	out := m.view()
	if !strings.Contains(out, "scope:") {
		t.Error("footer (scope chip) should always be visible")
	}
	if got := strings.Count(out, "\n"); got > 20 {
		t.Errorf("overlay rendered %d lines, want <= height 20", got)
	}
}

// --- test helpers ----------------------------------------------------------

func specMetaMinimal(id, status string) *markdown.SpecMeta {
	return &markdown.SpecMeta{ID: id, Title: "T", Status: status}
}
