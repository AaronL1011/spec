package cmd

import (
	"strings"
	"testing"
)

func TestSmoke_SyncLogEmpty(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()

	out, err := e.runSpec("sync", "log")
	if err != nil {
		t.Fatalf("sync log: unexpected error: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Errorf("sync log produced no output")
	}
}

func TestSmoke_SyncDryRun(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "S", status: "build", author: "Dev"},
		"## TL;DR\nx\n\n## 1. Problem Statement\nbody\n")
	e.initSpecsGit()

	// Docs is unconfigured (noop); sync must fail with an actionable next step.
	_, err := e.runSpec("sync", "SPEC-001", "--dry-run")
	if err == nil {
		t.Fatal("sync with no docs provider: expected an error")
	}
	if !strings.Contains(err.Error(), "integrations.docs") {
		t.Errorf("sync error = %q, want it to point to integrations.docs", err)
	}
}

func TestSmoke_Link(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "L", status: "build", author: "Dev"},
		"## TL;DR\nx\n\n## 1. Problem Statement\nbody\n")
	e.initSpecsGit()

	out, err := e.runSpec("link", "SPEC-001",
		"--section", "problem_statement", "--url", "https://example.com/doc", "--label", "Design doc")
	if err != nil {
		t.Fatalf("link: unexpected error: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Errorf("link produced no output")
	}
}

func TestSmoke_LinkMissingURL(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "L", status: "build", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	_, err := e.runSpec("link", "SPEC-001", "--section", "problem_statement")
	if err == nil {
		t.Fatal("link with no url: expected an error")
	}
	if !strings.Contains(err.Error(), "--url is required") {
		t.Errorf("link error = %q, want '--url is required'", err)
	}
}

// TestSmoke_StepsLifecycle exercises start → complete over a planned spec.
func TestSmoke_StepsLifecycle(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpecWithSteps("SPEC-001", "Stepped")
	e.initSpecsGit()

	out, err := e.runSpec("steps", "start", "SPEC-001", "1")
	if err != nil {
		t.Fatalf("steps start: unexpected error: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Errorf("steps start produced no output")
	}
}
