package cmd

import (
	"fmt"
	"time"

	"github.com/nexl/spec-cli/internal/dashboard"
	"github.com/spf13/cobra"
)

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Live-updating pipeline dashboard",
	RunE:  runWatch,
}

func init() {
	watchCmd.Flags().Int("interval", 30, "refresh interval in seconds")
	rootCmd.AddCommand(watchCmd)
}

func runWatch(cmd *cobra.Command, args []string) error {
	interval, _ := cmd.Flags().GetInt("interval")

	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	role, err := requireRole(rc)
	if err != nil {
		return err
	}

	reg := buildRegistry(rc)

	fmt.Println("Pipeline watch mode. Ctrl+C to exit.")
	fmt.Println()

	for {
		// Clear screen (ANSI escape)
		fmt.Print("\033[2J\033[H")

		data, err := dashboard.Aggregate(ctx(), rc, reg, role)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			dashboard.Render(data, rc.UserName(), role, rc.CycleLabel())
		}

		fmt.Printf("Refreshing every %ds. Ctrl+C to exit.", interval)
		time.Sleep(time.Duration(interval) * time.Second)
	}
}
