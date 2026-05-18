package config

import (
	"os"
	"path/filepath"
)

// Resolve loads the full configuration from all sources.
// Resolution chain: cwd → repo root → specs repo clone → user config.
func Resolve() (*ResolvedConfig, error) {
	rc := &ResolvedConfig{}

	// Load user config (always available, even if defaults)
	userCfg, userPath := LoadUserConfigWithDefaults()
	rc.User = userCfg
	rc.UserConfigPath = userPath

	// Try to find team config starting from cwd
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}

	teamPath, err := FindTeamConfigPath(cwd)
	if err == nil {
		teamCfg, loadErr := LoadTeamConfig(teamPath)
		if loadErr == nil {
			rc.Team = teamCfg
			rc.TeamConfigPath = teamPath
		}
	}

	// If team config wasn't found locally, try the specs repo clone
	if rc.Team == nil {
		specsDir := filepath.Join(UserConfigDir(), "repos")
		if entries, err := os.ReadDir(specsDir); err == nil {
			for _, owner := range entries {
				if !owner.IsDir() {
					continue
				}
				ownerPath := filepath.Join(specsDir, owner.Name())
				repoEntries, err := os.ReadDir(ownerPath)
				if err != nil {
					continue
				}
				for _, repo := range repoEntries {
					if !repo.IsDir() {
						continue
					}
					candidate := filepath.Join(ownerPath, repo.Name(), "spec.config.yaml")
					if _, err := os.Stat(candidate); err == nil {
						teamCfg, loadErr := LoadTeamConfig(candidate)
						if loadErr == nil {
							rc.Team = teamCfg
							rc.TeamConfigPath = candidate
							rc.SpecsRepoDir = filepath.Join(ownerPath, repo.Name(), "specs")
						}
						break
					}
				}
				if rc.Team != nil {
					break
				}
			}
		}
	}

	// SpecsRepoDir always points to the specs/ sub-directory of the internal
	// managed clone, not wherever spec.config.yaml was found. The config file
	// may live in the user's own checkout of the specs repo, but spec reads
	// and writes through the clone at ~/.spec/repos/<owner>/<repo>/specs/.
	if rc.SpecsRepoDir == "" && rc.Team != nil &&
		rc.Team.SpecsRepo.Owner != "" && rc.Team.SpecsRepo.Repo != "" {
		rc.SpecsRepoDir = filepath.Join(UserConfigDir(), "repos",
			rc.Team.SpecsRepo.Owner, rc.Team.SpecsRepo.Repo, "specs")
	}

	return rc, nil
}

// MustResolve loads configuration and returns it, panicking on error.
// Use only in places where config is absolutely required.
func MustResolve() *ResolvedConfig {
	rc, err := Resolve()
	if err != nil {
		panic(err)
	}
	return rc
}
