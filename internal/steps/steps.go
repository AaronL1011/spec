// Package steps handles build step execution and transitions.
package steps

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nexl/spec-cli/internal/config"
	"github.com/nexl/spec-cli/internal/markdown"
	"github.com/nexl/spec-cli/internal/planning"
)

// Engine orchestrates build step execution.
type Engine struct {
	userConfig *config.UserConfig
}

// NewEngine creates a new steps engine.
func NewEngine(userConfig *config.UserConfig) *Engine {
	return &Engine{
		userConfig: userConfig,
	}
}

// CurrentStep returns the current (first non-complete) step for a spec.
func (e *Engine) CurrentStep(meta *markdown.SpecMeta) *planning.Step {
	plan := planning.FromMeta(meta)
	if plan == nil {
		return nil
	}
	return plan.CurrentStep()
}

// StepByIndex returns a specific step by 1-based index.
func (e *Engine) StepByIndex(meta *markdown.SpecMeta, index int) *planning.Step {
	plan := planning.FromMeta(meta)
	if plan == nil || index < 1 || index > len(plan.Steps) {
		return nil
	}
	return &plan.Steps[index-1]
}

// BranchName generates a branch name for a step.
func (e *Engine) BranchName(specID string, stepIndex int, description string) string {
	// Sanitize description for branch name
	slug := slugify(description)
	if len(slug) > 30 {
		slug = slug[:30]
	}
	return fmt.Sprintf("%s/step-%d-%s", strings.ToLower(specID), stepIndex, slug)
}

// WorkspacePath returns the local path for a repo, or empty if not configured.
func (e *Engine) WorkspacePath(repoName string) string {
	if e.userConfig == nil {
		return ""
	}
	path := e.userConfig.GetWorkspacePath(repoName)
	if path == "" {
		return ""
	}
	// Expand ~ to home directory
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	return path
}

// StartStep marks a step as in-progress and returns the branch name.
func (e *Engine) StartStep(meta *markdown.SpecMeta, index int) (branchName string, err error) {
	plan := planning.FromMeta(meta)
	if plan == nil {
		return "", fmt.Errorf("no build plan defined")
	}

	if index < 1 || index > len(plan.Steps) {
		return "", fmt.Errorf("invalid step index: %d (have %d steps)", index, len(plan.Steps))
	}

	step := &plan.Steps[index-1]

	// Check if step can be started
	if step.Status == planning.StatusComplete {
		return "", fmt.Errorf("step %d is already complete", index)
	}
	if step.Status == planning.StatusBlocked {
		return "", fmt.Errorf("step %d is blocked: %s", index, step.BlockedReason)
	}

	// Check if previous steps are complete (unless this is step 1)
	for i := 0; i < index-1; i++ {
		if plan.Steps[i].Status != planning.StatusComplete {
			return "", fmt.Errorf("cannot start step %d: step %d is not complete", index, i+1)
		}
	}

	// Generate branch name if not already set
	branchName = step.Branch
	if branchName == "" {
		branchName = e.BranchName(plan.SpecID, index, step.Description)
	}

	// Update step
	if err := plan.StartStep(index, branchName); err != nil {
		return "", err
	}

	// Write back to meta
	steps, review := plan.ToFrontmatter()
	meta.Steps = steps
	meta.Review = review

	return branchName, nil
}

// CompleteStep marks a step as complete.
func (e *Engine) CompleteStep(meta *markdown.SpecMeta, index int, prNumber int) error {
	plan := planning.FromMeta(meta)
	if plan == nil {
		return fmt.Errorf("no build plan defined")
	}

	if err := plan.CompleteStep(index, prNumber); err != nil {
		return err
	}

	// Write back to meta
	steps, review := plan.ToFrontmatter()
	meta.Steps = steps
	meta.Review = review

	return nil
}

// BlockStep marks a step as blocked with a reason.
func (e *Engine) BlockStep(meta *markdown.SpecMeta, index int, reason string) error {
	plan := planning.FromMeta(meta)
	if plan == nil {
		return fmt.Errorf("no build plan defined")
	}

	if err := plan.BlockStep(index, reason); err != nil {
		return err
	}

	// Write back to meta
	steps, review := plan.ToFrontmatter()
	meta.Steps = steps
	meta.Review = review

	return nil
}

// UnblockStep removes the blocked status from a step.
func (e *Engine) UnblockStep(meta *markdown.SpecMeta, index int) error {
	plan := planning.FromMeta(meta)
	if plan == nil {
		return fmt.Errorf("no build plan defined")
	}

	if err := plan.UnblockStep(index); err != nil {
		return err
	}

	// Write back to meta
	steps, review := plan.ToFrontmatter()
	meta.Steps = steps
	meta.Review = review

	return nil
}

// Progress returns build progress info.
func (e *Engine) Progress(meta *markdown.SpecMeta) (completed, total int, currentStep *planning.Step) {
	plan := planning.FromMeta(meta)
	if plan == nil {
		return 0, 0, nil
	}
	completed, total = plan.Progress()
	currentStep = plan.CurrentStep()
	return
}

// AllComplete returns true if all steps are complete.
func (e *Engine) AllComplete(meta *markdown.SpecMeta) bool {
	plan := planning.FromMeta(meta)
	if plan == nil {
		return false
	}
	return plan.AllComplete()
}

// NextStep returns info about the next step to work on.
type NextStep struct {
	Index         int
	Description   string
	Repo          string
	Branch        string
	WorkspacePath string
	IsNewRepo     bool // true if different repo from previous step
}

// GetNextStep determines what to work on next.
func (e *Engine) GetNextStep(meta *markdown.SpecMeta) (*NextStep, error) {
	plan := planning.FromMeta(meta)
	if plan == nil || !plan.HasSteps() {
		return nil, fmt.Errorf("no build plan defined")
	}

	current := plan.CurrentStep()
	if current == nil {
		return nil, nil // all done
	}

	next := &NextStep{
		Index:       current.Index,
		Description: current.Description,
		Repo:        current.Repo,
		Branch:      current.Branch,
	}

	// Generate branch name if not set
	if next.Branch == "" {
		next.Branch = e.BranchName(plan.SpecID, current.Index, current.Description)
	}

	// Resolve workspace path
	if current.Repo != "" {
		next.WorkspacePath = e.WorkspacePath(current.Repo)

		// Check if this is a new repo compared to the previous step
		if current.Index > 1 {
			prevStep := &plan.Steps[current.Index-2]
			next.IsNewRepo = prevStep.Repo != current.Repo
		}
	}

	return next, nil
}

// slugify converts a string to a URL-safe slug.
func slugify(s string) string {
	// Convert to lowercase
	s = strings.ToLower(s)

	// Replace spaces and underscores with hyphens
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")

	// Remove non-alphanumeric characters except hyphens
	reg := regexp.MustCompile(`[^a-z0-9-]`)
	s = reg.ReplaceAllString(s, "")

	// Collapse multiple hyphens
	reg = regexp.MustCompile(`-+`)
	s = reg.ReplaceAllString(s, "-")

	// Trim leading/trailing hyphens
	s = strings.Trim(s, "-")

	return s
}
