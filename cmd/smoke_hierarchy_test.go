package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aaronl1011/spec/internal/markdown"
)

// hierarchyEnv builds a sandbox with one initiative and one standalone spec.
func hierarchyEnv(t *testing.T) *smokeEnv {
	t.Helper()
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-004", title: "API rate limiting", status: "build", author: "Dev"}, "## TL;DR\nvision\n")
	e.writeSpec(specFixture{id: "SPEC-009", title: "Token bucket", status: "draft", author: "Dev"}, "## TL;DR\nslice\n")
	e.initSpecsGit()
	return e
}

// specMeta reads a spec's frontmatter out of the sandboxed clone.
func specMeta(t *testing.T, e *smokeEnv, specID string) *markdown.SpecMeta {
	t.Helper()
	meta, err := markdown.ReadMeta(filepath.Join(e.specsDirPath(), specID+".md"))
	if err != nil {
		t.Fatalf("reading %s: %v", specID, err)
	}
	return meta
}

func TestSmoke_LinkParent(t *testing.T) {
	e := hierarchyEnv(t)

	out, err := e.runSpec("link", "SPEC-009", "--parent", "SPEC-004")
	if err != nil {
		t.Fatalf("link --parent: %v", err)
	}
	if !strings.Contains(out, "slice of SPEC-004") {
		t.Errorf("output = %q, want confirmation of the link", out)
	}
	if got := specMeta(t, e, "SPEC-009").Parent; got != "SPEC-004" {
		t.Errorf("parent = %q, want SPEC-004", got)
	}
}

func TestSmoke_LinkParentDetach(t *testing.T) {
	e := hierarchyEnv(t)
	if _, err := e.runSpec("link", "SPEC-009", "--parent", "SPEC-004"); err != nil {
		t.Fatalf("link: %v", err)
	}

	out, err := e.runSpec("link", "SPEC-009", "--parent", "")
	if err != nil {
		t.Fatalf("detach: %v", err)
	}
	if !strings.Contains(out, "Detached SPEC-009") {
		t.Errorf("output = %q, want detach confirmation", out)
	}
	if got := specMeta(t, e, "SPEC-009").Parent; got != "" {
		t.Errorf("parent = %q, want empty after detach", got)
	}
}

// Every link-time refusal must name what to do next.
func TestSmoke_LinkParentRefusals(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(e *smokeEnv)
		child    string
		parent   string
		wantText string
	}{
		{
			name:     "unknown parent",
			child:    "SPEC-009",
			parent:   "SPEC-999",
			wantText: "parent spec not found",
		},
		{
			name:     "self parent",
			child:    "SPEC-009",
			parent:   "SPEC-009",
			wantText: "cannot be its own parent",
		},
		{
			name: "depth three",
			setup: func(e *smokeEnv) {
				e.writeSpec(specFixture{id: "SPEC-010", title: "Grandchild", status: "draft", author: "Dev"}, "## TL;DR\nx\n")
			},
			child:    "SPEC-010",
			parent:   "SPEC-009", // itself linked to SPEC-004 by the test body
			wantText: "two levels deep",
		},
		{
			name: "a spec with slices cannot gain a parent",
			setup: func(e *smokeEnv) {
				e.writeSpec(specFixture{id: "SPEC-020", title: "Other", status: "draft", author: "Dev"}, "## TL;DR\nx\n")
			},
			child:    "SPEC-004",
			parent:   "SPEC-020",
			wantText: "already has slices of its own",
		},
		{
			name: "terminal parent names the escape",
			setup: func(e *smokeEnv) {
				e.writeSpec(specFixture{id: "SPEC-030", title: "Closed", status: "done", author: "Dev"}, "## TL;DR\nx\n")
			},
			child:    "SPEC-009",
			parent:   "SPEC-030",
			wantText: "spec revert SPEC-030",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newSmokeEnv(t)
			e.writeUserConfig("engineer")
			e.writeTeamConfig()
			e.writeSpec(specFixture{id: "SPEC-004", title: "Initiative", status: "build", author: "Dev"}, "## TL;DR\nx\n")
			e.writeSpec(specFixture{id: "SPEC-009", title: "Slice", status: "draft", author: "Dev", parent: "SPEC-004"}, "## TL;DR\nx\n")
			if tt.setup != nil {
				tt.setup(e)
			}
			e.initSpecsGit()

			_, err := e.runSpec("link", tt.child, "--parent", tt.parent)
			if err == nil {
				t.Fatalf("expected a refusal linking %s to %s", tt.child, tt.parent)
			}
			if !strings.Contains(err.Error(), tt.wantText) {
				t.Errorf("error = %q, want it to mention %q", err, tt.wantText)
			}
		})
	}
}

func TestSmoke_StatusShowsBothDirections(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-004", title: "API rate limiting", status: "build", author: "Dev"}, "## TL;DR\nx\n")
	e.writeSpec(specFixture{id: "SPEC-009", title: "Token bucket", status: "done", author: "Dev", parent: "SPEC-004"}, "## TL;DR\nx\n")
	e.writeSpec(specFixture{id: "SPEC-010", title: "Redis backend", status: "build", author: "Dev", parent: "SPEC-004"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	out, err := e.runSpec("status", "SPEC-004")
	if err != nil {
		t.Fatalf("status initiative: %v", err)
	}
	if !strings.Contains(out, "Slices: 1/2 complete") {
		t.Errorf("initiative status = %q, want the slice rollup", out)
	}
	if !strings.Contains(out, "SPEC-009") || !strings.Contains(out, "SPEC-010") {
		t.Errorf("initiative status should list its slices:\n%s", out)
	}

	out, err = e.runSpec("status", "SPEC-009")
	if err != nil {
		t.Fatalf("status slice: %v", err)
	}
	if !strings.Contains(out, "Parent: SPEC-004 — API rate limiting") {
		t.Errorf("slice status = %q, want the parent line", out)
	}
}

// The JSON shape is a scripting contract: a standalone spec must emit exactly
// what it always did.
func TestSmoke_StatusJSONShapeStableForStandaloneSpec(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-001", title: "Standalone", status: "build", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	out, err := e.runSpec("status", "SPEC-001", "--json")
	if err != nil {
		t.Fatalf("status --json: %v", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("status --json is not valid JSON (%v): %s", err, out)
	}
	for _, key := range []string{"parent", "children"} {
		if _, ok := payload[key]; ok {
			t.Errorf("standalone spec emitted %q; both fields must be omitempty", key)
		}
	}
}

func TestSmoke_StatusJSONCarriesHierarchy(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-004", title: "Initiative", status: "build", author: "Dev"}, "## TL;DR\nx\n")
	e.writeSpec(specFixture{id: "SPEC-009", title: "Slice", status: "done", author: "Dev", parent: "SPEC-004"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	out, err := e.runSpec("status", "SPEC-004", "--json")
	if err != nil {
		t.Fatalf("status --json: %v", err)
	}
	var payload struct {
		Children []struct {
			ID       string `json:"id"`
			Complete bool   `json:"complete"`
		} `json:"children"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decoding: %v\n%s", err, out)
	}
	if len(payload.Children) != 1 || payload.Children[0].ID != "SPEC-009" || !payload.Children[0].Complete {
		t.Errorf("children = %+v, want SPEC-009 marked complete", payload.Children)
	}
}

func TestSmoke_ListParent(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-004", title: "Initiative", status: "build", author: "Dev"}, "## TL;DR\nx\n")
	e.writeSpec(specFixture{id: "SPEC-009", title: "Slice one", status: "done", author: "Dev", parent: "SPEC-004"}, "## TL;DR\nx\n")
	e.writeSpec(specFixture{id: "SPEC-014", title: "Unrelated", status: "draft", author: "Dev"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	out, err := e.runSpec("list", "--parent", "SPEC-004")
	if err != nil {
		t.Fatalf("list --parent: %v", err)
	}
	if !strings.Contains(out, "SPEC-009") {
		t.Errorf("list --parent should show the slice:\n%s", out)
	}
	if strings.Contains(out, "SPEC-014") {
		t.Errorf("list --parent leaked an unrelated spec:\n%s", out)
	}

	out, err = e.runSpec("list", "--parent", "SPEC-014")
	if err != nil {
		t.Fatalf("list --parent on a childless spec: %v", err)
	}
	if !strings.Contains(out, "no slices") {
		t.Errorf("a childless spec should say so rather than print an empty list:\n%s", out)
	}
}

// A dangling parent makes every downstream query undefined, so validate fails.
func TestSmoke_ValidateDanglingParentIsAnError(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-009", title: "Slice", status: "draft", author: "Dev", parent: "SPEC-999"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	out, err := e.runSpec("validate", "SPEC-009")
	if err == nil {
		t.Fatalf("expected validation to fail on a dangling parent:\n%s", out)
	}
	if !strings.Contains(out, "parent_exists") {
		t.Errorf("validate output should name the rule that fired:\n%s", out)
	}
}

// A parent file that exists but will not parse is unverifiable, not broken:
// the child validates with a warning naming the file to fix, and is never
// blocked over someone else's typo.
func TestSmoke_ValidateUnreadableParentIsAWarning(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-009", title: "Slice", status: "draft", author: "Dev", parent: "SPEC-004"}, "## TL;DR\nx\n")
	mangled := "---\nid: SPEC-004\ntitle: [unclosed\nstatus: build\n---\n\n# mangled\n"
	if err := os.WriteFile(filepath.Join(e.specsDirPath(), "SPEC-004.md"), []byte(mangled), 0o644); err != nil {
		t.Fatal(err)
	}
	e.initSpecsGit()

	out, err := e.runSpec("validate", "SPEC-009")
	if err != nil {
		t.Fatalf("an unreadable parent must not fail validation: %v\n%s", err, out)
	}
	if !strings.Contains(out, "parent_unreadable") {
		t.Errorf("validate should warn about the unreadable parent:\n%s", out)
	}
	if !strings.Contains(out, "SPEC-004.md") {
		t.Errorf("the warning should name the file to fix:\n%s", out)
	}
}

// An initiative that closed last week must not retroactively wedge a slice
// someone is working on today, so a terminal parent is a warning.
func TestSmoke_ValidateTerminalParentIsAWarning(t *testing.T) {
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.writeSpec(specFixture{id: "SPEC-004", title: "Closed initiative", status: "done", author: "Dev"}, "## TL;DR\nx\n")
	e.writeSpec(specFixture{id: "SPEC-009", title: "Slice", status: "draft", author: "Dev", parent: "SPEC-004"}, "## TL;DR\nx\n")
	e.initSpecsGit()

	out, err := e.runSpec("validate", "SPEC-009")
	if err != nil {
		t.Fatalf("a terminal parent must not fail validation: %v\n%s", err, out)
	}
	if !strings.Contains(out, "parent_terminal") {
		t.Errorf("validate should warn about the terminal parent:\n%s", out)
	}
}

func TestSmoke_NewWithParent(t *testing.T) {
	e := hierarchyEnv(t)

	out, err := e.runSpec("new", "--title", "Redis backend", "--parent", "SPEC-004")
	if err != nil {
		t.Fatalf("new --parent: %v", err)
	}
	if !strings.Contains(out, "Parent: SPEC-004") {
		t.Errorf("output = %q, want the parent reported", out)
	}

	entries, err := os.ReadDir(e.specsDirPath())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		meta, err := markdown.ReadMeta(filepath.Join(e.specsDirPath(), entry.Name()))
		if err != nil {
			continue
		}
		if meta.Title == "Redis backend" {
			found = true
			if meta.Parent != "SPEC-004" {
				t.Errorf("scaffolded spec parent = %q, want SPEC-004", meta.Parent)
			}
		}
	}
	if !found {
		t.Error("the scaffolded spec was not written")
	}
}

// Refusing before the ID claim matters: the counter ref only moves forward, so
// a refusal after claiming would burn a spec number for nothing.
func TestSmoke_NewWithIneligibleParentIsRefused(t *testing.T) {
	e := hierarchyEnv(t)

	_, err := e.runSpec("new", "--title", "Doomed", "--parent", "SPEC-999")
	if err == nil {
		t.Fatal("expected a refusal for an unknown parent")
	}
	if !strings.Contains(err.Error(), "parent spec not found") {
		t.Errorf("error = %q, want the parent-not-found refusal", err)
	}
}
