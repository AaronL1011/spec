package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/pipeline"
	"github.com/aaronl1011/spec/internal/syncaudit"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status [id]",
	Short: "Show pipeline position, section completion, and cycle metrics",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	specID, err := resolveSpecIDArg(args, "spec status <id>")
	if err != nil {
		return err
	}

	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	path, err := resolveSpecPath(rc, specID)
	if err != nil {
		return err
	}

	meta, err := readSpecMeta(path)
	if err != nil {
		return err
	}

	sections, err := markdown.ExtractSectionsFromFile(path)
	if err != nil {
		return err
	}

	pl := rc.Pipeline()

	// Header
	fmt.Printf("%s — %s\n", meta.ID, meta.Title)
	fmt.Printf("Status: %s\n", meta.Status)
	fmt.Printf("Author: %s\n", meta.Author)
	fmt.Printf("Cycle: %s\n", meta.Cycle)
	fmt.Printf("Version: %s\n", meta.Version)
	if meta.EpicKey != "" {
		fmt.Printf("Epic: %s\n", meta.EpicKey)
	}
	if len(meta.Repos) > 0 {
		fmt.Printf("Repos: %s\n", strings.Join(meta.Repos, ", "))
	}
	if meta.Source != "" && meta.Source != "direct" {
		fmt.Printf("Source: %s\n", meta.Source)
	}
	if meta.RevertCount > 0 {
		fmt.Printf("Reversions: %d\n", meta.RevertCount)
	}
	fmt.Println()

	// Sync freshness/health line (AC-9, AC-19). Best-effort: drains the
	// queued-push backlog opportunistically, then reports staleness.
	printSyncFreshness(cmd, rc)

	// Pipeline diagram
	fmt.Println("Pipeline:")
	renderPipelineDiagram(pl, meta.Status)
	fmt.Println()

	// Section completion
	fmt.Println("Section completion:")
	for _, s := range sections {
		if s.Level != 2 {
			continue
		}
		hasContent := strings.TrimSpace(s.Content) != ""
		indicator := "✗"
		if hasContent {
			indicator = "✓"
		}
		ownerLabel := ""
		if s.Owner != "" {
			ownerLabel = fmt.Sprintf(" (%s)", s.Owner)
		}
		fmt.Printf("  %s %s%s\n", indicator, s.Slug, ownerLabel)
	}
	fmt.Println()

	// Gate check for current stage
	hasPRStack := markdown.IsSectionNonEmpty(sections, "pr_stack_plan")
	results := pipeline.EvaluateGates(pl, meta.Status, sections, hasPRStack, false, meta)
	if len(results) > 0 {
		fmt.Println("Gate checks (current stage):")
		for _, r := range results {
			if r.Passed {
				fmt.Printf("  ✓ %s\n", r.Gate)
			} else {
				fmt.Printf("  ✗ %s — %s\n", r.Gate, r.Reason)
			}
		}
	}

	return nil
}

// printSyncFreshness drains the queued-push backlog and prints a one-line
// freshness/health summary plus recent audit events.
func printSyncFreshness(cmd *cobra.Command, rc *config.ResolvedConfig) {
	if rc.Team == nil {
		return
	}
	rec := syncaudit.New(recorderDB)
	// Opportunistic queue drain on status (SPEC-013 §7.1).
	gitpkg.FlushQueue(ctx(), &rc.Team.SpecsRepo, syncOpts(cmd, ""))

	f := gitpkg.SyncFreshness(ctx(), &rc.Team.SpecsRepo, rec)
	age := "never"
	if !f.LastFetch.IsZero() {
		age = humanizeAge(time.Since(f.LastFetch))
	}
	fmt.Printf("Sync: last fetch %s ago", age)
	if f.CommitsBehind > 0 {
		fmt.Printf(" · %d new upstream commit(s)", f.CommitsBehind)
	}
	if f.QueuedPushes > 0 {
		fmt.Printf(" · %d queued push(es)", f.QueuedPushes)
	}
	fmt.Println()

	if recorderDB != nil {
		if entries, err := recorderDB.SyncAuditRecent(3); err == nil && len(entries) > 0 {
			fmt.Println("Recent sync activity:")
			for _, e := range entries {
				fmt.Printf("  %s %s/%s %s [%s]\n",
					humanizeAge(time.Since(e.CreatedAt)), e.Surface, e.Trigger, e.Op, e.Outcome)
			}
		}
	}
	fmt.Println()
}

// humanizeAge renders a duration as a compact age string.
func humanizeAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func renderPipelineDiagram(pl config.PipelineConfig, current string) {
	for _, stage := range pl.Stages {
		marker := "  "
		if stage.Name == current {
			marker = "▶ "
		}
		optional := ""
		if stage.Optional {
			optional = " (optional)"
		}
		fmt.Printf("  %s%-18s  %s%s\n", marker, stage.Name, stage.OwnerRole, optional)
	}
	if current == "blocked" {
		fmt.Printf("  ▶ %-18s  (escape hatch)\n", "blocked")
	}
}
