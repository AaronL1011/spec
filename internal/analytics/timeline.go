package analytics

import (
	"sort"
	"time"
)

// StageVisit is one closed interval a spec spent in a stage.
type StageVisit struct {
	Stage   string
	Entered time.Time
	Exited  time.Time
}

// Duration returns the visit's dwell.
func (v StageVisit) Duration() time.Duration { return v.Exited.Sub(v.Entered) }

// BlockedSpell is one closed eject→resume interval.
type BlockedSpell struct {
	Start time.Time
	End   time.Time
}

// Reversion records a single backwards transition.
type Reversion struct {
	From string
	To   string
	At   time.Time
}

// Timeline is the reconstructed journey of one spec through the pipeline.
type Timeline struct {
	SpecID       string
	Created      time.Time
	Visits       []StageVisit // closed visits only
	Blocked      []BlockedSpell
	Reversions   []Reversion
	Advances     int
	CurrentStage string    // stage the spec occupies after the last event
	CurrentSince time.Time // when it entered CurrentStage
	CompletedAt  time.Time // zero until the spec first enters a terminal stage
}

// Completed reports whether the spec has reached a terminal stage.
func (t *Timeline) Completed() bool { return !t.CompletedAt.IsZero() }

// LeadTime is creation → completion. Zero until completed.
func (t *Timeline) LeadTime() time.Duration {
	if !t.Completed() || t.Created.IsZero() {
		return 0
	}
	return t.CompletedAt.Sub(t.Created)
}

// StageDwellTotal accumulates the spec's dwell in one stage across revisits.
func (t *Timeline) StageDwellTotal(stage string) time.Duration {
	var total time.Duration
	for _, v := range t.Visits {
		if v.Stage == stage {
			total += v.Duration()
		}
	}
	return total
}

// FirstEntered returns when the spec first entered the given stage, or zero.
func (t *Timeline) FirstEntered(stage string) time.Time {
	for _, v := range t.Visits {
		if v.Stage == stage {
			return v.Entered
		}
	}
	if t.CurrentStage == stage {
		return t.CurrentSince
	}
	return time.Time{}
}

// BuildTimelines replays extracted events (any order) into per-spec timelines.
// terminalStages marks completion; the first entry into any of them stamps
// CompletedAt.
func BuildTimelines(events []Event, terminalStages []string) map[string]*Timeline {
	terminal := make(map[string]bool, len(terminalStages))
	for _, s := range terminalStages {
		terminal[s] = true
	}

	sorted := make([]Event, len(events))
	copy(sorted, events)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].At.Before(sorted[j].At) })

	timelines := make(map[string]*Timeline)
	for _, ev := range sorted {
		tl := timelines[ev.SpecID]
		if tl == nil {
			tl = &Timeline{SpecID: ev.SpecID}
			timelines[ev.SpecID] = tl
		}
		tl.apply(ev, terminal)
	}
	return timelines
}

// apply folds one event into the timeline.
func (t *Timeline) apply(ev Event, terminal map[string]bool) {
	if t.Created.IsZero() {
		t.Created = ev.At
	}
	t.closeCurrent(ev.At)

	switch ev.Kind {
	case KindAdvanced:
		t.Advances++
	case KindReverted:
		from := ev.FromStage
		if from == "" {
			from = t.CurrentStage
		}
		t.Reversions = append(t.Reversions, Reversion{From: from, To: ev.ToStage, At: ev.At})
	case KindScaffolded, KindEjected, KindResumed:
		// No counters beyond the stage move itself.
	}

	t.CurrentStage = ev.ToStage
	t.CurrentSince = ev.At
	if terminal[ev.ToStage] && t.CompletedAt.IsZero() {
		t.CompletedAt = ev.At
	}
}

// closeCurrent finalises the open visit (or blocked spell) at the given time.
func (t *Timeline) closeCurrent(at time.Time) {
	if t.CurrentStage == "" {
		return
	}
	if t.CurrentStage == StatusBlocked {
		t.Blocked = append(t.Blocked, BlockedSpell{Start: t.CurrentSince, End: at})
		return
	}
	t.Visits = append(t.Visits, StageVisit{Stage: t.CurrentStage, Entered: t.CurrentSince, Exited: at})
}
