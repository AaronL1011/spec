package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scriptRepo creates a temp git repo and returns a runner for git commands in it.
func scriptRepo(t *testing.T) (string, func(args ...string)) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=alice", "GIT_AUTHOR_EMAIL=alice@test",
			"GIT_COMMITTER_NAME=alice", "GIT_COMMITTER_EMAIL=alice@test",
			"GIT_AUTHOR_DATE=2026-01-02T10:00:00+00:00",
			"GIT_COMMITTER_DATE=2026-01-02T10:00:00+00:00")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q", "-b", "main")
	return dir, run
}

func writeAndCommit(t *testing.T, dir string, run func(args ...string), path, content, message string) {
	t.Helper()
	full := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run("add", "-A")
	run("commit", "-q", "-m", message)
}

func TestLog_OldestFirstWithFieldsAndPatch(t *testing.T) {
	dir, run := scriptRepo(t)
	writeAndCommit(t, dir, run, "specs/SPEC-001.md", "---\nstatus: draft\n---\nbody\n", "feat: scaffold SPEC-001 — First")
	writeAndCommit(t, dir, run, "specs/SPEC-001.md", "---\nstatus: build\n---\nbody\n", "feat: advance SPEC-001 to build")

	entries, err := Log(context.Background(), dir, LogOptions{Path: "specs", WithPatch: true})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("Log returned %d entries, want 2", len(entries))
	}

	first := entries[0]
	if first.Message != "feat: scaffold SPEC-001 — First" {
		t.Errorf("first.Message = %q", first.Message)
	}
	if first.Author != "alice" {
		t.Errorf("first.Author = %q, want alice", first.Author)
	}
	if first.When.IsZero() {
		t.Error("first.When is zero")
	}
	if !strings.Contains(first.Patch, "+status: draft") {
		t.Errorf("first.Patch missing added status line:\n%s", first.Patch)
	}

	second := entries[1]
	if second.Message != "feat: advance SPEC-001 to build" {
		t.Errorf("second.Message = %q", second.Message)
	}
	if !strings.Contains(second.Patch, "-status: draft") || !strings.Contains(second.Patch, "+status: build") {
		t.Errorf("second.Patch missing status transition:\n%s", second.Patch)
	}
}

func TestLog_PathScoping(t *testing.T) {
	dir, run := scriptRepo(t)
	writeAndCommit(t, dir, run, "specs/SPEC-001.md", "---\nstatus: draft\n---\n", "feat: scaffold SPEC-001 — First")
	writeAndCommit(t, dir, run, "README.md", "docs\n", "docs: readme")

	entries, err := Log(context.Background(), dir, LogOptions{Path: "specs"})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Log returned %d entries, want 1 (README commit must be excluded)", len(entries))
	}
	if entries[0].Patch != "" {
		t.Errorf("Patch = %q, want empty without WithPatch", entries[0].Patch)
	}
}

func TestLog_EmptyRepo(t *testing.T) {
	dir, _ := scriptRepo(t)
	// git log fails on a repo with no commits — Log must surface the error.
	if _, err := Log(context.Background(), dir, LogOptions{}); err == nil {
		t.Fatal("Log on empty repo: expected error")
	}
}

func TestParseLog_SkipsMalformedRecords(t *testing.T) {
	out := logRecordSep + "sha1" + logFieldSep + "alice" + logFieldSep + "not-a-date" + logFieldSep + "subject" +
		logRecordSep + "sha2" + logFieldSep + "bob" + logFieldSep + "2026-01-02T10:00:00+00:00" + logFieldSep + "feat: ok\n\npatchline"
	entries := parseLog(out)
	if len(entries) != 1 {
		t.Fatalf("parseLog returned %d entries, want 1", len(entries))
	}
	if entries[0].SHA != "sha2" || entries[0].Message != "feat: ok" || entries[0].Patch != "patchline" {
		t.Errorf("parseLog entry = %+v", entries[0])
	}
}
