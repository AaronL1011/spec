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
