package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The initiative block must reach a non-MCP agent's consolidated context file,
// ahead of the spec: an agent should read why the work exists before reading
// what to build.
func TestWriteContextFile_IncludesInheritedContext(t *testing.T) {
	inherited := "<!-- inherited from SPEC-004 — read-only context, do not implement -->\n" +
		"## Initiative context — SPEC-004: API rate limiting\n\n### TL;DR\n\nFairness.\n\n" +
		"<!-- end inherited context -->\n"

	out := filepath.Join(t.TempDir(), "context.md")
	err := WriteContextFile(&BuildContext{
		SpecContent:      "# SPEC-009 — Token bucket limiter\n",
		InheritedContext: inherited,
	}, out)
	if err != nil {
		t.Fatalf("WriteContextFile: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "Initiative context — SPEC-004") {
		t.Errorf("context file is missing the inherited block:\n%s", got)
	}
	if idx, specIdx := strings.Index(got, "Initiative context"), strings.Index(got, "## Spec"); idx > specIdx {
		t.Errorf("inherited context should precede the spec (%d > %d)", idx, specIdx)
	}
}

func TestWriteContextFile_StandaloneSpecGainsNothing(t *testing.T) {
	out := filepath.Join(t.TempDir(), "context.md")
	if err := WriteContextFile(&BuildContext{SpecContent: "# SPEC-014\n"}, out); err != nil {
		t.Fatalf("WriteContextFile: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "Initiative context") {
		t.Errorf("a standalone spec must not gain an initiative block:\n%s", data)
	}
}
