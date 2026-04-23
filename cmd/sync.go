package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync <id>",
	Short: "Bidirectional section-scoped sync with external tools",
	Args:  cobra.ExactArgs(1),
	RunE:  runSync,
}

func init() {
	syncCmd.Flags().String("direction", "both", "sync direction: in | out | both")
	syncCmd.Flags().Bool("dry-run", false, "preview changes without applying")
	syncCmd.Flags().Bool("force", false, "force inbound changes on conflict")
	syncCmd.Flags().Bool("skip", false, "skip conflicting sections")
	rootCmd.AddCommand(syncCmd)
}

func runSync(cmd *cobra.Command, args []string) error {
	specID := strings.ToUpper(args[0])

	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	if !rc.HasIntegration("docs") {
		return fmt.Errorf("docs integration not configured; sync requires a docs provider, configure 'integrations.docs' in spec.config.yaml")
	}

	// TODO: Implement full sync engine (Phase 3, PR #20)
	fmt.Printf("Syncing %s... (sync engine not yet implemented)\n", specID)
	fmt.Println("Configure docs integration and the sync engine will handle bidirectional sync.")
	return nil
}
