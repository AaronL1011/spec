package cmd

import (
	"strings"
	"testing"
)

func TestSmoke_Standup(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "S", status: "build", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	out, err := e.runSpec("standup")
	if err != nil {
		t.Fatalf("standup: unexpected error: %v", err)
	}
	if !strings.Contains(out, "standup") {
		t.Errorf("standup output = %q, want a standup report", out)
	}
}

func TestSmoke_Retro(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "R", status: "build", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	out, err := e.runSpec("retro")
	if err != nil {
		t.Fatalf("retro: unexpected error: %v", err)
	}
	if !strings.Contains(strings.ToLower(out), "retrospective") {
		t.Errorf("retro output = %q, want a retrospective report", out)
	}
}

func TestSmoke_DecideList(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "D", status: "build", author: "Dev"},
		"## TL;DR\nx\n\n## Decision Log\n| # | Question / Decision | Options Considered | Decision Made | Rationale | Decided By | Date |\n|---|---|---|---|---|---|---|\n")
	e.initSpecsGit()

	out, err := e.runSpec("decide", "SPEC-001", "--list")
	if err != nil {
		t.Fatalf("decide --list: unexpected error: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Errorf("decide --list produced no output")
	}
}

func TestSmoke_DecideNoMode(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "D", status: "build", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	_, err := e.runSpec("decide", "SPEC-001")
	if err == nil {
		t.Fatal("decide with no mode flag: expected an error")
	}
	if !strings.Contains(err.Error(), "--question") {
		t.Errorf("decide error = %q, want usage hint", err)
	}
}

func TestSmoke_AskList(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "A", status: "build", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	_, err := e.runSpec("ask", "SPEC-001", "--list")
	if err != nil {
		t.Fatalf("ask --list: unexpected error: %v", err)
	}
}

func TestSmoke_AskNoQuestion(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "A", status: "build", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	_, err := e.runSpec("ask", "SPEC-001")
	if err == nil {
		t.Fatal("ask with no question: expected an error")
	}
}

func TestSmoke_Metrics_JSONFlag(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "M", status: "build", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	// --since accepts a duration; exercise the flag-parse path.
	_, err := e.runSpec("metrics", "--since", "7d")
	if err != nil {
		t.Fatalf("metrics --since: unexpected error: %v", err)
	}
}
