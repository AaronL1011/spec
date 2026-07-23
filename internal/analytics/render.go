package analytics

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Rendering geometry shared by the report sections.
const (
	dwellBarWidth = 18
	stageColWidth = 14
)

// warnRatio is the aging-WIP multiple of p85 that earns a warning marker.
const warnRatio = 1.5

// reversionWarnRate is the reversions/advances rate above which the report
// surfaces a diagnostic callout.
const reversionWarnRate = 0.2

// FormatDuration renders a duration compactly: minutes under an hour, tenths
// of hours under a day, tenths of days beyond.
func FormatDuration(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return trimZero(fmt.Sprintf("%.1fh", d.Hours()))
	default:
		return trimZero(fmt.Sprintf("%.1fd", d.Hours()/24))
	}
}

// trimZero drops a redundant ".0" from a formatted number ("6.0d" → "6d").
func trimZero(s string) string {
	return strings.Replace(s, ".0", "", 1)
}

// RenderFlowReport renders the team flow report. prev, when non-nil, drives
// cross-window deltas; pass nil when no comparison window exists.
func RenderFlowReport(r, prev *FlowReport) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "  Flow — %s (%s – %s) · %d specs completed\n",
		r.Window.Label, r.Window.From.Format("Jan 2"), r.Window.To.Format("Jan 2"), r.Completed)

	if r.Completed == 0 {
		sb.WriteString("\n  No completed specs in this window.\n")
	} else {
		renderDurations(&sb, r, prev)
		renderStageDwell(&sb, r, prev)
	}
	renderReversions(&sb, r)
	renderAgingWIP(&sb, r)
	renderBlocked(&sb, r)
	renderFooter(&sb, r)
	return sb.String()
}

// renderDurations prints the lead/cycle distributions and the flow line.
func renderDurations(sb *strings.Builder, r, prev *FlowReport) {
	sb.WriteString("\n")
	renderDistLine(sb, "Lead time ", r.LeadTime)
	renderDistLine(sb, "Cycle time", r.CycleTime)
	if r.FastTrackLead.Count() > 0 {
		renderDistLine(sb, "Fast-track", r.FastTrackLead)
	}

	var parts []string
	if r.FlowEfficiency >= 0 {
		parts = append(parts, fmt.Sprintf("Flow efficiency %.0f%%", r.FlowEfficiency*100))
	}
	parts = append(parts, fmt.Sprintf("Throughput %.1f/wk%s", r.ThroughputPerWeek, throughputDelta(r, prev)))
	fmt.Fprintf(sb, "  %s\n", strings.Join(parts, "  ·  "))
}

// renderDistLine prints one labelled percentile row with its histogram.
func renderDistLine(sb *strings.Builder, label string, d Distribution) {
	if d.Count() == 0 {
		return
	}
	fmt.Fprintf(sb, "  %s   p50 %-6s p85 %-6s p95 %-6s %s\n",
		label, FormatDuration(d.P50), FormatDuration(d.P85), FormatDuration(d.P95), Sparkline(d.Samples))
}

// throughputDelta formats the percentage change vs the previous window.
func throughputDelta(r, prev *FlowReport) string {
	if prev == nil || prev.Completed == 0 || prev.ThroughputPerWeek == 0 {
		return ""
	}
	pct := (r.ThroughputPerWeek - prev.ThroughputPerWeek) / prev.ThroughputPerWeek * 100
	switch {
	case pct > 0:
		return fmt.Sprintf(" ↑ %.0f%% vs previous", pct)
	case pct < 0:
		return fmt.Sprintf(" ↓ %.0f%% vs previous", -pct)
	default:
		return " ▬ vs previous"
	}
}

// renderStageDwell prints the per-stage p50 bars with deltas and the
// bottleneck marker.
func renderStageDwell(sb *strings.Builder, r, prev *FlowReport) {
	if len(r.StageDwell) == 0 {
		return
	}
	prevP50 := map[string]time.Duration{}
	if prev != nil {
		for _, sd := range prev.StageDwell {
			prevP50[sd.Stage] = sd.Dist.P50
		}
	}

	var maxP50 time.Duration
	for _, sd := range r.StageDwell {
		if sd.Dist.P50 > maxP50 {
			maxP50 = sd.Dist.P50
		}
	}

	sb.WriteString("\n  Time in stage (p50)\n")
	for _, sd := range r.StageDwell {
		fmt.Fprintf(sb, "  %-*s %s  %-6s%s%s\n",
			stageColWidth, sd.Stage, dwellBar(sd.Dist.P50, maxP50), FormatDuration(sd.Dist.P50),
			stageDelta(sd, prevP50), bottleneckMark(sd.Stage, r.Bottleneck))
	}
}

// dwellBar renders a filled/empty bar proportional to value/scale.
func dwellBar(value, scale time.Duration) string {
	filled := dwellBarWidth
	if scale > 0 {
		filled = int(float64(value) / float64(scale) * dwellBarWidth)
	}
	if filled < 1 {
		filled = 1
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", dwellBarWidth-filled)
}

// stageDelta formats the p50 change vs the previous window for one stage.
func stageDelta(sd StageDwell, prevP50 map[string]time.Duration) string {
	prev, ok := prevP50[sd.Stage]
	if !ok {
		return ""
	}
	diff := sd.Dist.P50 - prev
	switch {
	case diff > 0:
		return fmt.Sprintf("  ▲ %s", FormatDuration(diff))
	case diff < 0:
		return fmt.Sprintf("  ▼ %s", FormatDuration(-diff))
	default:
		return "  ▬"
	}
}

// bottleneckMark annotates the bottleneck stage row.
func bottleneckMark(stage, bottleneck string) string {
	if stage == bottleneck && bottleneck != "" {
		return "   ◀ bottleneck"
	}
	return ""
}

// renderReversions prints the reversion boundaries and, when the rate is
// high, a diagnostic hypothesis pointing at the dominant boundary.
func renderReversions(sb *strings.Builder, r *FlowReport) {
	if r.TotalReversions == 0 {
		return
	}
	var parts []string
	for _, b := range r.Reversions {
		parts = append(parts, fmt.Sprintf("%s→%s ×%d", b.From, b.To, b.Count))
	}
	fmt.Fprintf(sb, "\n  Reversions: %d (%s)\n", r.TotalReversions, strings.Join(parts, ", "))

	if r.TotalAdvances == 0 {
		return
	}
	rate := float64(r.TotalReversions) / float64(r.TotalAdvances)
	if rate >= reversionWarnRate && len(r.Reversions) > 0 {
		top := r.Reversions[0]
		fmt.Fprintf(sb, "  ⚠ %s→%s reversion rate %.0f%% — specs may be entering %s under-baked\n",
			top.From, top.To, rate*100, top.From)
	}
}

// renderAgingWIP prints open specs dwelling beyond their stage's p85.
func renderAgingWIP(sb *strings.Builder, r *FlowReport) {
	if len(r.AgingWIP) == 0 {
		return
	}
	sb.WriteString("\n  Aging WIP\n")
	for _, item := range r.AgingWIP {
		warn := ""
		if item.Ratio >= warnRatio {
			warn = fmt.Sprintf("   ⚠ %.1f× p85", item.Ratio)
		}
		fmt.Fprintf(sb, "  %-10s %-*s %s in stage   p85 is %s%s\n",
			item.SpecID, stageColWidth, item.Stage, FormatDuration(item.Dwell), FormatDuration(item.P85), warn)
	}
}

// renderBlocked prints the eject-time summary line.
func renderBlocked(sb *strings.Builder, r *FlowReport) {
	if r.BlockedSpecs == 0 {
		return
	}
	noun := "specs"
	if r.BlockedSpecs == 1 {
		noun = "spec"
	}
	fmt.Fprintf(sb, "\n  Blocked: %d %s, %s total eject time in window\n",
		r.BlockedSpecs, noun, FormatDuration(r.BlockedTotal))
}

// renderFooter prints the coverage honesty line.
func renderFooter(sb *strings.Builder, r *FlowReport) {
	c := r.Coverage
	analysed := c.Transitions
	total := c.Transitions + c.Unattributable
	fmt.Fprintf(sb, "\n  analysed %d/%d transitions · %d commits scanned\n", analysed, total, c.CommitsScanned)
}

// RenderSpecTimeline renders a single spec's journey through the pipeline.
func RenderSpecTimeline(tl *Timeline, now time.Time) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "  %s — journey\n\n", tl.SpecID)

	for _, v := range tl.Visits {
		fmt.Fprintf(&sb, "  %s  %-*s %s\n", v.Entered.Format("Jan 02"), stageColWidth, v.Stage, FormatDuration(v.Duration()))
	}
	if tl.CurrentStage != "" {
		fmt.Fprintf(&sb, "  %s  %-*s %s  ◀ current\n",
			tl.CurrentSince.Format("Jan 02"), stageColWidth, tl.CurrentStage, FormatDuration(now.Sub(tl.CurrentSince)))
	}

	renderSpecSummary(&sb, tl)
	return sb.String()
}

// renderSpecSummary prints the per-spec totals under the journey listing.
func renderSpecSummary(sb *strings.Builder, tl *Timeline) {
	sb.WriteString("\n")
	if tl.Completed() {
		fmt.Fprintf(sb, "  Lead time: %s\n", FormatDuration(tl.LeadTime()))
	}
	if n := len(tl.Reversions); n > 0 {
		var parts []string
		for _, rev := range tl.Reversions {
			parts = append(parts, fmt.Sprintf("%s→%s", rev.From, rev.To))
		}
		fmt.Fprintf(sb, "  Reversions: %d (%s)\n", n, strings.Join(parts, ", "))
	}
	if n := len(tl.Blocked); n > 0 {
		var total time.Duration
		for _, spell := range tl.Blocked {
			total += spell.End.Sub(spell.Start)
		}
		fmt.Fprintf(sb, "  Blocked: %d spells, %s total\n", n, FormatDuration(total))
	}
}

// RenderStageReport renders a deep-dive for one stage: its dwell
// distribution, current occupants, and the reversion boundaries touching it.
func RenderStageReport(r *FlowReport, stage string, timelines map[string]*Timeline, now time.Time) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "  Stage %s — %s\n\n", stage, r.Window.Label)

	found := false
	for _, sd := range r.StageDwell {
		if sd.Stage == stage {
			renderDistLine(&sb, "Dwell     ", sd.Dist)
			fmt.Fprintf(&sb, "  Samples: %d completed specs\n", sd.Dist.Count())
			found = true
		}
	}
	if !found {
		sb.WriteString("  No completed dwell samples for this stage in the window.\n")
	}

	renderStageOccupants(&sb, stage, timelines, now)

	for _, b := range r.Reversions {
		if b.From == stage || b.To == stage {
			fmt.Fprintf(&sb, "  Reversions: %s→%s ×%d\n", b.From, b.To, b.Count)
		}
	}
	return sb.String()
}

// renderStageOccupants lists open specs currently sitting in the stage.
func renderStageOccupants(sb *strings.Builder, stage string, timelines map[string]*Timeline, now time.Time) {
	var ids []string
	for id, tl := range timelines {
		if !tl.Completed() && tl.CurrentStage == stage {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	sort.Strings(ids)
	sb.WriteString("\n  Currently in stage:\n")
	for _, id := range ids {
		tl := timelines[id]
		fmt.Fprintf(sb, "  %-10s %s\n", id, FormatDuration(now.Sub(tl.CurrentSince)))
	}
	sb.WriteString("\n")
}
