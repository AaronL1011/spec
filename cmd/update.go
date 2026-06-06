package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/aaronl1011/spec/internal/update"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update the spec CLI to the newest released version",
	Long: `Update brings the locally installed spec binary up to date by
delegating to whatever mechanism manages the install on this machine:
Homebrew (brew upgrade), go install, or a direct binary swap from the
GitHub release for raw installs.`,
	Example:       "  spec update\n  spec update --check\n  spec update --version v1.4.0 --yes",
	Args:          cobra.NoArgs,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          runUpdate,
}

func init() {
	updateCmd.Flags().Bool("check", false, "report available updates without applying them")
	updateCmd.Flags().Bool("force", false, "update even if already on the latest version")
	updateCmd.Flags().Bool("yes", false, "skip the confirmation prompt")
	updateCmd.Flags().String("version", "", "target a specific release tag instead of latest")
	rootCmd.AddCommand(updateCmd)
}

func runUpdate(cmd *cobra.Command, args []string) error {
	p := newPrinter(cmd)

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating the running binary: %w", err)
	}

	updater := update.NewUpdater(githubUpdateToken())
	plan, err := updater.Plan(ctx(), update.Options{
		CurrentVersion: resolveVersion(),
		ExecPath:       execPath,
		TargetVersion:  mustString(cmd, "version"),
		Force:          mustBool(cmd, "force"),
	})
	if err != nil {
		return err
	}

	if p.JSONEnabled() {
		return p.JSON(plan)
	}

	reportPlan(p, plan)

	if mustBool(cmd, "check") {
		return nil
	}
	if !plan.UpdateAvailable && !mustBool(cmd, "force") {
		return nil
	}
	if !mustBool(cmd, "yes") && !confirmUpdate(cmd, plan) {
		p.Line("Update cancelled.")
		return nil
	}

	if err := updater.Apply(ctx(), plan, cmd.OutOrStdout(), cmd.ErrOrStderr()); err != nil {
		return err
	}
	p.Line("Updated to %s.", plan.LatestVersion)
	return nil
}

// reportPlan prints the current/latest versions and the managing mechanism.
func reportPlan(p *printer, plan *update.Plan) {
	p.Line("Current version: %s", plan.CurrentVersion)
	p.Line("Latest version:  %s", plan.LatestVersion)
	p.Line("Install method:  %s", plan.Method)
	if !plan.UpdateAvailable {
		p.Line("Already up to date.")
	}
}

// confirmUpdate prompts for confirmation describing the action that will run.
func confirmUpdate(cmd *cobra.Command, plan *update.Plan) bool {
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Update spec to %s via %s? [y/N] ", plan.LatestVersion, plan.Method)
	reader := bufio.NewReader(cmd.InOrStdin())
	answer, _ := reader.ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

func mustBool(cmd *cobra.Command, name string) bool {
	v, _ := cmd.Flags().GetBool(name)
	return v
}

func mustString(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}

// githubUpdateToken returns an optional GitHub token to lift the anonymous API
// rate limit. Public release lookups work without it; it is read best-effort
// from the standard environment variables.
func githubUpdateToken() string {
	for _, key := range []string{"SPEC_GITHUB_TOKEN", "GITHUB_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}
