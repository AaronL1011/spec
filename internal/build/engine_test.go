package build

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/adapter"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/store"
)

// fakeAgent is a test double for adapter.AgentAdapter. The during hook runs
// inside Invoke to simulate agent behaviour (e.g. editing files, advancing the
// step via MCP).
type fakeAgent struct {
	caps        adapter.Capabilities
	result      adapter.InvokeResult
	during      func(req adapter.InvokeRequest)
	lastReq     adapter.InvokeRequest
	invokedDirs []string
}

func (f *fakeAgent) Invoke(_ context.Context, req adapter.InvokeRequest) (*adapter.InvokeResult, error) {
	f.lastReq = req
	f.invokedDirs = append(f.invokedDirs, req.WorkDir)
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
		{"init"},
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

// TestStartOrResume_MCPAdvanceAndDiff verifies that when an MCP agent advances
// the step (simulated here by AdvanceStep during Invoke), the engine detects
// the advance via session re-read, does not prompt, and captures the step diff.
func TestStartOrResume_MCPAdvanceAndDiff(t *testing.T) {
	t.Setenv("SPEC_HOME", t.TempDir())
	workDir := initRepo(t)
	specPath := writeSpec(t, workDir, filepath.Base(workDir))

	db, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	agent := &fakeAgent{
		caps: adapter.Capabilities{MCP: true, SystemPrompt: true},
		during: func(req adapter.InvokeRequest) {
			// Agent edits a file and commits.
			_ = os.WriteFile(filepath.Join(req.WorkDir, "feature.txt"), []byte("done\n"), 0o644)
			_ = gitpkg.Commit(context.Background(), req.WorkDir, "implement")
			// Simulate spec_step_complete via the MCP server (separate
			// process) by advancing the persisted session.
			s, _ := LoadSession(db, req.SpecID)
			_ = AdvanceStep(db, s)
		},
	}

	engine := NewEngine(db, agent, Options{})
	if err := engine.StartOrResume(context.Background(), "SPEC-900", specPath, workDir); err != nil {
		t.Fatalf("StartOrResume: %v", err)
	}

	// The request must carry the generated MCP config and a system prompt.
	if agent.lastReq.MCPConfigPath == "" {
		t.Error("expected MCPConfigPath to be set")
	}
	if agent.lastReq.SystemPrompt == "" {
		t.Error("expected SystemPrompt to be set")
	}

	// Session advanced to completion.
	s, _ := LoadSession(db, "SPEC-900")
	if !s.IsComplete() {
		t.Errorf("expected session complete, current step %d", s.CurrentStep)
	}

	// Step-1 diff captured for cumulative context.
	diffPath := filepath.Join(SessionDir("SPEC-900"), "step-1.diff")
	if _, err := os.Stat(diffPath); err != nil {
		t.Errorf("expected step-1.diff to be captured: %v", err)
	}
}

// TestProvision_SkillSeam verifies skills present under .spec/agent/skills/ are
// resolved and passed to a skill-capable agent, and that the consolidated
// context file includes the skill body.
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

	// Consolidated context file includes the skill body.
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

	// MCP:false, Skills:false → non-skill agent.
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

func TestResolveStepDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wsDir := filepath.Join(home, "code", "api-gateway")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	e := &Engine{opts: Options{Workspaces: map[string]string{
		"api-gateway": "~/code/api-gateway",
		"ghost":       "/no/such/dir",
	}}}

	t.Run("no repo uses start dir", func(t *testing.T) {
		got, err := e.resolveStepDir("SPEC-1", &PRStep{Number: 1}, "/start")
		if err != nil || got != "/start" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("matching basename uses start dir", func(t *testing.T) {
		got, err := e.resolveStepDir("SPEC-1", &PRStep{Number: 1, Repo: "api-gateway"}, "/x/api-gateway")
		if err != nil || got != "/x/api-gateway" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("workspace mapping resolves and expands tilde", func(t *testing.T) {
		got, err := e.resolveStepDir("SPEC-1", &PRStep{Number: 2, Repo: "api-gateway"}, "/elsewhere")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != wsDir {
			t.Errorf("got %q, want %q", got, wsDir)
		}
	})
	t.Run("missing mapping is an actionable error", func(t *testing.T) {
		_, err := e.resolveStepDir("SPEC-1", &PRStep{Number: 3, Repo: "frontend"}, "/elsewhere")
		if err == nil {
			t.Fatal("expected error for unmapped repo")
		}
		if !strings.Contains(err.Error(), "workspaces:") {
			t.Errorf("error should guide the user to configure workspaces: %v", err)
		}
	})
	t.Run("bad workspace path errors", func(t *testing.T) {
		_, err := e.resolveStepDir("SPEC-1", &PRStep{Number: 4, Repo: "ghost"}, "/elsewhere")
		if err == nil {
			t.Fatal("expected error for non-existent workspace path")
		}
	})
}

// TestStartOrResume_MultiRepoWalksWorkspaces verifies the engine walks a
// two-repo PR stack end-to-end, moving into each repo's workspace and capturing
// a diff per step, without the user re-running the command per repo.
func TestStartOrResume_MultiRepoWalksWorkspaces(t *testing.T) {
	t.Setenv("SPEC_HOME", t.TempDir())

	parent := t.TempDir()
	apiDir := initRepoAt(t, filepath.Join(parent, "api-gateway"))
	webDir := initRepoAt(t, filepath.Join(parent, "frontend"))

	specContent := `---
id: SPEC-901
title: Multi-repo
status: build
---

# SPEC-901

## 7. Technical Implementation

### 7.3 PR Stack Plan
1. [api-gateway] Backend change
2. [frontend] Frontend change
`
	specPath := filepath.Join(parent, "SPEC-901.md")
	if err := os.WriteFile(specPath, []byte(specContent), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	agent := &fakeAgent{
		caps: adapter.Capabilities{MCP: true},
		during: func(req adapter.InvokeRequest) {
			_ = os.WriteFile(filepath.Join(req.WorkDir, "change.txt"), []byte("x\n"), 0o644)
			_ = gitpkg.Commit(context.Background(), req.WorkDir, "work")
			s, _ := LoadSession(db, req.SpecID)
			_ = AdvanceStep(db, s)
		},
	}

	opts := Options{Workspaces: map[string]string{
		"api-gateway": apiDir,
		"frontend":    webDir,
	}}
	engine := NewEngine(db, agent, opts)

	// Launch from the api-gateway dir; the engine must hop to frontend itself.
	if err := engine.StartOrResume(context.Background(), "SPEC-901", specPath, apiDir); err != nil {
		t.Fatalf("StartOrResume: %v", err)
	}

	if len(agent.invokedDirs) != 2 {
		t.Fatalf("expected 2 step invocations, got %v", agent.invokedDirs)
	}
	if agent.invokedDirs[0] != apiDir {
		t.Errorf("step 1 dir = %q, want %q", agent.invokedDirs[0], apiDir)
	}
	if agent.invokedDirs[1] != webDir {
		t.Errorf("step 2 dir = %q, want %q (engine should auto-navigate)", agent.invokedDirs[1], webDir)
	}

	s, _ := LoadSession(db, "SPEC-901")
	if !s.IsComplete() {
		t.Errorf("expected all steps complete, current step %d", s.CurrentStep)
	}
	for _, n := range []int{1, 2} {
		p := filepath.Join(SessionDir("SPEC-901"), "step-"+itoa(n)+".diff")
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected step-%d.diff: %v", n, err)
		}
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
