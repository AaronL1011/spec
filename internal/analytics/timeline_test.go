package analytics

import (
	"testing"
	"time"
)

// evAt builds an event for the fixture spec at t0(minutes).
func evAt(kind EventKind, from, to string, minutes int) Event {
	return Event{SpecID: "SPEC-001", Kind: kind, FromStage: from, ToStage: to, At: t0(minutes), Source: SourceMessage}
}

var terminals = []string{"done", "closed"}

func TestBuildTimelines_HappyPath(t *testing.T) {
	events := []Event{
		evAt(KindScaffolded, "", "draft", 0),
		evAt(KindAdvanced, "draft", "engineering", 60),
		evAt(KindAdvanced, "engineering", "build", 180),
		evAt(KindAdvanced, "build", "done", 300),
	}
	tls := BuildTimelines(events, terminals)
	tl := tls["SPEC-001"]
	if tl == nil {
		t.Fatal("no timeline for SPEC-001")
	}
	if !tl.Completed() || !tl.CompletedAt.Equal(t0(300)) {
		t.Errorf("CompletedAt = %v, want %v", tl.CompletedAt, t0(300))
	}
	if tl.LeadTime() != 300*time.Minute {
		t.Errorf("LeadTime = %v, want 300m", tl.LeadTime())
	}
	if got := tl.StageDwellTotal("draft"); got != 60*time.Minute {
		t.Errorf("draft dwell = %v, want 60m", got)
	}
	if got := tl.StageDwellTotal("engineering"); got != 120*time.Minute {
		t.Errorf("engineering dwell = %v, want 120m", got)
	}
	if tl.CurrentStage != "done" || tl.Advances != 3 {
		t.Errorf("CurrentStage=%s Advances=%d", tl.CurrentStage, tl.Advances)
	}
}

func TestBuildTimelines_RevertLoopAccumulatesDwell(t *testing.T) {
	events := []Event{
		evAt(KindScaffolded, "", "draft", 0),
		evAt(KindAdvanced, "draft", "tl-review", 30),
		evAt(KindReverted, "tl-review", "draft", 60),
		evAt(KindAdvanced, "draft", "tl-review", 120),
	}
	tl := BuildTimelines(events, terminals)["SPEC-001"]
	if got := tl.StageDwellTotal("draft"); got != 90*time.Minute {
		t.Errorf("draft dwell = %v, want 90m (30m + 60m across revisits)", got)
	}
	if len(tl.Reversions) != 1 || tl.Reversions[0].From != "tl-review" || tl.Reversions[0].To != "draft" {
		t.Errorf("Reversions = %+v", tl.Reversions)
	}
	if tl.Completed() {
		t.Error("spec should not be completed")
	}
}

func TestBuildTimelines_EjectResumeTracksBlockedSpell(t *testing.T) {
	events := []Event{
		evAt(KindScaffolded, "", "build", 0),
		evAt(KindEjected, "build", "blocked", 60),
		evAt(KindResumed, "blocked", "build", 180),
	}
	tl := BuildTimelines(events, terminals)["SPEC-001"]
	if len(tl.Blocked) != 1 {
		t.Fatalf("Blocked spells = %d, want 1", len(tl.Blocked))
	}
	if got := tl.Blocked[0].End.Sub(tl.Blocked[0].Start); got != 120*time.Minute {
		t.Errorf("blocked duration = %v, want 120m", got)
	}
	if tl.CurrentStage != "build" {
		t.Errorf("CurrentStage = %s, want build", tl.CurrentStage)
	}
}

func TestBuildTimelines_HistoryPredatesScaffold(t *testing.T) {
	// First observed event is an advance — no scaffold in history.
	events := []Event{
		evAt(KindAdvanced, "draft", "build", 100),
		evAt(KindAdvanced, "build", "done", 200),
	}
	tl := BuildTimelines(events, terminals)["SPEC-001"]
	if !tl.Created.Equal(t0(100)) {
		t.Errorf("Created = %v, want first event time", tl.Created)
	}
	if got := tl.StageDwellTotal("build"); got != 100*time.Minute {
		t.Errorf("build dwell = %v, want 100m", got)
	}
	if !tl.Completed() {
		t.Error("spec should be completed")
	}
}

func TestBuildTimelines_NonMonotonicInputIsSorted(t *testing.T) {
	events := []Event{
		evAt(KindAdvanced, "draft", "build", 60),
		evAt(KindScaffolded, "", "draft", 0),
	}
	tl := BuildTimelines(events, terminals)["SPEC-001"]
	if !tl.Created.Equal(t0(0)) {
		t.Errorf("Created = %v, want scaffold time after sorting", tl.Created)
	}
	if got := tl.StageDwellTotal("draft"); got != 60*time.Minute {
		t.Errorf("draft dwell = %v, want 60m", got)
	}
}
