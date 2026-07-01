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

// TestSmoke_Ask_RecordsInlineAndExplicitMentions proves an ask records both
// an inline @handle and an explicit --to handle in the sidecar (unioned,
// deduped), and that ask --list surfaces them.
func TestSmoke_Ask_RecordsInlineAndExplicitMentions(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "T", status: "build", author: "Dev"},
		"## TL;DR\nx\n\n## 1. Problem Statement\nbody\n")
	e.initSpecsGit()

	out, err := e.runSpec("ask", "SPEC-001", "--section", "problem_statement",
		"ping @bob what do you think?", "--to", "carlos", "--json")
	if err != nil {
		t.Fatalf("ask: unexpected error: %v", err)
	}
	var created struct {
		Mentions []string `json:"mentions"`
	}
	if err := json.Unmarshal([]byte(jsonChunk(out)), &created); err != nil {
		t.Fatalf("could not parse ask --json output: %q (%v)", out, err)
	}
	if !containsAll(created.Mentions, "bob", "carlos") {
		t.Fatalf("Mentions = %v, want to contain bob and carlos", created.Mentions)
	}

	resetCmdState(t)
	out, err = e.runSpec("ask", "SPEC-001", "--list")
	if err != nil {
		t.Fatalf("ask --list: unexpected error: %v", err)
	}
	if !strings.Contains(out, "@bob") || !strings.Contains(out, "@carlos") {
		t.Errorf("ask --list output = %q, want both mentioned handles", out)
	}
}

func containsAll(haystack []string, wants ...string) bool {
	for _, w := range wants {
		found := false
		for _, h := range haystack {
			if h == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
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
