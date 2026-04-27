package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Full-text search across active and archived specs",
	Args:  cobra.ExactArgs(1),
	RunE:  runSearch,
}

func init() {
	rootCmd.AddCommand(searchCmd)
}

func runSearch(cmd *cobra.Command, args []string) error {
	query := strings.ToLower(args[0])

	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	if rc.Team != nil {
		if _, err := gitpkg.EnsureSpecsRepo(ctx(), &rc.Team.SpecsRepo); err != nil {
			return fmt.Errorf("syncing specs repo: %w", err)
		}
	}

	if rc.SpecsRepoDir == "" {
		return fmt.Errorf("specs repo not configured — run 'spec config init' to set up")
	}

	// Search in active specs
	results := searchDir(rc.SpecsRepoDir, query)

	// Search in archive
	archiveDir := filepath.Join(rc.SpecsRepoDir, "archive")
	results = append(results, searchDir(archiveDir, query)...)

	if len(results) == 0 {
		fmt.Println("No matches found.")
		return nil
	}

	fmt.Printf("Found %d match(es) for %q:\n\n", len(results), args[0])
	for _, r := range results {
		fmt.Printf("  %-10s  %-30s  [%s]\n", r.id, truncate(r.title, 30), r.status)
		for _, excerpt := range r.excerpts {
			fmt.Printf("             ...%s...\n", excerpt)
		}
		fmt.Println()
	}

	return nil
}

type searchResult struct {
	id       string
	title    string
	status   string
	excerpts []string
}

func searchDir(dir string, query string) []searchResult {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var results []searchResult
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}

		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		content := strings.ToLower(string(data))
		if !strings.Contains(content, query) {
			continue
		}

		meta, err := markdown.ReadMeta(path)
		if err != nil {
			continue
		}

		// Extract matching excerpts
		var excerpts []string
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.Contains(strings.ToLower(line), query) {
				excerpt := strings.TrimSpace(line)
				if len(excerpt) > 80 {
					excerpt = excerpt[:80]
				}
				excerpts = append(excerpts, excerpt)
				if len(excerpts) >= 3 {
					break
				}
			}
		}

		results = append(results, searchResult{
			id:       meta.ID,
			title:    meta.Title,
			status:   meta.Status,
			excerpts: excerpts,
		})
	}

	return results
}
