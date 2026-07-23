package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aaronl1011/spec/internal/analytics"
	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/pipeline"
	"github.com/spf13/cobra"
)

var metricsCmd = &cobra.Command{
	Use:   "metrics",
	Short: "Show team flow analytics from the specs repo history",
	Long: `Reconstruct team flow metrics — lead time, cycle time, stage dwell,
bottlenecks, reversion boundaries, and aging WIP — from the specs repo git
history. Metrics are system-centred: they show where work waits, never who
is slow.`,
	Example: "  spec metrics\n  spec metrics --cycle \"Cycle 7\"\n  spec metrics --spec SPEC-012\n  spec metrics --stage pr-review\n  spec metrics --json",
	RunE:    runMetrics,
}

// defaultMetricsWindow is the trailing report window when --since is not given.
const defaultMetricsWindow = "90d"

func init() {
	metricsCmd.Flags().String("cycle", "", "scope the report to a cycle label")
	metricsCmd.Flags().String("since", defaultMetricsWindow, "time window for metrics (e.g. 90d, 24h)")
	metricsCmd.Flags().String("spec", "", "render a single spec's journey")
	metricsCmd.Flags().String("stage", "", "deep-dive one stage")
	rootCmd.AddCommand(metricsCmd)
}

func runMetrics(cmd *cobra.Command, args []string) error {
	p := newPrinter(cmd)
	rc, err := resolveConfig()
	if err != nil {
		return err
	}
	if rc.Team == nil || rc.SpecsRepoDir == "" {
		p.Line("metrics requires a specs repo — run 'spec config init' to set one up")
		return nil
	}

	data, err := loadFlowData(rc, p)
	if err != nil {
		return err
	}

	if specID, _ := cmd.Flags().GetString("spec"); specID != "" {
		return renderMetricsSpec(p, data, specID)
	}

	report, prev, err := computeMetricsReport(cmd, rc, data)
	if err != nil {
		return err
	}

	if stage, _ := cmd.Flags().GetString("stage"); stage != "" {
		return renderMetricsStage(p, rc, report, data, stage)
	}
	if p.JSONEnabled() {
		return p.JSON(report.ToJSON())
	}
	p.Raw(analytics.RenderFlowReport(report, prev))
	return nil
}

// flowData is everything extracted from the specs repo for one metrics run.
type flowData struct {
	events    []analytics.Event
	timelines map[string]*analytics.Timeline
	coverage  analytics.Coverage
	fastTrack map[string]bool
	cycles    map[string]string // spec ID → cycle label
	now       time.Time
}

// loadFlowData syncs the specs repo (degrading to the stale local clone when
// the remote is unreachable), replays its history into events and timelines,
// and indexes spec frontmatter for cycle/fast-track scoping.
func loadFlowData(rc *config.ResolvedConfig, p *printer) (*flowData, error) {
	if _, err := gitpkg.EnsureSpecsRepo(ctx(), &rc.Team.SpecsRepo); err != nil {
		if _, statErr := os.Stat(rc.SpecsRepoDir); statErr != nil {
			return nil, fmt.Errorf("syncing specs repo: %w", err)
		}
		p.Warn("specs repo unreachable — using local clone, data may be stale")
	}

	repoRoot := filepath.Dir(rc.SpecsRepoDir)
	entries, err := gitpkg.Log(ctx(), repoRoot, gitpkg.LogOptions{Path: gitpkg.SpecsSubDir, WithPatch: true})
	if err != nil {
		return nil, fmt.Errorf("reading specs repo history: %w", err)
	}

	pipe := rc.Pipeline()
	res := analytics.ExtractEvents(entries, pipe.StageNames())
	data := &flowData{
		events:    res.Events,
		timelines: analytics.BuildTimelines(res.Events, pipeline.TerminalStages(pipe)),
		coverage: analytics.Coverage{
			CommitsScanned: res.CommitsScanned,
			Transitions:    res.Transitions,
			Unattributable: res.Unattributable,
		},
		fastTrack: make(map[string]bool),
		cycles:    make(map[string]string),
		now:       time.Now(),
	}
	indexSpecMeta(rc, data)
	return data, nil
}

// indexSpecMeta scans active and archived spec files for the frontmatter
// fields git history cannot provide: cycle labels and fast-track flags.
func indexSpecMeta(rc *config.ResolvedConfig, data *flowData) {
	dirs := []string{rc.SpecsRepoDir, filepath.Join(rc.SpecsRepoDir, config.ArchiveDir(rc.Team))}
	for _, dir := range dirs {
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || filepath.Ext(f.Name()) != ".md" {
				continue
			}
			meta, err := markdown.ReadMeta(filepath.Join(dir, f.Name()))
			if err != nil || !strings.HasPrefix(meta.ID, "SPEC-") {
				continue
			}
			data.cycles[meta.ID] = meta.Cycle
			if meta.FastTrack {
				data.fastTrack[meta.ID] = true
			}
		}
	}
}

// computeMetricsReport resolves the report window from flags and computes the
// flow report plus, in time-window mode, a preceding equal-length comparison.
func computeMetricsReport(cmd *cobra.Command, rc *config.ResolvedConfig, data *flowData) (report, prev *analytics.FlowReport, err error) {
	in := analytics.FlowInput{
		Timelines:  data.timelines,
		Events:     data.events,
		Now:        data.now,
		StageNames: rc.Pipeline().StageNames(),
		Terminal:   pipeline.TerminalStages(rc.Pipeline()),
		WorkStages: engineerStages(rc.Pipeline()),
		FastTrack:  data.fastTrack,
	}

	if cycle, _ := cmd.Flags().GetString("cycle"); cycle != "" {
		in.Filter, in.Window = cycleScope(cycle, data)
		report = analytics.ComputeFlow(in)
		report.Coverage = data.coverage
		return report, nil, nil
	}

	since, err := parseSinceFlag(cmd)
	if err != nil {
		return nil, nil, err
	}
	in.Window = analytics.Window{From: since, To: data.now, Label: "last " + cmd.Flag("since").Value.String()}
	report = analytics.ComputeFlow(in)
	report.Coverage = data.coverage

	span := data.now.Sub(since)
	in.Window = analytics.Window{From: since.Add(-span), To: since, Label: "previous"}
	prev = analytics.ComputeFlow(in)
	if prev.Completed == 0 {
		prev = nil
	}
	return report, prev, nil
}

// cycleScope builds the spec-ID filter and window for a --cycle report: the
// specs labelled with the cycle, spanning their earliest creation to now.
func cycleScope(cycle string, data *flowData) (map[string]bool, analytics.Window) {
	filter := make(map[string]bool)
	from := data.now
	for id, label := range data.cycles {
		if label != cycle {
			continue
		}
		filter[id] = true
		if tl := data.timelines[id]; tl != nil && !tl.Created.IsZero() && tl.Created.Before(from) {
			from = tl.Created
		}
	}
	return filter, analytics.Window{From: from, To: data.now, Label: cycle}
}

// engineerStages returns the stage names owned by the engineer role — the
// cycle-time anchor and flow-efficiency numerator.
func engineerStages(pipe config.PipelineConfig) []string {
	var stages []string
	for _, s := range pipe.Stages {
		if strings.Contains(strings.ToLower(s.GetOwner()), "engineer") {
			stages = append(stages, s.Name)
		}
	}
	return stages
}

// renderMetricsSpec renders one spec's journey.
func renderMetricsSpec(p *printer, data *flowData, specID string) error {
	tl, ok := data.timelines[specID]
	if !ok {
		return fmt.Errorf("no history for %s — check the ID with 'spec list --all'", specID)
	}
	if p.JSONEnabled() {
		return p.JSON(specTimelineJSON(tl))
	}
	p.Raw(analytics.RenderSpecTimeline(tl, data.now))
	return nil
}

// renderMetricsStage renders the deep-dive for one stage.
func renderMetricsStage(p *printer, rc *config.ResolvedConfig, report *analytics.FlowReport, data *flowData, stage string) error {
	if rc.Pipeline().StageByName(stage) == nil {
		return fmt.Errorf("unknown stage %q — valid stages: %s", stage, strings.Join(rc.Pipeline().StageNames(), ", "))
	}
	if p.JSONEnabled() {
		return p.JSON(report.ToJSON())
	}
	p.Raw(analytics.RenderStageReport(report, stage, data.timelines, data.now))
	return nil
}

// specTimelineJSON is the machine-readable single-spec journey.
func specTimelineJSON(tl *analytics.Timeline) map[string]interface{} {
	visits := make([]map[string]interface{}, 0, len(tl.Visits))
	for _, v := range tl.Visits {
		visits = append(visits, map[string]interface{}{
			"stage": v.Stage, "entered": v.Entered, "exited": v.Exited,
			"dwell_seconds": int64(v.Duration().Seconds()),
		})
	}
	out := map[string]interface{}{
		"spec_id":       tl.SpecID,
		"created":       tl.Created,
		"visits":        visits,
		"current_stage": tl.CurrentStage,
		"current_since": tl.CurrentSince,
		"reversions":    len(tl.Reversions),
		"blocked":       len(tl.Blocked),
		"completed":     tl.Completed(),
	}
	if tl.Completed() {
		out["completed_at"] = tl.CompletedAt
		out["lead_time_seconds"] = int64(tl.LeadTime().Seconds())
	}
	return out
}

// parseSinceFlag parses the --since duration flag into a time.Time.
func parseSinceFlag(cmd *cobra.Command) (time.Time, error) {
	raw, _ := cmd.Flags().GetString("since")
	if raw == "" {
		raw = defaultMetricsWindow
	}

	// Support "Nd" shorthand for days
	if strings.HasSuffix(raw, "d") {
		trimmed := strings.TrimSuffix(raw, "d")
		var days int
		if _, err := fmt.Sscanf(trimmed, "%d", &days); err == nil {
			return time.Now().Add(-time.Duration(days) * 24 * time.Hour), nil
		}
	}

	d, err := time.ParseDuration(raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid --since value %q — use e.g. 7d, 24h, 168h: %w", raw, err)
	}
	return time.Now().Add(-d), nil
}
