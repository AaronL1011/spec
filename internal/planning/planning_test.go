package planning

import (
	"testing"
	"time"

	"github.com/aaronl1011/spec/internal/markdown"
)

func TestFromMeta(t *testing.T) {
	meta := &markdown.SpecMeta{
		ID: "SPEC-001",
		Steps: []markdown.BuildStep{
			{Repo: "api", Description: "Add endpoint", Status: "complete"},
			{Repo: "api", Description: "Add tests", Status: "in_progress", Branch: "feat/tests"},
			{Repo: "web", Description: "Add UI"},
		},
		Review: &markdown.ReviewState{
			RequestedAt: time.Now().Format(time.RFC3339),
			Reviewers:   []string{"tl"},
			Status:      "pending",
		},
	}

	plan := FromMeta(meta)
	if plan == nil {
		t.Fatal("FromMeta returned nil")
	}

	if plan.SpecID != "SPEC-001" {
		t.Errorf("SpecID = %q", plan.SpecID)
	}

	if len(plan.Steps) != 3 {
		t.Fatalf("Steps count = %d, want 3", len(plan.Steps))
	}

	// Check step 1
	if plan.Steps[0].Status != StatusComplete {
		t.Errorf("step[0].Status = %q", plan.Steps[0].Status)
	}

	// Check step 2
	if plan.Steps[1].Status != StatusInProgress {
		t.Errorf("step[1].Status = %q", plan.Steps[1].Status)
	}
	if plan.Steps[1].Branch != "feat/tests" {
		t.Errorf("step[1].Branch = %q", plan.Steps[1].Branch)
	}

	// Check step 3 (should default to pending)
	if plan.Steps[2].Status != StatusPending {
		t.Errorf("step[2].Status = %q, want pending", plan.Steps[2].Status)
	}

	// Check review
	if plan.Review == nil {
		t.Fatal("Review should not be nil")
	}
	if plan.Review.Status != ReviewPending {
		t.Errorf("Review.Status = %q", plan.Review.Status)
	}
}

func TestFromMeta_NilMeta(t *testing.T) {
	if FromMeta(nil) != nil {
		t.Error("FromMeta(nil) should return nil")
	}
}

func TestPlan_HasSteps(t *testing.T) {
	var nilPlan *Plan
	if nilPlan.HasSteps() {
		t.Error("nil plan should not have steps")
	}

	emptyPlan := &Plan{}
	if emptyPlan.HasSteps() {
		t.Error("empty plan should not have steps")
	}

	plan := &Plan{Steps: []Step{{Description: "test"}}}
	if !plan.HasSteps() {
		t.Error("plan with steps should have steps")
	}
}

func TestPlan_CurrentStep(t *testing.T) {
	plan := &Plan{
		Steps: []Step{
			{Index: 1, Status: StatusComplete},
			{Index: 2, Status: StatusInProgress},
			{Index: 3, Status: StatusPending},
		},
	}

	current := plan.CurrentStep()
	if current == nil {
		t.Fatal("CurrentStep should not be nil")
	}
	if current.Index != 2 {
		t.Errorf("CurrentStep.Index = %d, want 2", current.Index)
	}
}

func TestPlan_CurrentStep_AllComplete(t *testing.T) {
	plan := &Plan{
		Steps: []Step{
			{Index: 1, Status: StatusComplete},
			{Index: 2, Status: StatusComplete},
		},
	}

	if plan.CurrentStep() != nil {
		t.Error("CurrentStep should be nil when all complete")
	}
}

func TestPlan_AllComplete(t *testing.T) {
	tests := []struct {
		name string
		plan *Plan
		want bool
	}{
		{"nil plan", nil, false},
		{"empty steps", &Plan{}, false},
		{"all complete", &Plan{Steps: []Step{
			{Status: StatusComplete},
			{Status: StatusComplete},
		}}, true},
		{"not all complete", &Plan{Steps: []Step{
			{Status: StatusComplete},
			{Status: StatusPending},
		}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.plan.AllComplete(); got != tt.want {
				t.Errorf("AllComplete() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlan_Progress(t *testing.T) {
	plan := &Plan{
		Steps: []Step{
			{Status: StatusComplete},
			{Status: StatusComplete},
			{Status: StatusInProgress},
			{Status: StatusPending},
			{Status: StatusBlocked},
		},
	}

	completed, total := plan.Progress()
	if completed != 2 {
		t.Errorf("completed = %d, want 2", completed)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}

	if plan.ProgressString() != "2/5 steps" {
		t.Errorf("ProgressString = %q", plan.ProgressString())
	}
}

func TestPlan_RequestReview(t *testing.T) {
	plan := &Plan{
		Steps: []Step{{Description: "test"}},
	}

	err := plan.RequestReview([]string{"tl", "@mike"})
	if err != nil {
		t.Fatalf("RequestReview: %v", err)
	}

	if plan.Review == nil {
		t.Fatal("Review should not be nil")
	}
	if plan.Review.Status != ReviewPending {
		t.Errorf("Review.Status = %q", plan.Review.Status)
	}
	if len(plan.Review.Reviewers) != 2 {
		t.Errorf("Reviewers count = %d", len(plan.Review.Reviewers))
	}
	if !plan.IsReviewPending() {
		t.Error("IsReviewPending should be true")
	}

	// Try to request again
	err = plan.RequestReview([]string{"tl"})
	if err == nil {
		t.Error("should error on duplicate request")
	}
}

func TestPlan_RequestReview_NoSteps(t *testing.T) {
	plan := &Plan{}
	err := plan.RequestReview([]string{"tl"})
	if err == nil {
		t.Error("should error when no steps")
	}
}

func TestPlan_Approve(t *testing.T) {
	plan := &Plan{
		Steps:  []Step{{Description: "test"}},
		Review: &ReviewState{Status: ReviewPending, Reviewers: []string{"tl"}},
	}

	// First approval (min 2)
	err := plan.Approve("tl", 2)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if plan.Review.Status != ReviewPending {
		t.Error("should still be pending after 1 approval")
	}

	// Second approval
	err = plan.Approve("@mike", 2)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if !plan.IsReviewApproved() {
		t.Error("should be approved after 2 approvals")
	}

	// Duplicate approval
	err = plan.Approve("tl", 2)
	if err == nil {
		t.Error("should error on duplicate approval")
	}
}

func TestPlan_RequestChanges(t *testing.T) {
	plan := &Plan{
		Steps:  []Step{{Description: "test"}},
		Review: &ReviewState{Status: ReviewPending, Reviewers: []string{"tl"}},
	}

	err := plan.RequestChanges("tl", "Need more detail on step 2")
	if err != nil {
		t.Fatalf("RequestChanges: %v", err)
	}

	if !plan.IsReviewChangesRequested() {
		t.Error("should be changes_requested")
	}
	if plan.Review.Feedback == "" {
		t.Error("Feedback should not be empty")
	}
}

func TestPlan_AddStep(t *testing.T) {
	plan := &Plan{SpecID: "SPEC-001"}
	plan.AddStep("api", "Add endpoint")
	plan.AddStep("web", "Add UI")

	if len(plan.Steps) != 2 {
		t.Fatalf("Steps count = %d", len(plan.Steps))
	}
	if plan.Steps[0].Index != 1 {
		t.Errorf("step[0].Index = %d", plan.Steps[0].Index)
	}
	if plan.Steps[1].Index != 2 {
		t.Errorf("step[1].Index = %d", plan.Steps[1].Index)
	}
	if plan.Steps[0].Status != StatusPending {
		t.Errorf("step[0].Status = %q", plan.Steps[0].Status)
	}
}

func TestPlan_StartStep(t *testing.T) {
	plan := &Plan{
		Steps: []Step{
			{Index: 1, Status: StatusPending},
		},
	}

	err := plan.StartStep(1, "feat/step-1")
	if err != nil {
		t.Fatalf("StartStep: %v", err)
	}

	if plan.Steps[0].Status != StatusInProgress {
		t.Errorf("Status = %q", plan.Steps[0].Status)
	}
	if plan.Steps[0].Branch != "feat/step-1" {
		t.Errorf("Branch = %q", plan.Steps[0].Branch)
	}

	// Invalid index
	if err := plan.StartStep(0, "branch"); err == nil {
		t.Error("should error on index 0")
	}
	if err := plan.StartStep(99, "branch"); err == nil {
		t.Error("should error on invalid index")
	}
}

func TestPlan_CompleteStep(t *testing.T) {
	plan := &Plan{
		Steps: []Step{
			{Index: 1, Status: StatusInProgress},
		},
	}

	err := plan.CompleteStep(1, 123)
	if err != nil {
		t.Fatalf("CompleteStep: %v", err)
	}

	if plan.Steps[0].Status != StatusComplete {
		t.Errorf("Status = %q", plan.Steps[0].Status)
	}
	if plan.Steps[0].PR != 123 {
		t.Errorf("PR = %d", plan.Steps[0].PR)
	}
}

func TestPlan_BlockUnblock(t *testing.T) {
	plan := &Plan{
		Steps: []Step{
			{Index: 1, Status: StatusPending},
		},
	}

	err := plan.BlockStep(1, "Waiting on API changes")
	if err != nil {
		t.Fatalf("BlockStep: %v", err)
	}
	if plan.Steps[0].Status != StatusBlocked {
		t.Errorf("Status = %q", plan.Steps[0].Status)
	}
	if plan.Steps[0].BlockedReason != "Waiting on API changes" {
		t.Errorf("BlockedReason = %q", plan.Steps[0].BlockedReason)
	}

	err = plan.UnblockStep(1)
	if err != nil {
		t.Fatalf("UnblockStep: %v", err)
	}
	if plan.Steps[0].Status != StatusPending {
		t.Errorf("Status after unblock = %q", plan.Steps[0].Status)
	}
	if plan.Steps[0].BlockedReason != "" {
		t.Errorf("BlockedReason after unblock = %q", plan.Steps[0].BlockedReason)
	}
}

func TestPlan_Validate(t *testing.T) {
	tests := []struct {
		name       string
		plan       *Plan
		wantIssues int
	}{
		{
			"empty plan",
			&Plan{},
			1, // no steps defined
		},
		{
			"valid plan",
			&Plan{Steps: []Step{
				{Description: "Do something", Status: StatusPending},
			}},
			0,
		},
		{
			"missing description",
			&Plan{Steps: []Step{
				{Status: StatusPending},
			}},
			1,
		},
		{
			"blocked without reason",
			&Plan{Steps: []Step{
				{Description: "test", Status: StatusBlocked},
			}},
			1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issues := tt.plan.Validate()
			if len(issues) != tt.wantIssues {
				t.Errorf("Validate() = %v, want %d issues", issues, tt.wantIssues)
			}
		})
	}
}

func TestPlan_ToFrontmatter(t *testing.T) {
	plan := &Plan{
		SpecID: "SPEC-001",
		Steps: []Step{
			{Index: 1, Repo: "api", Description: "Add endpoint", Status: StatusComplete, PR: 42},
			{Index: 2, Repo: "web", Description: "Add UI", Status: StatusPending},
		},
		Review: &ReviewState{
			RequestedAt: time.Now(),
			Reviewers:   []string{"tl"},
			Status:      ReviewApproved,
			Approvals: []Approval{
				{Reviewer: "tl", ApprovedAt: time.Now()},
			},
		},
	}

	steps, review := plan.ToFrontmatter()

	if len(steps) != 2 {
		t.Fatalf("steps count = %d", len(steps))
	}
	if steps[0].PR != 42 {
		t.Errorf("steps[0].PR = %d", steps[0].PR)
	}

	if review == nil {
		t.Fatal("review should not be nil")
	}
	if review.Status != ReviewApproved {
		t.Errorf("review.Status = %q", review.Status)
	}
	if len(review.Approvals) != 1 {
		t.Errorf("approvals count = %d", len(review.Approvals))
	}
}

func TestPlan_Summary(t *testing.T) {
	tests := []struct {
		name string
		plan *Plan
		want string
	}{
		{
			"no steps",
			&Plan{},
			"No build plan",
		},
		{
			"with progress",
			&Plan{Steps: []Step{
				{Status: StatusComplete},
				{Status: StatusPending},
			}},
			"1/2 steps",
		},
		{
			"with pending review",
			&Plan{
				Steps:  []Step{{Status: StatusPending}},
				Review: &ReviewState{Status: ReviewPending},
			},
			"0/1 steps, review pending",
		},
		{
			"approved",
			&Plan{
				Steps:  []Step{{Status: StatusComplete}},
				Review: &ReviewState{Status: ReviewApproved},
			},
			"1/1 steps, approved",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.plan.Summary(); got != tt.want {
				t.Errorf("Summary() = %q, want %q", got, tt.want)
			}
		})
	}
}
