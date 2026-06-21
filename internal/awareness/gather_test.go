package awareness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aaronl1011/spec/internal/config"
)

// writeSpec writes a spec markdown file with the given frontmatter body into
// the specs dir, creating parent directories as needed.
func writeSpec(t *testing.T, specsDir, name, frontmatter string) {
	t.Helper()
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatalf("mkdir specs: %v", err)
	}
	content := "---\n" + frontmatter + "\n---\n\n# Body\n"
	if err := os.WriteFile(filepath.Join(specsDir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write spec %s: %v", name, err)
	}
}

// gatherFixture stands up a HOME-rooted specs repo layout that Gather can read.
// ListSpecFiles resolves files from ~/.spec/repos/<owner>/<repo>/specs (the git
// ls-tree path fails on a non-git dir and falls back to a filesystem listing),
// and Gather reads file contents from rc.SpecsRepoDir, so the two must point at
// the same directory. It returns a ResolvedConfig wired to that layout.
func gatherFixture(t *testing.T, userName, userRole string) (*config.ResolvedConfig, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	owner, repo := "acme", "specs"
	specsDir := filepath.Join(home, ".spec", "repos", owner, repo, "specs")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	team := &config.TeamConfig{}
	team.SpecsRepo = config.SpecsRepoConfig{Owner: owner, Repo: repo, Branch: "main"}

	user := &config.UserConfig{}
	user.User.Name = userName
	user.User.OwnerRole = userRole

	rc := &config.ResolvedConfig{
		Team:         team,
		User:         user,
		SpecsRepoDir: specsDir,
	}
	return rc, specsDir
}

func TestGather_NoTeam_ReturnsEmpty(t *testing.T) {
	rc := &config.ResolvedConfig{}
	got, err := Gather(rc)
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	if got == nil || *got != (Summary{}) {
		t.Errorf("Gather() = %+v, want zero Summary", got)
	}
}

func TestGather_CountsOwnedSpecsBlockedAndInProgress(t *testing.T) {
	rc, specsDir := gatherFixture(t, "Alice", "engineer")

	// Owned + in build → SpecsTotal, SpecsInProgress.
	writeSpec(t, specsDir, "SPEC-001.md", "id: SPEC-001\nstatus: build\nauthor: Alice")
	// Owned + engineering → SpecsTotal, SpecsInProgress.
	writeSpec(t, specsDir, "SPEC-002.md", "id: SPEC-002\nstatus: engineering\nauthor: alice")
	// Owned + a blocked step → SpecsTotal, SpecsBlocked (not in-progress: status draft).
	writeSpec(t, specsDir, "SPEC-003.md",
		"id: SPEC-003\nstatus: draft\nauthor: Alice\nsteps:\n  - repo: api\n    description: x\n    status: blocked")
	// Not owned → must not touch owner counters.
	writeSpec(t, specsDir, "SPEC-004.md", "id: SPEC-004\nstatus: build\nauthor: Bob")

	got, err := Gather(rc)
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	if got.SpecsTotal != 3 {
		t.Errorf("SpecsTotal = %d, want 3 (case-insensitive author match, Bob excluded)", got.SpecsTotal)
	}
	if got.SpecsInProgress != 2 {
		t.Errorf("SpecsInProgress = %d, want 2 (build + engineering)", got.SpecsInProgress)
	}
	if got.SpecsBlocked != 1 {
		t.Errorf("SpecsBlocked = %d, want 1", got.SpecsBlocked)
	}
}

func TestGather_BlockedCountedOncePerSpec(t *testing.T) {
	rc, specsDir := gatherFixture(t, "Alice", "engineer")
	// Two blocked steps in one spec must increment SpecsBlocked exactly once:
	// the loop breaks on the first blocked step.
	writeSpec(t, specsDir, "SPEC-010.md",
		"id: SPEC-010\nstatus: build\nauthor: Alice\nsteps:\n"+
			"  - repo: api\n    description: a\n    status: blocked\n"+
			"  - repo: web\n    description: b\n    status: blocked")

	got, err := Gather(rc)
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	if got.SpecsBlocked != 1 {
		t.Errorf("SpecsBlocked = %d, want 1 (counted once per spec, not per step)", got.SpecsBlocked)
	}
}

func TestGather_PendingReviewMatchesByRole(t *testing.T) {
	rc, specsDir := gatherFixture(t, "Mike", "tl")

	// Pending review naming the user's role → counts even though Mike is not
	// the author (review needs are independent of ownership).
	writeSpec(t, specsDir, "SPEC-020.md",
		"id: SPEC-020\nstatus: tl-review\nauthor: Sara\nreview:\n  status: pending\n  reviewers:\n    - tl")
	// Pending review naming the user directly by handle.
	writeSpec(t, specsDir, "SPEC-021.md",
		"id: SPEC-021\nstatus: tl-review\nauthor: Sara\nreview:\n  status: pending\n  reviewers:\n    - \"@mike\"")
	// Pending review the user cannot action → ignored.
	writeSpec(t, specsDir, "SPEC-022.md",
		"id: SPEC-022\nstatus: tl-review\nauthor: Sara\nreview:\n  status: pending\n  reviewers:\n    - pm")
	// Already-approved review → not pending, ignored.
	writeSpec(t, specsDir, "SPEC-023.md",
		"id: SPEC-023\nstatus: done\nauthor: Sara\nreview:\n  status: approved\n  reviewers:\n    - tl")

	got, err := Gather(rc)
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	if got.ReviewsNeeded != 2 {
		t.Errorf("ReviewsNeeded = %d, want 2 (role + handle match; pm and approved excluded)", got.ReviewsNeeded)
	}
	if got.SpecsTotal != 0 {
		t.Errorf("SpecsTotal = %d, want 0 (Mike authors none)", got.SpecsTotal)
	}
}

func TestGather_SkipsMalformedSpecFiles(t *testing.T) {
	rc, specsDir := gatherFixture(t, "Alice", "engineer")
	// One valid owned spec.
	writeSpec(t, specsDir, "SPEC-030.md", "id: SPEC-030\nstatus: build\nauthor: Alice")
	// A .md file with broken frontmatter must be skipped, not abort the walk.
	if err := os.WriteFile(filepath.Join(specsDir, "SPEC-031.md"),
		[]byte("---\n: : not: valid: yaml\n  bad\n---\n"), 0o644); err != nil {
		t.Fatalf("write malformed: %v", err)
	}

	got, err := Gather(rc)
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	if got.SpecsTotal != 1 {
		t.Errorf("SpecsTotal = %d, want 1 (malformed file skipped, valid one counted)", got.SpecsTotal)
	}
}

func TestGather_ReportsListError(t *testing.T) {
	// HOME points at a temp dir with no repo clone, so ListSpecFiles fails to
	// read the specs directory and the error propagates (no silent empty).
	home := t.TempDir()
	t.Setenv("HOME", home)

	team := &config.TeamConfig{}
	team.SpecsRepo = config.SpecsRepoConfig{Owner: "ghost", Repo: "missing", Branch: "main"}
	rc := &config.ResolvedConfig{Team: team, User: &config.UserConfig{}}

	if _, err := Gather(rc); err == nil {
		t.Error("Gather() error = nil, want error when specs repo is unreadable")
	}
}
