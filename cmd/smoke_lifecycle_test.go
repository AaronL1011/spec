package cmd

import (
	"strings"
	"testing"
)

func TestSmoke_New(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("pm")
	e.writeTeamConfig()
	e.initSpecsGit()

	out, err := e.runSpec("new", "--title", "Auth refactor")
	if err != nil {
		t.Fatalf("new: unexpected error: %v", err)
	}
	if !strings.Contains(out, "SPEC-001") || !strings.Contains(out, "Auth refactor") {
		t.Errorf("new output = %q, want a created SPEC id and title", out)
	}
}

func TestSmoke_NewNoTitle(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("pm")
	e.writeTeamConfig()
	e.initSpecsGit()

	_, err := e.runSpec("new")
	if err == nil {
		t.Fatal("new with no title: expected an error")
	}
	if !strings.Contains(err.Error(), "--title is required") {
		t.Errorf("new error = %q, want '--title is required'", err)
	}
}

func TestSmoke_Intake(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("pm")
	e.writeTeamConfig()
	e.initSpecsGit()

	out, err := e.runSpec("intake", "A flaky test", "--source", "engineer", "--priority", "high")
	if err != nil {
		t.Fatalf("intake: unexpected error: %v", err)
	}
	if !strings.Contains(out, "TRIAGE-001") {
		t.Errorf("intake output = %q, want a created TRIAGE id", out)
	}
}

func TestSmoke_IntakeThenPromote(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("pm")
	e.writeTeamConfig()
	e.initSpecsGit()

	if _, err := e.runSpec("intake", "Promote me", "--priority", "high"); err != nil {
		t.Fatalf("intake: unexpected error: %v", err)
	}

	resetCmdState(t)
	out, err := e.runSpec("promote", "TRIAGE-001")
	if err != nil {
		t.Fatalf("promote: unexpected error: %v", err)
	}
	if !strings.Contains(out, "SPEC-") {
		t.Errorf("promote output = %q, want a promoted SPEC id", out)
	}
}

func TestSmoke_NewThenListAndStatus(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("pm")
	e.writeTeamConfig()
	e.initSpecsGit()

	if _, err := e.runSpec("new", "--title", "Lifecycle spec"); err != nil {
		t.Fatalf("new: unexpected error: %v", err)
	}

	resetCmdState(t)
	out, err := e.runSpec("list", "--all", "--json")
	if err != nil {
		t.Fatalf("list after new: unexpected error: %v", err)
	}
	if !strings.Contains(out, "SPEC-001") {
		t.Errorf("list output = %q, want the new spec", out)
	}

	resetCmdState(t)
	out, err = e.runSpec("status", "SPEC-001")
	if err != nil {
		t.Fatalf("status after new: unexpected error: %v", err)
	}
	if !strings.Contains(out, "Lifecycle spec") {
		t.Errorf("status output = %q, want the new spec title", out)
	}
}
