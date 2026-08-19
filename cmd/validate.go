package cmd

import (
	"fmt"

	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/pipeline"
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

	// Parent/child invariants are re-checked here because frontmatter is
	// hand-editable: link-time refusal cannot see a `parent:` typed straight
	// into the file. Errors block the advance; warnings are printed and the
	// author proceeds.
	tree := specHierarchyView(rc, specID, meta.Parent)
	hierarchyBroken := printHierarchyFindings(tree.Findings)

	// Determine the next stage to check gates for
	nextStage, err := pipeline.NextStage(pl, meta.Status, true)
	if err != nil {
		fmt.Printf("%s is at %s — no further stages to validate.\n", specID, meta.Status)
		return hierarchyBlocker(specID, hierarchyBroken)
	}

	hasPRStack := markdown.IsSectionNonEmpty(sections, "pr_stack_plan")
	results := pipeline.EvaluateGates(pl, nextStage, sections, hasPRStack, false, meta, tree.Rollup.ExprContext())

	if len(results) == 0 {
		if !hierarchyBroken {
			fmt.Printf("✓ %s: no gates defined for %s → %s\n", specID, meta.Status, nextStage)
		}
		return hierarchyBlocker(specID, hierarchyBroken)
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
	switch {
	case !allPassed:
		fmt.Printf("✗ Some gates failed. Resolve the issues above before advancing.\n")
	case hierarchyBroken:
		fmt.Printf("✓ All gates passed, but the parent link is broken.\n")
	default:
		fmt.Printf("✓ All gates passed. Run 'spec advance %s' to proceed.\n", specID)
	}

	return hierarchyBlocker(specID, hierarchyBroken)
}

// hierarchyBlocker turns a broken parent link into the command's exit error, so
// `spec validate` fails for the same reason `spec advance` will refuse.
func hierarchyBlocker(specID string, broken bool) error {
	if !broken {
		return nil
	}
	return fmt.Errorf("%s has a broken parent link — fix it with 'spec link %s --parent <id>' (or --parent \"\" to detach)", specID, specID)
}
