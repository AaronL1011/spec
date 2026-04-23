package cmd

//go:generate go run ../tools/gen-man --output ../docs/man

import (
	"github.com/nexl/spec-cli/internal/dashboard"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "spec",
	Short: "The End-Game Developer Control Plane",
	Long: `spec is a workflow tool that unifies spec management,
pipeline orchestration, build context, and team coordination
into a single CLI. Run 'spec' with no arguments to see your
personal dashboard.`,
	Example: "  spec\n  spec list --mine\n  spec do SPEC-042",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		rc, err := resolveConfig()
		if err != nil {
			return err
		}

		role := rc.OwnerRole("")
		if role == "" {
			cmd.Println("Welcome to spec — the end-game developer control plane.")
			cmd.Println("Run 'spec config init --user' to set up your identity.")
			cmd.Println("Run 'spec --help' for available commands.")
			return nil
		}

		if rc.Team == nil {
			cmd.Println("Welcome to spec — the end-game developer control plane.")
			cmd.Printf("Role: %s\n", role)
			cmd.Println("No team config found. Run 'spec config init' to set up your team.")
			return nil
		}

		reg := buildRegistry(rc)
		data, err := dashboard.Aggregate(ctx(), rc, reg, role)
		if err != nil {
			return err
		}

		dashboard.Render(data, rc.UserName(), role, rc.CycleLabel())
		return nil
	},
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

// RootCmd returns the root command for tooling integrations.
func RootCmd() *cobra.Command {
	return rootCmd
}

func init() {
	rootCmd.PersistentFlags().String("role", "", "temporarily override owner_role for this invocation")

	// Passive awareness: print pending count before every subcommand.
	// Does not apply to the root command itself (the dashboard) or completion.
	originalPreRun := rootCmd.PersistentPreRunE
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if originalPreRun != nil {
			if err := originalPreRun(cmd, args); err != nil {
				return err
			}
		}
		// Only print for subcommands, not the root dashboard or completion.
		// Awareness is best-effort — config resolution failure is not fatal.
		if cmd != rootCmd && cmd.Name() != "completion" {
			if rc, err := resolveConfig(); err == nil {
				role := rc.OwnerRole("")
				dashboard.PrintAwarenessLine(rc, role)
			}
		}
		return nil
	}
}
