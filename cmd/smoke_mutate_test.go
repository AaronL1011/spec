package cmd

import (
	"strings"
	"testing"
)

func TestSmoke_AdvanceDryRun(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("pm")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "A", status: "draft", author: "Dev"},
		"## TL;DR\nx\n\n## 1. Problem Statement\nproblem text\n")
	e.initSpecsGit()

	out, err := e.runSpec("advance", "SPEC-001", "--dry-run")
	if err != nil {
		t.Fatalf("advance --dry-run: unexpected error: %v", err)
	}
	if !strings.Contains(out, "would advance") {
		t.Errorf("advance --dry-run output = %q", out)
	}
}

func TestSmoke_Advance(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("pm")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "A", status: "draft", author: "Dev"},
		"## TL;DR\nx\n\n## 1. Problem Statement\nproblem text\n")
	e.initSpecsGit()

	out, err := e.runSpec("advance", "SPEC-001")
	if err != nil {
		t.Fatalf("advance: unexpected error: %v", err)
	}
	if !strings.Contains(out, "advanced") {
		t.Errorf("advance output = %q, want 'advanced'", out)
	}
}

func TestSmoke_AdvanceGateBlocked(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("pm")
	e.writeTeamConfig()
	// draft → tl-review gate requires a non-empty problem_statement; leave it
	// empty so the gate fails and the next-action contract renders.
	e.writeSpec(specFixture{id: "SPEC-001", title: "A", status: "draft", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	_, err := e.runSpec("advance", "SPEC-001")
	if err == nil {
		t.Fatal("advance with failing gate: expected an error")
	}
	if !strings.Contains(err.Error(), "gate") {
		t.Errorf("advance error = %q, want a gate failure", err)
	}
}

func TestSmoke_AdvanceJSON(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("pm")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "A", status: "draft", author: "Dev"},
		"## TL;DR\nx\n\n## 1. Problem Statement\nproblem text\n")
	e.initSpecsGit()

	out, err := e.runSpec("advance", "SPEC-001", "--dry-run", "--json")
	if err != nil {
		t.Fatalf("advance --json: unexpected error: %v", err)
	}
	if !strings.Contains(jsonChunk(out), "{") {
		t.Errorf("advance --json output = %q, want JSON", out)
	}
}

func TestSmoke_DecideQuestion(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("pm")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "D", status: "build", author: "Dev"},
		"## TL;DR\nx\n\n## Decision Log\n| # | Question / Decision | Options Considered | Decision Made | Rationale | Decided By | Date |\n|---|---|---|---|---|---|---|\n")
	e.initSpecsGit()

	out, err := e.runSpec("decide", "SPEC-001", "--question", "Which database?")
	if err != nil {
		t.Fatalf("decide --question: unexpected error: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Errorf("decide --question produced no output")
	}

	// The question should now appear in the decision log listing.
	resetCmdState(t)
	out, err = e.runSpec("decide", "SPEC-001", "--list")
	if err != nil {
		t.Fatalf("decide --list after add: unexpected error: %v", err)
	}
	if !strings.Contains(out, "Which database?") {
		t.Errorf("decide --list output = %q, want the recorded question", out)
	}
}

func TestSmoke_PlanAdd(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "P", status: "build", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	out, err := e.runSpec("plan", "add", "SPEC-001", "Implement the thing")
	if err != nil {
		t.Fatalf("plan add: unexpected error: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Errorf("plan add produced no output")
	}

	resetCmdState(t)
	out, err = e.runSpec("plan", "SPEC-001")
	if err != nil {
		t.Fatalf("plan show after add: unexpected error: %v", err)
	}
	if !strings.Contains(out, "Implement the thing") {
		t.Errorf("plan show output = %q, want the added step", out)
	}
}

func TestSmoke_Assign(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "Assignable", status: "build", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	out, err := e.runSpec("assign", "SPEC-001", "@dev")
	if err != nil {
		t.Fatalf("assign: unexpected error: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Errorf("assign produced no output")
	}
}
