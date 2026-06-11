package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestSmoke_ThreadLifecycle exercises ask → answer → resolve over the sidecar
// thread store, which rides the git mutate path.
func TestSmoke_ThreadLifecycle(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "T", status: "build", author: "Dev"},
		"## TL;DR\nx\n\n## 1. Problem Statement\nbody\n")
	e.initSpecsGit()

	out, err := e.runSpec("ask", "SPEC-001", "--section", "problem_statement", "Why this approach?", "--json")
	if err != nil {
		t.Fatalf("ask: unexpected error: %v", err)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(jsonChunk(out)), &created); err != nil || created.ID == "" {
		t.Fatalf("could not parse a thread id from ask --json output: %q (%v)", out, err)
	}
	threadID := created.ID

	resetCmdState(t)
	if _, err := e.runSpec("answer", "SPEC-001", threadID, "Because it is simplest."); err != nil {
		t.Fatalf("answer: unexpected error: %v", err)
	}

	resetCmdState(t)
	if _, err := e.runSpec("resolve", "SPEC-001", threadID); err != nil {
		t.Fatalf("resolve: unexpected error: %v", err)
	}

	resetCmdState(t)
	out, err = e.runSpec("ask", "SPEC-001", "--list")
	if err != nil {
		t.Fatalf("ask --list: unexpected error: %v", err)
	}
	if !strings.Contains(out, threadID) {
		t.Errorf("ask --list output = %q, want the thread id %q", out, threadID)
	}
}

func TestSmoke_AnswerNoReply(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "T", status: "build", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	_, err := e.runSpec("answer", "SPEC-001", "TH-001")
	if err == nil {
		t.Fatal("answer with no reply: expected an error")
	}
}
