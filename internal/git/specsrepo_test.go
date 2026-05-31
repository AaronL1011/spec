package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSectionOverlap_DisjointSections(t *testing.T) {
	ctx := context.Background()
	dir := initSectionRepo(t)

	base, _ := RevParse(ctx, dir, "HEAD")

	// Upstream branch edits §7; our HEAD edits §1 — disjoint, no conflict.
	remoteRef := makeUpstreamEdit(t, dir, base, "## 7. Technical Implementation", "upstream tech change")
	writeSpec(t, dir, sectionDoc("## 1. Problem Statement", "our problem change", "## 7. Technical Implementation", "orig tech"))
	if err := Commit(ctx, dir, "our edit §1"); err != nil {
		t.Fatal(err)
	}

	ourFiles, _ := CommittedFiles(ctx, dir, "HEAD")
	upstream, _ := DiffNameOnly(ctx, dir, base, remoteRef)
	conflict := sectionOverlap(ctx, dir, ourFiles, upstream, base, remoteRef)
	if conflict != "" {
		t.Errorf("disjoint section edits should not conflict, got %q", conflict)
	}
}

func TestSectionOverlap_SameSection(t *testing.T) {
	ctx := context.Background()
	dir := initSectionRepo(t)

	base, _ := RevParse(ctx, dir, "HEAD")

	// Both sides edit §1 — genuine same-section collision.
	remoteRef := makeUpstreamEdit(t, dir, base, "## 1. Problem Statement", "upstream problem change")
	writeSpec(t, dir, sectionDoc("## 1. Problem Statement", "our problem change", "## 7. Technical Implementation", "orig tech"))
	if err := Commit(ctx, dir, "our edit §1"); err != nil {
		t.Fatal(err)
	}

	ourFiles, _ := CommittedFiles(ctx, dir, "HEAD")
	upstream, _ := DiffNameOnly(ctx, dir, base, remoteRef)
	conflict := sectionOverlap(ctx, dir, ourFiles, upstream, base, remoteRef)
	if conflict == "" {
		t.Fatal("same-section edits should conflict")
	}
	if !containsSubstring(conflict, "problem_statement") {
		t.Errorf("conflict should name the section, got %q", conflict)
	}
}

func TestSectionOverlap_NonSpecFileWholeFile(t *testing.T) {
	ours := []string{"spec.config.yaml"}
	upstream := []string{"spec.config.yaml"}
	conflict := sectionOverlap(context.Background(), t.TempDir(), ours, upstream, "x", "y")
	if conflict != "spec.config.yaml" {
		t.Errorf("non-spec overlap should be whole-file, got %q", conflict)
	}
}

// --- section-test helpers ---

func sectionDoc(h1, c1, h7, c7 string) string {
	return "# SPEC-001\n\n" + h1 + "\n\n" + c1 + "\n\n" + h7 + "\n\n" + c7 + "\n"
}

func writeSpec(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "specs", "SPEC-001.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// initSectionRepo creates a non-bare repo with a two-section spec committed.
func initSectionRepo(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "t@t.com"}, {"config", "user.name", "T"}} {
		if _, err := Run(ctx, dir, args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSpec(t, dir, sectionDoc("## 1. Problem Statement", "orig problem", "## 7. Technical Implementation", "orig tech"))
	if err := Commit(ctx, dir, "initial"); err != nil {
		t.Fatal(err)
	}
	return dir
}

// makeUpstreamEdit creates a sibling commit off base that edits one section
// and returns a ref pointing at it (simulating origin/<branch> advancing).
func makeUpstreamEdit(t *testing.T, dir, base, heading, content string) string {
	t.Helper()
	ctx := context.Background()
	if _, err := Run(ctx, dir, "checkout", "-b", "upstream", base); err != nil {
		t.Fatal(err)
	}
	var doc string
	if heading == "## 1. Problem Statement" {
		doc = sectionDoc(heading, content, "## 7. Technical Implementation", "orig tech")
	} else {
		doc = sectionDoc("## 1. Problem Statement", "orig problem", heading, content)
	}
	writeSpec(t, dir, doc)
	if err := Commit(ctx, dir, "upstream edit"); err != nil {
		t.Fatal(err)
	}
	ref, _ := RevParse(ctx, dir, "HEAD")
	if _, err := Run(ctx, dir, "checkout", "-"); err != nil {
		// fall back to detaching base
		if _, err := Run(ctx, dir, "checkout", base); err != nil {
			t.Fatal(err)
		}
	}
	return ref
}

// containsSubstring is a test helper (strings.Contains without importing strings).
func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestRebaseAbort_NoRebaseInProgress(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	if _, err := Run(ctx, dir, "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, dir, "config", "user.email", "test@test.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, dir, "config", "user.name", "Test"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, dir, "initial"); err != nil {
		t.Fatal(err)
	}

	// Should not panic or leave repo in bad state
	RebaseAbort(ctx, dir)

	// Verify repo is still usable
	has, err := HasChanges(ctx, dir)
	if err != nil {
		t.Fatalf("HasChanges after RebaseAbort: %v", err)
	}
	if has {
		t.Error("unexpected changes after RebaseAbort")
	}
}

func TestCommittedFiles(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	if _, err := Run(ctx, dir, "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, dir, "config", "user.email", "test@test.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, dir, "config", "user.name", "Test"); err != nil {
		t.Fatal(err)
	}

	// Create initial commit
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, dir, "initial"); err != nil {
		t.Fatal(err)
	}

	// Create a second commit touching two files
	if err := os.MkdirAll(filepath.Join(dir, "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "specs", "SPEC-042.md"), []byte("# spec"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, dir, "add spec and b"); err != nil {
		t.Fatal(err)
	}

	files, err := CommittedFiles(ctx, dir, "HEAD")
	if err != nil {
		t.Fatalf("CommittedFiles: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(files), files)
	}

	// Check both files are present (order may vary)
	found := map[string]bool{}
	for _, f := range files {
		found[f] = true
	}
	if !found["b.txt"] {
		t.Errorf("expected b.txt in committed files: %v", files)
	}
	if !found["specs/SPEC-042.md"] {
		t.Errorf("expected specs/SPEC-042.md in committed files: %v", files)
	}
}

func TestDiffNameOnly(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	if _, err := Run(ctx, dir, "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, dir, "config", "user.email", "test@test.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, dir, "config", "user.name", "Test"); err != nil {
		t.Fatal(err)
	}

	// Commit 1
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, dir, "first"); err != nil {
		t.Fatal(err)
	}
	ref1, _ := RevParse(ctx, dir, "HEAD")

	// Commit 2
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, dir, "second"); err != nil {
		t.Fatal(err)
	}
	ref2, _ := RevParse(ctx, dir, "HEAD")

	files, err := DiffNameOnly(ctx, dir, ref1, ref2)
	if err != nil {
		t.Fatalf("DiffNameOnly: %v", err)
	}
	if len(files) != 1 || files[0] != "b.txt" {
		t.Errorf("expected [b.txt], got %v", files)
	}

	// Same ref should return nil
	files, err = DiffNameOnly(ctx, dir, ref2, ref2)
	if err != nil {
		t.Fatalf("DiffNameOnly same ref: %v", err)
	}
	if files != nil {
		t.Errorf("same ref should return nil, got %v", files)
	}
}

func TestHasUnpushedCommits(t *testing.T) {
	ctx := context.Background()

	// Set up a "remote" repo
	remote := t.TempDir()
	if _, err := Run(ctx, remote, "init", "--bare"); err != nil {
		t.Fatal(err)
	}

	// Clone it
	local := filepath.Join(t.TempDir(), "local")
	if err := Clone(ctx, remote, local); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, local, "config", "user.email", "test@test.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, local, "config", "user.name", "Test"); err != nil {
		t.Fatal(err)
	}

	// Create initial commit and push
	if err := os.WriteFile(filepath.Join(local, "test.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, local, "initial"); err != nil {
		t.Fatal(err)
	}
	branch, _ := CurrentBranch(ctx, local)
	if err := Push(ctx, local, branch); err != nil {
		t.Fatal(err)
	}

	// Should have no unpushed commits
	has, err := HasUnpushedCommits(ctx, local, "origin/"+branch)
	if err != nil {
		t.Fatalf("HasUnpushedCommits: %v", err)
	}
	if has {
		t.Error("expected no unpushed commits")
	}

	// Make a local commit
	if err := os.WriteFile(filepath.Join(local, "test.txt"), []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, local, "unpushed"); err != nil {
		t.Fatal(err)
	}

	has, err = HasUnpushedCommits(ctx, local, "origin/"+branch)
	if err != nil {
		t.Fatalf("HasUnpushedCommits: %v", err)
	}
	if !has {
		t.Error("expected unpushed commits")
	}
}
