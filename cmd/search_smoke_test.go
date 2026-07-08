package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/search"
)

// specBodyTLDR is a minimal level-2 section body so the FTS indexer (which
// only indexes ## sections) has content to search.
func specBodyTLDR(text string) string {
	return "## TL;DR\n\n" + text + "\n"
}

func TestSmoke_SearchFindsByBody(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "Sync Conflict", status: "draft", author: "Dev"},
		specBodyTLDR("the sync conflict strategy is warn and retries three times"))
	e.writeSpec(specFixture{id: "SPEC-002", title: "Payments", status: "draft", author: "Dev"},
		specBodyTLDR("payment retries ledger reconcile"))
	e.initSpecsGit()

	out, err := e.runSpec("search", "conflict")
	if err != nil {
		t.Fatalf("search: unexpected error: %v", err)
	}
	if !strings.Contains(out, "SPEC-001") {
		t.Errorf("search output = %q, want SPEC-001 hit", out)
	}
	if !strings.Contains(out, "Sync Conflict") {
		t.Errorf("search output = %q, want the title", out)
	}
	// SPEC-002 must not appear: it does not contain "conflict".
	if strings.Contains(out, "SPEC-002") {
		t.Errorf("search output = %q, should not include non-matching SPEC-002", out)
	}
}

func TestSmoke_SearchJSONShape(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "Sync Conflict", status: "draft", author: "Dev"},
		specBodyTLDR("the sync conflict strategy is warn"))
	e.initSpecsGit()

	out, err := e.runSpec("search", "conflict", "--json")
	if err != nil {
		t.Fatalf("search --json: unexpected error: %v", err)
	}
	var hits []search.Hit
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &hits); err != nil {
		t.Fatalf("search --json output not a []Hit: %v\noutput: %q", err, out)
	}
	if len(hits) == 0 {
		t.Fatal("search --json returned no hits")
	}
	if hits[0].SpecID != "SPEC-001" {
		t.Errorf("json hit[0].SpecID = %q, want SPEC-001", hits[0].SpecID)
	}
	if hits[0].SectionSlug == "" {
		t.Error("json hit should carry a section slug for deep-linking")
	}
}

func TestSmoke_SearchReindexRebuilds(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "Sync", status: "draft", author: "Dev"},
		specBodyTLDR("sync gateway configuration"))
	e.initSpecsGit()

	// A first query populates the index; --reindex then drops and rebuilds it
	// and still finds the spec.
	if _, err := e.runSpec("search", "sync"); err != nil {
		t.Fatalf("first search: %v", err)
	}
	out, err := e.runSpec("search", "sync", "--reindex")
	if err != nil {
		t.Fatalf("search --reindex: unexpected error: %v", err)
	}
	if !strings.Contains(out, "SPEC-001") {
		t.Errorf("search --reindex output = %q, want SPEC-001", out)
	}
	if !strings.Contains(out, "reindexed") {
		t.Errorf("search --reindex should report reindex stats, got: %q", out)
	}
}

func TestSmoke_SearchUnconfiguredHint(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	// No team config, no specs repo: the command must surface the next-action
	// hint, not a bare error.
	_, err := e.runSpec("search", "anything")
	if err == nil {
		t.Fatal("search with no specs repo should error")
	}
	if !strings.Contains(err.Error(), "spec config init") {
		t.Errorf("error = %q, want a next-action hint mentioning 'spec config init'", err.Error())
	}
}
