package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/spf13/cobra"
)

var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "List recently completed specs",
	RunE:  runHistory,
}

func init() {
	historyCmd.Flags().Int("limit", 20, "maximum number of specs to show")
	rootCmd.AddCommand(historyCmd)
}

func runHistory(cmd *cobra.Command, args []string) error {
	limit, _ := cmd.Flags().GetInt("limit")

	rc, err := resolveConfig()
	if err != nil {
		return err
	}

	if rc.SpecsRepoDir == "" {
		return fmt.Errorf("specs repo not configured")
	}

	archiveDir := filepath.Join(rc.SpecsRepoDir, config.ArchiveDir(rc.Team))
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		fmt.Println("No archived specs found.")
		return nil
	}

	count := 0
	fmt.Println("Completed specs (most recent first):")
	fmt.Println()
	// Read in reverse order (files are alphabetical, so higher IDs are newer)
	for i := len(entries) - 1; i >= 0 && count < limit; i-- {
		e := entries[i]
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}

		path := filepath.Join(archiveDir, e.Name())
		meta, err := markdown.ReadMeta(path)
		if err != nil {
			continue
		}

		fmt.Printf("  %-10s  %-40s  %s\n", meta.ID, truncate(meta.Title, 40), meta.Cycle)
		count++
	}

	if count == 0 {
		fmt.Println("  No completed specs yet.")
	}

	return nil
}
