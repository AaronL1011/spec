package cmd

import (
	"strings"
	"testing"
)

func TestSmoke_ConfigTest(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()

	out, err := e.runSpec("config", "test")
	if err != nil {
		t.Fatalf("config test: unexpected error: %v", err)
	}
	if !strings.Contains(out, "Team config") || !strings.Contains(out, "Integrations") {
		t.Errorf("config test output = %q, want config + integrations report", out)
	}
}

func TestSmoke_ConfigTestNoTeam(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")

	out, err := e.runSpec("config", "test")
	if err != nil {
		t.Fatalf("config test (no team): unexpected error: %v", err)
	}
	if !strings.Contains(out, "Team config") {
		t.Errorf("config test output = %q", out)
	}
}

func TestSmoke_ConfigCheckNoPM(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()

	out, err := e.runSpec("config", "check")
	if err != nil {
		t.Fatalf("config check (no pm): unexpected error: %v", err)
	}
	if !strings.Contains(out, "PM: not configured") {
		t.Errorf("config check output = %q, want 'PM: not configured'", out)
	}
}

func TestSmoke_ConfigCheckNoTeam(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")

	_, err := e.runSpec("config", "check")
	if err == nil {
		t.Fatal("config check with no team: expected an error")
	}
	if !strings.Contains(err.Error(), "spec config init") {
		t.Errorf("config check error = %q, want 'spec config init'", err)
	}
}

func TestSmoke_StepsNextNoSteps(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "NoSteps", status: "build", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	_, err := e.runSpec("steps", "next", "SPEC-001")
	if err == nil {
		t.Fatal("steps next (no steps): expected an error")
	}
	if !strings.Contains(err.Error(), "no build plan") {
		t.Errorf("steps next error = %q, want 'no build plan'", err)
	}
}

func TestSmoke_StepsShowNoSteps(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "NoSteps", status: "build", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	out, err := e.runSpec("steps", "SPEC-001")
	if err != nil {
		t.Fatalf("steps show (no steps): unexpected error: %v", err)
	}
	if !strings.Contains(out, "No build steps") {
		t.Errorf("steps show output = %q, want 'No build steps'", out)
	}
}

func TestSmoke_PlanShowNoPlan(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "NoPlan", status: "build", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	out, err := e.runSpec("plan", "SPEC-001")
	if err != nil {
		t.Fatalf("plan show (no plan): unexpected error: %v", err)
	}
	if !strings.Contains(out, "No build plan") {
		t.Errorf("plan show output = %q, want 'No build plan'", out)
	}
}
