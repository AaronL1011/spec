package cmd

import (
	"strings"
	"testing"
)

// TestSmoke_ListHuman exercises the human-readable (non-JSON) list render
// paths: by-role, --all by stage, and --mine.
func TestSmoke_ListHuman(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "Build me", status: "build", author: "Dev"}, "## TL;DR\nx\n")
	e.writeSpec(specFixture{id: "SPEC-002", title: "Draft me", status: "draft", author: "Other"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	for _, args := range [][]string{
		{"list"},
		{"list", "--all"},
		{"list", "--mine"},
	} {
		out, err := e.runSpec(args...)
		if err != nil {
			t.Fatalf("%v: unexpected error: %v", args, err)
		}
		if strings.TrimSpace(out) == "" {
			t.Errorf("%v produced no output", args)
		}
	}
}

// TestSmoke_StatusHuman exercises the human-readable status render including
// the pipeline diagram and section completion.
func TestSmoke_StatusHuman(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-007", title: "Statusable", status: "build", author: "Dev"},
		"## TL;DR\nsummary\n\n## 1. Problem Statement\ndetails\n")
	e.initSpecsGit()

	out, err := e.runSpec("status", "SPEC-007")
	if err != nil {
		t.Fatalf("status: unexpected error: %v", err)
	}
	if !strings.Contains(out, "SPEC-007") || !strings.Contains(out, "Pipeline:") {
		t.Errorf("status output = %q, want id and pipeline diagram", out)
	}
}

// TestSmoke_DashboardHuman exercises the static human dashboard render.
func TestSmoke_DashboardHuman(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "Do me", status: "build", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	out, err := e.runSpec("--static")
	if err != nil {
		t.Fatalf("dashboard --static: unexpected error: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Errorf("dashboard --static produced no output")
	}
}

// TestSmoke_ValidateHumanPasses exercises the gate dry-run when gates pass.
func TestSmoke_ValidateHumanPasses(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("pm")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "V", status: "draft", author: "Dev"},
		"## TL;DR\nx\n\n## 1. Problem Statement\nproblem text\n")
	e.initSpecsGit()

	out, err := e.runSpec("validate", "SPEC-001")
	if err != nil {
		t.Fatalf("validate: unexpected error: %v", err)
	}
	if !strings.Contains(out, "SPEC-001") {
		t.Errorf("validate output = %q", out)
	}
}
