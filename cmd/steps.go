package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/aaronl1011/spec-cli/internal/markdown"
	"github.com/aaronl1011/spec-cli/internal/planning"
	"github.com/aaronl1011/spec-cli/internal/steps"
	"github.com/aaronl1011/spec-cli/internal/tui"
	"github.com/spf13/cobra"
)

var stepsCmd = &cobra.Command{
	Use:   "steps [id]",
	Short: "View and manage build plan steps",
	Long: `View and manage build plan steps for a spec.

Without arguments, shows the steps for the current spec.
With a spec ID, shows that spec's build plan steps.

Use subcommands to start, complete, or block steps.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runStepsShow,
}

var stepsNextCmd = &cobra.Command{
	Use:   "next [id]",
	Short: "Show the next step to work on",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runStepsNext,
}

var stepsStartCmd = &cobra.Command{
	Use:   "start [id] [step-number]",
	Short: "Start working on a step",
	Long: `Start working on a step, creating a branch if needed.

Without a step number, starts the next pending step.
With a step number, starts that specific step (if valid).`,
	Args: cobra.MaximumNArgs(2),
	RunE: runStepsStart,
}

var stepsCompleteCmd = &cobra.Command{
	Use:   "complete [id] [step-number]",
	Short: "Mark a step as complete",
	Long: `Mark a step as complete.

Without a step number, completes the current in-progress step.
Use --pr to record the PR number.`,
	Args: cobra.MaximumNArgs(2),
	RunE: runStepsComplete,
}

var stepsBlockCmd = &cobra.Command{
	Use:   "block [id] [step-number] <reason>",
	Short: "Mark a step as blocked",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runStepsBlock,
}

var stepsUnblockCmd = &cobra.Command{
	Use:   "unblock [id] [step-number]",
	Short: "Remove blocked status from a step",
	Args:  cobra.MaximumNArgs(2),
	RunE:  runStepsUnblock,
}

func init() {
	stepsCompleteCmd.Flags().Int("pr", 0, "PR number to record")

	stepsCmd.AddCommand(stepsNextCmd)
	stepsCmd.AddCommand(stepsStartCmd)
	stepsCmd.AddCommand(stepsCompleteCmd)
	stepsCmd.AddCommand(stepsBlockCmd)
	stepsCmd.AddCommand(stepsUnblockCmd)
	rootCmd.AddCommand(stepsCmd)
}

func runStepsShow(cmd *cobra.Command, args []string) error {
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

	engine := steps.NewEngine(rc.User)
	completed, total, current := engine.Progress(meta)

	if total == 0 {
		fmt.Printf("No build steps defined for %s\n", specID)
		fmt.Printf("\nTo add steps, edit the spec or run:\n")
		fmt.Printf("  spec plan add %s \"Description of step\"\n", specID)
		return nil
	}

	// Print header
	tui.PrintTitle(fmt.Sprintf("Build Steps: %s", specID))
	fmt.Printf("\nProgress: %d/%d steps complete\n\n", completed, total)

	// Print steps
	plan := planning.FromMeta(meta)
	for _, step := range plan.Steps {
		var statusIcon, statusText string
		switch step.Status {
		case planning.StatusComplete:
			statusIcon = "✓"
			statusText = ""
		case planning.StatusInProgress:
			statusIcon = "▶"
			statusText = " (in progress)"
		case planning.StatusBlocked:
			statusIcon = "⊘"
			statusText = " (blocked)"
		default:
			statusIcon = "○"
			statusText = ""
		}

		repoPrefix := ""
		if step.Repo != "" {
			repoPrefix = fmt.Sprintf("[%s] ", step.Repo)
		}

		fmt.Printf("  %s %d. %s%s%s\n", statusIcon, step.Index, repoPrefix, step.Description, statusText)

		if step.Branch != "" {
			fmt.Printf("       Branch: %s\n", step.Branch)
		}
		if step.PR > 0 {
			fmt.Printf("       PR: #%d\n", step.PR)
		}
		if step.BlockedReason != "" {
			fmt.Printf("       Reason: %s\n", step.BlockedReason)
		}
	}

	fmt.Println()

	// Suggest next action
	if current != nil {
		switch current.Status {
		case planning.StatusPending:
			fmt.Printf("Next: spec steps start %s %d\n", specID, current.Index)
		case planning.StatusInProgress:
			fmt.Printf("Current: step %d in progress\n", current.Index)
			fmt.Printf("When done: spec steps complete %s\n", specID)
		case planning.StatusBlocked:
			fmt.Printf("Step %d is blocked: %s\n", current.Index, current.BlockedReason)
			fmt.Printf("To unblock: spec steps unblock %s %d\n", specID, current.Index)
		}
	} else if completed == total {
		tui.PrintSuccess("All steps complete!")
	}

	return nil
}

func runStepsNext(cmd *cobra.Command, args []string) error {
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

	engine := steps.NewEngine(rc.User)
	next, err := engine.GetNextStep(meta)
	if err != nil {
		return err
	}

	if next == nil {
		tui.PrintSuccess("All steps complete!")
		return nil
	}

	fmt.Printf("Next step for %s:\n\n", specID)
	fmt.Printf("  Step %d: %s\n", next.Index, next.Description)

	if next.Repo != "" {
		fmt.Printf("  Repo: %s\n", next.Repo)
		if next.WorkspacePath != "" {
			fmt.Printf("  Path: %s\n", next.WorkspacePath)
		} else {
			fmt.Printf("  Path: (not configured — add to workspaces in ~/.spec/config.yaml)\n")
		}
	}

	fmt.Printf("  Branch: %s\n", next.Branch)

	if next.IsNewRepo {
		fmt.Println("\n  ⚠ This step is in a different repo than the previous step.")
	}

	fmt.Println()
	fmt.Printf("To start: spec steps start %s\n", specID)

	return nil
}

func runStepsStart(cmd *cobra.Command, args []string) error {
	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	// Parse args
	var specID string
	var stepNum int

	switch len(args) {
	case 0:
		id, err := resolveSpecIDFromArgs(nil)
		if err != nil {
			return err
		}
		specID = id
		stepNum = 0 // will use next step
	case 1:
		// Could be spec ID or step number
		if isNumber(args[0]) {
			id, err := resolveSpecIDFromArgs(nil)
			if err != nil {
				return err
			}
			specID = id
			stepNum = parseNumber(args[0])
		} else {
			specID = normalizeSpecID(args[0])
			stepNum = 0
		}
	case 2:
		specID = normalizeSpecID(args[0])
		stepNum = parseNumber(args[1])
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

	engine := steps.NewEngine(rc.User)

	// Determine which step to start
	if stepNum == 0 {
		current := engine.CurrentStep(meta)
		if current == nil {
			return fmt.Errorf("all steps are complete")
		}
		stepNum = current.Index
	}

	// Start the step
	branch, err := engine.StartStep(meta, stepNum)
	if err != nil {
		return err
	}

	// Write back
	newContent, err := markdown.UpdateFrontmatter(string(content), meta)
	if err != nil {
		return fmt.Errorf("updating frontmatter: %w", err)
	}

	if err := os.WriteFile(path, []byte(newContent), 0o644); err != nil {
		return fmt.Errorf("writing spec: %w", err)
	}

	step := engine.StepByIndex(meta, stepNum)
	tui.PrintSuccess(fmt.Sprintf("Started step %d: %s", stepNum, step.Description))
	fmt.Printf("\nBranch: %s\n", branch)

	if step.Repo != "" {
		workspace := engine.WorkspacePath(step.Repo)
		if workspace != "" {
			fmt.Printf("\nTo switch to the repo:\n  cd %s && git checkout -b %s\n", workspace, branch)
		} else {
			fmt.Printf("\nCreate branch in %s:\n  git checkout -b %s\n", step.Repo, branch)
		}
	} else {
		fmt.Printf("\nCreate branch:\n  git checkout -b %s\n", branch)
	}

	return nil
}

func runStepsComplete(cmd *cobra.Command, args []string) error {
	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	// Parse args
	var specID string
	var stepNum int

	switch len(args) {
	case 0:
		id, err := resolveSpecIDFromArgs(nil)
		if err != nil {
			return err
		}
		specID = id
		stepNum = 0
	case 1:
		if isNumber(args[0]) {
			id, err := resolveSpecIDFromArgs(nil)
			if err != nil {
				return err
			}
			specID = id
			stepNum = parseNumber(args[0])
		} else {
			specID = normalizeSpecID(args[0])
			stepNum = 0
		}
	case 2:
		specID = normalizeSpecID(args[0])
		stepNum = parseNumber(args[1])
	}

	prNumber, _ := cmd.Flags().GetInt("pr")

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

	engine := steps.NewEngine(rc.User)

	// Find the step to complete
	if stepNum == 0 {
		// Find in-progress step
		plan := planning.FromMeta(meta)
		for _, s := range plan.Steps {
			if s.Status == planning.StatusInProgress {
				stepNum = s.Index
				break
			}
		}
		if stepNum == 0 {
			return fmt.Errorf("no in-progress step found — specify a step number")
		}
	}

	// Complete the step
	if err := engine.CompleteStep(meta, stepNum, prNumber); err != nil {
		return err
	}

	// Write back
	newContent, err := markdown.UpdateFrontmatter(string(content), meta)
	if err != nil {
		return fmt.Errorf("updating frontmatter: %w", err)
	}

	if err := os.WriteFile(path, []byte(newContent), 0o644); err != nil {
		return fmt.Errorf("writing spec: %w", err)
	}

	step := engine.StepByIndex(meta, stepNum)
	tui.PrintSuccess(fmt.Sprintf("Completed step %d: %s", stepNum, step.Description))

	// Show next step
	next := engine.CurrentStep(meta)
	if next != nil {
		fmt.Printf("\nNext step: %d. %s\n", next.Index, next.Description)
	} else if engine.AllComplete(meta) {
		fmt.Println("\n🎉 All steps complete!")
	}

	return nil
}

func runStepsBlock(cmd *cobra.Command, args []string) error {
	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	// Parse args: [id] [step] reason...
	var specID string
	var stepNum int
	var reason string

	// This is complex - we need to figure out what's a spec ID, step number, or reason
	// Simplest: require "spec steps block SPEC-001 1 reason here"
	if len(args) < 1 {
		return fmt.Errorf("usage: spec steps block [id] [step-number] <reason>")
	}

	// Try to determine format
	if len(args) == 1 {
		// Just a reason, need spec ID from context and current step
		id, err := resolveSpecIDFromArgs(nil)
		if err != nil {
			return fmt.Errorf("no spec ID — use 'spec steps block SPEC-001 <reason>'")
		}
		specID = id
		stepNum = 0 // will find current
		reason = args[0]
	} else if len(args) == 2 {
		// Could be: "SPEC-001 reason" or "1 reason"
		if isNumber(args[0]) {
			id, err := resolveSpecIDFromArgs(nil)
			if err != nil {
				return err
			}
			specID = id
			stepNum = parseNumber(args[0])
			reason = args[1]
		} else {
			specID = normalizeSpecID(args[0])
			stepNum = 0
			reason = args[1]
		}
	} else {
		// "SPEC-001 1 reason..." or "SPEC-001 reason reason..."
		specID = normalizeSpecID(args[0])
		if isNumber(args[1]) {
			stepNum = parseNumber(args[1])
			reason = strings.Join(args[2:], " ")
		} else {
			stepNum = 0
			reason = strings.Join(args[1:], " ")
		}
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

	engine := steps.NewEngine(rc.User)

	// Find step to block
	if stepNum == 0 {
		current := engine.CurrentStep(meta)
		if current == nil {
			return fmt.Errorf("no current step to block")
		}
		stepNum = current.Index
	}

	// Block the step
	if err := engine.BlockStep(meta, stepNum, reason); err != nil {
		return err
	}

	// Write back
	newContent, err := markdown.UpdateFrontmatter(string(content), meta)
	if err != nil {
		return fmt.Errorf("updating frontmatter: %w", err)
	}

	if err := os.WriteFile(path, []byte(newContent), 0o644); err != nil {
		return fmt.Errorf("writing spec: %w", err)
	}

	tui.PrintSuccess(fmt.Sprintf("Blocked step %d: %s", stepNum, reason))
	fmt.Println("\nTo unblock: spec steps unblock", specID, stepNum)

	return nil
}

func runStepsUnblock(cmd *cobra.Command, args []string) error {
	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	// Parse args
	var specID string
	var stepNum int

	switch len(args) {
	case 0:
		return fmt.Errorf("usage: spec steps unblock <id> <step-number>")
	case 1:
		if isNumber(args[0]) {
			id, err := resolveSpecIDFromArgs(nil)
			if err != nil {
				return err
			}
			specID = id
			stepNum = parseNumber(args[0])
		} else {
			return fmt.Errorf("usage: spec steps unblock <id> <step-number>")
		}
	case 2:
		specID = normalizeSpecID(args[0])
		stepNum = parseNumber(args[1])
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

	engine := steps.NewEngine(rc.User)

	// Unblock the step
	if err := engine.UnblockStep(meta, stepNum); err != nil {
		return err
	}

	// Write back
	newContent, err := markdown.UpdateFrontmatter(string(content), meta)
	if err != nil {
		return fmt.Errorf("updating frontmatter: %w", err)
	}

	if err := os.WriteFile(path, []byte(newContent), 0o644); err != nil {
		return fmt.Errorf("writing spec: %w", err)
	}

	tui.PrintSuccess(fmt.Sprintf("Unblocked step %d", stepNum))

	return nil
}

// isNumber returns true if s looks like a positive integer
func isNumber(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// parseNumber parses s as an integer, returning 0 on error
func parseNumber(s string) int {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}
