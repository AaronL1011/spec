package harness

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The completion plane's whole value rests on it being unable to touch the repo,
// so these tests assert the mechanics rather than trusting the flags.

// Run must execute outside any repo: a harness discovers project instructions
// and MCP config by walking up from its cwd, so running inside one would let
// unrelated context steer a draft even with tools disabled.
func TestRun_ExecutesInEmptyTempDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX-only")
	}
	out, err := Run(context.Background(), "sh", []string{"-c", "pwd; ls -A | wc -l"}, 10*time.Second)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	lines := strings.Fields(strings.TrimSpace(out))
	if len(lines) < 2 {
		t.Fatalf("unexpected output: %q", out)
	}
	dir, count := lines[0], lines[len(lines)-1]

	if count != "0" {
		t.Errorf("working directory is not empty (%s entries): a completion must not see project files", count)
	}
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(dir, repoRoot) {
		t.Errorf("working directory %q is inside the repo %q", dir, repoRoot)
	}
}

// A fixture repo must be byte-identical after a completion that is explicitly
// told to modify files. This is the end-to-end containment assertion.
func TestRun_CannotMutateAFixtureRepo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX-only")
	}
	repo := t.TempDir()
	target := filepath.Join(repo, "spec.md")
	original := "# SPEC-001\n\noriginal content\n"
	if err := os.WriteFile(target, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	// Stand in for a harness that tries to edit relative paths in its cwd. It
	// runs in the contained temp dir, so a relative write lands nowhere near the
	// fixture.
	_, err := Run(context.Background(), "sh",
		[]string{"-c", "echo clobbered > spec.md; echo done"}, 10*time.Second)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	after, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != original {
		t.Errorf("fixture repo was modified:\n got: %q\nwant: %q", after, original)
	}
}

// Stdin must be closed so a harness cannot block forever waiting for input that
// will never arrive — a hung draft is indistinguishable from a slow one.
func TestRun_ClosesStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX-only")
	}
	out, err := Run(context.Background(), "sh", []string{"-c", "cat; echo exited"}, 10*time.Second)
	if err != nil {
		t.Fatalf("Run should not hang or fail with stdin closed: %v", err)
	}
	if !strings.Contains(out, "exited") {
		t.Errorf("expected the process to finish reading stdin, got %q", out)
	}
}

// An exceeded budget must name the knob that raises it, not just fail.
func TestRun_TimeoutNamesTheKnob(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX-only")
	}
	_, err := Run(context.Background(), "sh", []string{"-c", "sleep 5"}, 150*time.Millisecond)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if !strings.Contains(err.Error(), "agent.generate.timeout") {
		t.Errorf("error should name the timeout setting, got %q", err)
	}
}

// Cancellation must kill descendants, not just the direct child. Both shipping
// harnesses spawn children, so without process-group signalling "Esc cancels"
// would orphan work that keeps running and burning tokens.
func TestRun_CancellationKillsDescendants(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process groups; Windows is documented best-effort")
	}

	dir := t.TempDir()
	marker := filepath.Join(dir, "descendant-alive")

	// The parent spawns a child that would create a marker after a delay, then
	// waits. If cancellation only killed the parent, the child would survive and
	// the marker would appear.
	script := "sh -c 'sleep 2; touch " + marker + "' & sleep 5"

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_, _ = Run(ctx, "sh", []string{"-c", script}, 10*time.Second)
		close(done)
	}()

	time.Sleep(300 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}

	// Wait past the point the descendant would have written its marker.
	time.Sleep(2500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Error("a descendant survived cancellation and kept working")
	}
}

func TestRun_MissingBinaryIsAnError(t *testing.T) {
	_, err := Run(context.Background(), "definitely-not-a-real-binary-xyz", nil, time.Second)
	if err == nil {
		t.Fatal("expected an error for a missing binary")
	}
	if !strings.Contains(err.Error(), "not found in PATH") {
		t.Errorf("error should say the binary is missing, got %q", err)
	}
}

// Sanity check that the fixture shell exists, so a skipped-everything run is
// visible rather than silently green.
func TestShellFixtureAvailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("not applicable")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Fatalf("containment tests need sh: %v", err)
	}
}
