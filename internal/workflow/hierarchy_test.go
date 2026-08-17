package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSpecFile drops an arbitrary spec document beside the one under test,
// so a transition can be exercised against a healthy, dangling, or corrupt
// parent in the same directory.
func writeSpecFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

// writeChildSpec writes SPEC-001 at draft with a filled problem statement and
// the given parent, satisfying the review gate so only hierarchy can refuse.
func writeChildSpec(t *testing.T, parent string) (path, dir string) {
	t.Helper()
	dir = t.TempDir()
	path = filepath.Join(dir, "SPEC-001.md")
	content := "---\n" +
		"id: SPEC-001\n" +
		"title: Child Spec\n" +
		"status: draft\n" +
		"version: 0.1.0\n" +
		"author: Tester\n" +
		"cycle: C1\n" +
		"parent: " + parent + "\n" +
		"revert_count: 0\n" +
		"created: 2026-01-01\n" +
		"updated: 2026-01-01\n" +
		"---\n\n" +
		"# SPEC-001 - Child Spec\n\n" +
		"## 1. Problem Statement\nA real problem.\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing child spec: %v", err)
	}
	return path, dir
}

func TestAdvance_DanglingParent_Blocks(t *testing.T) {
	path, dir := writeChildSpec(t, "SPEC-999")

	_, err := Advance(context.Background(), deps("pm"), AdvanceInput{
		SpecID: "SPEC-001", SpecPath: path, SpecDir: dir,
	})
	if err == nil {
		t.Fatal("a dangling parent must block the advance")
	}
	if !strings.Contains(err.Error(), "parent link is broken") {
		t.Errorf("error = %q, want the broken-parent refusal", err)
	}
	if got := statusOf(t, path); got != "draft" {
		t.Errorf("a refused advance must not mutate the spec; status = %q", got)
	}
}

// The reason corrupt refs exist: a parent file someone else mangled must
// degrade to a warning on the child, never block its transition. "Does not
// exist" was a lie the old skip-on-parse-failure scan told about a file that
// was right there.
func TestAdvance_UnreadableParent_DoesNotBlock(t *testing.T) {
	path, dir := writeChildSpec(t, "SPEC-004")
	writeSpecFile(t, dir, "SPEC-004", "---\nid: SPEC-004\ntitle: [unclosed\nstatus: build\n---\n\n# mangled\n")

	res, err := Advance(context.Background(), deps("pm"), AdvanceInput{
		SpecID: "SPEC-001", SpecPath: path, SpecDir: dir,
	})
	if err != nil {
		t.Fatalf("an unreadable parent must not block the child's advance: %v", err)
	}
	if res.NewStage != "review" {
		t.Errorf("advanced to %q, want review", res.NewStage)
	}
}
