package cmd

import (
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
