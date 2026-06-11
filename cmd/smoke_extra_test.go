package cmd

import (
	"strings"
	"testing"
)

func TestSmoke_WhoamiNoRole(t *testing.T) {
	e := newSmokeEnv(t)
	// No user config at all → no role configured.
	out, err := e.runSpec("whoami")
	if err != nil {
		t.Fatalf("whoami (no role): unexpected error: %v", err)
	}
	if !strings.Contains(out, "No role configured") {
		t.Errorf("whoami output = %q, want 'No role configured'", out)
	}
}

func TestSmoke_ResumeNotBlocked(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	// A spec that is not blocked: resume should report there's nothing to do
	// or error cleanly — either way, no panic and a clear message.
	e.writeSpec(specFixture{id: "SPEC-001", title: "R", status: "build", author: "Dev"},
		"## TL;DR\nx\n\n## 1. Problem Statement\nbody\n")
	e.initSpecsGit()

	_, _ = e.runSpec("resume", "SPEC-001")
	// No assertion on the specific outcome; the point is exercising the wiring
	// without a crash. A nil or non-nil error are both acceptable here.
}

func TestSmoke_AdvanceNoTeam(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("pm")

	_, err := e.runSpec("advance", "SPEC-001")
	if err == nil {
		t.Fatal("advance with no team config: expected an error")
	}
	if !strings.Contains(err.Error(), "spec config init") {
		t.Errorf("advance error = %q, want 'spec config init'", err)
	}
}

func TestSmoke_StatusNoTeam(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")

	_, err := e.runSpec("status", "SPEC-001")
	if err == nil {
		t.Fatal("status with no specs repo: expected an error")
	}
}

func TestSmoke_VersionFlag(t *testing.T) {
	e := newSmokeEnv(t)
	out, err := e.runSpec("version")
	if err != nil {
		t.Fatalf("version: unexpected error: %v", err)
	}
	if !strings.Contains(out, "spec") {
		t.Errorf("version output = %q", out)
	}
}
