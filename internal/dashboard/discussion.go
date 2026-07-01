package dashboard

import (
	"fmt"
	"strings"
	"time"

	"github.com/aaronl1011/spec/internal/identity"
	"github.com/aaronl1011/spec/internal/thread"
	"github.com/aaronl1011/spec/internal/urgency"
)

// isViewerTurn reports whether an open thread is awaiting the viewer: they
// are a participant (author, replier, or mentioned) and did not speak last.
// Shared by the DISCUSSION dashboard section and the passive awareness line
// so the two can never disagree about what counts as "your turn".
func isViewerTurn(t thread.Thread, v identity.Viewer) bool {
	if !t.IsOpen() {
		return false
	}
	if !identity.AnyIdentity(t.Participants(), v) {
		return false
	}
	return !identity.MatchesIdentity(lastContributor(t), v)
}

// lastContributor returns whoever spoke last in a thread: the most recent
// reply's author, or the original asker when there are no replies yet.
func lastContributor(t thread.Thread) string {
	if n := len(t.Replies); n > 0 {
		return t.Replies[n-1].Author
	}
	return t.Author
}

// latestActivity returns a thread's most recent activity time: the last
// reply's timestamp, or when it was created if there are no replies yet.
func latestActivity(t thread.Thread) time.Time {
	if n := len(t.Replies); n > 0 {
		return t.Replies[n-1].At
	}
	return t.Created
}

// latestLine returns the text of a thread's most recent contribution: the
// last reply's body, or the original question if there are no replies yet.
func latestLine(t thread.Thread) string {
	if n := len(t.Replies); n > 0 {
		return t.Replies[n-1].Body
	}
	return t.Question
}

// displayHandle ensures handle carries exactly one leading '@' for display,
// regardless of whether it was stored with one (a handle) or without (a
// bare display name would not normally reach here, but a stray '@' is never
// doubled either way).
func displayHandle(handle string) string {
	return "@" + strings.TrimPrefix(handle, "@")
}

// discussionItems scans one spec's open threads and returns a DashboardItem
// for each where it is the viewer's turn. Reading the sidecar is local-file
// I/O, so it rides the same offline-capable, uncached-by-design contract as
// the rest of Aggregate (see the doc comment there).
//
// discussionWindow/curve reuse the dashboard's REVIEW staleness window: both
// sections share the same "something is waiting on you" semantics, so a
// second config knob isn't warranted for v1. A thread whose Section no longer
// matches a live section slug is not filtered out here — the dashboard is a
// pointer to the sidecar, not a live join against parsed sections, and stays
// the fallback discovery path when a heading rename orphans a reader anchor
// (see discussion-03-reader-cockpit.md §2.4).
func discussionItems(store *thread.SidecarStore, specID, specTitle string, viewer identity.Viewer, discussionWindow time.Duration, curve urgency.Curve, now time.Time) []DashboardItem {
	threads, err := store.List(specID)
	if err != nil {
		return nil
	}

	var items []DashboardItem
	for _, t := range threads {
		if !isViewerTurn(t, viewer) {
			continue
		}
		activity := latestActivity(t)
		frac := ReviewUrgency(discussionWindow, curve, activity, now)
		items = append(items, DashboardItem{
			SpecID:        specID,
			Title:         specTitle,
			Stage:         "§" + t.Section,
			Detail:        fmt.Sprintf("%s: %q", displayHandle(lastContributor(t)), truncStr(latestLine(t), 70)),
			SortTime:      activity,
			StaleFraction: frac,
			Urgency:       urgencyLabel(frac),
		})
	}
	return items
}
