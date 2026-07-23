package analytics

import "time"

// JSON DTOs for --json output. Durations are emitted both as integer seconds
// (stable machine contract) and a human string (convenience).

// DurationJSON is a duration in machine and human form.
type DurationJSON struct {
	Seconds int64  `json:"seconds"`
	Human   string `json:"human"`
}

// DistributionJSON is the percentile summary of a distribution.
type DistributionJSON struct {
	Samples int          `json:"samples"`
	P50     DurationJSON `json:"p50"`
	P85     DurationJSON `json:"p85"`
	P95     DurationJSON `json:"p95"`
}

// StageDwellJSON is one stage's dwell distribution.
type StageDwellJSON struct {
	Stage string           `json:"stage"`
	Dwell DistributionJSON `json:"dwell"`
}

// ReversionJSON is one reversion boundary count.
type ReversionJSON struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Count int    `json:"count"`
}

// AgingJSON is one aging-WIP entry.
type AgingJSON struct {
	SpecID string       `json:"spec_id"`
	Stage  string       `json:"stage"`
	Dwell  DurationJSON `json:"dwell"`
	P85    DurationJSON `json:"p85"`
	Ratio  float64      `json:"ratio"`
}

// ReportJSON is the machine-readable flow report.
type ReportJSON struct {
	Window struct {
		From  time.Time `json:"from"`
		To    time.Time `json:"to"`
		Label string    `json:"label"`
	} `json:"window"`
	Completed         int               `json:"completed"`
	LeadTime          DistributionJSON  `json:"lead_time"`
	CycleTime         DistributionJSON  `json:"cycle_time"`
	FastTrackLead     *DistributionJSON `json:"fast_track_lead,omitempty"`
	FlowEfficiency    *float64          `json:"flow_efficiency,omitempty"`
	ThroughputPerWeek float64           `json:"throughput_per_week"`
	StageDwell        []StageDwellJSON  `json:"stage_dwell"`
	Bottleneck        string            `json:"bottleneck,omitempty"`
	Reversions        []ReversionJSON   `json:"reversions"`
	TotalAdvances     int               `json:"total_advances"`
	TotalReversions   int               `json:"total_reversions"`
	AgingWIP          []AgingJSON       `json:"aging_wip"`
	BlockedSpecs      int               `json:"blocked_specs"`
	BlockedTotal      DurationJSON      `json:"blocked_total"`
	Coverage          struct {
		CommitsScanned int `json:"commits_scanned"`
		Transitions    int `json:"transitions"`
		Unattributable int `json:"unattributable"`
	} `json:"coverage"`
}

// ToJSON converts a FlowReport into its machine-readable form.
func (r *FlowReport) ToJSON() ReportJSON {
	var out ReportJSON
	out.Window.From = r.Window.From
	out.Window.To = r.Window.To
	out.Window.Label = r.Window.Label
	out.Completed = r.Completed
	out.LeadTime = distJSON(r.LeadTime)
	out.CycleTime = distJSON(r.CycleTime)
	if r.FastTrackLead.Count() > 0 {
		ft := distJSON(r.FastTrackLead)
		out.FastTrackLead = &ft
	}
	if r.FlowEfficiency >= 0 {
		fe := r.FlowEfficiency
		out.FlowEfficiency = &fe
	}
	out.ThroughputPerWeek = r.ThroughputPerWeek
	out.StageDwell = make([]StageDwellJSON, 0, len(r.StageDwell))
	for _, sd := range r.StageDwell {
		out.StageDwell = append(out.StageDwell, StageDwellJSON{Stage: sd.Stage, Dwell: distJSON(sd.Dist)})
	}
	out.Bottleneck = r.Bottleneck
	out.Reversions = make([]ReversionJSON, 0, len(r.Reversions))
	for _, b := range r.Reversions {
		out.Reversions = append(out.Reversions, ReversionJSON(b))
	}
	out.TotalAdvances = r.TotalAdvances
	out.TotalReversions = r.TotalReversions
	out.AgingWIP = make([]AgingJSON, 0, len(r.AgingWIP))
	for _, item := range r.AgingWIP {
		out.AgingWIP = append(out.AgingWIP, AgingJSON{
			SpecID: item.SpecID, Stage: item.Stage,
			Dwell: durJSON(item.Dwell), P85: durJSON(item.P85), Ratio: item.Ratio,
		})
	}
	out.BlockedSpecs = r.BlockedSpecs
	out.BlockedTotal = durJSON(r.BlockedTotal)
	out.Coverage.CommitsScanned = r.Coverage.CommitsScanned
	out.Coverage.Transitions = r.Coverage.Transitions
	out.Coverage.Unattributable = r.Coverage.Unattributable
	return out
}

// distJSON converts a Distribution to its DTO.
func distJSON(d Distribution) DistributionJSON {
	return DistributionJSON{
		Samples: d.Count(),
		P50:     durJSON(d.P50),
		P85:     durJSON(d.P85),
		P95:     durJSON(d.P95),
	}
}

// durJSON converts a duration to its DTO.
func durJSON(d time.Duration) DurationJSON {
	return DurationJSON{Seconds: int64(d.Seconds()), Human: FormatDuration(d)}
}
