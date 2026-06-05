package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePRStack_ParsesACAnnotation(t *testing.T) {
	steps, err := ParsePRStack("## PR Stack Plan\n\n1. [svc:rails-api] Add limiter (ac: 1,3)\n2. [svc:rails-api] Wire it (after: 1) (ac: 2)\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(steps))
	}
	if len(steps[0].ACs) != 2 || steps[0].ACs[0] != 1 || steps[0].ACs[1] != 3 {
		t.Errorf("step 1 ACs = %v, want [1 3]", steps[0].ACs)
	}
	// The (ac: ...) annotation must be stripped from the description.
	if strings.Contains(steps[0].Description, "ac:") {
		t.Errorf("description still contains annotation: %q", steps[0].Description)
	}
	if len(steps[1].ACs) != 1 || steps[1].ACs[0] != 2 {
		t.Errorf("step 2 ACs = %v, want [2]", steps[1].ACs)
	}
}

func TestAcItems_StripsMarkers(t *testing.T) {
	got := acItems("- [ ] First criterion\n- [x] Second done\nsome prose\n- [X] Third\n")
	want := []string{"First criterion", "Second done", "Third"}
	if len(got) != len(want) {
		t.Fatalf("acItems = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("acItems[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestNodeContext_ResolvesACAndGates builds a server over a small DAG + registry
// and asserts spec_node_context returns resolved AC text, routed skills, and the
// node's quality gates.
func TestNodeContext_ResolvesACAndGates(t *testing.T) {
	repo := t.TempDir()
	skillsDir := filepath.Join(repo, ".agents", "skills")
	writeSkill(t, skillsDir, "rails", "# rails")
	registry := "version: \"1\"\nskills:\n  - name: rails\n    kind: layer\n    path: .agents/skills/rails\n    applies_to: [\"layer:rails-api\"]\n    quality_gates: [\"bundle exec rspec\", \"rubocop\"]\n"
	if err := os.WriteFile(filepath.Join(skillsDir, "registry.yaml"), []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}

	steps := []PRStep{{Number: 1, ID: "n1", Repo: filepath.Base(repo), Layer: "rails-api", Description: "Add limiter", ACs: []int{1, 2}, Status: "pending"}}
	session := &SessionState{SpecID: "SPEC-1", WorkDir: repo, Steps: steps}
	ctx := &BuildContext{SpecContent: "## Acceptance Criteria\n\n- [ ] Requests are limited\n- [ ] Over-limit returns 429\n"}
	s := NewMCPServer(session, ctx, nil, "", Options{})

	out, ok := s.nodeContextJSON("n1")
	if !ok {
		t.Fatal("nodeContextJSON returned not-ok for n1")
	}
	var doc nodeContextDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.AcceptanceCriteria) != 2 || doc.AcceptanceCriteria[0] != "Requests are limited" {
		t.Errorf("acceptanceCriteria = %v", doc.AcceptanceCriteria)
	}
	if len(doc.QualityGates) != 2 || doc.QualityGates[0] != "bundle exec rspec" {
		t.Errorf("qualityGates = %v", doc.QualityGates)
	}
	if len(doc.SkillPaths) != 1 || filepath.Base(doc.SkillPaths[0]) != "rails" {
		t.Errorf("skillPaths = %v", doc.SkillPaths)
	}

	if _, ok := s.nodeContextJSON("nope"); ok {
		t.Error("unknown node id should return ok=false")
	}
}
