package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/aaronl1011/spec-cli/internal/markdown"
	"github.com/aaronl1011/spec-cli/internal/planning"
	"github.com/aaronl1011/spec-cli/internal/tui"
	"github.com/spf13/cobra"
)

var planCmd = &cobra.Command{
	Use:   "plan [id]",
	Short: "View or manage technical build plans",
	Long: `View or manage technical build plans for a spec.

Without arguments, shows the plan for the current spec (from .spec/context).
With a spec ID, shows that spec's build plan.

Subcommands allow editing plans and requesting reviews.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runPlanShow,
}

var planEditCmd = &cobra.Command{
	Use:   "edit [id]",
	Short: "Edit a spec's build plan section in $EDITOR",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runPlanEdit,
}

var planAddCmd = &cobra.Command{
	Use:   "add [id] <description>",
	Short: "Add a step to the build plan",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runPlanAdd,
}

var planReadyCmd = &cobra.Command{
	Use:   "ready [id]",
	Short: "Request review for the build plan",
	Long: `Mark the build plan as ready for review.

This notifies configured reviewers (typically TL) that the technical
plan is ready for approval before build work begins.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runPlanReady,
}

func init() {
	planAddCmd.Flags().String("repo", "", "target repository for this step")

	planCmd.AddCommand(planEditCmd)
	planCmd.AddCommand(planAddCmd)
	planCmd.AddCommand(planReadyCmd)
	rootCmd.AddCommand(planCmd)
}

func runPlanShow(cmd *cobra.Command, args []string) error {
	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	specID, err := resolveSpecIDFromArgs(args)
	if err != nil {
		return err
	}

	path, err := resolveSpecPath(rc, specID)
	if err != nil {
		return err
	}

	meta, err := markdown.ReadMeta(path)
	if err != nil {
		return fmt.Errorf("reading spec: %w", err)
	}

	plan := planning.FromMeta(meta)
	if plan == nil || !plan.HasSteps() {
		fmt.Printf("No build plan defined for %s\n", specID)
		fmt.Printf("\nTo create a plan, add steps to the frontmatter or run:\n")
		fmt.Printf("  spec plan add %s \"Description of first step\"\n", specID)
		return nil
	}

	// Print plan header
	tui.PrintTitle(fmt.Sprintf("Build Plan: %s", specID))
	fmt.Println()

	// Print review status
	if plan.Review != nil {
		switch plan.Review.Status {
		case planning.ReviewPending:
			fmt.Println("  Review: ⏳ pending")
		case planning.ReviewApproved:
			fmt.Println("  Review: ✓ approved")
		case planning.ReviewChangesRequested:
			fmt.Println("  Review: ✗ changes requested")
			if plan.Review.Feedback != "" {
				fmt.Printf("  Feedback: %s\n", plan.Review.Feedback)
			}
		}
		fmt.Println()
	}

	// Print progress
	completed, total := plan.Progress()
	fmt.Printf("  Progress: %d/%d steps\n\n", completed, total)

	// Print steps
	for _, step := range plan.Steps {
		var statusIcon string
		switch step.Status {
		case planning.StatusComplete:
			statusIcon = "✓"
		case planning.StatusInProgress:
			statusIcon = "▶"
		case planning.StatusBlocked:
			statusIcon = "⊘"
		default:
			statusIcon = "○"
		}

		repoPrefix := ""
		if step.Repo != "" {
			repoPrefix = fmt.Sprintf("[%s] ", step.Repo)
		}

		fmt.Printf("  %s %d. %s%s\n", statusIcon, step.Index, repoPrefix, step.Description)

		if step.Branch != "" {
			fmt.Printf("       Branch: %s\n", step.Branch)
		}
		if step.PR > 0 {
			fmt.Printf("       PR: #%d\n", step.PR)
		}
		if step.BlockedReason != "" {
			fmt.Printf("       Blocked: %s\n", step.BlockedReason)
		}
	}

	fmt.Println()

	// Suggest next action
	if plan.NeedsReview() {
		fmt.Printf("Run 'spec plan ready %s' to request review.\n", specID)
	} else if plan.IsReviewPending() {
		fmt.Println("Waiting for plan review approval.")
	} else if plan.IsReviewChangesRequested() {
		fmt.Printf("Run 'spec plan edit %s' to address feedback.\n", specID)
	} else if !plan.AllComplete() {
		current := plan.CurrentStep()
		if current != nil {
			fmt.Printf("Current step: %d. %s\n", current.Index, current.Description)
		}
	}

	return nil
}

func runPlanEdit(cmd *cobra.Command, args []string) error {
	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	specID, err := resolveSpecIDFromArgs(args)
	if err != nil {
		return err
	}

	path, err := resolveSpecPath(rc, specID)
	if err != nil {
		return err
	}

	editor := os.Getenv("EDITOR")
	if rc.User != nil && rc.User.Preferences.Editor != "" {
		editor = rc.User.Preferences.Editor
	}
	if editor == "" {
		editor = "vi"
	}

	// Open editor at the spec file
	editorCmd := exec.Command(editor, path)
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr

	fmt.Printf("Opening %s to edit build plan...\n", path)
	fmt.Println("Add or edit the 'steps:' section in the frontmatter.")
	fmt.Println()

	return editorCmd.Run()
}

func runPlanAdd(cmd *cobra.Command, args []string) error {
	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	// Parse args: either "add <desc>" or "add <id> <desc>"
	var specID, description string
	if len(args) == 1 {
		// Use context spec
		id, err := resolveSpecIDFromArgs(nil)
		if err != nil {
			return fmt.Errorf("no spec ID provided and no context set — use 'spec plan add SPEC-001 \"description\"'")
		}
		specID = id
		description = args[0]
	} else {
		specID = normalizeSpecID(args[0])
		description = strings.Join(args[1:], " ")
	}

	repo, _ := cmd.Flags().GetString("repo")

	path, err := resolveSpecPath(rc, specID)
	if err != nil {
		return err
	}

	// Read current content
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading spec: %w", err)
	}

	meta, err := markdown.ParseMeta(string(content))
	if err != nil {
		return fmt.Errorf("parsing spec: %w", err)
	}

	// Add the step
	plan := planning.FromMeta(meta)
	if plan == nil {
		plan = &planning.Plan{SpecID: specID}
	}
	plan.AddStep(repo, description)

	// Update frontmatter
	steps, _ := plan.ToFrontmatter()
	meta.Steps = steps

	// Write back
	newContent, err := markdown.UpdateFrontmatter(string(content), meta)
	if err != nil {
		return fmt.Errorf("updating frontmatter: %w", err)
	}

	if err := os.WriteFile(path, []byte(newContent), 0o644); err != nil {
		return fmt.Errorf("writing spec: %w", err)
	}

	stepNum := len(plan.Steps)
	tui.PrintSuccess(fmt.Sprintf("Added step %d to %s: %s", stepNum, specID, description))

	return nil
}

func runPlanReady(cmd *cobra.Command, args []string) error {
	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	specID, err := resolveSpecIDFromArgs(args)
	if err != nil {
		return err
	}

	path, err := resolveSpecPath(rc, specID)
	if err != nil {
		return err
	}

	// Read current content
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading spec: %w", err)
	}

	meta, err := markdown.ParseMeta(string(content))
	if err != nil {
		return fmt.Errorf("parsing spec: %w", err)
	}

	plan := planning.FromMeta(meta)
	if plan == nil || !plan.HasSteps() {
		return fmt.Errorf("no build plan defined — run 'spec plan add %s \"description\"' first", specID)
	}

	// Validate the plan
	issues := plan.Validate()
	if len(issues) > 0 {
		fmt.Println("Plan has issues:")
		for _, issue := range issues {
			fmt.Printf("  • %s\n", issue)
		}
		return fmt.Errorf("fix issues before requesting review")
	}

	// Determine reviewers from pipeline config
	reviewers := []string{"tl"} // default
	if rc.Team != nil {
		pl := rc.Pipeline()
		stage := pl.StageByName(meta.Status)
		if stage != nil && stage.Review != nil && len(stage.Review.Reviewers) > 0 {
			reviewers = stage.Review.Reviewers
		}
	}

	// Request review
	if err := plan.RequestReview(reviewers); err != nil {
		return err
	}

	// Update frontmatter
	steps, review := plan.ToFrontmatter()
	meta.Steps = steps
	meta.Review = review

	// Write back
	newContent, err := markdown.UpdateFrontmatter(string(content), meta)
	if err != nil {
		return fmt.Errorf("updating frontmatter: %w", err)
	}

	if err := os.WriteFile(path, []byte(newContent), 0o644); err != nil {
		return fmt.Errorf("writing spec: %w", err)
	}

	tui.PrintSuccess(fmt.Sprintf("Review requested for %s build plan", specID))
	fmt.Printf("Reviewers: %s\n", strings.Join(reviewers, ", "))
	fmt.Println("\nThe reviewer can approve with:")
	fmt.Printf("  spec review --plan %s --approve\n", specID)

	return nil
}

// resolveSpecIDFromArgs gets the spec ID from args or tries to detect from current context
