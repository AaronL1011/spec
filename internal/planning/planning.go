// Package planning handles technical plan creation and review workflows.
package planning

import (
	"fmt"
	"strings"
	"time"

	"github.com/aaronl1011/spec/internal/markdown"
)

// Plan represents a technical build plan for a spec.
type Plan struct {
	SpecID string
	Steps  []Step
	Review *ReviewState
}

// Step represents a single step in a build plan.
type Step struct {
	Index         int
	Repo          string
	Description   string
	Branch        string
	PR            int // PR number, 0 if not yet created
	Status        string
	BlockedReason string
}

// StepStatus constants.
const (
	StatusPending    = "pending"
	StatusInProgress = "in_progress"
	StatusComplete   = "complete"
	StatusBlocked    = "blocked"
)

// ReviewState tracks plan review status.
type ReviewState struct {
	RequestedAt time.Time
	Reviewers   []string
	Approvals   []Approval
	Status      string
	Feedback    string
}

// ReviewStatus constants.
const (
	ReviewPending          = "pending"
	ReviewApproved         = "approved"
	ReviewChangesRequested = "changes_requested"
)

// Approval records a single reviewer's approval.
type Approval struct {
	Reviewer   string
	ApprovedAt time.Time
}

// FromMeta extracts a Plan from a spec's metadata.
func FromMeta(meta *markdown.SpecMeta) *Plan {
	if meta == nil {
		return nil
	}

	plan := &Plan{
		SpecID: meta.ID,
		Steps:  make([]Step, len(meta.Steps)),
	}

	for i, s := range meta.Steps {
		plan.Steps[i] = Step{
			Index:         i + 1,
			Repo:          s.Repo,
			Description:   s.Description,
			Branch:        s.Branch,
			PR:            s.PR,
			Status:        s.Status,
			BlockedReason: s.BlockedReason,
		}
		// Default status to pending if empty
		if plan.Steps[i].Status == "" {
			plan.Steps[i].Status = StatusPending
		}
	}

	// Convert review state
	if meta.Review != nil {
		plan.Review = &ReviewState{
			Reviewers: meta.Review.Reviewers,
			Status:    meta.Review.Status,
			Feedback:  meta.Review.Feedback,
		}
		// Parse time string
		if meta.Review.RequestedAt != "" {
			if t, err := time.Parse(time.RFC3339, meta.Review.RequestedAt); err == nil {
				plan.Review.RequestedAt = t
			}
		}
		for _, a := range meta.Review.Approvals {
			approval := Approval{Reviewer: a.Reviewer}
			if a.ApprovedAt != "" {
				if t, err := time.Parse(time.RFC3339, a.ApprovedAt); err == nil {
					approval.ApprovedAt = t
				}
			}
			plan.Review.Approvals = append(plan.Review.Approvals, approval)
		}
	}

	return plan
}

// ToFrontmatter converts a Plan back to frontmatter types.
func (p *Plan) ToFrontmatter() ([]markdown.BuildStep, *markdown.ReviewState) {
	steps := make([]markdown.BuildStep, len(p.Steps))
	for i, s := range p.Steps {
		steps[i] = markdown.BuildStep{
			Repo:          s.Repo,
			Description:   s.Description,
			Branch:        s.Branch,
			PR:            s.PR,
			Status:        s.Status,
			BlockedReason: s.BlockedReason,
		}
	}

	var review *markdown.ReviewState
	if p.Review != nil {
		review = &markdown.ReviewState{
			Reviewers: p.Review.Reviewers,
			Status:    p.Review.Status,
			Feedback:  p.Review.Feedback,
		}
		if !p.Review.RequestedAt.IsZero() {
			review.RequestedAt = p.Review.RequestedAt.Format(time.RFC3339)
		}
		for _, a := range p.Review.Approvals {
			approval := markdown.ReviewApproval{Reviewer: a.Reviewer}
			if !a.ApprovedAt.IsZero() {
				approval.ApprovedAt = a.ApprovedAt.Format(time.RFC3339)
			}
			review.Approvals = append(review.Approvals, approval)
		}
	}

	return steps, review
}

// HasSteps returns true if the plan has any steps defined.
func (p *Plan) HasSteps() bool {
	return p != nil && len(p.Steps) > 0
}

// CurrentStep returns the first non-complete step, or nil if all done.
func (p *Plan) CurrentStep() *Step {
	if p == nil {
		return nil
	}
	for i := range p.Steps {
		if p.Steps[i].Status != StatusComplete {
			return &p.Steps[i]
		}
	}
	return nil
}

// AllComplete returns true if all steps are complete.
func (p *Plan) AllComplete() bool {
	if p == nil || len(p.Steps) == 0 {
		return false
	}
	for _, s := range p.Steps {
		if s.Status != StatusComplete {
			return false
		}
	}
	return true
}

// Progress returns (completed, total) step counts.
func (p *Plan) Progress() (int, int) {
	if p == nil {
		return 0, 0
	}
	completed := 0
	for _, s := range p.Steps {
		if s.Status == StatusComplete {
			completed++
		}
	}
	return completed, len(p.Steps)
}

// ProgressString returns a progress string like "3/7 steps".
func (p *Plan) ProgressString() string {
	completed, total := p.Progress()
	return fmt.Sprintf("%d/%d steps", completed, total)
}

// IsReviewPending returns true if review is requested but not yet approved.
func (p *Plan) IsReviewPending() bool {
	return p != nil && p.Review != nil && p.Review.Status == ReviewPending
}

// IsReviewApproved returns true if plan review is approved.
func (p *Plan) IsReviewApproved() bool {
	return p != nil && p.Review != nil && p.Review.Status == ReviewApproved
}

// IsReviewChangesRequested returns true if reviewer requested changes.
func (p *Plan) IsReviewChangesRequested() bool {
	return p != nil && p.Review != nil && p.Review.Status == ReviewChangesRequested
}

// NeedsReview returns true if plan exists but has no review state.
func (p *Plan) NeedsReview() bool {
	return p.HasSteps() && p.Review == nil
}

// RequestReview initializes the review state.
func (p *Plan) RequestReview(reviewers []string) error {
	if !p.HasSteps() {
		return fmt.Errorf("cannot request review: no steps defined")
	}
	if p.Review != nil && p.Review.Status == ReviewPending {
		return fmt.Errorf("review already pending")
	}

	p.Review = &ReviewState{
		RequestedAt: time.Now(),
		Reviewers:   reviewers,
		Status:      ReviewPending,
	}
	return nil
}

// Approve records an approval from a reviewer.
func (p *Plan) Approve(reviewer string, minApprovals int) error {
	if p.Review == nil {
		return fmt.Errorf("no review in progress")
	}

	// Check if already approved by this reviewer
	for _, a := range p.Review.Approvals {
		if a.Reviewer == reviewer {
			return fmt.Errorf("already approved by %s", reviewer)
		}
	}

	p.Review.Approvals = append(p.Review.Approvals, Approval{
		Reviewer:   reviewer,
		ApprovedAt: time.Now(),
	})

	// Check if we have enough approvals
	if len(p.Review.Approvals) >= minApprovals {
		p.Review.Status = ReviewApproved
	}

	return nil
}

// RequestChanges marks the review as needing changes.
func (p *Plan) RequestChanges(reviewer string, feedback string) error {
	if p.Review == nil {
		return fmt.Errorf("no review in progress")
	}

	p.Review.Status = ReviewChangesRequested
	if feedback != "" {
		if p.Review.Feedback != "" {
			p.Review.Feedback += "\n"
		}
		p.Review.Feedback += fmt.Sprintf("%s: %s", reviewer, feedback)
	}

	return nil
}

// AddStep appends a new step to the plan.
func (p *Plan) AddStep(repo, description string) {
	p.Steps = append(p.Steps, Step{
		Index:       len(p.Steps) + 1,
		Repo:        repo,
		Description: description,
		Status:      StatusPending,
	})
}

// StartStep marks a step as in-progress and sets its branch.
func (p *Plan) StartStep(index int, branch string) error {
	if index < 1 || index > len(p.Steps) {
		return fmt.Errorf("invalid step index: %d", index)
	}

	step := &p.Steps[index-1]
	if step.Status == StatusComplete {
		return fmt.Errorf("step %d already complete", index)
	}

	step.Status = StatusInProgress
	step.Branch = branch
	return nil
}

// CompleteStep marks a step as complete and sets its PR number.
func (p *Plan) CompleteStep(index int, prNumber int) error {
	if index < 1 || index > len(p.Steps) {
		return fmt.Errorf("invalid step index: %d", index)
	}

	step := &p.Steps[index-1]
	step.Status = StatusComplete
	if prNumber > 0 {
		step.PR = prNumber
	}
	return nil
}

// BlockStep marks a step as blocked with a reason.
func (p *Plan) BlockStep(index int, reason string) error {
	if index < 1 || index > len(p.Steps) {
		return fmt.Errorf("invalid step index: %d", index)
	}

	step := &p.Steps[index-1]
	step.Status = StatusBlocked
	step.BlockedReason = reason
	return nil
}

// UnblockStep removes the blocked status from a step.
func (p *Plan) UnblockStep(index int) error {
	if index < 1 || index > len(p.Steps) {
		return fmt.Errorf("invalid step index: %d", index)
	}

	step := &p.Steps[index-1]
	if step.Status != StatusBlocked {
		return fmt.Errorf("step %d is not blocked", index)
	}

	step.Status = StatusPending
	step.BlockedReason = ""
	return nil
}

// Validate checks the plan for common issues.
func (p *Plan) Validate() []string {
	var issues []string

	if !p.HasSteps() {
		issues = append(issues, "no steps defined")
		return issues
	}

	for i, step := range p.Steps {
		if step.Description == "" {
			issues = append(issues, fmt.Sprintf("step %d: missing description", i+1))
		}
		if step.Status == StatusBlocked && step.BlockedReason == "" {
			issues = append(issues, fmt.Sprintf("step %d: blocked but no reason given", i+1))
		}
	}

	return issues
}

// Summary returns a brief text summary of the plan.
func (p *Plan) Summary() string {
	if !p.HasSteps() {
		return "No build plan"
	}

	var parts []string
	completed, total := p.Progress()
	parts = append(parts, fmt.Sprintf("%d/%d steps", completed, total))

	if p.Review != nil {
		switch p.Review.Status {
		case ReviewPending:
			parts = append(parts, "review pending")
		case ReviewApproved:
			parts = append(parts, "approved")
		case ReviewChangesRequested:
			parts = append(parts, "changes requested")
		}
	}

	return strings.Join(parts, ", ")
}
