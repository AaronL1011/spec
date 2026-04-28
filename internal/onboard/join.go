// Package onboard handles team onboarding workflows.
package onboard

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/git"
)

// Join clones a specs repo and validates it contains spec.config.yaml.
// Returns actionable errors for common failure modes.
func Join(ctx context.Context, repoRef, branch, tokenOverride string) error {
	// 1. Parse repo reference
	provider, owner, repo, err := ParseRepoRef(repoRef)
	if err != nil {
		return err
	}

	// 2. Resolve token (flag > env var)
	token := resolveToken(provider, tokenOverride)
	if token == "" {
		return fmt.Errorf("no access token — set %s or pass --token", tokenEnvVar(provider))
	}

	// 3. Check if already joined
	targetDir := filepath.Join(config.UserConfigDir(), "repos", owner, repo)
	configPath := filepath.Join(targetDir, "spec.config.yaml")
	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf("already joined %s/%s — config exists at %s", owner, repo, targetDir)
	}

	// 4. Build SpecsRepoConfig for clone
	cfg := &config.SpecsRepoConfig{
		Provider: provider,
		Owner:    owner,
		Repo:     repo,
		Branch:   branch,
		Token:    token,
	}

	// 5. Clone via existing EnsureSpecsRepo
	fmt.Printf("Cloning %s/%s...\n", owner, repo)
	dir, err := git.EnsureSpecsRepo(ctx, cfg)
	if err != nil {
		return fmt.Errorf("cloning specs repo: %w", err)
	}

	// 6. Validate spec.config.yaml exists
	configPath = filepath.Join(dir, "spec.config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Clean up failed join
		_ = os.RemoveAll(dir)
		return fmt.Errorf("no spec.config.yaml found in %s/%s — is this a spec-managed repo?", owner, repo)
	}

	// 7. Validate config is parseable
	teamCfg, err := config.LoadTeamConfig(configPath)
	if err != nil {
		return fmt.Errorf("invalid spec.config.yaml: %w", err)
	}

	// 8. Success output
	fmt.Println()
	fmt.Printf("✓ Joined team %q\n", teamCfg.Team.Name)
	fmt.Printf("  Specs repo: %s/%s (branch: %s)\n", owner, repo, branch)
	fmt.Printf("  Local path: %s\n", dir)

	// 9. Check for user config, prompt if missing
	if !hasUserConfig() {
		fmt.Println()
		fmt.Println("No user identity configured.")
		fmt.Println("Run 'spec config init --user' to set up your name and role.")
	} else {
		fmt.Println()
		fmt.Println("Run 'spec list' to see available specs.")
	}

	return nil
}

// ParseRepoRef extracts provider, owner, repo from various input formats.
//
// Supported formats:
//   - org/repo              → github (default)
//   - github.com/org/repo   → github
//   - gitlab.com/org/repo   → gitlab
//   - https://github.com/org/repo
//   - https://github.com/org/repo.git
func ParseRepoRef(ref string) (provider, owner, repo string, err error) {
	// Strip protocol
	ref = strings.TrimPrefix(ref, "https://")
	ref = strings.TrimPrefix(ref, "http://")
	ref = strings.TrimSuffix(ref, ".git")
	ref = strings.TrimSuffix(ref, "/")

	parts := strings.Split(ref, "/")

	switch len(parts) {
	case 2:
		// org/repo → assume GitHub
		if parts[0] == "" || parts[1] == "" {
			return "", "", "", fmt.Errorf("invalid repo reference %q — use 'org/repo' or 'github.com/org/repo'", ref)
		}
		return "github", parts[0], parts[1], nil

	case 3:
		// github.com/org/repo
		provider := providerFromHost(parts[0])
		if provider == "" {
			return "", "", "", fmt.Errorf("unknown provider %q — use github.com, gitlab.com, or bitbucket.org", parts[0])
		}
		if parts[1] == "" || parts[2] == "" {
			return "", "", "", fmt.Errorf("invalid repo reference %q — use 'org/repo' or 'github.com/org/repo'", ref)
		}
		return provider, parts[1], parts[2], nil

	default:
		return "", "", "", fmt.Errorf("invalid repo reference %q — use 'org/repo' or 'github.com/org/repo'", ref)
	}
}

func providerFromHost(host string) string {
	switch host {
	case "github.com":
		return "github"
	case "gitlab.com":
		return "gitlab"
	case "bitbucket.org":
		return "bitbucket"
	default:
		return ""
	}
}

func tokenEnvVar(provider string) string {
	switch provider {
	case "gitlab":
		return "GITLAB_TOKEN"
	case "bitbucket":
		return "BITBUCKET_TOKEN"
	default:
		return "GITHUB_TOKEN"
	}
}

func resolveToken(provider, override string) string {
	if override != "" {
		return override
	}
	return os.Getenv(tokenEnvVar(provider))
}

func hasUserConfig() bool {
	path := config.UserConfigPath()
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
