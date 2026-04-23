package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aaronl1011/spec-cli/internal/config"
	"github.com/aaronl1011/spec-cli/internal/markdown"
)

func defaultPipeline() config.PipelineConfig {
	return config.DefaultPipeline()
}

func TestStageOwner(t *testing.T) {
	p := defaultPipeline()
	tests := []struct {
		stage string
		want  string
	}{
		{"triage", "pm"},
		{"build", "engineer"},
		{"qa-validation", "qa"},
		{"done", "tl"},
		{"nonexistent", ""},
	}
	for _, tt := range tests {
		got := StageOwner(p, tt.stage)
		if got != tt.want {
			t.Errorf("StageOwner(%q) = %q, want %q", tt.stage, got, tt.want)
		}
	}
}

func TestValidateAdvance_OwnerCheck(t *testing.T) {
	p := defaultPipeline()

	// Engineer can advance from build
	if err := ValidateAdvance(p, "build", "", "engineer"); err != nil {
		t.Errorf("engineer should be able to advance from build: %v", err)
	}

	// PM cannot advance from build
	if err := ValidateAdvance(p, "build", "", "pm"); err == nil {
		t.Error("PM should not be able to advance from build")
	}

	// TL can advance from any stage
	if err := ValidateAdvance(p, "build", "", "tl"); err != nil {
		t.Errorf("TL should be able to advance from any stage: %v", err)
	}
}

func TestValidateAdvance_Blocked(t *testing.T) {
	p := defaultPipeline()
	if err := ValidateAdvance(p, "blocked", "", "engineer"); err == nil {
		t.Error("should not be able to advance from blocked")
	}
}

func TestValidateAdvance_FastTrack(t *testing.T) {
	p := defaultPipeline()

	// TL can fast-track
	if err := ValidateAdvance(p, "triage", "engineering", "tl"); err != nil {
		t.Errorf("TL should be able to fast-track: %v", err)
	}

	// Non-TL cannot fast-track
	if err := ValidateAdvance(p, "triage", "engineering", "pm"); err == nil {
		t.Error("non-TL should not be able to fast-track")
	}

	// Cannot fast-track backwards
	if err := ValidateAdvance(p, "build", "draft", "tl"); err == nil {
		t.Error("cannot fast-track backwards")
	}
}

func TestValidateRevert(t *testing.T) {
	p := defaultPipeline()

	// Engineer can revert from build to draft
	if err := ValidateRevert(p, "build", "draft", "engineer"); err != nil {
		t.Errorf("engineer should revert from build: %v", err)
	}

	// Cannot revert forward
	if err := ValidateRevert(p, "draft", "build", "pm"); err == nil {
		t.Error("cannot revert forward")
	}

	// Non-owner cannot revert
	if err := ValidateRevert(p, "build", "draft", "pm"); err == nil {
		t.Error("non-owner should not revert")
	}
}

func TestSkippedStages(t *testing.T) {
	p := defaultPipeline()
	skipped := SkippedStages(p, "triage", "engineering")
	expected := []string{"draft", "tl-review", "design", "qa-expectations"}
	if len(skipped) != len(expected) {
		t.Fatalf("skipped = %v, want %v", skipped, expected)
	}
	for i, s := range skipped {
		if s != expected[i] {
			t.Errorf("skipped[%d] = %q, want %q", i, s, expected[i])
		}
	}
}

func TestEvaluateGates(t *testing.T) {
	p := defaultPipeline()

	// Create sections with content
	sections := []markdown.Section{
		{Slug: "problem_statement", Content: "This is the problem."},
		{Slug: "user_stories", Content: "As a user..."},
		{Slug: "acceptance_criteria", Content: ""},
	}

	// tl-review gate: section_complete: problem_statement → should pass
	results := EvaluateGates(p, "tl-review", sections, false, false)
	if !AllGatesPassed(results) {
		t.Errorf("tl-review gates should pass, failed: %v", FailedGates(results))
	}

	// engineering gate: section_complete: acceptance_criteria → should fail
	results = EvaluateGates(p, "engineering", sections, false, false)
	if AllGatesPassed(results) {
		t.Error("engineering gates should fail (empty acceptance_criteria)")
	}

	// pr-review gate: pr_stack_exists → should fail
	results = EvaluateGates(p, "pr-review", sections, false, false)
	if AllGatesPassed(results) {
		t.Error("pr-review gates should fail (no PR stack)")
	}

	// pr-review with PR stack → should pass
	results = EvaluateGates(p, "pr-review", sections, true, false)
	if !AllGatesPassed(results) {
		t.Errorf("pr-review gates should pass with PR stack, failed: %v", FailedGates(results))
	}
}

func TestAdvance(t *testing.T) {
	dir := t.TempDir()
	content := markdown.ScaffoldSpec("SPEC-001", "Test Spec", "Aaron", "Cycle 1", "direct")
	path := filepath.Join(dir, "SPEC-001.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	meta, err := markdown.ReadMeta(path)
	if err != nil {
		t.Fatal(err)
	}

	result, err := Advance(path, meta, "tl-review")
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if result.PreviousStage != "draft" {
		t.Errorf("previous = %q, want draft", result.PreviousStage)
	}
	if result.NewStage != "tl-review" {
		t.Errorf("new = %q, want tl-review", result.NewStage)
	}

	// Read back and verify
	meta2, err := markdown.ReadMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	if meta2.Status != "tl-review" {
		t.Errorf("status = %q, want tl-review", meta2.Status)
	}
}

func TestEjectAndResume(t *testing.T) {
	dir := t.TempDir()
	content := markdown.ScaffoldSpec("SPEC-001", "Test Spec", "Aaron", "Cycle 1", "direct")
	path := filepath.Join(dir, "SPEC-001.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Set to build stage
	meta, _ := markdown.ReadMeta(path)
	meta.Status = "build"
	_ = markdown.WriteMeta(path, meta)

	// Eject
	meta, _ = markdown.ReadMeta(path)
	result, err := Eject(path, meta, "Upstream dependency blocked", "Aaron")
	if err != nil {
		t.Fatalf("Eject: %v", err)
	}
	if result.PreviousStage != "build" {
		t.Errorf("previous = %q, want build", result.PreviousStage)
	}

	meta, _ = markdown.ReadMeta(path)
	if meta.Status != StatusBlocked {
		t.Errorf("status = %q, want blocked", meta.Status)
	}

	// Resume
	if err := Resume(path, meta, "build"); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	meta, _ = markdown.ReadMeta(path)
	if meta.Status != "build" {
		t.Errorf("status after resume = %q, want build", meta.Status)
	}
}

func TestRevert(t *testing.T) {
	dir := t.TempDir()
	content := markdown.ScaffoldSpec("SPEC-001", "Test Spec", "Aaron", "Cycle 1", "direct")
	path := filepath.Join(dir, "SPEC-001.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	meta, _ := markdown.ReadMeta(path)
	meta.Status = "build"
	_ = markdown.WriteMeta(path, meta)

	meta, _ = markdown.ReadMeta(path)
	if err := Revert(path, meta, "draft", "Needs more detail", "QA Lead"); err != nil {
		t.Fatalf("Revert: %v", err)
	}

	meta, _ = markdown.ReadMeta(path)
	if meta.Status != "draft" {
		t.Errorf("status = %q, want draft", meta.Status)
	}
	if meta.RevertCount != 1 {
		t.Errorf("revert_count = %d, want 1", meta.RevertCount)
	}
}
