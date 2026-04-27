package cmd

import (
	"fmt"
	"strings"

	"github.com/aaronl1011/spec-cli/internal/config"
	"github.com/aaronl1011/spec-cli/internal/markdown"
	"github.com/aaronl1011/spec-cli/internal/pipeline"
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
