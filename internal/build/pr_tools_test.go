package build

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/adapter/noop"
)

// fakeRepo records draft-PR and retarget calls and hands out incrementing PR
// numbers. It embeds noop.Repo so it satisfies the full RepoAdapter interface.
type fakeRepo struct {
	noop.Repo
	opened    []openCall
	retargets []retargetCall
	next      int
}

type openCall struct{ repo, head, base, title string }
type retargetCall struct {
	repo   string
	number int
	base   string
}

func (f *fakeRepo) OpenDraftPR(_ context.Context, repo, head, base, title, _ string) (int, string, error) {
	f.next++
	f.opened = append(f.opened, openCall{repo, head, base, title})
	return f.next, fmt.Sprintf("https://github.com/o/%s/pull/%d", repo, f.next), nil
}

func (f *fakeRepo) SetPRBase(_ context.Context, repo string, number int, base string) error {
	f.retargets = append(f.retargets, retargetCall{repo, number, base})
	return nil
}

func provision(t *testing.T, srv *MCPServer, id string) {
	t.Helper()
	if _, err := srv.CallTool("spec_provision_node", json.RawMessage(fmt.Sprintf(`{"node_id":%q}`, id))); err != nil {
		t.Fatalf("provision %s: %v", id, err)
	}
}

func openPR(t *testing.T, srv *MCPServer, id string) prResult {
	t.Helper()
	res, err := srv.CallTool("spec_open_pr", json.RawMessage(fmt.Sprintf(`{"node_id":%q}`, id)))
	if err != nil {
		t.Fatalf("open_pr %s: %v", id, err)
	}
	if !res.Success {
		t.Fatalf("open_pr %s failed: %s", id, res.Message)
	}
	var pr prResult
	if err := json.Unmarshal([]byte(res.Message), &pr); err != nil {
		t.Fatalf("pr payload: %v", err)
	}
	return pr
}

// TestOpenPR_DraftWithBaseChaining verifies spec_open_pr opens a draft per node
// with the base chained to the node's recorded base ref (root → default branch,
// child → parent branch), records the PR in the ledger, and is idempotent.
func TestOpenPR_DraftWithBaseChaining(t *testing.T) {
	srv, db := newDAGServer(t) // n1 → n2 in repo "svc"
	fake := &fakeRepo{}
	srv.WithRepo(fake)

	provision(t, srv, "n1")
	provision(t, srv, "n2")

	pr1 := openPR(t, srv, "n1")
	pr2 := openPR(t, srv, "n2")

	// n1 is a root → base is the repo default branch (main).
	if pr1.Base != "main" {
		t.Errorf("n1 PR base = %q, want main", pr1.Base)
	}
	// n2 depends on n1 → base is n1's branch (stack chaining).
	n1Branch := srv.session.node("n1").Branch
	if pr2.Base != n1Branch {
		t.Errorf("n2 PR base = %q, want n1 branch %q", pr2.Base, n1Branch)
	}
	if len(fake.opened) != 2 {
		t.Fatalf("expected 2 draft PRs opened, got %d", len(fake.opened))
	}

	// Recorded in the ledger.
	if srv.session.node("n1").PRURL != pr1.URL || srv.session.node("n1").PRNumber != pr1.Number {
		t.Error("n1 PR not recorded in ledger")
	}

	// Idempotent: re-opening returns the same PR without a new API call.
	again := openPR(t, srv, "n1")
	if again.URL != pr1.URL || len(fake.opened) != 2 {
		t.Errorf("re-open should be idempotent: url=%q opened=%d", again.URL, len(fake.opened))
	}

	// Persisted across reload.
	reloaded, _ := LoadSession(db, "SPEC-800")
	if reloaded.Nodes["n2"].PRURL != pr2.URL {
		t.Error("PR URL should survive reload")
	}
}

func TestLinkPRs_RetargetsStack(t *testing.T) {
	srv, _ := newDAGServer(t)
	fake := &fakeRepo{}
	srv.WithRepo(fake)

	provision(t, srv, "n1")
	provision(t, srv, "n2")
	openPR(t, srv, "n1")
	openPR(t, srv, "n2")

	res, err := srv.CallTool("spec_link_prs", json.RawMessage(`{}`))
	if err != nil || !res.Success {
		t.Fatalf("link_prs: %v / %s", err, res.Message)
	}
	if len(fake.retargets) != 2 {
		t.Fatalf("expected 2 retargets, got %d", len(fake.retargets))
	}

	// Single-node retarget to an explicit base.
	fake.retargets = nil
	if _, err := srv.CallTool("spec_link_prs", json.RawMessage(`{"node_id":"n2","base":"main"}`)); err != nil {
		t.Fatal(err)
	}
	if len(fake.retargets) != 1 || fake.retargets[0].base != "main" {
		t.Errorf("expected single retarget to main, got %v", fake.retargets)
	}
}

// TestRenderPRTitle covers the pr_title template substitution, including the
// whitespace collapse that keeps a title tidy when a slot (e.g. epic) is empty.
func TestRenderPRTitle(t *testing.T) {
	cases := []struct{ name, tmpl, typ, epic, desc, want string }{
		{"conventional", "{type}: {epic} {desc}", "feat", "PLAT-1", "do it", "feat: PLAT-1 do it"},
		{"empty epic collapses", "{type}: {epic} {desc}", "fix", "", "patch", "fix: patch"},
		{"bracketed epic", "[{epic}] {desc}", "", "EPIC-9", "ship", "[EPIC-9] ship"},
		{"empty template falls back", "", "feat", "E", "d", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := renderPRTitle(c.tmpl, c.typ, c.epic, c.desc); got != c.want {
				t.Errorf("renderPRTitle = %q, want %q", got, c.want)
			}
		})
	}
}

// TestOpenPR_AppliesRepoTitleConvention verifies spec_open_pr renders the node
// repo's pr_title convention from type+summary, with {epic} taken from the
// spec's epic_key — the contract pr-finisher relies on.
func TestOpenPR_AppliesRepoTitleConvention(t *testing.T) {
	srv, _ := newDAGServer(t)
	fake := &fakeRepo{}
	srv.WithRepo(fake)

	repoPath := srv.opts.Workspaces["svc"]
	skillsDir := filepath.Join(repoPath, ".agents", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	registry := `version: "1"
conventions:
  pr_title: "{type}: {epic} {desc}"
skills:
  - name: svc-layer
    kind: layer
    path: svc-layer
    applies_to: ["svc"]
`
	writeSkill(t, skillsDir, "svc-layer", "# svc")
	if err := os.WriteFile(filepath.Join(skillsDir, "registry.yaml"), []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}

	specPath := filepath.Join(t.TempDir(), "SPEC-800.md")
	if err := os.WriteFile(specPath, []byte("---\nid: SPEC-800\nepic_key: PLAT-42\n---\n# Spec\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv.specPath = specPath

	provision(t, srv, "n1")
	res, err := srv.CallTool("spec_open_pr", json.RawMessage(`{"node_id":"n1","type":"feat","summary":"add the widget"}`))
	if err != nil || !res.Success {
		t.Fatalf("open_pr: %v / %s", err, res.Message)
	}
	if len(fake.opened) != 1 {
		t.Fatalf("want 1 PR opened, got %d", len(fake.opened))
	}
	if got, want := fake.opened[0].title, "feat: PLAT-42 add the widget"; got != want {
		t.Errorf("PR title = %q, want %q", got, want)
	}

	// An explicit title overrides the convention.
	provision(t, srv, "n2")
	if _, err := srv.CallTool("spec_open_pr", json.RawMessage(`{"node_id":"n2","type":"feat","summary":"ignored","title":"Custom title"}`)); err != nil {
		t.Fatal(err)
	}
	if got := fake.opened[1].title; got != "Custom title" {
		t.Errorf("explicit title = %q, want %q", got, "Custom title")
	}
}

func TestOpenPR_NoRepoAdapter(t *testing.T) {
	srv, _ := newDAGServer(t)
	provision(t, srv, "n1")
	// No WithRepo → guarded.
	res, err := srv.CallTool("spec_open_pr", json.RawMessage(`{"node_id":"n1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Success || !strings.Contains(res.Message, "no repo adapter") {
		t.Errorf("expected graceful no-adapter result, got %q", res.Message)
	}
}
