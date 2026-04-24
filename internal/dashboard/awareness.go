package dashboard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aaronl1011/spec-cli/internal/config"
	"github.com/aaronl1011/spec-cli/internal/markdown"
)

// PendingCount returns the number of specs awaiting action from the
// current user's role. Reads local files only — never blocks on network.
func PendingCount(rc *config.ResolvedConfig, role string) int {
	if rc == nil || rc.SpecsRepoDir == "" || role == "" {
		return 0
	}

	pl := rc.Pipeline()
	count := 0

	entries, err := os.ReadDir(rc.SpecsRepoDir)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		meta, err := markdown.ReadMeta(filepath.Join(rc.SpecsRepoDir, e.Name()))
		if err != nil || !strings.HasPrefix(meta.ID, "SPEC-") {
			continue
		}
		stage := pl.StageByName(meta.Status)
		if stage != nil && stage.HasOwner(role) {
			count++
		}
	}
	return count
}

// PrintAwarenessLine prints the passive "you have mail" indicator.
// Returns true if something was printed.
func PrintAwarenessLine(rc *config.ResolvedConfig, role string) bool {
	count := PendingCount(rc, role)
	if count == 0 {
		return false
	}

	fmt.Fprintf(os.Stderr, "⚠ %d pending · run 'spec' for details\n", count)
	return true
}
