package steps

import (
	"testing"

	"github.com/nexl/spec-cli/internal/config"
	"github.com/nexl/spec-cli/internal/markdown"
	"github.com/nexl/spec-cli/internal/planning"
)

func TestEngine_BranchName(t *testing.T) {
	e := NewEngine(nil)

	tests := []struct {
		specID      string
		stepIndex   int
		description string
		want        string
	}{
		{"SPEC-001", 1, "Add endpoint", "spec-001/step-1-add-endpoint"},
		{"SPEC-042", 3, "Update the UI components", "spec-042/step-3-update-the-ui-components"},
		{"SPEC-001", 1, "This is a very long description that should be truncated", "spec-001/step-1-this-is-a-very-long-descriptio"},
		{"SPEC-001", 1, "Fix bug #123!", "spec-001/step-1-fix-bug-123"},
	}

	for _, tt := range tests {
		got := e.BranchName(tt.specID, tt.stepIndex, tt.description)
		if got != tt.want {
			t.Errorf("BranchName(%q, %d, %q) = %q, want %q",
				tt.specID, tt.stepIndex, tt.description, got, tt.want)
		}
	}
}

func TestEngine_WorkspacePath(t *testing.T) {
	cfg := &config.UserConfig{
		Workspaces: map[string]string{
			"api":     "/home/user/code/api",
			"web":     "~/code/web",
			"unknown": "",
		},
	}
	e := NewEngine(cfg)

	// Absolute path
	if got := e.WorkspacePath("api"); got != "/home/user/code/api" {
		t.Errorf("WorkspacePath(api) = %q", got)
	}

	// Tilde expansion (can't fully test without knowing home dir)
	web := e.WorkspacePath("web")
	if web == "" || web == "~/code/web" {
		t.Errorf("WorkspacePath(web) should expand ~, got %q", web)
	}

	// Not configured
	if got := e.WorkspacePath("not-configured"); got != "" {
		t.Errorf("WorkspacePath(not-configured) = %q, want empty", got)
	}

	// Nil config
	e2 := NewEngine(nil)
	if got := e2.WorkspacePath("api"); got != "" {
		t.Errorf("nil config WorkspacePath = %q, want empty", got)
	}
}

func TestEngine_CurrentStep(t *testing.T) {
	e := NewEngine(nil)

	meta := &markdown.SpecMeta{
		ID: "SPEC-001",
		Steps: []markdown.BuildStep{
			{Description: "Step 1", Status: "complete"},
			{Description: "Step 2", Status: "in_progress"},
			{Description: "Step 3", Status: "pending"},
		},
	}

	current := e.CurrentStep(meta)
	if current == nil {
		t.Fatal("CurrentStep should not be nil")
	}
	if current.Index != 2 {
		t.Errorf("CurrentStep.Index = %d, want 2", current.Index)
	}
	if current.Description != "Step 2" {
		t.Errorf("CurrentStep.Description = %q", current.Description)
	}
}

func TestEngine_StartStep(t *testing.T) {
	e := NewEngine(nil)

	meta := &markdown.SpecMeta{
		ID: "SPEC-001",
		Steps: []markdown.BuildStep{
			{Description: "Step 1", Status: "pending"},
			{Description: "Step 2", Status: "pending"},
		},
	}

	// Start step 1
	branch, err := e.StartStep(meta, 1)
	if err != nil {
		t.Fatalf("StartStep(1): %v", err)
	}
	if branch == "" {
		t.Error("branch should not be empty")
	}
	if meta.Steps[0].Status != planning.StatusInProgress {
		t.Errorf("step 1 status = %q", meta.Steps[0].Status)
	}

	// Cannot start step 2 before step 1 is complete
	_, err = e.StartStep(meta, 2)
	if err == nil {
		t.Error("should error when starting step 2 before step 1 complete")
	}

	// Invalid index
	_, err = e.StartStep(meta, 99)
	if err == nil {
		t.Error("should error on invalid index")
	}
}

func TestEngine_CompleteStep(t *testing.T) {
	e := NewEngine(nil)

	meta := &markdown.SpecMeta{
		ID: "SPEC-001",
		Steps: []markdown.BuildStep{
			{Description: "Step 1", Status: "in_progress"},
		},
	}

	err := e.CompleteStep(meta, 1, 42)
	if err != nil {
		t.Fatalf("CompleteStep: %v", err)
	}

	if meta.Steps[0].Status != planning.StatusComplete {
		t.Errorf("status = %q", meta.Steps[0].Status)
	}
	if meta.Steps[0].PR != 42 {
		t.Errorf("PR = %d", meta.Steps[0].PR)
	}
}

func TestEngine_BlockUnblock(t *testing.T) {
	e := NewEngine(nil)

	meta := &markdown.SpecMeta{
		ID: "SPEC-001",
		Steps: []markdown.BuildStep{
			{Description: "Step 1", Status: "pending"},
		},
	}

	// Block
	err := e.BlockStep(meta, 1, "Waiting on API")
	if err != nil {
		t.Fatalf("BlockStep: %v", err)
	}
	if meta.Steps[0].Status != planning.StatusBlocked {
		t.Errorf("status = %q", meta.Steps[0].Status)
	}

	// Cannot start blocked step
	_, err = e.StartStep(meta, 1)
	if err == nil {
		t.Error("should not be able to start blocked step")
	}

	// Unblock
	err = e.UnblockStep(meta, 1)
	if err != nil {
		t.Fatalf("UnblockStep: %v", err)
	}
	if meta.Steps[0].Status != planning.StatusPending {
		t.Errorf("status after unblock = %q", meta.Steps[0].Status)
	}
}

func TestEngine_Progress(t *testing.T) {
	e := NewEngine(nil)

	meta := &markdown.SpecMeta{
		ID: "SPEC-001",
		Steps: []markdown.BuildStep{
			{Description: "Step 1", Status: "complete"},
			{Description: "Step 2", Status: "complete"},
			{Description: "Step 3", Status: "in_progress"},
			{Description: "Step 4", Status: "pending"},
		},
	}

	completed, total, current := e.Progress(meta)
	if completed != 2 {
		t.Errorf("completed = %d, want 2", completed)
	}
	if total != 4 {
		t.Errorf("total = %d, want 4", total)
	}
	if current == nil || current.Index != 3 {
		t.Errorf("current = %v", current)
	}
}

func TestEngine_GetNextStep(t *testing.T) {
	cfg := &config.UserConfig{
		Workspaces: map[string]string{
			"api": "/code/api",
			"web": "/code/web",
		},
	}
	e := NewEngine(cfg)

	meta := &markdown.SpecMeta{
		ID: "SPEC-001",
		Steps: []markdown.BuildStep{
			{Repo: "api", Description: "Add endpoint", Status: "complete"},
			{Repo: "web", Description: "Add UI", Status: "pending"},
		},
	}

	next, err := e.GetNextStep(meta)
	if err != nil {
		t.Fatalf("GetNextStep: %v", err)
	}
	if next == nil {
		t.Fatal("next should not be nil")
	}

	if next.Index != 2 {
		t.Errorf("Index = %d", next.Index)
	}
	if next.Repo != "web" {
		t.Errorf("Repo = %q", next.Repo)
	}
	if next.WorkspacePath != "/code/web" {
		t.Errorf("WorkspacePath = %q", next.WorkspacePath)
	}
	if !next.IsNewRepo {
		t.Error("IsNewRepo should be true")
	}
}

func TestEngine_AllComplete(t *testing.T) {
	e := NewEngine(nil)

	tests := []struct {
		name   string
		meta   *markdown.SpecMeta
		want   bool
	}{
		{
			"all complete",
			&markdown.SpecMeta{
				Steps: []markdown.BuildStep{
					{Status: "complete"},
					{Status: "complete"},
				},
			},
			true,
		},
		{
			"not all complete",
			&markdown.SpecMeta{
				Steps: []markdown.BuildStep{
					{Status: "complete"},
					{Status: "pending"},
				},
			},
			false,
		},
		{
			"no steps",
			&markdown.SpecMeta{},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := e.AllComplete(tt.meta); got != tt.want {
				t.Errorf("AllComplete() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Hello World", "hello-world"},
		{"add_endpoint", "add-endpoint"},
		{"Fix Bug #123!", "fix-bug-123"},
		{"Multiple   Spaces", "multiple-spaces"},
		{"--leading-trailing--", "leading-trailing"},
		{"UPPERCASE", "uppercase"},
	}

	for _, tt := range tests {
		if got := slugify(tt.input); got != tt.want {
			t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
