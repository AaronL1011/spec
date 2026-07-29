package tui

import (
	"hash/fnv"
	"image/color"
	"math"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/lucasb-eyer/go-colorful"

	"github.com/aaronl1011/spec/internal/config"
)

// Bounty marker tuning.
//
// The marker is shaded like a lit surface rather than filled with a colour: a
// base gradient runs from the body tone at the glyph to the shadow tone at the
// end of the ID, as though lit from the upper left, and a narrow specular
// highlight travels across it periodically. Flat colour reads as coloured text;
// this reads as metal. The boot splash shades its logotype the same way.
//
// The light falls on the glyph rather than the middle of the span because the
// glyph is the stone: a symmetric gradient left the gem as the dimmest cell on
// the row, which inverted the emphasis.
//
// The cadence is unhurried on purpose: one ~1.2s pass, then a long rest. Frames
// are counted at spinnerInterval (100ms), the repaint tick the app already runs,
// so the animation costs no extra timers.
const (
	bountySweepFrames = 12   // ticks for one edge-to-edge pass (~1.2s)
	bountyPauseFrames = 48   // ticks the marker rests between passes (~4.8s)
	bountyShadowDepth = 0.35 // how far the edges lean from body toward shadow
	bountySheenRadius = 3.0  // half-width of the specular highlight, in cells
	bountySheenPeak   = 1.0  // highlight strength at its centre (1 = full specular)
	bountyPrismChroma = 0.5  // dispersion saturation, in HCL chroma
)

// bountyCycleFrames is the full sweep+rest period of the marker animation.
const bountyCycleFrames = bountySweepFrames + bountyPauseFrames

// bountyPainter holds everything a render pass needs that is constant across
// rows, so a call site passes only the row's own ID and text. Constructed once
// per view render.
type bountyPainter struct {
	metal   Metal
	frame   int
	shimmer bool
}

// newBountyPainter resolves the team's finish and shimmer preference against the
// active theme. Safe on a nil config: bounties are then off and nothing calls it.
func newBountyPainter(rc *config.ResolvedConfig, t Theme, frame int) bountyPainter {
	cfg := rc.Bounties()
	return bountyPainter{
		metal:   t.Metal(cfg.MetalFinish()),
		frame:   frame,
		shimmer: cfg.ShimmerEnabled(),
	}
}

// paint renders the leading span of a bountied row — the bounty glyph and the
// SPEC-ID — as a shaded metal surface.
//
// It is the single source of the bounty treatment: every surface that shows a
// spec row calls this, so the marker cannot drift between the dashboard, the
// pipeline screen, the spec list and the detail header.
//
// base is the row's own style, so a selected row keeps its background and the
// marker sits on it rather than punching a hole. The caller keeps ownership of
// the rest of the row — title, stage, assignee, detail — which is what lets the
// time-urgency gradient stay fully readable on a bountied row. The rendered text
// is unchanged in width, so column alignment never depends on whether a spec is
// bountied.
//
// specID seeds the animation phase so multiple bountied rows twinkle
// independently instead of flashing in unison, which is most of what separates
// "a gem catching light" from "a blinking cursor".
func (p bountyPainter) paint(base lipgloss.Style, specID, text string) string {
	runes := []rune(text)

	// Shade only the inked run. A marker arrives padded — leading indent on the
	// detail header, trailing column padding in a row — and shading blanks would
	// both waste most of the highlight's pass and skew the gradient's centre away
	// from the glyph.
	first, last := 0, len(runes)-1
	for first <= last && runes[first] == ' ' {
		first++
	}
	for last >= first && runes[last] == ' ' {
		last--
	}
	if first > last {
		return base.Render(text)
	}

	width := last - first + 1
	center, progress := p.sheen(specID, width)
	var b strings.Builder
	if first > 0 {
		b.WriteString(base.Render(string(runes[:first])))
	}
	for i := 0; i < width; i++ {
		c := p.shade(i, width, center, progress)
		b.WriteString(base.Foreground(c).Bold(true).Render(string(runes[first+i])))
	}
	if last+1 < len(runes) {
		b.WriteString(base.Render(string(runes[last+1:])))
	}
	return b.String()
}

// shade resolves one cell's colour: the base gradient, brightened toward the
// specular where the highlight falls.
func (p bountyPainter) shade(i, width int, center, progress float64) color.Color {
	c := blendColor(p.metal.Body, p.metal.Shadow, bountyShadowDepth*falloff(i, width))
	k := bountySpecular(i, center)
	if k <= 0 {
		return c
	}
	return blendColor(c, p.specularColor(progress), bountySheenPeak*k)
}

// specularColor returns the highlight's colour. A dispersive finish rotates the
// highlight through hue as it crosses, mimicking the fire of a cut stone; the
// rotation runs in HCL so every hue lands at the same perceived lightness,
// without the yellow-bright/blue-dark lurching a raw HSL sweep produces.
func (p bountyPainter) specularColor(progress float64) color.Color {
	if !p.Dispersive() {
		return p.metal.Specular
	}
	base, _ := colorful.MakeColor(p.metal.Specular)
	_, _, l := base.Hcl()
	spectrum := colorful.Hcl(math.Mod(progress*360, 360), bountyPrismChroma, l).Clamped()
	return lipgloss.Color(spectrum.Hex())
}

// Dispersive reports whether the active finish's highlight rotates through hue.
func (p bountyPainter) Dispersive() bool { return p.metal.Dispersive }

// sheen returns the highlight's centre column for the current frame and how far
// through its pass it is. A resting marker reports a centre fully off the span,
// so only the base gradient shows.
func (p bountyPainter) sheen(specID string, width int) (center, progress float64) {
	if !p.shimmer {
		return -2 * bountySheenRadius, 0
	}
	f := (p.frame + bountyPhase(specID)) % bountyCycleFrames
	if f > bountySweepFrames {
		return -2 * bountySheenRadius, 0
	}
	progress = float64(f) / bountySweepFrames
	eased := smoothstep(progress)
	return -bountySheenRadius + eased*(float64(width-1)+2*bountySheenRadius), progress
}

// bountyPhase maps a spec ID to a stable offset within the animation cycle, so
// two bountied rows on screen do not pulse together. Stable across renders and
// restarts because it is derived from the ID, not from arrival order.
func bountyPhase(specID string) int {
	if specID == "" {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(specID))
	return int(h.Sum32() % bountyCycleFrames)
}

// falloff returns how far cell i sits from the lit end of a width-wide span, as
// 0 at the glyph rising to 1 at the last character. It shapes the base gradient
// so the span reads as a surface lit from the upper left, with the glyph — the
// stone — brightest.
func falloff(i, width int) float64 {
	if width <= 1 {
		return 0
	}
	return float64(i) / float64(width-1)
}

// bountySpecular returns the highlight's strength at column x: 1 at its centre,
// falling to 0 at its edge. The falloff is squared, which keeps a tight bright
// core with a soft skirt — a glint rather than a smear.
func bountySpecular(x int, center float64) float64 {
	d := math.Abs(float64(x)-center) / bountySheenRadius
	if d >= 1 {
		return 0
	}
	return (1 - d) * (1 - d)
}

// bountyReasonLine formats a bounty's reason and granter for the spec detail
// header, or returns "" when there is nothing to say.
func bountyReasonLine(reason, granter string) string {
	reason = strings.TrimSpace(reason)
	granter = strings.TrimSpace(granter)
	switch {
	case reason == "" && granter == "":
		return ""
	case reason == "":
		return "bountied by " + granter
	case granter == "":
		return reason
	default:
		return reason + " (" + granter + ")"
	}
}

// bountyDetailStyle is the flat body tone, for one-off bounty labels such as the
// spec detail header's reason line. No gradient and no shimmer: a line of prose
// is not a surface, and the marker above it already carries the motion.
func bountyDetailStyle(rc *config.ResolvedConfig, t Theme) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.BountyColor(rc.Bounties().MetalFinish()))
}
