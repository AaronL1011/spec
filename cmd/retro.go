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
	"github.com/aaronl1011/spec/internal/store"
	"github.com/spf13/cobra"
)

var retroCmd = &cobra.Command{
	Use:   "retro",
	Short: "Auto-populate retrospective with cycle metrics",
	RunE:  runRetro,
}

func init() {
	retroCmd.Flags().String("cycle", "", "retrospect a specific cycle")
	retroCmd.Flags().String("since", "30d", "time window for retrospective (e.g. 7d, 24h)")
	retroCmd.Flags().Bool("write", false, "write retrospective section into completed spec files")
	rootCmd.AddCommand(retroCmd)
}

func runRetro(cmd *cobra.Command, args []string) error {
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

	writeFlag, _ := cmd.Flags().GetBool("write")

	db, err := openDB()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// Load activity
	entries, err := db.ActivitySince(since)
	if err != nil {
		return fmt.Errorf("loading activity: %w", err)
	}

	// Scan specs
	specsByStage := scanSpecsByStage(rc.SpecsRepoDir)

	pipe := rc.Pipeline()
	stageNames := pipe.StageNames()
	terminalStages := pipeline.TerminalStages(pipe)

	m := metrics.Compute(entries, specsByStage, stageNames, terminalStages)

	// Render header
	label := cycle
	if label == "" {
		label = fmt.Sprintf("last %s", cmd.Flag("since").Value.String())
	}
	fmt.Printf("Retrospective — %s\n", label)
	fmt.Println("────────────────────────────────────────────────")

	// Summary metrics
	fmt.Println("\nMetrics:")
	fmt.Printf("  Specs completed:     %d\n", m.SpecsCompleted)
	fmt.Printf("  Total advances:      %d\n", m.TotalAdvances)
	fmt.Printf("  Total reversions:    %d\n", m.TotalReversions)
	if m.TotalAdvances > 0 {
		fmt.Printf("  Reversion rate:      %.0f%%\n", m.ReversionRate*100)
	} else {
		fmt.Printf("  Reversion rate:      —\n")
	}
	if m.BottleneckStage != "" {
		fmt.Printf("  Bottleneck stage:    %s (%s avg dwell)\n",
			m.BottleneckStage, metrics.FormatDuration(m.AvgTimePerStage[m.BottleneckStage]))
	} else {
		fmt.Printf("  Bottleneck stage:    —\n")
	}

	// Stage dwell times
	if len(m.AvgTimePerStage) > 0 {
		fmt.Println("\nStage dwell times:")
		for _, name := range stageNames {
			if avg, ok := m.AvgTimePerStage[name]; ok {
				fmt.Printf("  %-20s %s\n", name, metrics.FormatDuration(avg))
			}
		}
	}

	// Reversions detail
	reverts, _ := db.ActivityForType("revert", since)
	if len(reverts) > 0 {
		fmt.Println("\nReversions:")
		for _, r := range reverts {
			fmt.Printf("  • %s: %s\n", r.SpecID, r.Summary)
		}
	}

	// Ejections
	ejects, _ := db.ActivityForType("eject", since)
	if len(ejects) > 0 {
		fmt.Println("\nEjections:")
		for _, e := range ejects {
			fmt.Printf("  • %s: %s\n", e.SpecID, e.Summary)
		}
	}

	if len(entries) == 0 {
		fmt.Println("\n  (no activity in time window — advance/revert specs to generate data)")
	}

	// Optionally write retrospective section into completed spec files
	if writeFlag {
		written := writeRetroSections(rc.SpecsRepoDir, terminalStages, entries, m, stageNames, label)
		if written > 0 {
			fmt.Printf("\n✓ Wrote retrospective section to %d completed spec(s).\n", written)
		} else {
			fmt.Println("\n  (no completed specs to write retrospective into)")
		}
	}

	return nil
}

// writeRetroSections writes a ## Retrospective section into specs in terminal stages.
// Each spec gets its own per-spec journey metrics plus cycle-level context.
func writeRetroSections(specsDir string, terminalStages []string, entries []store.ActivityEntry, cycleMet *metrics.PipelineMetrics, stageNames []string, label string) int {
	terminalSet := make(map[string]bool, len(terminalStages))
	for _, s := range terminalStages {
		terminalSet[s] = true
	}

	dirEntries, err := os.ReadDir(specsDir)
	if err != nil {
		return 0
	}

	written := 0
	for _, e := range dirEntries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		path := filepath.Join(specsDir, e.Name())
		meta, err := markdown.ReadMeta(path)
		if err != nil || !strings.HasPrefix(meta.ID, "SPEC-") {
			continue
		}
		if !terminalSet[meta.Status] {
			continue
		}

		specMet := metrics.ComputeForSpec(meta.ID, entries)
		content := buildRetroSection(specMet, cycleMet, stageNames, label)
		if err := markdown.ReplaceSection(path, "retrospective", content); err != nil {
			// Section might not exist — that's fine, skip silently
			continue
		}
		written++
	}
	return written
}

// buildRetroSection generates the markdown content for a retrospective section.
// Per-spec journey first, then cycle-level context.
func buildRetroSection(spec *metrics.SpecMetrics, cycle *metrics.PipelineMetrics, stageNames []string, label string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "*Auto-generated retrospective — %s — %s*\n\n",
		label, time.Now().Format("2006-01-02"))

	// Per-spec journey
	sb.WriteString("### This spec\n\n")
	sb.WriteString(metrics.FormatSpecSummary(spec))

	// Cycle context
	sb.WriteString("\n### Cycle context\n\n")
	fmt.Fprintf(&sb, "- **Specs completed**: %d\n", cycle.SpecsCompleted)
	if cycle.TotalAdvances > 0 {
		fmt.Fprintf(&sb, "- **Reversion rate**: %.0f%%\n", cycle.ReversionRate*100)
	}
	if cycle.BottleneckStage != "" {
		fmt.Fprintf(&sb, "- **Bottleneck**: %s (%s avg)\n",
			cycle.BottleneckStage, metrics.FormatDuration(cycle.AvgTimePerStage[cycle.BottleneckStage]))
	}

	sb.WriteString("\n")
	return sb.String()
}
