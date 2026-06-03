package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var contextCmd = &cobra.Command{
	Use:   "context <question>",
	Short: "Semantic search — find relevant specs and decisions for a question",
	Args:  cobra.ExactArgs(1),
	RunE:  runContext,
}

func init() {
	rootCmd.AddCommand(contextCmd)
}

func runContext(cmd *cobra.Command, args []string) error {
	question := args[0]

	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	// Keyword search over the specs repo.
	query := strings.ToLower(question)

	// Extract keywords (simple: split on spaces, filter short words)
	words := strings.Fields(query)
	var keywords []string
	for _, w := range words {
		if len(w) > 3 {
			keywords = append(keywords, w)
		}
	}

	if len(keywords) == 0 {
		return fmt.Errorf("could not extract keywords from question — try a more specific query")
	}

	fmt.Printf("Searching for: %s\n\n", strings.Join(keywords, ", "))

	if rc.SpecsRepoDir == "" {
		return fmt.Errorf("specs repo not configured")
	}

	// Search using the first keyword (simplified)
	for _, kw := range keywords {
		results := searchDir(rc.SpecsRepoDir, kw)
		if len(results) > 0 {
			fmt.Printf("Relevant specs for %q:\n\n", kw)
			for _, r := range results {
				fmt.Printf("  %-10s  %s  [%s]\n", r.id, r.title, r.status)
			}
			fmt.Println()
		}
	}

	return nil
}
