package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSpecWithSteps writes a spec containing a build-plan steps block so the
// steps/plan command surfaces have real data to render.
func (e *smokeEnv) writeSpecWithSteps(id, title string) {
	e.t.Helper()
	dir := e.specsDirPath()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.t.Fatalf("mkdir specs: %v", err)
	}
	content := "---\n" +
		"id: " + id + "\n" +
		"title: " + title + "\n" +
		"status: build\n" +
		"version: 0.1.0\n" +
		"author: Dev\n" +
		"cycle: Cycle 0\n" +
		"revert_count: 0\n" +
		"created: \"2026-01-01\"\n" +
		"updated: \"2026-01-01\"\n" +
		"steps:\n" +
		"  - repo: spec-cli\n" +
		"    description: First step\n" +
		"    status: pending\n" +
		"  - repo: spec-cli\n" +
		"    description: Second step\n" +
		"    status: complete\n" +
		"---\n\n## TL;DR\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(content), 0o644); err != nil {
		e.t.Fatalf("write spec %s: %v", id, err)
	}
}

func TestSmoke_StepsShow(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpecWithSteps("SPEC-010", "Stepped")
	e.initSpecsGit()

	out, err := e.runSpec("steps", "SPEC-010")
	if err != nil {
		t.Fatalf("steps: unexpected error: %v", err)
	}
	if !strings.Contains(out, "First step") {
		t.Errorf("steps output = %q, want step descriptions", out)
	}
}

func TestSmoke_StepsNext(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpecWithSteps("SPEC-011", "Stepped")
	e.initSpecsGit()

	_, err := e.runSpec("steps", "next", "SPEC-011")
	if err != nil {
		t.Fatalf("steps next: unexpected error: %v", err)
	}
}

func TestSmoke_PlanShow(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpecWithSteps("SPEC-012", "Planned")
	e.initSpecsGit()

	out, err := e.runSpec("plan", "SPEC-012")
	if err != nil {
		t.Fatalf("plan: unexpected error: %v", err)
	}
	if !strings.Contains(out, "First step") {
		t.Errorf("plan output = %q, want step descriptions", out)
	}
}

func TestSmoke_PipelinePresets(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	out, err := e.runSpec("pipeline", "presets")
	if err != nil {
		t.Fatalf("pipeline presets: unexpected error: %v", err)
	}
	if !strings.Contains(out, "presets") {
		t.Errorf("pipeline presets output = %q", out)
	}
}

func TestSmoke_PipelineExport(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	out, err := e.runSpec("pipeline", "export")
	if err != nil {
		t.Fatalf("pipeline export: unexpected error: %v", err)
	}
	if !strings.Contains(out, "stages:") {
		t.Errorf("pipeline export output = %q, want resolved yaml", out)
	}
}

func TestSmoke_PipelineVerbose(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	out, err := e.runSpec("pipeline", "--verbose")
	if err != nil {
		t.Fatalf("pipeline --verbose: unexpected error: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Errorf("pipeline --verbose produced no output")
	}
}

func TestSmoke_PipelineValidate(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	_, err := e.runSpec("pipeline", "validate")
	if err != nil {
		t.Fatalf("pipeline validate: unexpected error: %v", err)
	}
}

func TestSmoke_Search(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "Searchable widget", status: "build", author: "Dev"},
		"## TL;DR\nThe widget does searchable things.\n")
	e.initSpecsGit()

	out, err := e.runSpec("search", "widget")
	if err != nil {
		t.Fatalf("search: unexpected error: %v", err)
	}
	if !strings.Contains(out, "SPEC-001") {
		t.Errorf("search output = %q, want the matching spec", out)
	}
}

func TestSmoke_Context(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "Authentication flow", status: "build", author: "Dev"},
		"## TL;DR\nHandles authentication and login.\n")
	e.initSpecsGit()

	out, err := e.runSpec("context", "how does authentication work")
	if err != nil {
		t.Fatalf("context: unexpected error: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Errorf("context produced no output")
	}
}

func TestSmoke_Metrics(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "M", status: "build", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	out, err := e.runSpec("metrics")
	if err != nil {
		t.Fatalf("metrics: unexpected error: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Errorf("metrics produced no output")
	}
}

func TestSmoke_ListTriageJSON(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	// Triage item lives under specs/triage/.
	triageDir := filepath.Join(e.specsDirPath(), "triage")
	if err := os.MkdirAll(triageDir, 0o755); err != nil {
		t.Fatalf("mkdir triage: %v", err)
	}
	tri := "---\nid: TRIAGE-001\ntitle: A bug\npriority: high\n---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(triageDir, "TRIAGE-001.md"), []byte(tri), 0o644); err != nil {
		t.Fatalf("write triage: %v", err)
	}
	e.initSpecsGit()

	out, err := e.runSpec("list", "--triage", "--json")
	if err != nil {
		t.Fatalf("list --triage --json: unexpected error: %v", err)
	}
	if !strings.Contains(out, "TRIAGE-001") {
		t.Errorf("triage JSON = %q, want the triage item", out)
	}
}

func TestSmoke_ListRoleNoTeamConfigError(t *testing.T) {
	e := newSmokeEnv(t)
	// No user config and no team config: list must still error cleanly.
	_, err := e.runSpec("list")
	if err == nil {
		t.Fatal("list with nothing configured: expected an error")
	}
}

func TestSmoke_ValidateMissingSpec(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	_, err := e.runSpec("validate", "SPEC-999")
	if err == nil {
		t.Fatal("validate missing spec: expected an error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("validate error = %q, want 'not found'", err)
	}
}

func TestSmoke_FocusSet(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "Focusable", status: "build", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	out, err := e.runSpec("focus", "SPEC-001")
	if err != nil {
		t.Fatalf("focus SPEC-001: unexpected error: %v", err)
	}
	if !strings.Contains(out, "SPEC-001") {
		t.Errorf("focus output = %q, want the focused id", out)
	}
}
