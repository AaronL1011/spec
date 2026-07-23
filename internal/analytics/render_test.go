package analytics

import (
	"strings"
	"testing"
	"time"
)

func sampleReport(t *testing.T) (*FlowReport, map[string]*Timeline) {
	t.Helper()
	var events []Event
	events = append(events, specJourney("SPEC-001", 2, 4, 10)...)
	events = append(events, specJourney("SPEC-002", 4, 6, 20)...)
	events = append(events, specJourney("SPEC-003", 6, 8, 30)...)
	events = append(events,
		dayEv("SPEC-004", KindScaffolded, "", "draft", 0),
		dayEv("SPEC-005", KindScaffolded, "", "build", 0),
		dayEv("SPEC-005", KindEjected, "build", "blocked", 1),
	)
	in := flowInputFor(events, func(in *FlowInput) {
		in.Window.Label = "Cycle 7"
	})
	in.Timelines["SPEC-001"].Reversions = nil // keep fixture deterministic
	r := ComputeFlow(in)
	r.Coverage = Coverage{CommitsScanned: 20, Transitions: 15, Unattributable: 1}
	return r, in.Timelines
}

func TestRenderFlowReport_Sections(t *testing.T) {
	r, _ := sampleReport(t)
	out := RenderFlowReport(r, nil)

	for _, want := range []string{
		"Flow — Cycle 7",
		"3 specs completed",
		"Lead time",
		"Cycle time",
		"p50 20d",
		"Flow efficiency",
		"Throughput",
		"Time in stage (p50)",
		"◀ bottleneck",
		"Aging WIP",
		"SPEC-004",
		"Blocked: 1 spec,",
		"analysed 15/16 transitions · 20 commits scanned",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
}

func TestRenderFlowReport_EmptyWindow(t *testing.T) {
	r := ComputeFlow(flowInputFor(nil, nil))
	out := RenderFlowReport(r, nil)
	if !strings.Contains(out, "No completed specs in this window.") {
		t.Errorf("empty report missing note:\n%s", out)
	}
	if strings.Contains(out, "Aging WIP") || strings.Contains(out, "Reversions") {
		t.Errorf("empty report should hide empty sections:\n%s", out)
	}
}

func TestRenderFlowReport_DeltasAgainstPrevious(t *testing.T) {
	r, _ := sampleReport(t)
	prev := *r
	prev.ThroughputPerWeek = r.ThroughputPerWeek / 2
	out := RenderFlowReport(r, &prev)
	if !strings.Contains(out, "↑ 100% vs previous") {
		t.Errorf("report missing throughput delta:\n%s", out)
	}
	if !strings.Contains(out, "▬") {
		t.Errorf("report missing unchanged stage delta markers:\n%s", out)
	}
}

func TestRenderFlowReport_ReversionCallout(t *testing.T) {
	var events []Event
	events = append(events, specJourney("SPEC-001", 2, 4, 10)...)
	events = append(events,
		dayEv("SPEC-002", KindScaffolded, "", "draft", 0),
		dayEv("SPEC-002", KindAdvanced, "draft", "tl-review", 1),
		dayEv("SPEC-002", KindReverted, "tl-review", "draft", 2),
	)
	r := ComputeFlow(flowInputFor(events, nil))
	out := RenderFlowReport(r, nil)
	if !strings.Contains(out, "Reversions: 1 (tl-review→draft ×1)") {
		t.Errorf("report missing reversion boundary:\n%s", out)
	}
	if !strings.Contains(out, "⚠ tl-review→draft reversion rate 25%") {
		t.Errorf("report missing diagnostic callout:\n%s", out)
	}
}

func TestRenderSpecTimeline(t *testing.T) {
	events := []Event{
		dayEv("SPEC-001", KindScaffolded, "", "draft", 0),
		dayEv("SPEC-001", KindAdvanced, "draft", "build", 2),
		dayEv("SPEC-001", KindEjected, "build", "blocked", 3),
		dayEv("SPEC-001", KindResumed, "blocked", "build", 5),
		dayEv("SPEC-001", KindAdvanced, "build", "done", 6),
	}
	tl := BuildTimelines(events, terminals)["SPEC-001"]
	out := RenderSpecTimeline(tl, t0(7*24*60))

	for _, want := range []string{"SPEC-001 — journey", "draft", "build", "◀ current", "Lead time: 6d", "Blocked: 1 spells, 2d total"} {
		if !strings.Contains(out, want) {
			t.Errorf("timeline missing %q:\n%s", want, out)
		}
	}
}

func TestRenderStageReport(t *testing.T) {
	r, timelines := sampleReport(t)
	out := RenderStageReport(r, "draft", timelines, t0(100*24*60))

	for _, want := range []string{"Stage draft", "Dwell", "Samples: 3 completed specs", "Currently in stage:", "SPEC-004"} {
		if !strings.Contains(out, want) {
			t.Errorf("stage report missing %q:\n%s", want, out)
		}
	}
}

func TestRenderStageReport_NoSamples(t *testing.T) {
	r, timelines := sampleReport(t)
	out := RenderStageReport(r, "qa-validation", timelines, t0(0))
	if !strings.Contains(out, "No completed dwell samples") {
		t.Errorf("stage report missing empty note:\n%s", out)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Minute, "30m"},
		{90 * time.Minute, "1.5h"},
		{24 * time.Hour, "1d"},
		{36 * time.Hour, "1.5d"},
		{6 * 24 * time.Hour, "6d"},
	}
	for _, tt := range tests {
		if got := FormatDuration(tt.d); got != tt.want {
			t.Errorf("FormatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestReportJSON_Shape(t *testing.T) {
	r, _ := sampleReport(t)
	j := r.ToJSON()

	if j.Completed != 3 || j.LeadTime.Samples != 3 {
		t.Errorf("JSON completed/lead samples = %d/%d", j.Completed, j.LeadTime.Samples)
	}
	if j.LeadTime.P50.Seconds != int64((20 * 24 * time.Hour).Seconds()) {
		t.Errorf("JSON lead p50 = %d", j.LeadTime.P50.Seconds)
	}
	if j.FlowEfficiency == nil {
		t.Error("JSON flow efficiency missing")
	}
	if len(j.StageDwell) == 0 || j.StageDwell[0].Stage != "draft" {
		t.Errorf("JSON stage dwell = %+v", j.StageDwell)
	}
	if len(j.AgingWIP) != 1 || j.AgingWIP[0].SpecID != "SPEC-004" {
		t.Errorf("JSON aging = %+v", j.AgingWIP)
	}
	if j.Coverage.Transitions != 15 || j.Coverage.Unattributable != 1 {
		t.Errorf("JSON coverage = %+v", j.Coverage)
	}
}
