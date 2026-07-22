package dashboard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/identity"
	"github.com/aaronl1011/spec/internal/markdown"
	"github.com/aaronl1011/spec/internal/thread"
)

// PendingCount returns the number of specs awaiting action from the
// current user's role. Reads local files only — never blocks on network.
func PendingCount(rc *config.ResolvedConfig, role string) int {
	if rc == nil || rc.SpecsRepoDir == "" || role == "" {
		return 0
	}

	pl := rc.Pipeline()
	viewer := viewerFor(rc, role)
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
		// Reuse the dashboard DO resolver so the awareness count and the DO
		// section never disagree about what needs the viewer's attention.
		view := SpecView{
			Author:      meta.Author,
			Assignees:   meta.Assignees,
			Status:      meta.Status,
			BlockedFrom: meta.BlockedFrom,
		}
		if VisibleInDo(pl, view, viewer) {
			count++
		}
	}
	return count
}

// DiscussionCount returns the number of open discussion threads across every
// spec where it is the viewer's turn (isViewerTurn, discussion.go) — reading
// sidecars only, never blocking on network. Shares its turn predicate with
// the DISCUSSION dashboard section so the two can never disagree about what
// counts as "your turn".
func DiscussionCount(rc *config.ResolvedConfig, role string) int {
	if rc == nil || rc.SpecsRepoDir == "" {
		return 0
	}

	viewer := viewerFor(rc, role)
	store := thread.NewSidecarStore(rc.SpecsRepoDir)
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
		threads, err := store.List(meta.ID)
		if err != nil {
			continue
		}
		claimed := identity.AnyIdentity(meta.Assignees, viewer)
		for _, t := range threads {
			if isViewerTurn(t, viewer, claimed) {
				count++
			}
		}
	}
	return count
}

// PrintAwarenessLine prints the passive "you have mail" indicator, combining
// pending-stage work and awaited discussion replies into one row. A clause is
// omitted when its count is zero; nothing prints when both are zero.
func PrintAwarenessLine(rc *config.ResolvedConfig, role string) bool {
	pending := PendingCount(rc, role)
	discussions := DiscussionCount(rc, role)
	if pending == 0 && discussions == 0 {
		return false
	}

	var parts []string
	if pending > 0 {
		parts = append(parts, fmt.Sprintf("%d pending", pending))
	}
	if discussions > 0 {
		parts = append(parts, fmt.Sprintf("%d %s awaited", discussions, replyNoun(discussions)))
	}

	fmt.Fprintf(os.Stderr, "⚠ %s · run 'spec' for details\n", strings.Join(parts, " · "))
	return true
}

// replyNoun pluralises "reply" for the awareness line.
func replyNoun(n int) string {
	if n == 1 {
		return "reply"
	}
	return "replies"
}
