package analytics

import (
	"testing"
	"time"
)

// dayEv builds an event at day-granularity offsets from t0.
func dayEv(specID string, kind EventKind, from, to string, day int) Event {
	return Event{SpecID: specID, Kind: kind, FromStage: from, ToStage: to, At: t0(day * 24 * 60), Source: SourceMessage}
}

// specJourney emits a standard scaffold→draft→engineering→build→done journey:
// scaffolded at day 0, then advancing at the given day offsets.
func specJourney(id string, eng, build, done int) []Event {
	return []Event{
		dayEv(id, KindScaffolded, "", "draft", 0),
		dayEv(id, KindAdvanced, "draft", "engineering", eng),
		dayEv(id, KindAdvanced, "engineering", "build", build),
		dayEv(id, KindAdvanced, "build", "done", done),
	}
}

func flowInputFor(events []Event, extra func(*FlowInput)) FlowInput {
	in := FlowInput{
		Timelines:  BuildTimelines(events, terminals),
		Events:     events,
		Window:     Window{From: t0(0), To: t0(100 * 24 * 60), Label: "test"},
		Now:        t0(100 * 24 * 60),
		StageNames: []string{"draft", "tl-review", "engineering", "build", "done"},
		Terminal:   terminals,
		WorkStages: []string{"engineering", "build"},
	}
	if extra != nil {
		extra(&in)
	}
	return in
}

func TestComputeFlow_LeadAndCycleTime(t *testing.T) {
	var events []Event
	events = append(events, specJourney("SPEC-001", 2, 4, 10)...) // lead 10d, cycle 8d
	events = append(events, specJourney("SPEC-002", 4, 6, 20)...) // lead 20d, cycle 16d
	events = append(events, specJourney("SPEC-003", 6, 8, 30)...) // lead 30d, cycle 24d

	r := ComputeFlow(flowInputFor(events, nil))

	if r.Completed != 3 {
		t.Fatalf("Completed = %d, want 3", r.Completed)
	}
	if r.LeadTime.P50 != 20*24*time.Hour {
		t.Errorf("LeadTime.P50 = %v, want 20d", r.LeadTime.P50)
	}
	if r.CycleTime.P50 != 16*24*time.Hour {
		t.Errorf("CycleTime.P50 = %v, want 16d", r.CycleTime.P50)
	}
	if r.LeadTime.P95 != 30*24*time.Hour {
		t.Errorf("LeadTime.P95 = %v, want 30d (nearest rank)", r.LeadTime.P95)
	}
	if r.TotalAdvances != 9 || r.TotalReversions != 0 {
		t.Errorf("advances=%d reversions=%d", r.TotalAdvances, r.TotalReversions)
	}
}

func TestComputeFlow_FastTrackSegmented(t *testing.T) {
	var events []Event
	events = append(events, specJourney("SPEC-001", 2, 4, 10)...)
	events = append(events, specJourney("SPEC-002", 0, 0, 1)...) // fast-track: 1d

	r := ComputeFlow(flowInputFor(events, func(in *FlowInput) {
		in.FastTrack = map[string]bool{"SPEC-002": true}
	}))

	if r.LeadTime.Count() != 1 || r.LeadTime.P50 != 10*24*time.Hour {
		t.Errorf("LeadTime = %+v, want single 10d sample (fast-track excluded)", r.LeadTime)
	}
	if r.FastTrackLead.Count() != 1 || r.FastTrackLead.P50 != 24*time.Hour {
		t.Errorf("FastTrackLead = %+v, want single 1d sample", r.FastTrackLead)
	}
}

func TestComputeFlow_BottleneckAndReversionBoundaries(t *testing.T) {
	var events []Event
	events = append(events, specJourney("SPEC-001", 8, 9, 10)...) // engineering entered day 8 → dwell driven by draft
	events = append(events,
		dayEv("SPEC-002", KindScaffolded, "", "draft", 0),
		dayEv("SPEC-002", KindAdvanced, "draft", "tl-review", 1),
		dayEv("SPEC-002", KindReverted, "tl-review", "draft", 2),
		dayEv("SPEC-002", KindAdvanced, "draft", "tl-review", 3),
		dayEv("SPEC-002", KindReverted, "tl-review", "draft", 4),
	)

	r := ComputeFlow(flowInputFor(events, nil))

	if r.Bottleneck != "draft" {
		t.Errorf("Bottleneck = %q, want draft", r.Bottleneck)
	}
	if len(r.Reversions) != 1 || r.Reversions[0] != (ReversionBoundary{From: "tl-review", To: "draft", Count: 2}) {
		t.Errorf("Reversions = %+v", r.Reversions)
	}
	if r.TotalReversions != 2 {
		t.Errorf("TotalReversions = %d, want 2", r.TotalReversions)
	}
}

func TestComputeFlow_AgingWIP(t *testing.T) {
	var events []Event
	// Three completed specs give draft a dwell population: 1d, 2d, 3d → p85 = 3d.
	events = append(events, specJourney("SPEC-001", 1, 4, 10)...)
	events = append(events, specJourney("SPEC-002", 2, 5, 11)...)
	events = append(events, specJourney("SPEC-003", 3, 6, 12)...)
	// Open spec sitting in draft since day 0; "now" is day 100 → far beyond p85.
	events = append(events, dayEv("SPEC-004", KindScaffolded, "", "draft", 0))

	r := ComputeFlow(flowInputFor(events, nil))

	if len(r.AgingWIP) != 1 {
		t.Fatalf("AgingWIP = %+v, want 1 item", r.AgingWIP)
	}
	item := r.AgingWIP[0]
	if item.SpecID != "SPEC-004" || item.Stage != "draft" || item.Ratio <= 1 {
		t.Errorf("AgingWIP[0] = %+v", item)
	}
	if item.P85 != 3*24*time.Hour {
		t.Errorf("P85 = %v, want 3d", item.P85)
	}
}

func TestComputeFlow_BlockedTime(t *testing.T) {
	events := []Event{
		dayEv("SPEC-001", KindScaffolded, "", "build", 0),
		dayEv("SPEC-001", KindEjected, "build", "blocked", 1),
		dayEv("SPEC-001", KindResumed, "blocked", "build", 3),
		dayEv("SPEC-002", KindScaffolded, "", "build", 0),
		dayEv("SPEC-002", KindEjected, "build", "blocked", 2), // still blocked at now
	}
	in := flowInputFor(events, func(in *FlowInput) {
		in.Window = Window{From: t0(0), To: t0(4 * 24 * 60), Label: "test"}
		in.Now = t0(4 * 24 * 60)
	})
	r := ComputeFlow(in)

	if r.BlockedSpecs != 2 {
		t.Errorf("BlockedSpecs = %d, want 2", r.BlockedSpecs)
	}
	if r.BlockedTotal != 4*24*time.Hour { // 2d closed spell + 2d ongoing
		t.Errorf("BlockedTotal = %v, want 4d", r.BlockedTotal)
	}
}

func TestComputeFlow_FilterScopesEverything(t *testing.T) {
	var events []Event
	events = append(events, specJourney("SPEC-001", 2, 4, 10)...)
	events = append(events, specJourney("SPEC-002", 4, 6, 20)...)

	r := ComputeFlow(flowInputFor(events, func(in *FlowInput) {
		in.Filter = map[string]bool{"SPEC-001": true}
	}))

	if r.Completed != 1 {
		t.Errorf("Completed = %d, want 1", r.Completed)
	}
	if r.TotalAdvances != 3 {
		t.Errorf("TotalAdvances = %d, want 3 (SPEC-002 excluded)", r.TotalAdvances)
	}
}

func TestComputeFlow_EmptyWindow(t *testing.T) {
	r := ComputeFlow(flowInputFor(nil, nil))
	if r.Completed != 0 || r.LeadTime.Count() != 0 || r.Bottleneck != "" {
		t.Errorf("empty report = %+v", r)
	}
	if r.FlowEfficiency != -1 {
		t.Errorf("FlowEfficiency = %v, want -1 (unknown)", r.FlowEfficiency)
	}
}

func TestPercentile_NearestRank(t *testing.T) {
	samples := []time.Duration{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	d := NewDistribution(samples)
	if d.P50 != 5 {
		t.Errorf("P50 = %d, want 5", d.P50)
	}
	if d.P85 != 9 {
		t.Errorf("P85 = %d, want 9", d.P85)
	}
	if d.P95 != 10 {
		t.Errorf("P95 = %d, want 10", d.P95)
	}
}

func TestSparkline(t *testing.T) {
	if got := Sparkline(nil); got != "" {
		t.Errorf("Sparkline(nil) = %q, want empty", got)
	}
	got := Sparkline([]time.Duration{1, 1, 1, 1, 10})
	if got == "" {
		t.Fatal("Sparkline returned empty for non-empty samples")
	}
	runes := []rune(got)
	if runes[0] != '█' {
		t.Errorf("Sparkline = %q, want peak bucket first", got)
	}
}
