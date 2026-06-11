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

// statusReport is the machine-readable shape emitted by `spec status --json`.
// It is a stable scripting/CI contract: field names and types must not change
// without a deliberate version bump.
type statusReport struct {
	ID          string             `json:"id"`
	Title       string             `json:"title"`
	Status      string             `json:"status"`
	Author      string             `json:"author"`
	Cycle       string             `json:"cycle"`
	Version     string             `json:"version"`
	EpicKey     string             `json:"epic_key,omitempty"`
	Repos       []string           `json:"repos,omitempty"`
	Source      string             `json:"source,omitempty"`
	RevertCount int                `json:"revert_count"`
	Sections    []statusSection    `json:"sections"`
	Gates       []statusGateResult `json:"gates"`
}

type statusSection struct {
	Slug    string `json:"slug"`
	Owner   string `json:"owner,omitempty"`
	HasData bool   `json:"has_data"`
}

type statusGateResult struct {
	Gate   string `json:"gate"`
	Passed bool   `json:"passed"`
	Reason string `json:"reason,omitempty"`
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

	if jsonOut, _ := cmd.Flags().GetBool("json"); jsonOut {
		return newPrinter(cmd).JSON(buildStatusReport(pl, meta, sections))
	}

	// Header
	fmt.Printf("%s — %s\n", meta.ID, meta.Title)
	fmt.Printf("Status: %s\n", meta.Status)
	fmt.Printf("Author: %s\n", meta.Author)
	fmt.Printf("Cycle: %s\n", meta.Cycle)
	fmt.Printf("Version: %s\n", meta.Version)
	if meta.EpicKey != "" {
		fmt.Printf("Epic: %s%s\n", meta.EpicKey, pmDriftSuffix(specID))
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

// buildStatusReport assembles the machine-readable status shape from the spec
// metadata, its level-2 sections, and the current-stage gate evaluation.
func buildStatusReport(pl config.PipelineConfig, meta *markdown.SpecMeta, sections []markdown.Section) statusReport {
	rep := statusReport{
		ID:          meta.ID,
		Title:       meta.Title,
		Status:      meta.Status,
		Author:      meta.Author,
		Cycle:       meta.Cycle,
		Version:     meta.Version,
		EpicKey:     meta.EpicKey,
		Repos:       meta.Repos,
		Source:      meta.Source,
		RevertCount: meta.RevertCount,
	}
	for _, s := range sections {
		if s.Level != 2 {
			continue
		}
		rep.Sections = append(rep.Sections, statusSection{
			Slug:    s.Slug,
			Owner:   s.Owner,
			HasData: strings.TrimSpace(s.Content) != "",
		})
	}
	hasPRStack := markdown.IsSectionNonEmpty(sections, "pr_stack_plan")
	for _, r := range pipeline.EvaluateGates(pl, meta.Status, sections, hasPRStack, false, meta) {
		rep.Gates = append(rep.Gates, statusGateResult{Gate: r.Gate, Passed: r.Passed, Reason: r.Reason})
	}
	return rep
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

// pmDriftSuffix reports whether the spec has deferred PM operations awaiting
// reconciliation, so a stale Jira board is visible at a glance. Returns ""
// when in sync or when no local DB is available.
func pmDriftSuffix(specID string) string {
	if recorderDB == nil {
		return ""
	}
	pending, err := recorderDB.PMQueuePending(specID)
	if err != nil || len(pending) == 0 {
		return ""
	}
	return fmt.Sprintf("  ⚠ Jira out of sync (%d pending — run 'spec sync --pm')", len(pending))
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
