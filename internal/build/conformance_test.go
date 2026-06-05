package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// fixtureServer builds a synthetic, harness-agnostic build over a temp repo: a
// registry with one layer skill (carrying a quality gate) plus an auto-composed
// modifier, a two-node DAG, and an AC section. It is the conformance substrate —
// no agent, no network, no coupling to any real consumer repo.
func fixtureServer(t *testing.T, opts Options) *MCPServer {
	t.Helper()
	repo := t.TempDir()
	skillsDir := filepath.Join(repo, ".agents", "skills")
	writeSkill(t, skillsDir, "service", "# service")
	writeSkill(t, skillsDir, "tdd", "# tdd")
	registry := "version: \"1\"\n" +
		"modifiers: [tdd]\n" +
		"skills:\n" +
		"  - name: service\n    kind: layer\n    path: .agents/skills/service\n    applies_to: [\"layer:service\"]\n    quality_gates: [\"make test\"]\n" +
		"  - name: tdd\n    kind: modifier\n    path: .agents/skills/tdd\n"
	if err := os.WriteFile(filepath.Join(skillsDir, "registry.yaml"), []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}

	steps := []PRStep{
		{Number: 1, ID: "n1", Repo: filepath.Base(repo), Layer: "service", Description: "Implement core", ACs: []int{1}, Status: "pending"},
		{Number: 2, ID: "n2", Repo: filepath.Base(repo), Layer: "service", Description: "Wire it", DependsOn: []int{1}, ACs: []int{2}, Status: "pending"},
	}
	session := &SessionState{SpecID: "SPEC-CONF", WorkDir: repo, Steps: steps}
	ctx := &BuildContext{SpecContent: "## Acceptance Criteria\n\n- [ ] Core works\n- [ ] Wiring works\n"}
	return NewMCPServer(session, ctx, nil, "", opts)
}

func hasResource(s *MCPServer, uri string) bool {
	for _, r := range s.ListResources() {
		if r.URI == uri {
			return true
		}
	}
	return false
}

func toolNames(s *MCPServer) map[string]bool {
	m := make(map[string]bool)
	for _, spec := range s.ToolSpecs() {
		m[spec.Name] = true
	}
	return m
}

// TestConformance_DefaultStack asserts the full port contract under the shipped
// default adapters (registry router + stacked-draft-pr strategy).
func TestConformance_DefaultStack(t *testing.T) {
	s := fixtureServer(t, Options{})

	// Versioned resources are present.
	for _, uri := range []string{"spec://current/dag", "spec://current/capabilities"} {
		if !hasResource(s, uri) {
			t.Errorf("missing resource %s", uri)
		}
	}

	// DAG conforms and routes skills per node.
	var dag dagDocument
	if err := json.Unmarshal([]byte(s.dagJSON()), &dag); err != nil {
		t.Fatal(err)
	}
	if dag.SchemaVersion != DAGSchemaVersion || len(dag.Nodes) != 2 {
		t.Fatalf("dag = %+v", dag)
	}
	if len(dag.Nodes[0].SkillPaths) != 2 { // service + tdd
		t.Errorf("node n1 skillPaths = %v, want service+tdd", dag.Nodes[0].SkillPaths)
	}
	if len(dag.Nodes[0].QualityGates) != 1 || dag.Nodes[0].QualityGates[0] != "make test" {
		t.Errorf("node n1 qualityGates = %v", dag.Nodes[0].QualityGates)
	}

	// Capabilities advertise the stacked strategy + finishing tools.
	var caps buildCapabilities
	if err := json.Unmarshal([]byte(s.capabilitiesJSON()), &caps); err != nil {
		t.Fatal(err)
	}
	if caps.Strategy != "stacked-draft-pr" || len(caps.FinishingTools) != 3 {
		t.Errorf("caps = %+v", caps)
	}

	// Finishing tools are advertised and the per-node context resolves AC text.
	if !toolNames(s)["spec_open_pr"] {
		t.Error("default stack should advertise spec_open_pr")
	}
	out, ok := s.nodeContextJSON("n2")
	if !ok {
		t.Fatal("node context for n2 missing")
	}
	var nc nodeContextDoc
	if err := json.Unmarshal([]byte(out), &nc); err != nil {
		t.Fatal(err)
	}
	if len(nc.AcceptanceCriteria) != 1 || nc.AcceptanceCriteria[0] != "Wiring works" {
		t.Errorf("n2 acceptanceCriteria = %v", nc.AcceptanceCriteria)
	}
}

// TestConformance_BYONoneStack proves a fully-different consumer — no skill
// routing, no PR workflow — completes the same kernel contract with zero kernel
// change, purely by selecting different adapters.
func TestConformance_BYONoneStack(t *testing.T) {
	s := fixtureServer(t, Options{Router: "none", Strategy: "none"})

	// No skills routed.
	var dag dagDocument
	if err := json.Unmarshal([]byte(s.dagJSON()), &dag); err != nil {
		t.Fatal(err)
	}
	for _, n := range dag.Nodes {
		if len(n.SkillPaths) != 0 {
			t.Errorf("none router must route no skills, node %s got %v", n.ID, n.SkillPaths)
		}
	}

	// Capabilities reflect the BYO adapters; no finishing tools.
	var caps buildCapabilities
	if err := json.Unmarshal([]byte(s.capabilitiesJSON()), &caps); err != nil {
		t.Fatal(err)
	}
	if caps.Strategy != "none" || caps.Router != "none" || len(caps.FinishingTools) != 0 {
		t.Errorf("BYO caps = %+v", caps)
	}

	// Finishing tools are neither advertised nor callable.
	if toolNames(s)["spec_open_pr"] {
		t.Error("none strategy must not advertise spec_open_pr")
	}
	if _, err := s.CallTool("spec_open_pr", []byte(`{"node_id":"n1"}`)); err == nil {
		t.Error("none strategy must reject spec_open_pr")
	}

	// Node context still works (kernel projection is adapter-independent).
	if _, ok := s.nodeContextJSON("n1"); !ok {
		t.Error("spec_node_context must work regardless of adapters")
	}

	// Completion is done once all nodes complete (no PR requirement).
	c := s.strategy.Complete(s.session, s.graph)
	if !c.Done {
		t.Errorf("none strategy should complete on nodes alone: %+v", c)
	}
}
