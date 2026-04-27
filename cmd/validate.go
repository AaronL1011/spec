package cmd

import (
	"fmt"

	"github.com/aaronl1011/spec-cli/internal/markdown"
	"github.com/aaronl1011/spec-cli/internal/pipeline"
	"github.com/spf13/cobra"
)

var validateCmd = &cobra.Command{
	Use:   "validate [id]",
	Short: "Dry-run all gate checks for the current stage without advancing",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runValidate,
}

func init() {
	rootCmd.AddCommand(validateCmd)
}

func runValidate(cmd *cobra.Command, args []string) error {
	specID, err := resolveSpecIDArg(args, "spec validate <id>")
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

	// Determine the next stage to check gates for
	nextStage, err := pipeline.NextStage(pl, meta.Status, true)
	if err != nil {
		fmt.Printf("%s is at %s — no further stages to validate.\n", specID, meta.Status)
		return nil
	}

	hasPRStack := markdown.IsSectionNonEmpty(sections, "pr_stack_plan")
	results := pipeline.EvaluateGates(pl, nextStage, sections, hasPRStack, false, meta)

	if len(results) == 0 {
		fmt.Printf("✓ %s: no gates defined for %s → %s\n", specID, meta.Status, nextStage)
		return nil
	}

	fmt.Printf("Gate checks for %s: %s → %s\n\n", specID, meta.Status, nextStage)

	allPassed := true
	for _, r := range results {
		if r.Passed {
			fmt.Printf("  ✓ %s\n", r.Gate)
		} else {
			fmt.Printf("  ✗ %s\n    %s\n", r.Gate, r.Reason)
			allPassed = false
		}
	}

	fmt.Println()
	if allPassed {
		fmt.Printf("✓ All gates passed. Run 'spec advance %s' to proceed.\n", specID)
	} else {
		fmt.Printf("✗ Some gates failed. Resolve the issues above before advancing.\n")
	}

	return nil
}
