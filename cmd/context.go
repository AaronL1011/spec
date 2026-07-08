package cmd

import (
	"fmt"
	"strings"

	"github.com/aaronl1011/spec/internal/search"
	"github.com/spf13/cobra"
)

var contextCmd = &cobra.Command{
	Use:   "context <question>",
	Short: "Keyword search — find specs and decisions matching words in a question",
	Long: `Find specs whose content matches the keywords in your question.

This is a literal keyword search, not semantic/vector search: the question is
split into words (short words are dropped) and specs containing those words are
listed. It does not rank by meaning or embeddings. For exact substring matching
across active and archived specs, use 'spec search' instead.`,
	Args: cobra.ExactArgs(1),
	RunE: runContext,
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

	// Search using each keyword via the shared search keyword scan (the
	// relocated substring logic), so spec context and the search overlay share
	// one source of truth.
	for _, kw := range keywords {
		results := search.KeywordScan(rc, kw)
		if len(results) > 0 {
			fmt.Printf("Relevant specs for %q:\n\n", kw)
			for _, r := range results {
				fmt.Printf("  %-10s  %s  [%s]\n", r.SpecID, r.Title, r.Status)
			}
			fmt.Println()
		}
	}

	return nil
}
