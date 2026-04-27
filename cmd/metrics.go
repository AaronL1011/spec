package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/metrics"
	"github.com/aaronl1011/spec/internal/pipeline"
	"github.com/spf13/cobra"
)

var metricsCmd = &cobra.Command{
	Use:   "metrics",
	Short: "Show quantitative pipeline health metrics",
	RunE:  runMetrics,
}

func init() {
	metricsCmd.Flags().String("cycle", "", "metrics for a specific cycle")
	metricsCmd.Flags().String("since", "30d", "time window for metrics (e.g. 7d, 24h)")
	rootCmd.AddCommand(metricsCmd)
}

func runMetrics(cmd *cobra.Command, args []string) error {
	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	cycle, _ := cmd.Flags().GetString("cycle")
	if cycle == "" {
		cycle = rc.CycleLabel()
	}

	since, err := parseSinceFlag(cmd)
	if err != nil {
		return err
	}

	db, err := openDB()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// Load activity entries for the time window
	entries, err := db.ActivitySince(since)
	if err != nil {
		return fmt.Errorf("loading activity: %w", err)
	}

	// Scan spec files for current stage distribution
	specsByStage := scanSpecsByStage(rc.SpecsRepoDir)

	// Derive pipeline info
	pipe := rc.Pipeline()
	stageNames := pipe.StageNames()
	terminalStages := pipeline.TerminalStages(pipe)

	m := metrics.Compute(entries, specsByStage, stageNames, terminalStages)

	// Render output
	label := cycle
	if label == "" {
		label = fmt.Sprintf("last %s", cmd.Flag("since").Value.String())
	}
	fmt.Printf("Pipeline metrics — %s\n", label)
	fmt.Println("────────────────────────────────────────────────")

	fmt.Printf("\n  Specs completed:     %d\n", m.SpecsCompleted)
	fmt.Printf("  Total advances:      %d\n", m.TotalAdvances)
	fmt.Printf("  Total reversions:    %d\n", m.TotalReversions)
	if m.TotalAdvances > 0 {
		fmt.Printf("  Reversion rate:      %.0f%%\n", m.ReversionRate*100)
	} else {
		fmt.Printf("  Reversion rate:      —\n")
	}
	if m.BottleneckStage != "" {
		fmt.Printf("  Bottleneck stage:    %s (%s avg)\n",
			m.BottleneckStage, metrics.FormatDuration(m.AvgTimePerStage[m.BottleneckStage]))
	} else {
		fmt.Printf("  Bottleneck stage:    —\n")
	}

	// Stage distribution
	fmt.Println("\nCurrent stage distribution:")
	hasSpecs := false
	for _, name := range stageNames {
		count := specsByStage[name]
		if count > 0 {
			fmt.Printf("  %-20s %d\n", name, count)
			hasSpecs = true
		}
	}
	if !hasSpecs {
		fmt.Println("  (no specs found)")
	}

	// Average time per stage
	if len(m.AvgTimePerStage) > 0 {
		fmt.Println("\nAvg time per stage:")
		for _, name := range stageNames {
			if avg, ok := m.AvgTimePerStage[name]; ok {
				fmt.Printf("  %-20s %s\n", name, metrics.FormatDuration(avg))
			}
		}
	}

	if len(entries) == 0 {
		fmt.Println("\n  (no activity in time window — advance/revert specs to generate data)")
	}

	return nil
}

// parseSinceFlag parses the --since duration flag into a time.Time.
func parseSinceFlag(cmd *cobra.Command) (time.Time, error) {
	raw, _ := cmd.Flags().GetString("since")
	if raw == "" {
		return time.Now().Add(-30 * 24 * time.Hour), nil
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

// scanSpecsByStage reads spec files and counts how many are in each stage.
func scanSpecsByStage(specsDir string) map[string]int {
	counts := make(map[string]int)
	entries, err := os.ReadDir(specsDir)
	if err != nil {
		return counts
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		meta, err := markdown.ReadMeta(filepath.Join(specsDir, e.Name()))
		if err != nil || !strings.HasPrefix(meta.ID, "SPEC-") {
			continue
		}
		if meta.Status != "" {
			counts[meta.Status]++
		}
	}
	return counts
}


