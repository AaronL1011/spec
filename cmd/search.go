package cmd

import (
	"fmt"

	gitpkg "github.com/aaronl1011/spec/internal/git"
	"github.com/aaronl1011/spec/internal/search"
	"github.com/spf13/cobra"
)

var (
	searchReindex bool
	searchScope   string
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Full-text search across active and archived specs",
	Long: `Full-text search across active and archived specs using a local FTS5 index.

Results are ranked by bm25 relevance and anchored to the matching section.
The index lives in the local spec store and is reconciled before each query;
pass --reindex to force a full rebuild. --scope limits results to active or
archived specs (default: all).`,
	Args: cobra.ExactArgs(1),
	RunE: runSearch,
}

func init() {
	searchCmd.Flags().BoolVar(&searchReindex, "reindex", false, "drop the index and rebuild it fully before querying")
	searchCmd.Flags().StringVar(&searchScope, "scope", "all", "search scope: all | active | archived")
	rootCmd.AddCommand(searchCmd)
}

func runSearch(cmd *cobra.Command, args []string) error {
	query := args[0]

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

	scope, err := parseSearchScope(searchScope)
	if err != nil {
		return err
	}

	db, err := openDB()
	if err != nil {
		return fmt.Errorf("opening local store: %w", err)
	}
	defer func() { _ = db.Close() }()

	ix := search.NewIndexer(rc, db)

	if searchReindex {
		if err := db.TruncateSearchIndex(ctx()); err != nil {
			return fmt.Errorf("clearing search index: %w", err)
		}
	}

	// Reconcile before querying so a fresh or externally-edited repo is current.
	stats, _ := ix.Reconcile(ctx())
	if searchReindex {
		p := newPrinter(cmd)
		if !p.JSONEnabled() {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
				"reindexed %d specs (%d deleted, %d skipped, %d failed)\n",
				stats.Reindexed, stats.Deleted, stats.Skipped, stats.Failed)
		}
	}

	hits, err := ix.Search(ctx(), query, search.Options{Scope: scope, Limit: search.DefaultLimit})
	if err != nil {
		return fmt.Errorf("searching: %w", err)
	}

	p := newPrinter(cmd)
	if p.JSONEnabled() {
		return p.JSON(hits)
	}

	if len(hits) == 0 {
		p.Line("No matches found.")
		return nil
	}

	p.Line("Found %d match(es) for %q:", len(hits), query)
	for _, h := range hits {
		marker := ""
		if h.Archived {
			marker = " [archived]"
		}
		author := ""
		if h.Author != "" {
			author = "  by " + h.Author
		}
		section := ""
		if h.SectionHeading != "" {
			section = "  " + h.SectionHeading
		}
		p.Line("  %-10s  %-30s  [%s]%s%s%s", h.SpecID, truncate(h.Title, 30), h.Status, author, marker, section)
		if h.Snippet != "" {
			p.Line("             %s", h.Snippet)
		}
		p.Line("")
	}
	return nil
}

// parseSearchScope maps the --scope flag to a search.SearchScope.
func parseSearchScope(s string) (search.SearchScope, error) {
	switch s {
	case "all", "":
		return search.ScopeAll, nil
	case "active":
		return search.ScopeActive, nil
	case "archived":
		return search.ScopeArchived, nil
	default:
		return search.ScopeAll, fmt.Errorf("invalid --scope %q — use all, active, or archived", s)
	}
}
