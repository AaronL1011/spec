package metrics

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aaronl1011/spec/internal/store"
)

// SpecMetrics holds computed metrics for a single spec's journey.
type SpecMetrics struct {
	SpecID        string
	StagesVisited []string
	TimePerStage  map[string]time.Duration
	TotalTime     time.Duration
	Reversions    int
	Ejections     int
}

// ComputeForSpec calculates per-spec metrics from activity entries.
func ComputeForSpec(specID string, allEntries []store.ActivityEntry) *SpecMetrics {
	m := &SpecMetrics{
		SpecID:       specID,
		TimePerStage: make(map[string]time.Duration),
	}

	// Filter entries for this spec
	var entries []store.ActivityEntry
	for _, e := range allEntries {
		if e.SpecID == specID {
			entries = append(entries, e)
		}
	}

	if len(entries) == 0 {
		return m
	}

	// Sort by time ascending
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].CreatedAt.Before(entries[j].CreatedAt)
	})

	// Track stages visited and counts
	stagesSeen := make(map[string]bool)
	var advances []store.ActivityEntry

	for _, e := range entries {
		switch e.EventType {
		case "advance":
			advances = append(advances, e)
			var meta advanceMeta
			if json.Unmarshal([]byte(e.Metadata), &meta) == nil {
				if meta.FromStage != "" {
					stagesSeen[meta.FromStage] = true
				}
				if meta.ToStage != "" {
					stagesSeen[meta.ToStage] = true
				}
			}
		case "revert":
			m.Reversions++
		case "eject":
			m.Ejections++
		}
	}

	// Preserve order of first visit
	var orderedStages []string
	seen := make(map[string]bool)
	for _, e := range advances {
		var meta advanceMeta
		if json.Unmarshal([]byte(e.Metadata), &meta) != nil {
			continue
		}
		for _, s := range []string{meta.FromStage, meta.ToStage} {
			if s != "" && !seen[s] {
				orderedStages = append(orderedStages, s)
				seen[s] = true
			}
		}
	}
	m.StagesVisited = orderedStages

	// Compute time-in-stage from consecutive advance events
	for i := 0; i < len(advances)-1; i++ {
		var meta advanceMeta
		if json.Unmarshal([]byte(advances[i].Metadata), &meta) != nil || meta.FromStage == "" {
			continue
		}
		dwell := advances[i+1].CreatedAt.Sub(advances[i].CreatedAt)
		m.TimePerStage[meta.FromStage] += dwell
		m.TotalTime += dwell
	}

	return m
}

// FormatSpecSummary renders a human-readable summary of a spec's journey.
func FormatSpecSummary(sm *SpecMetrics) string {
	var sb strings.Builder

	if len(sm.StagesVisited) > 0 {
		fmt.Fprintf(&sb, "- **Stages**: %s\n", strings.Join(sm.StagesVisited, " → "))
	}
	if sm.TotalTime > 0 {
		fmt.Fprintf(&sb, "- **Total time**: %s\n", FormatDuration(sm.TotalTime))
	}
	if sm.Reversions > 0 {
		fmt.Fprintf(&sb, "- **Reversions**: %d\n", sm.Reversions)
	}
	if sm.Ejections > 0 {
		fmt.Fprintf(&sb, "- **Ejections**: %d\n", sm.Ejections)
	}

	if len(sm.TimePerStage) > 0 {
		sb.WriteString("\n**Time per stage:**\n\n")
		for _, stage := range sm.StagesVisited {
			if d, ok := sm.TimePerStage[stage]; ok {
				fmt.Fprintf(&sb, "- **%s**: %s\n", stage, FormatDuration(d))
			}
		}
	}

	return sb.String()
}
