package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aaronl1011/spec/internal/adapter"
	"github.com/aaronl1011/spec/internal/adapter/noop"
	"github.com/aaronl1011/spec/internal/adapter/resolve"
	"github.com/aaronl1011/spec/internal/build"
	"github.com/aaronl1011/spec/internal/config"
	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/store"
	"github.com/aaronl1011/spec/internal/syncaudit"
	"github.com/spf13/cobra"
)

// specsDir returns the specs content directory within a repo root path.
// Use inside WithSpecsRepo mutators where repoPath is the git repo root.
func specsDir(repoPath string) string {
	return filepath.Join(repoPath, gitpkg.SpecsSubDir)
}

// claimSpecID claims an authoritative SPEC-NNN via the remote counter ref
// (SPEC-018). The bootstrap seed is the current max over spec + archive
// filenames, used only when the repo has no counter ref yet. This is the only
// allocation path for `spec new`/`promote`/`fix`; the claim is made before the
// scaffold-and-write step and hard-fails offline rather than guessing.
func claimSpecID(ctx context.Context, rc *config.ResolvedConfig) (string, error) {
	specFiles, _ := gitpkg.ListSpecFiles(&rc.Team.SpecsRepo)
	archiveFiles, _ := gitpkg.ListArchiveFiles(&rc.Team.SpecsRepo, config.ArchiveDir(rc.Team))
	bootstrapMax := markdown.MaxSpecNum(append(append([]string{}, specFiles...), archiveFiles...))
	return gitpkg.ClaimNextID(ctx, &rc.Team.SpecsRepo, gitpkg.CounterSpec, bootstrapMax)
}

// claimTriageID claims an authoritative TRIAGE-NNN via the remote counter ref.
// The bootstrap seed is the current max over triage filenames.
func claimTriageID(ctx context.Context, rc *config.ResolvedConfig) (string, error) {
	triageFiles, _ := gitpkg.ListTriageFiles(&rc.Team.SpecsRepo)
	bootstrapMax := markdown.MaxTriageNum(triageFiles)
	return gitpkg.ClaimNextID(ctx, &rc.Team.SpecsRepo, gitpkg.CounterTriage, bootstrapMax)
}

// cachedConfig memoizes the resolved configuration for the lifetime of a
// single CLI invocation. A process runs exactly one command, so resolving the
// chain once (rather than separately for the pre-run awareness line and the
// command body) is safe and avoids a redundant config load + specs-dir scan.
var (
	cachedConfig    *config.ResolvedConfig
	cachedConfigEr  error
	cachedConfigSet bool
)

// resolveConfig loads the full configuration chain, memoizing the result.
func resolveConfig() (*config.ResolvedConfig, error) {
	if cachedConfigSet {
		return cachedConfig, cachedConfigEr
	}
	cachedConfig, cachedConfigEr = config.Resolve()
	cachedConfigSet = true
	return cachedConfig, cachedConfigEr
}

// awarenessAllowed reports whether the passive "pending" line should print.
// It is suppressed under --quiet/--json and when stderr is not a terminal,
// keeping scripted and machine-readable invocations free of chatter.
func awarenessAllowed(cmd *cobra.Command) bool {
	if quiet, _ := cmd.Flags().GetBool("quiet"); quiet {
		return false
	}
	if jsonOut, _ := cmd.Flags().GetBool("json"); jsonOut {
		return false
	}
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// requireRole ensures the user has a role configured (or overridden).
func requireRole(rc *config.ResolvedConfig) (string, error) {
	override, _ := rootCmd.PersistentFlags().GetString("role")
	role := rc.OwnerRole(override)
	if role == "" {
		return "", fmt.Errorf("no role configured — run 'spec config init --user' to set up your identity")
	}
	return role, nil
}

// requireTeamConfig ensures team config is loaded.
func requireTeamConfig(rc *config.ResolvedConfig) error {
	if rc.Team == nil {
		return fmt.Errorf("team config not found — run 'spec config init' to set up, or ensure spec.config.yaml exists")
	}
	return nil
}

// openDB opens the default SQLite database.
func openDB() (*store.DB, error) {
	return store.Open(store.DefaultDBPath())
}

// recorderDB holds the long-lived DB backing the injected sync recorder for
// the lifetime of the process. A single CLI invocation runs one command, so
// keeping it open is safe and avoids re-opening for every git operation.
var recorderDB *store.DB

// installSyncRecorder injects the store-backed git.Recorder once per process.
// Best-effort: a DB open failure leaves git's no-op recorder in place.
func installSyncRecorder() {
	if recorderDB != nil {
		return
	}
	db, err := openDB()
	if err != nil {
		return
	}
	recorderDB = db
	gitpkg.SetRecorder(syncaudit.New(db))
}

// syncOpts builds git.SyncOptions for a CLI command, attributing the audit log
// to the CLI surface with the command name as the trigger.
func syncOpts(cmd *cobra.Command, specID string) gitpkg.SyncOptions {
	trigger := "cli"
	if cmd != nil {
		trigger = cmd.Name()
	}
	return gitpkg.SyncOptions{
		Surface:  store.SurfaceCLI,
		Trigger:  trigger,
		SpecID:   specID,
		Recorder: syncaudit.New(recorderDB),
	}
}

func normalizeSpecID(specID string) string {
	return strings.ToUpper(strings.TrimSpace(specID))
}

func resolveSpecIDArg(args []string, usage string) (string, error) {
	if len(args) > 0 {
		return normalizeSpecID(args[0]), nil
	}

	specID, err := resolveFocusedSpecID()
	if err != nil {
		return "", err
	}
	if specID != "" {
		return specID, nil
	}

	return "", fmt.Errorf("no spec ID provided — use '%s' or set one with 'spec focus <id>'", usage)
}

// resolveSpecIDFromArgs gets the spec ID from args, focused state, branch, or recent session.
func resolveSpecIDFromArgs(args []string) (string, error) {
	if len(args) > 0 {
		return normalizeSpecID(args[0]), nil
	}

	specID, err := resolveFocusedSpecID()
	if err != nil {
		return "", err
	}
	if specID != "" {
		return specID, nil
	}

	workDir, err := os.Getwd()
	if err == nil {
		if specID := gitpkg.DetectSpecFromBranch(ctx(), workDir); specID != "" {
			return specID, nil
		}
	}

	db, err := openDB()
	if err == nil {
		defer func() { _ = db.Close() }()
		if recent, err := db.SessionMostRecent(); err == nil && recent != "" {
			return recent, nil
		}
	}

	return "", fmt.Errorf("no spec ID provided — pass an ID or set one with 'spec focus <id>'")
}

func resolveFocusedSpecID() (string, error) {
	db, err := openDB()
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()

	specID, err := db.FocusedSpecGet()
	if err != nil {
		return "", err
	}
	return specID, nil
}

// resolveSpecPath finds a spec file by ID in the specs repo.
func resolveSpecPath(rc *config.ResolvedConfig, specID string) (string, error) {
	if rc.SpecsRepoDir == "" {
		return "", fmt.Errorf("specs repo not configured — ensure spec.config.yaml has specs_repo settings")
	}
	return resolveSpecPathIn(rc.SpecsRepoDir, config.ArchiveDir(rc.Team), specID)
}

// resolveSpecPathIn finds a spec file by ID within a given base directory.
// Use this inside WithSpecsRepo mutators to ensure the repoPath is used.
func resolveSpecPathIn(baseDir, archiveDir, specID string) (string, error) {
	// Check root
	path := filepath.Join(baseDir, specID+".md")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	// Check triage/
	path = filepath.Join(baseDir, "triage", specID+".md")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	// Check archive/
	path = filepath.Join(baseDir, archiveDir, specID+".md")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	return "", fmt.Errorf("spec %s not found in specs repo — check the ID and try again", specID)
}

// resolveLocalSpecPath finds a spec in the local .spec/ directory.
func resolveLocalSpecPath(specID string) (string, error) {
	path := filepath.Join(".spec", specID+".md")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("spec %s not found locally — run 'spec pull %s' first", specID, specID)
}

// readSpecMeta reads the frontmatter of a spec file.
func readSpecMeta(path string) (*markdown.SpecMeta, error) {
	return markdown.ReadMeta(path)
}

// buildRegistry creates an adapter registry from config.
// Uses resolve.All to wire concrete adapters from spec.config.yaml;
// falls back to all-noop if no team config is present.
func buildRegistry(rc *config.ResolvedConfig) *adapter.Registry {
	var reg *adapter.Registry
	if rc.Team != nil {
		var warnings []string
		reg, warnings = resolve.All(rc.Team)
		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "warning: %s\n", w)
		}
	} else {
		// No team config — all noop
		reg = adapter.NewRegistry(nil)
		reg.WithComms(noop.Comms{}).
			WithPM(noop.PM{}).
			WithDocs(noop.Docs{}).
			WithRepo(noop.Repo{}).
			WithAgent(noop.Agent{}).
			WithDeploy(noop.Deploy{}).
			WithAI(noop.AI{})
	}

	// Per-user coding-agent override: a harness is a personal tool, so the
	// user's ~/.spec/config.yaml `agent:` wins over the team default.
	if rc.User != nil && rc.User.Agent != nil && rc.User.Agent.Provider != "" {
		agent, warn := resolve.Agent(*rc.User.Agent)
		if warn != "" {
			fmt.Fprintf(os.Stderr, "warning: %s\n", warn)
		}
		reg.WithAgent(agent)
	}

	return reg
}

// buildEngineOptions assembles build.Options from agent integration config.
// headless plumbs the autonomous run mode (e.g. `spec fix --auto`).
func buildEngineOptions(rc *config.ResolvedConfig, headless bool) build.Options {
	opts := build.Options{Headless: headless}
	// Use the effective agent config so per-user skill/command/test_command
	// overrides apply alongside the per-user provider choice.
	agent := rc.EffectiveAgentConfig()
	if refs := agent.Get("skill"); refs != "" {
		opts.SkillRefs = splitConfigList(refs)
	}
	opts.TestCommand = agent.Get("test_command")
	if rc.User != nil && len(rc.User.Workspaces) > 0 {
		opts.Workspaces = rc.User.Workspaces
	}
	return opts
}

// splitConfigList splits a comma- or newline-separated config value into
// trimmed, non-empty entries.
func splitConfigList(v string) []string {
	fields := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == '\n' })
	var out []string
	for _, f := range fields {
		if t := strings.TrimSpace(f); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// specPathIn is a shorthand for resolveSpecPathIn using the team config's archive dir.
// repoPath is the git repo root; specs are resolved under the specs/ sub-directory.
func specPathIn(repoPath string, rc *config.ResolvedConfig, specID string) (string, error) {
	return resolveSpecPathIn(specsDir(repoPath), config.ArchiveDir(rc.Team), specID)
}

// ctx returns a background context.
func ctx() context.Context {
	return context.Background()
}

// warnf prints a warning to stderr. Use for non-fatal adapter errors
// that should not block the command but should be visible to the user.
func warnf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "warning: "+format+"\n", args...)
}

func persistEpicKey(rc *config.ResolvedConfig, specID, epicKey string) error {
	if epicKey == "" {
		return nil
	}
	return gitpkg.WithSpecsRepoOpts(context.Background(), &rc.Team.SpecsRepo, syncOpts(nil, specID), func(repoPath string) (string, error) {
		path, err := specPathIn(repoPath, rc, specID)
		if err != nil {
			return "", err
		}
		meta, err := readSpecMeta(path)
		if err != nil {
			return "", err
		}
		if meta.EpicKey == epicKey {
			return "", nil
		}
		meta.EpicKey = epicKey
		if err := markdown.WriteMeta(path, meta); err != nil {
			return "", err
		}
		return fmt.Sprintf("chore: link %s to epic %s", specID, epicKey), nil
	})
}
