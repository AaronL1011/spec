package cmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gitpkg "github.com/aaronl1011/spec/internal/git"
)

// TestSmoke_Nuke_ReclonesCleanly verifies `spec nuke --yes` deletes the local
// clone and re-clones it. The URL rewrite is pinned in the sandbox's GLOBAL
// gitconfig (not the clone-local config) so the fresh clone stays offline.
func TestSmoke_Nuke_ReclonesCleanly(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	e := newSmokeEnv(t)
	e.writeUserConfig("engineer")
	e.writeTeamConfig()
	e.initSpecsGit()

	// Move the URL rewrite to global config so a fresh clone (which has no
	// clone-local config yet) still resolves origin to the local bare repo.
	bareURL := "file://" + filepath.Join(e.home, "origin.git")
	ghURL := gitpkg.SpecsRepoURL(e.repoCfg())
	globalGit := exec.CommandContext(context.Background(), "git", "config", "--global", "url."+bareURL+".insteadOf", ghURL)
	globalGit.Env = append(os.Environ(), "HOME="+e.home)
	if out, err := globalGit.CombinedOutput(); err != nil {
		t.Fatalf("set global insteadOf: %v\n%s", err, out)
	}

	// Leave a stray uncommitted file to prove nuke discards local state.
	stray := filepath.Join(e.cloneRoot(), "specs", "STRAY.md")
	if err := os.WriteFile(stray, []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := e.runSpec("nuke", "--yes")
	if err != nil {
		t.Fatalf("spec nuke: unexpected error: %v\n%s", err, out)
	}
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Errorf("nuke should have discarded the stray local file")
	}
	if _, err := os.Stat(filepath.Join(e.cloneRoot(), ".git")); err != nil {
		t.Errorf("nuke should have left a fresh clone in place: %v", err)
	}
	if !strings.Contains(out, "Re-cloned") {
		t.Errorf("nuke output = %q, want a re-clone confirmation", out)
	}
}
