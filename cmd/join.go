package cmd

import (
	"github.com/aaronl1011/spec/internal/onboard"
	"github.com/spf13/cobra"
)

var joinCmd = &cobra.Command{
	Use:   "join <repo>",
	Short: "Join an existing team by cloning their specs repo",
	Long: `Clone a specs repo containing spec.config.yaml to bootstrap your local environment.

Accepts repository references in multiple formats:
  org/repo                    GitHub (default provider)
  github.com/org/repo         Explicit provider in host
  gitlab.com/org/repo         GitLab
  https://github.com/org/repo Full URL

The repo is cloned to ~/.spec/repos/<owner>/<repo>/, and subsequent
spec commands will automatically use the team configuration.

Requires an access token with read permissions on the specs repo.
Set GITHUB_TOKEN (or GITLAB_TOKEN / BITBUCKET_TOKEN) in your environment,
or pass --token explicitly.`,
	Example: `  spec join acme/specs
  spec join github.com/acme/specs
  spec join gitlab.com/acme/specs
  spec join --branch develop acme/specs`,
	Args: cobra.ExactArgs(1),
	RunE: runJoin,
}

func init() {
	joinCmd.Flags().String("branch", "main", "branch to clone")
	joinCmd.Flags().String("token", "", "access token (defaults to $GITHUB_TOKEN)")
	rootCmd.AddCommand(joinCmd)
}

func runJoin(cmd *cobra.Command, args []string) error {
	branch, _ := cmd.Flags().GetString("branch")
	token, _ := cmd.Flags().GetString("token")

	return onboard.Join(ctx(), args[0], branch, token)
}
