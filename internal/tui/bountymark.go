package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Bounty marker tuning. The marker is a gold spark plus the SPEC-ID, crossed
// periodically by a single soft sheen — the boot-splash shimmer, slowed down
// and looped, so a bountied row reads as a rare item rather than an alarm.
//
// The cadence is deliberately unhurried: one ~1.4s pass every ~6s. Frames are
// counted at spinnerInterval (100ms), the repaint tick the app already runs, so
// the animation costs no extra timers or wake-ups.
const (
	bountySweepFrames = 14  // ticks for one edge-to-edge pass (~1.4s)
	bountyPauseFrames = 46  // ticks the marker rests between passes (~4.6s)
	bountySheenRadius = 4   // half-width of the sheen, in cells
	bountySheenPeak   = 0.7 // peak brightening at the sheen's centre
)

// bountyCycleFrames is the full sweep+rest period of the marker animation.
const bountyCycleFrames = bountySweepFrames + bountyPauseFrames

// renderBountyMark renders the leading span of a bountied row — the spark glyph
// and the SPEC-ID — in gold, with an animated sheen when shimmer is enabled.
//
// It is the single source of the bounty treatment: every surface that shows a
// spec row calls this, so the marker cannot drift between the dashboard, the
// pipeline screen, the spec list, the detail header, and search results.
//
// base is the row's own style, so a selected row keeps its background and the
// marker sits on it rather than punching a hole. The caller keeps ownership of
// the rest of the row — title, stage, assignee, detail — which is what lets the
// time-urgency gradient stay fully readable on a bountied row. The rendered
// text is unchanged in width, so column alignment never depends on whether a
// spec is bountied.
func renderBountyMark(base lipgloss.Style, text string, frame int, shimmer bool, t Theme) string {
	gold := t.BountyColor()
	if !shimmer {
		return base.Foreground(gold).Bold(true).Render(text)
	}

	runes := []rune(text)
	center := bountySheenCenter(frame, len(runes))
	var b strings.Builder
	for i, r := range runes {
		c := gold
		if k := shimmerIntensity(i, center); k > 0 {
			c = blendColor(gold, t.Text, bountySheenPeak*k)
		}
		b.WriteString(base.Foreground(c).Bold(true).Render(string(r)))
	}
	return b.String()
}

// bountySheenCenter returns the sheen's centre column for the current frame, or
// a position fully off the marker while it rests between passes. The pass is
// eased (smoothstep) so it enters and leaves gently instead of snapping.
func bountySheenCenter(frame, width int) float64 {
	f := ((frame % bountyCycleFrames) + bountyCycleFrames) % bountyCycleFrames
	if f > bountySweepFrames {
		return -2 * bountySheenRadius // resting: off-marker
	}
	p := smoothstep(float64(f) / bountySweepFrames)
	return -bountySheenRadius + p*float64(width-1+2*bountySheenRadius)
}

// Shimmer is a team preference read through config.ResolvedConfig.Bounties()
// at each call site. NO_COLOR and low-colour terminals need no special case:
// lipgloss drops the per-cell colours, and the spark glyph plus bold weight
// still mark the row.

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

// bountyDetailStyle is the static gold used for one-off bounty labels, such as
// the spec detail header's reason line. No shimmer: a paragraph of moving text
// is noise, and the marker above it already carries the motion.
func bountyDetailStyle(t Theme) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.BountyColor())
}
