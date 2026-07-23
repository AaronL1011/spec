package analytics

import (
	"sort"
	"time"
)

// Window is the reporting interval.
type Window struct {
	From  time.Time
	To    time.Time
	Label string
}

// Contains reports whether t falls inside the window (inclusive bounds).
func (w Window) Contains(t time.Time) bool {
	return !t.Before(w.From) && !t.After(w.To)
}

// Weeks returns the window length in (fractional) weeks, minimum one day.
func (w Window) Weeks() float64 {
	d := w.To.Sub(w.From)
	if d < 24*time.Hour {
		d = 24 * time.Hour
	}
	return d.Hours() / (24 * 7)
}

// Coverage carries the extraction honesty counters for the report footer.
type Coverage struct {
	CommitsScanned int
	Transitions    int
	Unattributable int
}

// StageDwell is the dwell distribution for one stage over completed specs.
type StageDwell struct {
	Stage string
	Dist  Distribution
}

// ReversionBoundary counts backwards transitions across one stage boundary.
type ReversionBoundary struct {
	From  string
	To    string
	Count int
}

// AgingItem flags an open spec dwelling beyond the historical p85 of its stage.
type AgingItem struct {
	SpecID string
	Stage  string
	Dwell  time.Duration
	P85    time.Duration
	Ratio  float64
}

// FlowReport is the computed team flow picture for one window.
type FlowReport struct {
	Window            Window
	Completed         int
	LeadTime          Distribution
	CycleTime         Distribution
	FastTrackLead     Distribution // separate population, may be empty
	FlowEfficiency    float64      // work-stage time / lead time; -1 when unknown
	ThroughputPerWeek float64
	StageDwell        []StageDwell // pipeline order, stages with samples only
	Bottleneck        string       // stage with largest p50 dwell
	Reversions        []ReversionBoundary
	TotalAdvances     int
	TotalReversions   int
	AgingWIP          []AgingItem
	BlockedSpecs      int
	BlockedTotal      time.Duration
	Coverage          Coverage
}

// FlowInput is everything ComputeFlow needs; it performs no I/O.
type FlowInput struct {
	Timelines  map[string]*Timeline
	Events     []Event
	Window     Window
	Now        time.Time
	StageNames []string        // full pipeline order
	Terminal   []string        // terminal stage names
	WorkStages []string        // engineer-owned stages (flow-efficiency numerator)
	FastTrack  map[string]bool // spec IDs flagged fast_track
	Filter     map[string]bool // optional spec-ID scope (nil = all)
}

// minAgingSamples is the smallest dwell population that makes a p85
// comparison meaningful for aging-WIP flagging.
const minAgingSamples = 3

// ComputeFlow aggregates per-spec timelines into the team flow report.
func ComputeFlow(in FlowInput) *FlowReport {
	r := &FlowReport{Window: in.Window, FlowEfficiency: -1}

	completed := in.completedInWindow()
	r.Completed = len(completed)
	r.ThroughputPerWeek = float64(len(completed)) / in.Window.Weeks()

	r.LeadTime, r.CycleTime, r.FastTrackLead = in.durations(completed)
	r.FlowEfficiency = in.flowEfficiency(completed)
	r.StageDwell, r.Bottleneck = in.stageDwell(completed)
	r.TotalAdvances, r.TotalReversions, r.Reversions = in.reversionStats()
	r.AgingWIP = in.agingWIP(r.StageDwell)
	r.BlockedSpecs, r.BlockedTotal = in.blockedStats()

	return r
}

// inScope reports whether a spec ID passes the optional filter.
func (in FlowInput) inScope(specID string) bool {
	return in.Filter == nil || in.Filter[specID]
}

// completedInWindow returns timelines completed inside the window, in scope.
func (in FlowInput) completedInWindow() []*Timeline {
	var out []*Timeline
	for _, tl := range in.Timelines {
		if in.inScope(tl.SpecID) && tl.Completed() && in.Window.Contains(tl.CompletedAt) {
			out = append(out, tl)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SpecID < out[j].SpecID })
	return out
}

// durations computes the lead- and cycle-time distributions. Fast-track specs
// form their own lead-time population.
func (in FlowInput) durations(completed []*Timeline) (lead, cycle, fastTrack Distribution) {
	var leadS, cycleS, fastS []time.Duration
	for _, tl := range completed {
		lt := tl.LeadTime()
		if lt <= 0 {
			continue
		}
		if in.FastTrack[tl.SpecID] {
			fastS = append(fastS, lt)
			continue
		}
		leadS = append(leadS, lt)
		if start := in.workStart(tl); !start.IsZero() && tl.CompletedAt.After(start) {
			cycleS = append(cycleS, tl.CompletedAt.Sub(start))
		}
	}
	return NewDistribution(leadS), NewDistribution(cycleS), NewDistribution(fastS)
}

// workStart returns when the spec first entered any work (engineer-owned)
// stage, or zero if it never did.
func (in FlowInput) workStart(tl *Timeline) time.Time {
	var earliest time.Time
	for _, stage := range in.WorkStages {
		if at := tl.FirstEntered(stage); !at.IsZero() && (earliest.IsZero() || at.Before(earliest)) {
			earliest = at
		}
	}
	return earliest
}

// flowEfficiency is total work-stage dwell over total lead time across the
// completed population. Returns -1 when there is no lead time to divide by.
func (in FlowInput) flowEfficiency(completed []*Timeline) float64 {
	var work, lead time.Duration
	for _, tl := range completed {
		lead += tl.LeadTime()
		for _, stage := range in.WorkStages {
			work += tl.StageDwellTotal(stage)
		}
	}
	if lead <= 0 {
		return -1
	}
	return float64(work) / float64(lead)
}

// stageDwell builds per-stage dwell distributions over the completed specs,
// in pipeline order, and identifies the bottleneck (largest p50 among
// non-terminal stages).
func (in FlowInput) stageDwell(completed []*Timeline) ([]StageDwell, string) {
	terminal := make(map[string]bool, len(in.Terminal))
	for _, s := range in.Terminal {
		terminal[s] = true
	}

	var dwells []StageDwell
	var bottleneck string
	var maxP50 time.Duration
	for _, stage := range in.StageNames {
		var samples []time.Duration
		for _, tl := range completed {
			if d := tl.StageDwellTotal(stage); d > 0 {
				samples = append(samples, d)
			}
		}
		if len(samples) == 0 {
			continue
		}
		dist := NewDistribution(samples)
		dwells = append(dwells, StageDwell{Stage: stage, Dist: dist})
		if !terminal[stage] && dist.P50 > maxP50 {
			maxP50 = dist.P50
			bottleneck = stage
		}
	}
	return dwells, bottleneck
}

// reversionStats counts in-window advances and reversions and groups
// reversions by stage boundary, most frequent first.
func (in FlowInput) reversionStats() (advances, reversions int, boundaries []ReversionBoundary) {
	counts := make(map[[2]string]int)
	for _, ev := range in.Events {
		if !in.inScope(ev.SpecID) || !in.Window.Contains(ev.At) {
			continue
		}
		switch ev.Kind {
		case KindAdvanced:
			advances++
		case KindReverted:
			reversions++
			counts[[2]string{ev.FromStage, ev.ToStage}]++
		case KindScaffolded, KindEjected, KindResumed:
			// Not flow transitions.
		}
	}
	for key, n := range counts {
		boundaries = append(boundaries, ReversionBoundary{From: key[0], To: key[1], Count: n})
	}
	sort.Slice(boundaries, func(i, j int) bool {
		if boundaries[i].Count != boundaries[j].Count {
			return boundaries[i].Count > boundaries[j].Count
		}
		return boundaries[i].From < boundaries[j].From
	})
	return advances, reversions, boundaries
}

// agingWIP flags open specs whose current-stage dwell exceeds the historical
// p85 for that stage, worst ratio first.
func (in FlowInput) agingWIP(dwells []StageDwell) []AgingItem {
	p85 := make(map[string]Distribution, len(dwells))
	for _, sd := range dwells {
		p85[sd.Stage] = sd.Dist
	}

	var items []AgingItem
	for _, tl := range in.Timelines {
		if !in.inScope(tl.SpecID) || tl.Completed() || tl.CurrentStage == "" || tl.CurrentStage == StatusBlocked {
			continue
		}
		dist, ok := p85[tl.CurrentStage]
		if !ok || dist.Count() < minAgingSamples || dist.P85 <= 0 {
			continue
		}
		dwell := in.Now.Sub(tl.CurrentSince)
		if dwell <= dist.P85 {
			continue
		}
		items = append(items, AgingItem{
			SpecID: tl.SpecID,
			Stage:  tl.CurrentStage,
			Dwell:  dwell,
			P85:    dist.P85,
			Ratio:  float64(dwell) / float64(dist.P85),
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Ratio > items[j].Ratio })
	const maxAging = 10
	if len(items) > maxAging {
		items = items[:maxAging]
	}
	return items
}

// blockedStats sums eject time overlapping the window and counts the specs
// affected, including specs still blocked now.
func (in FlowInput) blockedStats() (specs int, total time.Duration) {
	for _, tl := range in.Timelines {
		if !in.inScope(tl.SpecID) {
			continue
		}
		var specTotal time.Duration
		for _, spell := range tl.Blocked {
			specTotal += overlap(spell.Start, spell.End, in.Window)
		}
		if tl.CurrentStage == StatusBlocked {
			specTotal += overlap(tl.CurrentSince, in.Now, in.Window)
		}
		if specTotal > 0 {
			specs++
			total += specTotal
		}
	}
	return specs, total
}

// overlap returns how much of [start, end] falls inside the window.
func overlap(start, end time.Time, w Window) time.Duration {
	if start.Before(w.From) {
		start = w.From
	}
	if end.After(w.To) {
		end = w.To
	}
	if !end.After(start) {
		return 0
	}
	return end.Sub(start)
}
