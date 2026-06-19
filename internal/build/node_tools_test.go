package build

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/store"
)

// newDAGServer builds an MCP server over a real temp repo with a two-node
// linear stack (n1 → n2) in a single repo mapped via workspaces.
func newDAGServer(t *testing.T) (*MCPServer, *store.DB) {
	t.Helper()
	t.Setenv("SPEC_HOME", t.TempDir())
	repo := initWorktreeRepoForBuild(t)

	db, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	steps := []PRStep{
		{Number: 1, Repo: "svc", Description: "root"},
		{Number: 2, Repo: "svc", Description: "child", DependsOn: []int{1}},
	}
	session, err := CreateSession(db, "SPEC-800", steps, repo)
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Workspaces: map[string]string{"svc": repo}, MaxParallel: 3}
	srv := NewMCPServer(session, &BuildContext{SpecContent: "# spec"}, db, "", opts)
	return srv, db
}

func initWorktreeRepoForBuild(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "t@t.com"},
		{"config", "user.name", "T"},
	} {
		if _, err := gitpkg.Run(ctx, dir, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := gitpkg.Commit(ctx, dir, "init"); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDAGResource_Lists(t *testing.T) {
	srv, _ := newDAGServer(t)
	var found *MCPResource
	for _, r := range srv.ListResources() {
		if r.URI == "spec://current/dag" {
			r := r
			found = &r
		}
	}
	if found == nil {
		t.Fatal("spec://current/dag resource not listed")
		return
	}
	var doc dagDocument
	if err := json.Unmarshal([]byte(found.Content), &doc); err != nil {
		t.Fatalf("dag json: %v", err)
	}
	if len(doc.Nodes) != 2 || len(doc.Waves) != 2 {
		t.Fatalf("dag = %d nodes / %d waves, want 2/2", len(doc.Nodes), len(doc.Waves))
	}
	if doc.SchemaVersion != DAGSchemaVersion {
		t.Errorf("schemaVersion = %q, want %q", doc.SchemaVersion, DAGSchemaVersion)
	}
	if doc.MaxParallel != 3 {
		t.Errorf("maxParallel = %d, want 3", doc.MaxParallel)
	}
	if doc.Nodes[1].DependsOn[0] != "n1" {
		t.Errorf("n2 should depend on n1, got %v", doc.Nodes[1].DependsOn)
	}
}

func TestProvisionNode_ReturnsUsableWorktree(t *testing.T) {
	srv, _ := newDAGServer(t)

	res, err := srv.CallTool("spec_provision_node", json.RawMessage(`{"node_id":"n1"}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.Success {
		t.Fatalf("provision failed: %s", res.Message)
	}
	var p provisionResult
	if err := json.Unmarshal([]byte(res.Message), &p); err != nil {
		t.Fatalf("provision payload: %v", err)
	}
	if info, err := os.Stat(p.WorkDir); err != nil || !info.IsDir() {
		t.Fatalf("provisioned workdir not usable: %v", err)
	}
	if p.Branch == "" || p.BaseRef == "" {
		t.Errorf("provision should return branch+baseRef, got %+v", p)
	}
	// Ledger recorded the placement.
	if srv.session.node("n1").Worktree != p.WorkDir {
		t.Error("ledger should record the worktree path")
	}
	if srv.session.NodeStatus("n1") != NodeInProgress {
		t.Errorf("node status = %q, want in-progress", srv.session.NodeStatus("n1"))
	}
}

func TestNodeComplete_Idempotent(t *testing.T) {
	srv, _ := newDAGServer(t)
	if _, err := srv.CallTool("spec_provision_node", json.RawMessage(`{"node_id":"n1"}`)); err != nil {
		t.Fatal(err)
	}

	first, err := srv.CallTool("spec_node_complete", json.RawMessage(`{"node_id":"n1"}`))
	if err != nil || !first.Success {
		t.Fatalf("first complete: %v / %s", err, first.Message)
	}
	if srv.session.NodeStatus("n1") != NodeComplete {
		t.Fatal("node should be complete")
	}
	// Idempotent: second call still succeeds and stays complete.
	second, err := srv.CallTool("spec_node_complete", json.RawMessage(`{"node_id":"n1"}`))
	if err != nil || !second.Success {
		t.Fatalf("second complete: %v / %s", err, second.Message)
	}
	if !strings.Contains(second.Message, "already complete") {
		t.Errorf("second complete should report idempotency, got %q", second.Message)
	}
	if srv.session.NodeStatus("n1") != NodeComplete {
		t.Error("node should remain complete after idempotent call")
	}
}

func TestNodeFailed_RecordsReason(t *testing.T) {
	srv, db := newDAGServer(t)
	res, err := srv.CallTool("spec_node_failed", json.RawMessage(`{"node_id":"n2","reason":"compile error"}`))
	if err != nil || !res.Success {
		t.Fatalf("node_failed: %v / %s", err, res.Message)
	}
	// Persisted: a reload still shows the failure + reason.
	reloaded, err := LoadSession(db, "SPEC-800")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.NodeStatus("n2") != NodeFailed {
		t.Errorf("status = %q, want failed", reloaded.NodeStatus("n2"))
	}
	if reloaded.Nodes["n2"].Reason != "compile error" {
		t.Errorf("reason = %q", reloaded.Nodes["n2"].Reason)
	}
}

// initRemoteBackedRepo creates a bare "origin", a consumer clone whose local
// `main` is intentionally STALE (one commit behind origin/main), and returns the
// consumer clone path plus the SHA of the fresh tip only present on origin.
func initRemoteBackedRepo(t *testing.T) (clone, freshSHA string) {
	t.Helper()
	ctx := context.Background()
	run := func(dir string, args ...string) string {
		out, err := gitpkg.Run(ctx, dir, args...)
		if err != nil {
			t.Fatalf("git %v in %s: %v", args, dir, err)
		}
		return strings.TrimSpace(out)
	}

	bare := t.TempDir()
	run(bare, "init", "--bare", "-b", "main")

	// Seed the remote with a base commit via a throwaway clone.
	seed := t.TempDir()
	run(".", "clone", bare, seed)
	run(seed, "config", "user.email", "t@t.com")
	run(seed, "config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(seed, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(seed, "add", ".")
	run(seed, "commit", "-m", "base")
	run(seed, "push", "origin", "main")

	// The consumer clone the build will use — its local `main` is at `base`.
	clone = t.TempDir()
	run(".", "clone", bare, clone)
	run(clone, "config", "user.email", "t@t.com")
	run(clone, "config", "user.name", "T")

	// A teammate pushes a fresh commit to origin AFTER the consumer cloned, so
	// the consumer's local `main` is now stale relative to `origin/main`.
	if err := os.WriteFile(filepath.Join(seed, "fresh.txt"), []byte("fresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(seed, "add", ".")
	run(seed, "commit", "-m", "fresh upstream change")
	freshSHA = run(seed, "rev-parse", "HEAD")
	run(seed, "push", "origin", "main")
	return clone, freshSHA
}

// TestProvisionNode_RefreshesStaleBase is the regression guard for the wasted
// build: a root node must be cut from up-to-date origin code, never a stale
// local default branch.
func TestProvisionNode_RefreshesStaleBase(t *testing.T) {
	t.Setenv("SPEC_HOME", t.TempDir())
	repo, freshSHA := initRemoteBackedRepo(t)

	db, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	steps := []PRStep{{Number: 1, Repo: "svc", Description: "root"}}
	session, err := CreateSession(db, "SPEC-802", steps, repo)
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Workspaces: map[string]string{"svc": repo}, MaxParallel: 1}
	srv := NewMCPServer(session, &BuildContext{SpecContent: "# spec"}, db, "", opts)

	res, err := srv.CallTool("spec_provision_node", json.RawMessage(`{"node_id":"n1"}`))
	if err != nil || !res.Success {
		t.Fatalf("provision: %v / %s", err, res.Message)
	}
	var p provisionResult
	if err := json.Unmarshal([]byte(res.Message), &p); err != nil {
		t.Fatalf("payload: %v", err)
	}

	// The root must base on the remote-tracking ref, not local `main`.
	if p.BaseRef != "origin/main" {
		t.Errorf("baseRef = %q, want origin/main (fresh base)", p.BaseRef)
	}
	// The worktree must contain the upstream commit that local `main` lacked —
	// i.e. the branch was genuinely cut from fresh code.
	head, err := gitpkg.Run(context.Background(), p.WorkDir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse worktree HEAD: %v", err)
	}
	if strings.TrimSpace(head) != freshSHA {
		t.Errorf("worktree HEAD = %q, want fresh upstream %q", strings.TrimSpace(head), freshSHA)
	}
	if _, err := os.Stat(filepath.Join(p.WorkDir, "fresh.txt")); err != nil {
		t.Errorf("worktree missing the fresh upstream file — base was stale: %v", err)
	}
}

func TestProvisionNode_UnmappedRepoErrors(t *testing.T) {
	t.Setenv("SPEC_HOME", t.TempDir())
	db, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	steps := []PRStep{{Number: 1, Repo: "ghost", Description: "x"}}
	session, _ := CreateSession(db, "SPEC-801", steps, t.TempDir())
	srv := NewMCPServer(session, &BuildContext{}, db, "", Options{})

	_, err = srv.CallTool("spec_provision_node", json.RawMessage(`{"node_id":"n1"}`))
	if err == nil || !strings.Contains(err.Error(), "workspaces.ghost") {
		t.Errorf("expected actionable unmapped-repo error, got %v", err)
	}
}
