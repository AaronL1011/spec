package cmd

import (
	"fmt"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/spf13/cobra"
)

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Display resolved user identity and config source",
	RunE:  runWhoami,
}

func init() {
	rootCmd.AddCommand(whoamiCmd)
}

func runWhoami(cmd *cobra.Command, args []string) error {
	rc, err := config.Resolve()
	if err != nil {
		return err
	}

	role := rc.OwnerRole("")
	if role == "" {
		fmt.Println("No role configured. Run 'spec config init --user' to set up your identity.")
		return nil
	}

	fmt.Printf("Name:   %s\n", rc.UserName())
	fmt.Printf("Role:   %s\n", role)
	fmt.Printf("Handle: %s\n", rc.UserHandle())
	fmt.Printf("Config: %s\n", rc.UserConfigPath)

	if rc.Team != nil {
		fmt.Printf("Team:   %s\n", rc.TeamName())
		fmt.Printf("Cycle:  %s\n", rc.CycleLabel())
		fmt.Printf("Team config: %s\n", rc.TeamConfigPath)
	}

	return nil
}
