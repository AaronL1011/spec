package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/planning"
	"github.com/aaronl1011/spec/internal/tui"
	"github.com/spf13/cobra"
)

var reviewCmd = &cobra.Command{
	Use:   "review [id]",
	Short: "Post structured review request with all stacked PRs",
	Long: `Post structured review request with all stacked PRs.

With --plan flag, review the technical build plan instead of PRs.
Use --approve or --request-changes to submit your review decision.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runReview,
}

func init() {
	reviewCmd.Flags().Bool("plan", false, "review the technical build plan instead of PRs")
	reviewCmd.Flags().Bool("approve", false, "approve the plan review (use with --plan)")
	reviewCmd.Flags().Bool("request-changes", false, "request changes to the plan (use with --plan)")
	reviewCmd.Flags().String("feedback", "", "feedback message when requesting changes")
	rootCmd.AddCommand(reviewCmd)
}

func runReview(cmd *cobra.Command, args []string) error {
	specID, err := resolveSpecIDArg(args, "spec review <id>")
	if err != nil {
		return err
	}

	isPlanReview, _ := cmd.Flags().GetBool("plan")
	if isPlanReview {
		return runPlanReview(cmd, specID)
	}

	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	path, err := resolveSpecPath(rc, specID)
	if err != nil {
		return err
	}

	meta, err := markdown.ReadMeta(path)
	if err != nil {
		return err
	}

	if len(meta.Repos) == 0 {
		return fmt.Errorf("no repos listed in %s frontmatter — add 'repos:' to the spec", specID)
	}

	reg := buildRegistry(rc)

	// List PRs from all repos
	prs, err := reg.Repo().ListPRs(ctx(), meta.Repos, specID)
	if err != nil {
		return fmt.Errorf("listing PRs: %w", err)
	}

	if len(prs) == 0 {
		fmt.Printf("No open PRs found for %s across %s\n", specID, strings.Join(meta.Repos, ", "))
		return nil
	}

	// Display PRs in order
	fmt.Printf("Review request for %s — %s\n\n", specID, meta.Title)
	for i, pr := range prs {
		fmt.Printf("  %d. PR #%d — %s (%s)\n", i+1, pr.Number, pr.Title, pr.Repo)
		if pr.URL != "" {
			fmt.Printf("     %s\n", pr.URL)
		}
	}

	// Post to comms — non-fatal, warn on failure
	if rc.HasIntegration("comms") {
		msg := fmt.Sprintf("[%s] Review requested — %s\n", specID, meta.Title)
		for _, pr := range prs {
			msg += fmt.Sprintf("  • PR #%d: %s (%s)\n", pr.Number, pr.Title, pr.Repo)
		}
		if err := reg.Comms().Notify(ctx(), adapter.Notification{
			SpecID:  specID,
			Title:   meta.Title,
			Message: msg,
		}); err != nil {
			warnf("could not send notification: %v", err)
		} else {
			fmt.Println("\n✓ Review request posted to comms.")
		}
	}

	return nil
}

func runPlanReview(cmd *cobra.Command, specID string) error {
	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	path, err := resolveSpecPath(rc, specID)
	if err != nil {
		return err
	}

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
		return fmt.Errorf("no build plan defined for %s", specID)
	}

	if plan.Review == nil || plan.Review.Status != planning.ReviewPending {
		fmt.Printf("No pending plan review for %s\n", specID)
		if plan.Review != nil {
			fmt.Printf("Current status: %s\n", plan.Review.Status)
		}
		return nil
	}

	approve, _ := cmd.Flags().GetBool("approve")
	requestChanges, _ := cmd.Flags().GetBool("request-changes")
	feedback, _ := cmd.Flags().GetString("feedback")

	// Get current user
	reviewer := "reviewer" // default
	if rc.User != nil && rc.User.User.Name != "" {
		reviewer = rc.User.User.Name
	}

	// If no action specified, show the plan for review
	if !approve && !requestChanges {
		printPlanForReview(specID, plan)
		return nil
	}

	if approve {
		return approvePlan(rc, plan, meta, specID, path, string(content), reviewer)
	}
	return requestPlanChanges(plan, meta, specID, path, string(content), reviewer, feedback)
}

// printPlanForReview renders the pending plan and the approve/request-changes
// follow-up commands.
func printPlanForReview(specID string, plan *planning.Plan) {
	tui.PrintTitle(fmt.Sprintf("Plan Review: %s", specID))
	fmt.Println()

	fmt.Printf("  Requested: %s\n", plan.Review.RequestedAt.Format("2006-01-02 15:04"))
	fmt.Printf("  Reviewers: %s\n", strings.Join(plan.Review.Reviewers, ", "))
	fmt.Println()

	fmt.Println("  Build Plan:")
	for _, step := range plan.Steps {
		repoPrefix := ""
		if step.Repo != "" {
			repoPrefix = fmt.Sprintf("[%s] ", step.Repo)
		}
		fmt.Printf("    %d. %s%s\n", step.Index, repoPrefix, step.Description)
	}

	fmt.Println()
	fmt.Println("To approve:")
	fmt.Printf("  spec review %s --plan --approve\n", specID)
	fmt.Println()
	fmt.Println("To request changes:")
	fmt.Printf("  spec review %s --plan --request-changes --feedback \"your feedback\"\n", specID)
}

// approvePlan records an approval and persists the review state to the spec
// frontmatter.
func approvePlan(rc *config.ResolvedConfig, plan *planning.Plan, meta *markdown.SpecMeta, specID, path, content, reviewer string) error {
	minApprovals := 1
	if rc.Team != nil {
		pl := rc.Pipeline()
		stage := pl.StageByName(meta.Status)
		if stage != nil && stage.Review != nil {
			minApprovals = stage.Review.GetMinApprovals()
		}
	}

	if err := plan.Approve(reviewer, minApprovals); err != nil {
		return err
	}
	if err := writePlanFrontmatter(plan, meta, path, content); err != nil {
		return err
	}

	if plan.IsReviewApproved() {
		tui.PrintSuccess(fmt.Sprintf("Plan approved for %s", specID))
		fmt.Println("The engineer can now begin build work with 'spec do'.")
	} else {
		tui.PrintSuccess(fmt.Sprintf("Approval recorded for %s (%d/%d required)", specID, len(plan.Review.Approvals), minApprovals))
	}
	return nil
}

// requestPlanChanges records a change request with feedback and persists the
// review state to the spec frontmatter.
func requestPlanChanges(plan *planning.Plan, meta *markdown.SpecMeta, specID, path, content, reviewer, feedback string) error {
	if feedback == "" {
		return fmt.Errorf("--feedback is required when requesting changes")
	}

	if err := plan.RequestChanges(reviewer, feedback); err != nil {
		return err
	}
	if err := writePlanFrontmatter(plan, meta, path, content); err != nil {
		return err
	}

	tui.PrintSuccess(fmt.Sprintf("Changes requested for %s plan", specID))
	fmt.Printf("Feedback: %s\n", feedback)
	fmt.Println("\nThe engineer should update the plan and run 'spec plan ready' again.")
	return nil
}

// writePlanFrontmatter mirrors the plan's steps + review into the spec
// frontmatter and writes the file.
func writePlanFrontmatter(plan *planning.Plan, meta *markdown.SpecMeta, path, content string) error {
	steps, review := plan.ToFrontmatter()
	meta.Steps = steps
	meta.Review = review

	newContent, err := markdown.UpdateFrontmatter(content, meta)
	if err != nil {
		return fmt.Errorf("updating frontmatter: %w", err)
	}
	if err := os.WriteFile(path, []byte(newContent), 0o644); err != nil {
		return fmt.Errorf("writing spec: %w", err)
	}
	return nil
}
