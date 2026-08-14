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
// shimmer disabled, using the default (gold) finish.
func enableBounties(rc *config.ResolvedConfig, shimmer bool) {
	rc.Team.Bounty = &config.BountyConfig{Enabled: true, Shimmer: &shimmer}
}

// goldPainter is a painter over the gold finish with shimmer on.
func goldPainter(t Theme, frame int) bountyPainter {
	shimmer := true
	rc := &config.ResolvedConfig{Team: &config.TeamConfig{
		Bounty: &config.BountyConfig{Enabled: true, Shimmer: &shimmer},
	}}
	return newBountyPainter(rc, t, frame)
}

// finishPainter is a painter over an explicit finish, with shimmer configurable.
func finishPainter(t Theme, finish config.BountyFinish, shimmer bool, frame int) bountyPainter {
	rc := &config.ResolvedConfig{Team: &config.TeamConfig{
		Bounty: &config.BountyConfig{Enabled: true, Shimmer: &shimmer, Finish: string(finish)},
	}}
	return newBountyPainter(rc, t, frame)
}

// TestBountyColor_DistinctFromWarning is why the gold is its own token: the
// urgency ramp already owns Warning, so reusing it would make a bountied row
// indistinguishable from a merely warm one.
func TestBountyColor_DistinctFromWarning(t *testing.T) {
	for _, name := range []string{"catppuccin-mocha", "github-light", "graphite", "nord"} {
		th := ResolveTheme(name)
		wr, wg, wb := rgb(th.Warning)
		br, bg, bb := rgb(th.BountyColor(config.BountyFinishGold))
		if wr == br && wg == bg && wb == bb {
			t.Errorf("%s: bounty gold equals Warning (#%02x%02x%02x) — the two signals would collide",
				name, wr, wg, wb)
		}
	}
}

// TestBountyColor_HonoursThemeOverride covers graphite, which conveys status by
// luminance and sets the token explicitly.
func TestBountyColor_HonoursThemeOverride(t *testing.T) {
	th := Theme{Base: lipgloss.Color("#000000"), BountyRamp: []color.Color{
		lipgloss.Color("#101010"), lipgloss.Color("#abcdef"), lipgloss.Color("#ffffff"),
	}}
	r, g, b := rgb(th.BountyColor(config.BountyFinishGold))
	if r != 0xab || g != 0xcd || b != 0xef {
		t.Errorf("BountyColor() = #%02x%02x%02x, want the theme's explicit value", r, g, b)
	}
	if got := rgbHex(ResolveTheme("graphite").BountyColor(config.BountyFinishGold)); got != "#d8d8d8" {
		t.Errorf("graphite bounty body = %s, want its bright neutral", got)
	}
}

// TestBountyColor_AdaptsToBackground keeps the gold readable on light themes.
func TestBountyColor_AdaptsToBackground(t *testing.T) {
	dark := Theme{Base: lipgloss.Color("#101010")}
	light := Theme{Base: lipgloss.Color("#fafafa")}
	if rgbHex(dark.BountyColor(config.BountyFinishGold)) == rgbHex(light.BountyColor(config.BountyFinishGold)) {
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
			got := finishPainter(th, config.BountyFinishGold, shimmer, frame).
				paint(lipgloss.NewStyle(), "SPEC-001", text)
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

	// Sweep the whole cycle, not just the first frames: each spec's phase offset
	// decides where in the cycle its pass falls.
	distinct := map[string]bool{}
	for frame := 0; frame < bountyCycleFrames; frame++ {
		distinct[goldPainter(th, frame).paint(lipgloss.NewStyle(), "SPEC-001", text)] = true
	}
	if len(distinct) < 4 {
		t.Errorf("shimmer produced %d distinct frames across a cycle, want a moving sheen", len(distinct))
	}

	staticPainter := func(frame int) string {
		return finishPainter(th, config.BountyFinishGold, false, frame).
			paint(lipgloss.NewStyle(), "SPEC-001", text)
	}
	static := staticPainter(0)
	for frame := 0; frame < bountyCycleFrames; frame += 5 {
		if got := staticPainter(frame); got != static {
			t.Fatalf("shimmer=false must be frame-independent, differed at frame %d", frame)
		}
	}
}

// TestBountySheen_RestsBetweenSweeps keeps the animation calm: most of the cycle
// is a rest, not a pulse.
func TestBountySheen_RestsBetweenSweeps(t *testing.T) {
	th := bountyTheme()
	const width = 12
	resting := 0
	for frame := 0; frame < bountyCycleFrames; frame++ {
		if center, _ := goldPainter(th, frame).sheen("SPEC-001", width); center < -bountySheenRadius {
			resting++
		}
	}
	if resting < bountyCycleFrames/2 {
		t.Errorf("sheen rests for %d of %d frames; a bounty marker should be still most of the time",
			resting, bountyCycleFrames)
	}
	// The cycle repeats, so the animation never drifts or jumps.
	first, _ := goldPainter(th, 0).sheen("SPEC-001", width)
	next, _ := goldPainter(th, bountyCycleFrames).sheen("SPEC-001", width)
	if first != next {
		t.Errorf("sheen position must repeat every cycle: %v then %v", first, next)
	}
	// With shimmer off there is never a highlight, only the base gradient.
	if center, _ := finishPainter(th, config.BountyFinishGold, false, 3).sheen("SPEC-001", width); center >= -bountySheenRadius {
		t.Errorf("shimmer=false must park the highlight off the marker, got centre %v", center)
	}
}

// TestBountyPhase_StaggersRows is what separates a row of gems catching light
// from a row of synchronised blinks: each spec's pass starts at its own offset,
// stably derived from its ID.
func TestBountyPhase_StaggersRows(t *testing.T) {
	ids := []string{"SPEC-001", "SPEC-002", "SPEC-003", "SPEC-042", "SPEC-100"}
	phases := map[int]bool{}
	for _, id := range ids {
		p := bountyPhase(id)
		if p < 0 || p >= bountyCycleFrames {
			t.Fatalf("phase for %s = %d, want within [0, %d)", id, p, bountyCycleFrames)
		}
		phases[p] = true
		if again := bountyPhase(id); again != p {
			t.Errorf("phase for %s is unstable: %d then %d", id, p, again)
		}
	}
	if len(phases) < len(ids)-1 {
		t.Errorf("only %d distinct phases across %d specs — rows would pulse together", len(phases), len(ids))
	}
	if got := bountyPhase(""); got != 0 {
		t.Errorf("phase for an empty ID = %d, want 0", got)
	}
}

// TestBountyShade_GradientAndGlint is the fix for "it looks like coloured text":
// at rest the span falls away from the glyph toward the end of the ID, and the
// travelling highlight brightens whatever cell it lands on.
func TestBountyShade_GradientAndGlint(t *testing.T) {
	th := bountyTheme()
	const width = 13
	const noSheen = -99.0
	rest := finishPainter(th, config.BountyFinishGold, false, 0)

	// The glyph is the stone: it must be the brightest cell at rest, and the
	// gradient must fall monotonically from it.
	prev := lumOf(rest.shade(0, width, noSheen, 0))
	for i := 1; i < width; i++ {
		cur := lumOf(rest.shade(i, width, noSheen, 0))
		if cur > prev {
			t.Fatalf("cell %d (lum %.1f) is brighter than cell %d (lum %.1f) — the gradient must fall from the glyph",
				i, cur, i-1, prev)
		}
		prev = cur
	}
	if head, tail := lumOf(rest.shade(0, width, noSheen, 0)), lumOf(rest.shade(width-1, width, noSheen, 0)); head-tail < 3 {
		t.Errorf("head lum %.1f vs tail %.1f — too flat to read as a lit surface", head, tail)
	}

	// A highlight centred on a cell must brighten it beyond its resting tone.
	target := width / 2
	body := lumOf(rest.shade(target, width, noSheen, 0))
	lit := lumOf(rest.shade(target, width, float64(target), 0.5))
	if lit <= body {
		t.Errorf("highlight lum %.1f <= resting lum %.1f — the glint does not read", lit, body)
	}
}

// TestBountySpecular_TightCore keeps the highlight a glint rather than a smear:
// full strength at its centre, gone by its edge, and falling faster than linear.
func TestBountySpecular_TightCore(t *testing.T) {
	if got := bountySpecular(5, 5); got != 1 {
		t.Errorf("specular at centre = %v, want 1", got)
	}
	if got := bountySpecular(5+int(bountySheenRadius), 5); got != 0 {
		t.Errorf("specular at edge = %v, want 0", got)
	}
	half := bountySpecular(5, 5+bountySheenRadius/2)
	if half >= 0.5 {
		t.Errorf("specular at half-radius = %v, want below 0.5 (squared falloff keeps the core tight)", half)
	}
}

// TestBountyFinishes_AllBrightenAndDiffer is the fix for the measured bug: the
// old highlight blended toward Theme.Text, which darkened the marker on light
// and monochrome themes. Every finish must brighten on every theme.
func TestBountyFinishes_AllBrightenAndDiffer(t *testing.T) {
	finishes := []config.BountyFinish{
		config.BountyFinishGold, config.BountyFinishPlatinum, config.BountyFinishPrismatic,
	}
	for _, name := range []string{"catppuccin-mocha", "github-light", "modus-operandi", "graphite"} {
		th := ResolveTheme(name)
		for _, finish := range finishes {
			m := th.Metal(finish)
			body, spec, shadow := lumOf(m.Body), lumOf(m.Specular), lumOf(m.Shadow)
			if spec <= body {
				t.Errorf("%s/%s: specular lum %.1f <= body %.1f — the sheen would darken", name, finish, spec, body)
			}
			if shadow >= body {
				t.Errorf("%s/%s: shadow lum %.1f >= body %.1f — no gradient depth", name, finish, shadow, body)
			}
		}
	}

	// The finishes must actually look different on a theme that can express them.
	dark := ResolveTheme("catppuccin-mocha")
	seen := map[string]bool{}
	for _, finish := range finishes {
		seen[rgbHex(dark.Metal(finish).Body)] = true
	}
	if len(seen) != len(finishes) {
		t.Errorf("finishes share body tones (%v) — switching finish would look identical", seen)
	}
}

// TestBountyPrismatic_RotatesHue covers the dispersion finish: the highlight
// changes hue across a pass, at a steady perceived lightness.
func TestBountyPrismatic_RotatesHue(t *testing.T) {
	th := bountyTheme()
	prism := finishPainter(th, config.BountyFinishPrismatic, true, 0)
	if !prism.Dispersive() {
		t.Fatal("prismatic finish should be dispersive")
	}
	gold := finishPainter(th, config.BountyFinishGold, true, 0)
	if gold.Dispersive() {
		t.Error("gold must not be dispersive — a metal reflects one hue")
	}

	hues := map[string]bool{}
	var lums []float64
	for _, progress := range []float64{0, 0.2, 0.4, 0.6, 0.8} {
		c := prism.specularColor(progress)
		hues[rgbHex(c)] = true
		lums = append(lums, lumOf(c))
	}
	if len(hues) < 4 {
		t.Errorf("prismatic highlight produced %d distinct colours across a pass, want a hue sweep", len(hues))
	}
	for i, l := range lums {
		if l < 55 || l > 100 {
			t.Errorf("dispersion step %d has lum %.1f — a hue sweep must hold a steady lightness", i, l)
		}
	}
	// Gold's highlight is a single colour regardless of pass position.
	if gold.specularColor(0) != gold.specularColor(0.7) {
		t.Error("a non-dispersive finish must keep one specular colour")
	}
}

// TestGraphiteForcesItsOwnRamp asserts a theme override beats the configured
// finish, so a monochrome palette is never handed a hue it cannot express.
func TestGraphiteForcesItsOwnRamp(t *testing.T) {
	th := ResolveTheme("graphite")
	for _, finish := range []config.BountyFinish{
		config.BountyFinishGold, config.BountyFinishPlatinum, config.BountyFinishPrismatic,
	} {
		m := th.Metal(finish)
		if m.Dispersive {
			t.Errorf("%s on graphite should not disperse", finish)
		}
		for _, c := range []color.Color{m.Shadow, m.Body, m.Specular} {
			r, g, b := rgb(c)
			if r != g || g != b {
				t.Errorf("%s on graphite yields chromatic %s — the theme must stay monochrome", finish, rgbHex(c))
			}
		}
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

	gold := rgbHex(m.styles.Theme.BountyColor(config.BountyFinishGold))
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
	if !strings.Contains(ansiColors(row), rgbHex(m.styles.Theme.BountyColor(config.BountyFinishGold))) {
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
		mark, rest := m.formatRow(specListItem{
			ID:      "SPEC-001",
			Title:   "A very long spec title that could overflow the row width boundary",
			Status:  "in_progress",
			Author:  "alice",
			Updated: "2026-05-26",
		}, m.bountyGutter(true), w)
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

// lumOf returns a colour's BT.601 perceived luminance as a 0-100 percentage, so
// assertions can talk about "brighter" without naming exact channels.
func lumOf(c color.Color) float64 {
	r, g, b := rgb(c)
	return (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 255 * 100
}
