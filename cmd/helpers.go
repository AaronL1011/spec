package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nexl/spec-cli/internal/adapter"
	"github.com/nexl/spec-cli/internal/adapter/noop"
	"github.com/nexl/spec-cli/internal/adapter/resolve"
	"github.com/nexl/spec-cli/internal/config"
	"github.com/nexl/spec-cli/internal/markdown"
	"github.com/nexl/spec-cli/internal/store"
)

// resolveConfig loads the full configuration chain.
func resolveConfig() (*config.ResolvedConfig, error) {
	return config.Resolve()
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
	if rc.Team != nil {
		reg, warnings := resolve.All(rc.Team)
		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "warning: %s\n", w)
		}
		return reg
	}

	// No team config — all noop
	reg := adapter.NewRegistry(nil)
	reg.WithComms(noop.Comms{}).
		WithPM(noop.PM{}).
		WithDocs(noop.Docs{}).
		WithRepo(noop.Repo{}).
		WithAgent(noop.Agent{}).
		WithDeploy(noop.Deploy{}).
		WithAI(noop.AI{})
	return reg
}

// specPathIn is a shorthand for resolveSpecPathIn using the team config's archive dir.
func specPathIn(repoPath string, rc *config.ResolvedConfig, specID string) (string, error) {
	return resolveSpecPathIn(repoPath, config.ArchiveDir(rc.Team), specID)
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
