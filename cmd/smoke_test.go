package cmd

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/store"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// smokeEnv is a fully isolated spec environment for a single smoke test case.
// Every case gets its own HOME (so config, the SQLite DB, and the specs-repo
// clone are all sandboxed) and its own working directory (so team-config
// discovery never escapes upward into the real repo).
type smokeEnv struct {
	t         *testing.T
	home      string
	workDir   string
	repoOwner string
	repoName  string
	branch    string
}

const (
	smokeOwner  = "testorg"
	smokeRepo   = "specs-repo"
	smokeBranch = "main"
	smokeToken  = "test-token-not-a-secret"
)

// newSmokeEnv builds an isolated environment and resets all process-global
// state the cmd package memoizes between invocations. A spec process normally
// runs exactly one command, so these globals are reset here to make the
// table-driven harness behave like many independent processes.
func newSmokeEnv(t *testing.T) *smokeEnv {
	t.Helper()
	home := t.TempDir()
	work := t.TempDir()
	t.Setenv("HOME", home)
	// XDG / Windows fallbacks os.UserHomeDir may consult — pin them too so the
	// sandbox holds on every platform CI may run.
	t.Setenv("USERPROFILE", home)
	t.Chdir(work)

	resetCmdState(t)

	return &smokeEnv{
		t:         t,
		home:      home,
		workDir:   work,
		repoOwner: smokeOwner,
		repoName:  smokeRepo,
		branch:    smokeBranch,
	}
}

// resetCmdState clears the cmd package's per-process memoization so each smoke
// case resolves config and opens the DB against its own sandboxed HOME.
func resetCmdState(t *testing.T) {
	t.Helper()
	cachedConfig = nil
	cachedConfigEr = nil
	cachedConfigSet = false
	if recorderDB != nil {
		_ = recorderDB.Close()
		recorderDB = nil
	}
	gitpkg.SetRecorder(nil)
	resetFlags(rootCmd)
}

// resetFlags restores every flag in the command tree to its declared default,
// because cobra retains the last-set value across Execute calls in-process.
func resetFlags(cmd *cobra.Command) {
	reset := func(c *cobra.Command) {
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if f.Changed {
				_ = f.Value.Set(f.DefValue)
				f.Changed = false
			}
		})
	}
	reset(cmd)
	for _, sub := range cmd.Commands() {
		resetFlags(sub)
	}
}

// specsDirPath returns the sandboxed specs/ directory (the clone's content dir).
func (e *smokeEnv) specsDirPath() string {
	return filepath.Join(e.home, ".spec", "repos", e.repoOwner, e.repoName, "specs")
}

// cloneRoot returns the sandboxed clone root (where .git lives).
func (e *smokeEnv) cloneRoot() string {
	return filepath.Join(e.home, ".spec", "repos", e.repoOwner, e.repoName)
}

// writeUserConfig writes ~/.spec/config.yaml with the given role. The display
// name ("Dev") and handle ("dev") are fixed; smoke assertions key off the role
// and the well-known name/handle.
func (e *smokeEnv) writeUserConfig(role string) {
	e.t.Helper()
	dir := filepath.Join(e.home, ".spec")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.t.Fatalf("mkdir user config: %v", err)
	}
	content := "user:\n"
	if role != "" {
		content += "  owner_role: " + role + "\n"
	}
	content += "  name: Dev\n"
	content += "  handle: dev\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0o644); err != nil {
		e.t.Fatalf("write user config: %v", err)
	}
}

// writeTeamConfig writes spec.config.yaml into the working directory so config
// resolution finds it from cwd.
func (e *smokeEnv) writeTeamConfig() {
	e.t.Helper()
	content := "version: \"1\"\n" +
		"team:\n" +
		"  name: Test Team\n" +
		"  cycle: Cycle 0\n" +
		"specs_repo:\n" +
		"  provider: github\n" +
		"  owner: " + e.repoOwner + "\n" +
		"  repo: " + e.repoName + "\n" +
		"  branch: " + e.branch + "\n" +
		"  token: " + smokeToken + "\n"
	if err := os.WriteFile(filepath.Join(e.workDir, "spec.config.yaml"), []byte(content), 0o644); err != nil {
		e.t.Fatalf("write team config: %v", err)
	}
}

// specFixture is the frontmatter content for a fabricated spec file.
type specFixture struct {
	id     string
	title  string
	status string
	author string
}

// writeSpec writes a spec markdown file into the sandboxed specs directory.
func (e *smokeEnv) writeSpec(s specFixture, body string) {
	e.t.Helper()
	dir := e.specsDirPath()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.t.Fatalf("mkdir specs: %v", err)
	}
	fm := "---\n" +
		"id: " + s.id + "\n" +
		"title: " + s.title + "\n" +
		"status: " + s.status + "\n" +
		"version: 0.1.0\n" +
		"author: " + s.author + "\n" +
		"cycle: Cycle 0\n" +
		"revert_count: 0\n" +
		"created: \"2026-01-01\"\n" +
		"updated: \"2026-01-01\"\n" +
		"---\n\n" + body
	if err := os.WriteFile(filepath.Join(dir, s.id+".md"), []byte(fm), 0o644); err != nil {
		e.t.Fatalf("write spec %s: %v", s.id, err)
	}
}

// initSpecsGit turns the sandboxed clone into a git repo backed by a local
// bare "origin". The clone keeps the canonical GitHub origin URL (so
// ensureRemoteURL is satisfied) but a git insteadOf rewrite transparently maps
// that URL to the local bare repo, so fetch/push stay fully offline and
// hermetic — exercising the real mutate path (advance/assign/decide/etc.)
// without any network access.
func (e *smokeEnv) initSpecsGit() {
	e.t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		e.t.Skip("git not available")
	}
	root := e.cloneRoot()
	if err := os.MkdirAll(filepath.Join(root, "specs"), 0o755); err != nil {
		e.t.Fatalf("mkdir clone: %v", err)
	}
	run := func(dir string, args ...string) {
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test")
		if out, err := cmd.CombinedOutput(); err != nil {
			e.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	// Local bare repo standing in for the remote origin.
	bare := filepath.Join(e.home, "origin.git")
	run(e.home, "init", "-q", "--bare", "-b", e.branch, bare)
	bareURL := "file://" + bare
	ghURL := gitpkg.SpecsRepoURL(e.repoCfg())

	// Ensure the tree is never empty so the seed commit always succeeds, even
	// when no spec fixtures were written.
	if err := os.WriteFile(filepath.Join(root, "specs", ".gitkeep"), nil, 0o644); err != nil {
		e.t.Fatalf("write gitkeep: %v", err)
	}

	run(root, "init", "-q", "-b", e.branch)
	run(root, "config", "user.email", "test@test")
	run(root, "config", "user.name", "test")
	// Rewrite the canonical GitHub URL to the local bare repo for all git
	// operations (fetch + push). The clone still records the GitHub URL as
	// origin so ensureRemoteURL's get-url/set-url round trip is a no-op.
	run(root, "config", "url."+bareURL+".insteadOf", ghURL)
	run(root, "remote", "add", "origin", ghURL)
	run(root, "add", "-A")
	run(root, "commit", "-q", "-m", "init specs")
	run(root, "push", "-q", "-u", "origin", e.branch)

	// Pre-seed the last-fetch timestamp so read-path commands skip the fetch
	// within the freshness TTL (harmless for the now-reachable origin).
	db, err := store.Open(store.DefaultDBPath())
	if err != nil {
		e.t.Fatalf("open store: %v", err)
	}
	if err := db.LastFetchSet(e.repoOwner+"/"+e.repoName, time.Now()); err != nil {
		e.t.Fatalf("seed last fetch: %v", err)
	}
	_ = db.Close()
}

// repoCfg builds a minimal SpecsRepoConfig matching the sandbox.
func (e *smokeEnv) repoCfg() *config.SpecsRepoConfig {
	return &config.SpecsRepoConfig{
		Provider: "github",
		Owner:    e.repoOwner,
		Repo:     e.repoName,
		Branch:   e.branch,
		Token:    smokeToken,
	}
}

// runSpec executes the root command with args, capturing output from both the
// cobra streams and the process os.Stdout/os.Stderr (some commands print via
// fmt directly). Returns the combined stdout+stderr text and the exec error;
// callers assert on output substrings and the error's next-action contract.
func (e *smokeEnv) runSpec(args ...string) (string, error) {
	e.t.Helper()
	resetFlags(rootCmd)

	var cobraOut, cobraErr bytes.Buffer
	rootCmd.SetOut(&cobraOut)
	rootCmd.SetErr(&cobraErr)
	rootCmd.SetArgs(args)

	// Redirect process stdout/stderr to capture fmt.Printf-style output.
	origOut, origErr := os.Stdout, os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout, os.Stderr = wOut, wErr

	execErr := rootCmd.Execute()

	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout, os.Stderr = origOut, origErr

	var pipeOut, pipeErr bytes.Buffer
	_, _ = pipeOut.ReadFrom(rOut)
	_, _ = pipeErr.ReadFrom(rErr)

	// stdout first, then stderr — both are searched by substring assertions.
	return cobraOut.String() + pipeOut.String() + cobraErr.String() + pipeErr.String(), execErr
}
