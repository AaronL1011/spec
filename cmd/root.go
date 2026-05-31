package cmd

//go:generate go run ../tools/gen-man --output ../docs/man

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/aaronl1011/spec/internal/dashboard"
	"github.com/aaronl1011/spec/internal/tui"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "spec",
	Short: "The End-Game Developer Control Plane",
	Long: `spec is a workflow tool that unifies spec management,
pipeline orchestration, build context, and team coordination
into a single CLI. Run 'spec' with no arguments to see your
personal dashboard.`,
	Example:       "  spec\n  spec list --mine\n  spec do SPEC-042",
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

		// --static flag bypasses the TUI for scripts and legacy usage.
		staticMode, _ := cmd.Flags().GetBool("static")

		// Interactive terminal → launch the persistent TUI.
		// Non-interactive (piped, redirected) → static dashboard render.
		if !staticMode && tui.IsInteractive() {
			app := tui.New(rc, reg, role)
			defer func() { _ = app.Close() }()
			opts := []tea.ProgramOption{tea.WithAltScreen()}
			if rc.User != nil && rc.User.Preferences.Mouse {
				opts = append(opts, tea.WithMouseCellMotion())
			}
			p := tea.NewProgram(app, opts...)
			_, err := p.Run()
			return err
		}

		data, err := dashboard.Aggregate(ctx(), rc, reg, role)
		if err != nil {
			return err
		}
		if jsonOut, _ := cmd.Flags().GetBool("json"); jsonOut {
			return newPrinter(cmd).JSON(data)
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
	rootCmd.PersistentFlags().Bool("json", false, "output machine-readable JSON where supported")
	rootCmd.PersistentFlags().Bool("quiet", false, "suppress non-essential output")
	rootCmd.Flags().Bool("static", false, "render static dashboard instead of interactive TUI")

	// Passive awareness: print pending count before every subcommand.
	// Does not apply to the root command itself (the dashboard) or completion.
	originalPreRun := rootCmd.PersistentPreRunE
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if originalPreRun != nil {
			if err := originalPreRun(cmd, args); err != nil {
				return err
			}
		}
		// Inject the store-backed sync audit/freshness recorder once per
		// invocation so the git read path records fetches and skips within
		// the freshness TTL. Best-effort: a DB open failure leaves git's
		// no-op recorder in place.
		installSyncRecorder()
		// Only print for subcommands, not the root dashboard or completion.
		// Awareness is best-effort — config resolution failure is not fatal.
		// Suppressed under --quiet/--json and when stderr is not a terminal so
		// scripted and machine-readable invocations stay clean.
		if cmd != rootCmd && cmd.Name() != "completion" && awarenessAllowed(cmd) {
			if rc, err := resolveConfig(); err == nil {
				role := rc.OwnerRole("")
				dashboard.PrintAwarenessLine(rc, role)
			}
		}
		return nil
	}
}
