package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/adapter/noop"
	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/markdown"
)

// testConfig builds a minimal ResolvedConfig with a three-stage pipeline:
// draft (pm) → review (tl, gated on problem_statement) → done (tl).
func testConfig(role string) *config.ResolvedConfig {
	tc := &config.TeamConfig{}
	tc.Pipeline = config.PipelineConfig{Stages: []config.StageConfig{
		{Name: "draft", Owner: config.Owners{"pm"}},
		{Name: "review", Owner: config.Owners{"tl"}, Gates: []config.GateConfig{{SectionNotEmpty: "problem_statement"}}},
		{Name: "done", Owner: config.Owners{"tl"}},
	}}
	tc.Sync.ConflictStrategy = "warn"
	uc := &config.UserConfig{}
	uc.User.Name = "Tester"
	uc.User.OwnerRole = role
	return &config.ResolvedConfig{Team: tc, User: uc}
}

func testRegistry() *adapter.Registry {
	reg := adapter.NewRegistry(nil)
	reg.WithComms(noop.Comms{}).WithPM(noop.PM{}).WithDocs(noop.Docs{}).
		WithRepo(noop.Repo{}).WithAgent(noop.Agent{}).WithDeploy(noop.Deploy{}).WithAI(noop.AI{})
	return reg
}

// writeSpec creates a spec file at status `status` with the given
// problem-statement body, returning its path and containing directory.
func writeSpec(t *testing.T, status, problem string) (path, dir string) {
	t.Helper()
	dir = t.TempDir()
	path = filepath.Join(dir, "SPEC-001.md")
	content := "---\n" +
		"id: SPEC-001\n" +
		"title: Test Spec\n" +
		"status: " + status + "\n" +
		"version: 0.1.0\n" +
		"author: Tester\n" +
		"cycle: C1\n" +
		"revert_count: 0\n" +
		"created: 2026-01-01\n" +
		"updated: 2026-01-01\n" +
		"---\n\n" +
		"# SPEC-001 - Test Spec\n\n" +
		"## 1. Problem Statement\n" + problem + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing spec: %v", err)
	}
	return path, dir
}

func deps(role string) Deps {
	return Deps{Config: testConfig(role), Registry: testRegistry(), DB: nil, Role: role}
}

func statusOf(t *testing.T, path string) string {
	t.Helper()
	meta, err := markdown.ReadMeta(path)
	if err != nil {
		t.Fatalf("reading meta: %v", err)
	}
	return meta.Status
}

func TestAdvance_Success(t *testing.T) {
	path, dir := writeSpec(t, "draft", "We have a real problem to solve.")
	res, err := Advance(context.Background(), deps("pm"), AdvanceInput{
		SpecID: "SPEC-001", SpecPath: path, SpecDir: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.NewStage != "review" || res.PreviousStage != "draft" {
		t.Fatalf("got %s → %s, want draft → review", res.PreviousStage, res.NewStage)
	}
	if res.CommitMsg == "" {
		t.Fatal("expected a commit message on success")
	}
	if got := statusOf(t, path); got != "review" {
		t.Fatalf("spec status = %q, want review", got)
	}
}

func TestAdvance_GateNotMet_DoesNotMutate(t *testing.T) {
	path, dir := writeSpec(t, "draft", "") // empty problem statement fails the gate
	res, err := Advance(context.Background(), deps("pm"), AdvanceInput{
		SpecID: "SPEC-001", SpecPath: path, SpecDir: dir,
	})
	if !errors.Is(err, ErrGatesNotMet) {
		t.Fatalf("err = %v, want ErrGatesNotMet", err)
	}
	if len(res.GateFailures) == 0 {
		t.Fatal("expected gate failures to be populated")
	}
	if res.CommitMsg != "" {
		t.Fatal("gate failure must not produce a commit message")
	}
	if got := statusOf(t, path); got != "draft" {
		t.Fatalf("spec status = %q, want draft (unchanged)", got)
	}
}

func TestAdvance_DryRun_DoesNotMutate(t *testing.T) {
	path, dir := writeSpec(t, "draft", "We have a real problem.")
	res, err := Advance(context.Background(), deps("pm"), AdvanceInput{
		SpecID: "SPEC-001", SpecPath: path, SpecDir: dir, DryRun: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.DryRun {
		t.Fatal("expected DryRun result flag")
	}
	if res.CommitMsg != "" {
		t.Fatal("dry-run must not produce a commit message")
	}
	if got := statusOf(t, path); got != "draft" {
		t.Fatalf("spec status = %q, want draft (unchanged)", got)
	}
}

func TestAdvance_RoleGuard(t *testing.T) {
	path, dir := writeSpec(t, "draft", "problem text")
	// A designer does not own the draft stage and is not a TL.
	_, err := Advance(context.Background(), deps("designer"), AdvanceInput{
		SpecID: "SPEC-001", SpecPath: path, SpecDir: dir,
	})
	if err == nil {
		t.Fatal("expected role-guard error")
	}
	if got := statusOf(t, path); got != "draft" {
		t.Fatalf("spec status = %q, want draft (unchanged)", got)
	}
}

func TestRevert_Success(t *testing.T) {
	path, dir := writeSpec(t, "review", "problem text")
	res, err := Revert(context.Background(), deps("tl"), RevertInput{
		SpecID: "SPEC-001", SpecPath: path, SpecDir: dir, TargetStage: "draft", Reason: "needs rework",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TargetStage != "draft" || res.PreviousStage != "review" {
		t.Fatalf("got %s → %s, want review → draft", res.PreviousStage, res.TargetStage)
	}
	if got := statusOf(t, path); got != "draft" {
		t.Fatalf("spec status = %q, want draft", got)
	}
	meta, _ := markdown.ReadMeta(path)
	if meta.RevertCount != 1 {
		t.Fatalf("revert_count = %d, want 1", meta.RevertCount)
	}
}

func TestEjectAndResume(t *testing.T) {
	path, dir := writeSpec(t, "review", "problem text")
	_ = dir

	ejectRes, err := Eject(context.Background(), deps("tl"), EjectInput{
		SpecID: "SPEC-001", SpecPath: path, Reason: "waiting on API",
	})
	if err != nil {
		t.Fatalf("eject error: %v", err)
	}
	if ejectRes.PreviousStage != "review" {
		t.Fatalf("eject previous = %q, want review", ejectRes.PreviousStage)
	}
	if got := statusOf(t, path); got != "blocked" {
		t.Fatalf("status = %q, want blocked", got)
	}

	// Already-blocked eject must error.
	if _, err := Eject(context.Background(), deps("tl"), EjectInput{SpecID: "SPEC-001", SpecPath: path, Reason: "again"}); err == nil {
		t.Fatal("expected error ejecting an already-blocked spec")
	}

	resumeRes, err := Resume(context.Background(), deps("tl"), ResumeInput{
		SpecID: "SPEC-001", SpecPath: path, ResumeStage: "review",
	})
	if err != nil {
		t.Fatalf("resume error: %v", err)
	}
	if resumeRes.ResumeStage != "review" {
		t.Fatalf("resume stage = %q, want review", resumeRes.ResumeStage)
	}
	if got := statusOf(t, path); got != "review" {
		t.Fatalf("status = %q, want review", got)
	}
}

func TestResume_RequiresStage(t *testing.T) {
	path, _ := writeSpec(t, "blocked", "problem text")
	if _, err := Resume(context.Background(), deps("tl"), ResumeInput{SpecID: "SPEC-001", SpecPath: path}); err == nil {
		t.Fatal("expected error when resume stage cannot be determined")
	}
}
