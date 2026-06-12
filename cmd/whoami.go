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
	fmt.Printf("Handle: %s\n", rc.CanonicalHandle())
	fmt.Printf("Config: %s\n", rc.UserConfigPath)

	if rc.Team != nil {
		fmt.Printf("Team:   %s\n", rc.TeamName())
		fmt.Printf("Cycle:  %s\n", rc.CycleLabel())
		fmt.Printf("Team config: %s\n", rc.TeamConfigPath)
		printResolvedIdentities(rc)
	}

	return nil
}

// identityCategories lists the integration categories whose resolved handle is
// worth showing the user, in a stable display order.
var identityCategories = []string{"repo", "comms", "pm", "docs", "design", "deploy"}

// printResolvedIdentities shows, per configured integration, the exact handle
// each adapter receives — so a user can see when their canonical handle is
// standing in for an unmapped provider.
func printResolvedIdentities(rc *config.ResolvedConfig) {
	var lines []string
	for _, cat := range identityCategories {
		if !rc.HasIntegration(cat) {
			continue
		}
		provider := rc.IntegrationProvider(cat)
		lines = append(lines, fmt.Sprintf("  %-7s (%s): %s", cat, provider, rc.IdentityForCategory(cat)))
	}
	if len(lines) == 0 {
		return
	}
	fmt.Println("Identities:")
	for _, l := range lines {
		fmt.Println(l)
	}
}
