package tui

import (
	"fmt"
	"image/color"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/dashboard"
	"github.com/aaronl1011/spec/internal/markdown"
)

// bountyTheme is a theme with known colours so assertions can name them.
func bountyTheme() Theme {
	return ResolveTheme("catppuccin-mocha")
}

// enableBounties turns bounties on for a test model's config, optionally with
// shimmer disabled.
func enableBounties(rc *config.ResolvedConfig, shimmer bool) {
	rc.Team.Bounty = &config.BountyConfig{Enabled: true, Shimmer: &shimmer}
}

// TestBountyColor_DistinctFromWarning is why the gold is its own token: the
// urgency ramp already owns Warning, so reusing it would make a bountied row
// indistinguishable from a merely warm one.
func TestBountyColor_DistinctFromWarning(t *testing.T) {
	for _, name := range []string{"catppuccin-mocha", "github-light", "graphite", "nord"} {
		th := ResolveTheme(name)
		wr, wg, wb := rgb(th.Warning)
		br, bg, bb := rgb(th.BountyColor())
		if wr == br && wg == bg && wb == bb {
			t.Errorf("%s: bounty gold equals Warning (#%02x%02x%02x) — the two signals would collide",
				name, wr, wg, wb)
		}
	}
}

// TestBountyColor_HonoursThemeOverride covers graphite, which conveys status by
// luminance and sets the token explicitly.
func TestBountyColor_HonoursThemeOverride(t *testing.T) {
	th := Theme{Base: lipgloss.Color("#000000"), Bounty: lipgloss.Color("#abcdef")}
	r, g, b := rgb(th.BountyColor())
	if r != 0xab || g != 0xcd || b != 0xef {
		t.Errorf("BountyColor() = #%02x%02x%02x, want the theme's explicit value", r, g, b)
	}
	if got := rgbHex(ResolveTheme("graphite").BountyColor()); got != "#ffffff" {
		t.Errorf("graphite bounty = %s, want its brightest neutral", got)
	}
}

// TestBountyColor_AdaptsToBackground keeps the gold readable on light themes.
func TestBountyColor_AdaptsToBackground(t *testing.T) {
	dark := Theme{Base: lipgloss.Color("#101010")}
	light := Theme{Base: lipgloss.Color("#fafafa")}
	if rgbHex(dark.BountyColor()) == rgbHex(light.BountyColor()) {
		t.Error("gold must differ between light and dark backgrounds for contrast")
	}
}

// TestRenderBountyMark_PreservesWidth is AC-12: the marker must never change a
// row's width, or columns would shift when a spec is bountied.
func TestRenderBountyMark_PreservesWidth(t *testing.T) {
	th := bountyTheme()
	text := IconBounty + " SPEC-001  "
	for _, shimmer := range []bool{false, true} {
		for frame := 0; frame < bountyCycleFrames; frame += 7 {
			got := renderBountyMark(lipgloss.NewStyle(), text, frame, shimmer, th)
			if lipgloss.Width(got) != lipgloss.Width(text) {
				t.Fatalf("shimmer=%v frame=%d: width %d, want %d",
					shimmer, frame, lipgloss.Width(got), lipgloss.Width(text))
			}
			if !strings.Contains(stripANSI(got), strings.TrimSpace(text)) {
				t.Fatalf("shimmer=%v frame=%d: marker text lost: %q", shimmer, frame, got)
			}
		}
	}
}

// TestRenderBountyMark_ShimmerAnimates is AC-5: with shimmer on, the rendering
// changes between frames within a sweep; with it off, it never does.
func TestRenderBountyMark_ShimmerAnimates(t *testing.T) {
	th := bountyTheme()
	text := IconBounty + " SPEC-001"

	var seen []string
	for frame := 0; frame <= bountySweepFrames; frame++ {
		seen = append(seen, renderBountyMark(lipgloss.NewStyle(), text, frame, true, th))
	}
	distinct := map[string]bool{}
	for _, s := range seen {
		distinct[s] = true
	}
	if len(distinct) < 3 {
		t.Errorf("shimmer produced %d distinct frames across a sweep, want a moving sheen", len(distinct))
	}

	static := renderBountyMark(lipgloss.NewStyle(), text, 0, false, th)
	for frame := 0; frame < bountyCycleFrames; frame += 5 {
		if got := renderBountyMark(lipgloss.NewStyle(), text, frame, false, th); got != static {
			t.Fatalf("shimmer=false must be frame-independent, differed at frame %d", frame)
		}
	}
}

// TestBountySheenCenter_RestsBetweenSweeps keeps the animation calm: most of the
// cycle is a rest, not a pulse.
func TestBountySheenCenter_RestsBetweenSweeps(t *testing.T) {
	const width = 12
	resting := 0
	for frame := 0; frame < bountyCycleFrames; frame++ {
		if bountySheenCenter(frame, width) < -bountySheenRadius {
			resting++
		}
	}
	if resting < bountyCycleFrames/2 {
		t.Errorf("sheen rests for %d of %d frames; a bounty marker should be still most of the time",
			resting, bountyCycleFrames)
	}
	// The cycle repeats, and negative frames (never expected, but cheap to
	// guarantee) must not index out of the cycle.
	if bountySheenCenter(0, width) != bountySheenCenter(bountyCycleFrames, width) {
		t.Error("sheen position must repeat every cycle")
	}
	if bountySheenCenter(-1, width) < -2*bountySheenRadius {
		t.Error("negative frames must stay within the cycle")
	}
}

func TestBountyReasonLine(t *testing.T) {
	tests := []struct {
		reason, granter, want string
	}{
		{reason: "unblocks billing", granter: "aaron", want: "unblocks billing (aaron)"},
		{reason: "unblocks billing", granter: "", want: "unblocks billing"},
		{reason: "", granter: "aaron", want: "bountied by aaron"},
		{reason: "  ", granter: "  ", want: ""},
	}
	for _, tt := range tests {
		if got := bountyReasonLine(tt.reason, tt.granter); got != tt.want {
			t.Errorf("bountyReasonLine(%q, %q) = %q, want %q", tt.reason, tt.granter, got, tt.want)
		}
	}
}

// TestDashboard_BountyMarkerAndUrgencyCoexist is AC-4: the bounty owns the
// glyph and ID, the urgency gradient owns the title. A bountied, fully-stale
// row shows both.
func TestDashboard_BountyMarkerAndUrgencyCoexist(t *testing.T) {
	m := testDashboard()
	enableBounties(m.rc, false)
	m.loading = false
	m.width = 100
	m.data = &dashboard.DashboardData{
		Do: []dashboard.DashboardItem{
			{
				SpecID: "SPEC-001", Title: "Bountied and stale", Stage: "build",
				Bounty:        &markdown.BountyState{GrantedBy: "aaron", GrantedAt: "2026-07-28T09:00:00Z"},
				StaleFraction: 1,
			},
			{SpecID: "SPEC-002", Title: "Plain row", Stage: "build"},
		},
	}
	m.items = m.buildRows()

	rows := map[string]string{}
	for _, row := range m.items {
		rows[row.specID] = m.renderRow(row, false, 100)
	}

	bountied, plain := rows["SPEC-001"], rows["SPEC-002"]
	if !strings.Contains(stripANSI(bountied), IconBounty) {
		t.Errorf("bountied row should lead with the bounty glyph: %q", stripANSI(bountied))
	}
	if strings.Contains(stripANSI(plain), IconBounty) {
		t.Errorf("unbountied row must not show the bounty glyph: %q", stripANSI(plain))
	}

	gold := rgbHex(m.styles.Theme.BountyColor())
	hot := rgbHex(m.styles.Theme.RampColor(1))
	if !strings.Contains(ansiColors(bountied), gold) {
		t.Errorf("bountied row missing gold %s: %q", gold, bountied)
	}
	if !strings.Contains(ansiColors(bountied), hot) {
		t.Errorf("bountied row lost its urgency colour %s — the two signals must coexist: %q", hot, bountied)
	}
}

// TestDashboard_FocusWinsGlyphKeepsGold is the precedence rule: focus owns the
// glyph cell, the bounty keeps the gold ID, so neither signal disappears.
func TestDashboard_FocusWinsGlyphKeepsGold(t *testing.T) {
	m := testDashboard()
	enableBounties(m.rc, false)
	m.loading = false
	m.width = 100
	m.focusedSpecID = "SPEC-001"
	m.data = &dashboard.DashboardData{
		Do: []dashboard.DashboardItem{{
			SpecID: "SPEC-001", Title: "Focused and bountied", Stage: "build",
			Bounty: &markdown.BountyState{GrantedAt: "2026-07-28T09:00:00Z"},
		}},
	}
	m.items = m.buildRows()
	row := m.renderRow(m.items[0], false, 100)

	plainRow := stripANSI(row)
	if !strings.Contains(plainRow, IconFocus) {
		t.Errorf("focus should win the glyph cell: %q", plainRow)
	}
	if !strings.Contains(ansiColors(row), rgbHex(m.styles.Theme.BountyColor())) {
		t.Errorf("focused row must keep the gold SPEC-ID: %q", row)
	}
}

// TestDashboard_BountyRowWidthUnchanged is AC-12 on a real row.
func TestDashboard_BountyRowWidthUnchanged(t *testing.T) {
	m := testDashboard()
	enableBounties(m.rc, true)
	m.loading = false
	m.data = &dashboard.DashboardData{
		Do: []dashboard.DashboardItem{
			{SpecID: "SPEC-001", Title: "Same title", Stage: "build",
				Bounty: &markdown.BountyState{GrantedAt: "2026-07-28T09:00:00Z"}},
			{SpecID: "SPEC-002", Title: "Same title", Stage: "build"},
		},
	}
	m.items = m.buildRows()
	for _, width := range []int{50, 80, 120} {
		a := lipgloss.Width(m.renderRow(m.items[0], false, width))
		b := lipgloss.Width(m.renderRow(m.items[1], false, width))
		if a != b {
			t.Errorf("width=%d: bountied row is %d cells, unbountied %d — columns would shift", width, a, b)
		}
	}
}

// TestSpecList_BountyGutterOnlyWhenEnabled keeps the layout byte-identical for
// teams that do not use bounties.
func TestSpecList_BountyGutterOnlyWhenEnabled(t *testing.T) {
	m := testSpecListModel()
	if got := m.bountyGutter(true); got != "" {
		t.Errorf("gutter with bounties disabled = %q, want empty", got)
	}

	enableBounties(m.rc, false)
	if got := m.bountyGutter(false); got != "  " {
		t.Errorf("unbountied gutter = %q, want two blanks so columns hold", got)
	}
	if got := m.bountyGutter(true); got != IconBounty+" " {
		t.Errorf("bountied gutter = %q, want the bounty glyph", got)
	}
	// Both gutters are the same width, so rows stay aligned.
	if lipgloss.Width(m.bountyGutter(true)) != lipgloss.Width(m.bountyGutter(false)) {
		t.Error("bountied and unbountied gutters must be the same width")
	}
}

// TestSpecList_BountyRowFitsWidth guards the row budget with the gutter present.
func TestSpecList_BountyRowFitsWidth(t *testing.T) {
	m := testSpecListModel()
	enableBounties(m.rc, false)
	for _, w := range []int{50, 60, 70, 80, 100, 120} {
		mark, rest := m.formatRow("SPEC-001",
			"A very long spec title that could overflow the row width boundary",
			"in_progress", "alice", "2026-05-26", m.bountyGutter(true), w)
		if got := lipgloss.Width(mark + rest); got > w {
			t.Errorf("width=%d: row is %d cells", w, got)
		}
	}
}

// TestPipeline_BountyRowWidthUnchanged is AC-12 on the pipeline screen.
func TestPipeline_BountyRowWidthUnchanged(t *testing.T) {
	rc := testResolvedConfig()
	enableBounties(rc, false)
	m := newPipeline(rc, NewStyles(bountyTheme()), DefaultKeyMap())
	m.width = 100

	bountied := m.renderPipelineRow(pipelineSpec{ID: "SPEC-001", Title: "Same", Updated: "2026-07-28", bountied: true}, false)
	plain := m.renderPipelineRow(pipelineSpec{ID: "SPEC-002", Title: "Same", Updated: "2026-07-28"}, false)
	if lipgloss.Width(bountied) != lipgloss.Width(plain) {
		t.Errorf("bountied row %d cells, plain %d", lipgloss.Width(bountied), lipgloss.Width(plain))
	}
	if !strings.Contains(stripANSI(bountied), IconBounty) {
		t.Errorf("bountied pipeline row should show the bounty glyph: %q", stripANSI(bountied))
	}
	if strings.Contains(stripANSI(plain), IconBounty) {
		t.Errorf("plain pipeline row must not: %q", stripANSI(plain))
	}
}

// rgbHex renders a colour as #rrggbb for comparison against rendered output.
func rgbHex(c color.Color) string {
	r, g, b := rgb(c)
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

// truecolorPattern matches lipgloss's 24-bit foreground/background sequences.
var truecolorPattern = regexp.MustCompile(`3[48];2;(\d+);(\d+);(\d+)`)

// ansiColors extracts every 24-bit colour in s as a space-joined list of hex
// values, so a test can assert that a rendered row carries a specific colour
// without depending on escape-sequence ordering.
func ansiColors(s string) string {
	matches := truecolorPattern.FindAllStringSubmatch(s, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		r, _ := strconv.Atoi(m[1])
		g, _ := strconv.Atoi(m[2])
		b, _ := strconv.Atoi(m[3])
		out = append(out, fmt.Sprintf("#%02x%02x%02x", r, g, b))
	}
	return strings.Join(out, " ")
}
