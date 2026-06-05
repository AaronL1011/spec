package build

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/adapter"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/store"
)

// fakeAgent is a test double for adapter.AgentAdapter. The during hook runs
// inside Invoke to simulate the orchestrator walking the DAG via MCP tools.
type fakeAgent struct {
	caps        adapter.Capabilities
	result      adapter.InvokeResult
	during      func(req adapter.InvokeRequest)
	lastReq     adapter.InvokeRequest
	invokedDirs []string
	invocations int
}

func (f *fakeAgent) Invoke(_ context.Context, req adapter.InvokeRequest) (*adapter.InvokeResult, error) {
	f.lastReq = req
	f.invokedDirs = append(f.invokedDirs, req.WorkDir)
	f.invocations++
	if f.during != nil {
		f.during(req)
	}
	r := f.result
	return &r, nil
}

func (f *fakeAgent) Capabilities() adapter.Capabilities { return f.caps }

func initRepo(t *testing.T) string {
	t.Helper()
	return initRepoAt(t, t.TempDir())
}

func initRepoAt(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test"},
	} {
		if _, err := gitpkg.Run(ctx, dir, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := gitpkg.Commit(ctx, dir, "initial"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return dir
}

func writeSpec(t *testing.T, dir, repoName string) string {
	t.Helper()
	content := `---
id: SPEC-900
title: Test build
status: build
---

# SPEC-900 — Test build

## 6. Acceptance Criteria

- [ ] it works

## 7. Technical Implementation

### 7.3 PR Stack Plan
1. [` + repoName + `] Implement the thing
`
	path := filepath.Join(dir, "SPEC-900.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// driveDAG simulates the pi orchestrator: it walks ready nodes wave by wave,
// calling spec_provision_node then spec_node_complete for each via the build
// MCP server (which shares the session DB). It returns the set of node IDs it
// provisioned so callers can assert that completed nodes are not re-run.
func driveDAG(t *testing.T, db *store.DB, specID string, opts Options) map[string]int {
	t.Helper()
	provisioned := map[string]int{}
	for {
		session, err := LoadSession(db, specID)
		if err != nil {
			t.Fatalf("LoadSession: %v", err)
		}
		if session.NodesComplete() {
			return provisioned
		}
		graph, err := BuildGraph(session.Steps)
		if err != nil {
			t.Fatalf("BuildGraph: %v", err)
		}
		ready := graph.ReadySet(session.DoneSet())
		if len(ready) == 0 {
			t.Fatalf("no ready nodes but session incomplete: done=%v", session.DoneSet())
		}
		srv := NewMCPServer(session, &BuildContext{SpecContent: "# spec"}, db, "", opts)
		for _, n := range ready {
			id := n.NodeID()
			args := json.RawMessage(fmt.Sprintf(`{"node_id":%q}`, id))
			if _, err := srv.CallTool("spec_provision_node", args); err != nil {
				t.Fatalf("provision %s: %v", id, err)
			}
			provisioned[id]++
			// Make a node-scoped change in the worktree so a diff is captured.
			wt := srv.session.node(id).Worktree
			_ = os.WriteFile(filepath.Join(wt, id+".txt"), []byte(id+"\n"), 0o644)
			_ = gitpkg.Commit(context.Background(), wt, "work "+id)
			if _, err := srv.CallTool("spec_node_complete", args); err != nil {
				t.Fatalf("complete %s: %v", id, err)
			}
		}
	}
}

// completeNodesVia provisions and completes the given nodes (in order) through
// the build MCP server, creating their real git branches. Used to seed a
// partially-built session for resume tests.
func completeNodesVia(t *testing.T, db *store.DB, specID string, opts Options, ids []string) {
	t.Helper()
	session, err := LoadSession(db, specID)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewMCPServer(session, &BuildContext{SpecContent: "# spec"}, db, "", opts)
	for _, id := range ids {
		args := json.RawMessage(fmt.Sprintf(`{"node_id":%q}`, id))
		if _, err := srv.CallTool("spec_provision_node", args); err != nil {
			t.Fatalf("seed provision %s: %v", id, err)
		}
		wt := srv.session.node(id).Worktree
		_ = os.WriteFile(filepath.Join(wt, id+".txt"), []byte(id+"\n"), 0o644)
		_ = gitpkg.Commit(context.Background(), wt, "seed "+id)
		if _, err := srv.CallTool("spec_node_complete", args); err != nil {
			t.Fatalf("seed complete %s: %v", id, err)
		}
	}
}

func writeDiamondSpec(t *testing.T, dir string) string {
	const repo = "svc"
	t.Helper()
	content := `---
id: SPEC-950
title: Diamond
status: build
---

# SPEC-950

### 7.3 PR Stack Plan
1. [` + repo + `] root
2. [` + repo + `] left (after: 1)
3. [` + repo + `] right (after: 1)
4. [` + repo + `] merge (after: 2, 3)
`
	path := filepath.Join(dir, "SPEC-950.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestStartOrResume_DrivesDiamondInOneInvocation verifies the single-invocation
// handoff: a fake orchestrator that provisions+completes every node drives a
// 4-node diamond to completion in exactly one agent invocation.
func TestStartOrResume_DrivesDiamondInOneInvocation(t *testing.T) {
	t.Setenv("SPEC_HOME", t.TempDir())
	parent := t.TempDir()
	repo := initRepoAt(t, filepath.Join(parent, "svc"))
	specPath := writeDiamondSpec(t, parent)

	db, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	opts := Options{Workspaces: map[string]string{"svc": repo}}
	agent := &fakeAgent{
		caps:   adapter.Capabilities{MCP: true, SystemPrompt: true},
		during: func(req adapter.InvokeRequest) { driveDAG(t, db, req.SpecID, opts) },
	}

	engine := NewEngine(db, agent, opts)
	if err := engine.StartOrResume(context.Background(), "SPEC-950", specPath, repo); err != nil {
		t.Fatalf("StartOrResume: %v", err)
	}

	if agent.invocations != 1 {
		t.Errorf("agent invoked %d times, want exactly 1 (single-invocation handoff)", agent.invocations)
	}
	if agent.lastReq.MCPConfigPath == "" || agent.lastReq.SystemPrompt == "" {
		t.Error("request must carry MCP config + system prompt")
	}

	session, _ := LoadSession(db, "SPEC-950")
	if !session.NodesComplete() {
		t.Fatalf("expected all nodes complete, ledger: %+v", session.Nodes)
	}
	// n4 bases on a merge of n2+n3 → its diff is captured.
	if _, err := os.Stat(filepath.Join(SessionDir("SPEC-950"), "node-n4.diff")); err != nil {
		t.Errorf("expected node-n4.diff captured: %v", err)
	}
}

// TestStartOrResume_ResumesOnlySurvivors verifies a partial run resumes
// correctly: with n1+n2 already complete, a fresh invocation provisions only
// the remaining nodes and never re-runs the completed ones.
func TestStartOrResume_ResumesOnlySurvivors(t *testing.T) {
	t.Setenv("SPEC_HOME", t.TempDir())
	parent := t.TempDir()
	repo := initRepoAt(t, filepath.Join(parent, "svc"))
	specPath := writeDiamondSpec(t, parent)

	db, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	opts := Options{Workspaces: map[string]string{"svc": repo}}

	// Seed a prior run: actually provision + complete n1 and n2 (creating their
	// real branches), leaving n3 and n4 outstanding — exactly what a killed run
	// would leave behind.
	steps, err := ParsePRStackFromFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateSession(db, "SPEC-950", steps, repo); err != nil {
		t.Fatal(err)
	}
	completeNodesVia(t, db, "SPEC-950", opts, []string{"n1", "n2"})

	var provisioned map[string]int
	agent := &fakeAgent{
		caps:   adapter.Capabilities{MCP: true},
		during: func(req adapter.InvokeRequest) { provisioned = driveDAG(t, db, req.SpecID, opts) },
	}
	engine := NewEngine(db, agent, opts)
	if err := engine.StartOrResume(context.Background(), "SPEC-950", specPath, repo); err != nil {
		t.Fatalf("StartOrResume: %v", err)
	}

	if provisioned["n1"] != 0 || provisioned["n2"] != 0 {
		t.Errorf("completed nodes must not be re-provisioned, got %v", provisioned)
	}
	if provisioned["n3"] != 1 || provisioned["n4"] != 1 {
		t.Errorf("survivors n3,n4 should each be provisioned once, got %v", provisioned)
	}
	final, _ := LoadSession(db, "SPEC-950")
	if !final.NodesComplete() {
		t.Error("expected completion after resume")
	}
}

// TestStartOrResume_InvalidWorkspaceErrors verifies workspace validation runs
// before the agent is invoked, with an actionable error.
func TestStartOrResume_InvalidWorkspaceErrors(t *testing.T) {
	t.Setenv("SPEC_HOME", t.TempDir())
	parent := t.TempDir()
	specPath := writeDiamondSpec(t, parent)

	db, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	// "svc" maps to a non-git directory.
	notRepo := filepath.Join(parent, "not-a-repo")
	if err := os.MkdirAll(notRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	agent := &fakeAgent{caps: adapter.Capabilities{MCP: true}}
	engine := NewEngine(db, agent, Options{Workspaces: map[string]string{"svc": notRepo}})

	err = engine.StartOrResume(context.Background(), "SPEC-950", specPath, parent)
	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("expected actionable workspace error, got %v", err)
	}
	if agent.invocations != 0 {
		t.Error("agent must not be invoked when workspace validation fails")
	}
}

// TestStartOrResume_MissingWorkspaceErrors verifies the Item 9 acceptance: a
// node whose repo has no workspace mapping produces an actionable error naming
// the repo and the config key, before the agent runs.
func TestStartOrResume_MissingWorkspaceErrors(t *testing.T) {
	t.Setenv("SPEC_HOME", t.TempDir())
	parent := t.TempDir()
	specPath := writeDiamondSpec(t, parent)

	db, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	agent := &fakeAgent{caps: adapter.Capabilities{MCP: true}}
	engine := NewEngine(db, agent, Options{}) // no workspaces configured

	err = engine.StartOrResume(context.Background(), "SPEC-950", specPath, parent)
	if err == nil || !strings.Contains(err.Error(), "workspaces.svc") {
		t.Fatalf("expected error naming repo + config key, got %v", err)
	}
	if agent.invocations != 0 {
		t.Error("agent must not be invoked when a workspace is missing")
	}
}

// TestProvision_SkillSeam verifies skills under .spec/agent/skills/ are resolved
// and passed to a skill-capable agent, with the body also in the context file.
func TestProvision_SkillSeam(t *testing.T) {
	t.Setenv("SPEC_HOME", t.TempDir())
	workDir := initRepo(t)
	specPath := writeSpec(t, workDir, filepath.Base(workDir))

	skillDir := filepath.Join(workDir, agentDir, "skills", "spec-build")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Build playbook\nDo it well."), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	agent := &fakeAgent{caps: adapter.Capabilities{MCP: true, Skills: true, SystemPrompt: true}}
	engine := NewEngine(db, agent, Options{})
	if err := engine.StartOrResume(context.Background(), "SPEC-900", specPath, workDir); err != nil {
		t.Fatalf("StartOrResume: %v", err)
	}

	if len(agent.lastReq.SkillPaths) != 1 {
		t.Fatalf("expected 1 skill path, got %v", agent.lastReq.SkillPaths)
	}
	data, err := os.ReadFile(filepath.Join(SessionDir("SPEC-900"), "context.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Build playbook") {
		t.Error("context.md should include the skill body")
	}
}

// TestProvision_NonSkillAgentFoldsSkillIntoPrompt verifies that a non-skill
// agent receives skill bodies via the system prompt and no skill paths.
func TestProvision_NonSkillAgentFoldsSkillIntoPrompt(t *testing.T) {
	t.Setenv("SPEC_HOME", t.TempDir())
	workDir := initRepo(t)
	specPath := writeSpec(t, workDir, filepath.Base(workDir))

	skillDir := filepath.Join(workDir, agentDir, "skills", "spec-build")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("PLAYBOOK-MARKER"), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	agent := &fakeAgent{caps: adapter.Capabilities{}}
	engine := NewEngine(db, agent, Options{})
	if err := engine.StartOrResume(context.Background(), "SPEC-900", specPath, workDir); err != nil {
		t.Fatalf("StartOrResume: %v", err)
	}

	if len(agent.lastReq.SkillPaths) != 0 {
		t.Errorf("non-skill agent should get no skill paths, got %v", agent.lastReq.SkillPaths)
	}
	if !strings.Contains(agent.lastReq.SystemPrompt, "PLAYBOOK-MARKER") {
		t.Error("non-skill agent system prompt should include the skill body")
	}
}

// TestConductorSkillPaths_StartDirScoped proves the conductor's skills are
// resolved from the start dir alone — never the cross-repo per-node union — so
// same-named skills in other workspaces can't collide in the conductor.
func TestConductorSkillPaths_StartDirScoped(t *testing.T) {
	start := t.TempDir()
	other := t.TempDir()
	for _, dir := range []string{start, other} {
		sk := filepath.Join(dir, agentDir, "skills", "playbook")
		if err := os.MkdirAll(sk, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sk, "SKILL.md"), []byte("# playbook"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	e := &Engine{opts: Options{Workspaces: map[string]string{"other": other}}}
	got := e.conductorSkillPaths(start)
	if len(got) != 1 {
		t.Fatalf("expected exactly the start-dir skill, got %v", got)
	}
	if !strings.HasPrefix(got[0], start) {
		t.Errorf("conductor skill %q must be start-dir scoped (never the cross-repo %q)", got[0], other)
	}
}

// TestLeafPRStatus verifies artifact-defined completion accounting: a stack
// leaf is only counted as done once it carries a recorded draft-PR URL.
func TestLeafPRStatus(t *testing.T) {
	steps := []PRStep{
		{Number: 1, ID: "n1", Repo: "svc", Description: "root"},
		{Number: 2, ID: "n2", Repo: "svc", Description: "leaf", DependsOn: []int{1}},
	}
	g, err := BuildGraph(steps)
	if err != nil {
		t.Fatal(err)
	}
	s := &SessionState{SpecID: "SPEC-1", Steps: steps}
	s.InitNodes()

	applicable, withPR, missing := leafPRStatus(s, g)
	if applicable != 1 || withPR != 0 || len(missing) != 1 || missing[0] != "n2" {
		t.Fatalf("before PR: applicable=%d withPR=%d missing=%v", applicable, withPR, missing)
	}

	s.node("n2").PRURL = "https://example/pr/2"
	applicable, withPR, missing = leafPRStatus(s, g)
	if applicable != 1 || withPR != 1 || len(missing) != 0 {
		t.Fatalf("after PR: applicable=%d withPR=%d missing=%v", applicable, withPR, missing)
	}
}
