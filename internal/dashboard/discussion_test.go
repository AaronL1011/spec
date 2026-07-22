package dashboard

import (
	"context"
	"testing"
	"time"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/identity"
	"github.com/aaronl1011/spec/internal/thread"
)

func TestIsViewerTurn(t *testing.T) {
	ana := identity.Viewer{Name: "Ana", Handle: "@ana"}
	ben := identity.Viewer{Name: "Ben", Handle: "@ben"}

	openAskedByBenNoReplies := thread.Thread{Status: thread.StatusOpen, Author: "@ben"}
	resolvedAskedByBen := thread.Thread{Status: thread.StatusResolved, Author: "@ben"}
	openMentionsAna := thread.Thread{Status: thread.StatusOpen, Author: "@ben", Mentions: []string{"ana"}}
	openAnaRepliedLast := thread.Thread{
		Status: thread.StatusOpen, Author: "@ben",
		Replies: []thread.Reply{{Author: "@ana"}},
	}
	openBenRepliedLast := thread.Thread{
		Status: thread.StatusOpen, Author: "@ben",
		Replies: []thread.Reply{{Author: "@ana"}, {Author: "@ben"}},
	}

	tests := []struct {
		name    string
		th      thread.Thread
		v       identity.Viewer
		claimed bool
		want    bool
	}{
		{"open, viewer is participant (asker), viewer spoke last (asked it)", openAskedByBenNoReplies, ben, false, false},
		{"open, addressed to a different viewer", openAskedByBenNoReplies, ana, false, false},
		{"resolved threads never need a turn", resolvedAskedByBen, ben, false, false},
		{"open, viewer mentioned, viewer has not spoken", openMentionsAna, ana, false, true},
		{"open, viewer replied last — not their turn anymore", openAnaRepliedLast, ana, false, false},
		{"open, someone else replied last after viewer — viewer's turn", openAnaRepliedLast, ben, false, true},
		{"open, viewer (asker) replied last again", openBenRepliedLast, ben, false, false},
		{"uninvolved viewer never has a turn", openMentionsAna, identity.Viewer{Name: "Carlos"}, false, false},
		{"claimant sees a thread they never participated in", openAskedByBenNoReplies, ana, true, true},
		{"claimant who spoke last still has no turn", openAnaRepliedLast, ana, true, false},
		{"claiming never surfaces resolved threads", resolvedAskedByBen, ana, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isViewerTurn(tt.th, tt.v, tt.claimed); got != tt.want {
				t.Errorf("isViewerTurn() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLastContributor(t *testing.T) {
	noReplies := thread.Thread{Author: "@ben"}
	withReplies := thread.Thread{Author: "@ben", Replies: []thread.Reply{{Author: "@ana"}, {Author: "@carlos"}}}

	if got := lastContributor(noReplies); got != "@ben" {
		t.Errorf("lastContributor(no replies) = %q, want @ben", got)
	}
	if got := lastContributor(withReplies); got != "@carlos" {
		t.Errorf("lastContributor(with replies) = %q, want @carlos", got)
	}
}

func TestLatestActivityAndLine(t *testing.T) {
	created := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	replyAt := created.Add(time.Hour)
	noReplies := thread.Thread{Created: created, Question: "why?"}
	withReplies := thread.Thread{
		Created: created, Question: "why?",
		Replies: []thread.Reply{{At: replyAt, Body: "because"}},
	}

	if got := latestActivity(noReplies); !got.Equal(created) {
		t.Errorf("latestActivity(no replies) = %v, want %v", got, created)
	}
	if got := latestActivity(withReplies); !got.Equal(replyAt) {
		t.Errorf("latestActivity(with replies) = %v, want %v", got, replyAt)
	}
	if got := latestLine(noReplies); got != "why?" {
		t.Errorf("latestLine(no replies) = %q, want %q", got, "why?")
	}
	if got := latestLine(withReplies); got != "because" {
		t.Errorf("latestLine(with replies) = %q, want %q", got, "because")
	}
}

func TestDiscussionItems(t *testing.T) {
	store := thread.NewSidecarStore(t.TempDir())
	if _, err := store.Create("SPEC-039", "technical_implementation", "@carlos",
		"can we use token bucket instead?", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}

	ana := identity.Viewer{Name: "Ana", Handle: "@ana", Identities: []string{"@ana"}}
	items := discussionItems(store, "SPEC-039", "Rate limiting", ana, false, 0, dashboardCurve(&config.ResolvedConfig{}), time.Now())
	// Ana was not mentioned and did not ask — not her turn.
	if len(items) != 0 {
		t.Fatalf("items for uninvolved viewer = %v, want none", items)
	}

	// If Ana has claimed the spec, the unanswered question surfaces for her
	// even though she was never mentioned.
	items = discussionItems(store, "SPEC-039", "Rate limiting", ana, true, 0, dashboardCurve(&config.ResolvedConfig{}), time.Now())
	if len(items) != 1 {
		t.Fatalf("items for claimant = %v, want 1", items)
	}

	carlos := identity.Viewer{Name: "Carlos", Handle: "@carlos", Identities: []string{"@carlos"}}
	items = discussionItems(store, "SPEC-039", "Rate limiting", carlos, false, 0, dashboardCurve(&config.ResolvedConfig{}), time.Now())
	// Carlos asked it and nobody has replied yet — it's not "his turn" either
	// (he spoke last), so no item for the asker until someone replies.
	if len(items) != 0 {
		t.Fatalf("items for the asker with no reply yet = %v, want none", items)
	}

	threads, _ := store.List("SPEC-039")
	if _, err := store.Reply("SPEC-039", threads[0].ID, "@bob", "yes, token bucket works", nil); err != nil {
		t.Fatalf("Reply: %v", err)
	}

	items = discussionItems(store, "SPEC-039", "Rate limiting", carlos, false, 0, dashboardCurve(&config.ResolvedConfig{}), time.Now())
	if len(items) != 1 {
		t.Fatalf("items for the asker after a reply = %v, want 1", items)
	}
	got := items[0]
	if got.SpecID != "SPEC-039" || got.Title != "Rate limiting" || got.Stage != "§technical_implementation" {
		t.Errorf("item = %+v, want SPEC-039/Rate limiting/§technical_implementation", got)
	}
	if got.Detail != `@bob: "yes, token bucket works"` {
		t.Errorf("Detail = %q, want the last contributor quoted", got.Detail)
	}
}

func TestDiscussionItems_MissingSidecarIsEmpty(t *testing.T) {
	store := thread.NewSidecarStore(t.TempDir())
	items := discussionItems(store, "SPEC-999", "No threads", identity.Viewer{}, false, 0, dashboardCurve(&config.ResolvedConfig{}), time.Now())
	if items != nil {
		t.Errorf("items for a spec with no sidecar = %v, want nil", items)
	}
}

// TestAggregate_Discussion_TurnSemantics is the golden test discussion-01 §8
// calls for: an open thread where it is the viewer's turn produces a
// DISCUSSION row, and a thread where the viewer replied last does not.
func TestAggregate_Discussion_TurnSemantics(t *testing.T) {
	dir := t.TempDir()
	writeSpec(t, dir, "SPEC-039", "engineering", nil)

	store := thread.NewSidecarStore(dir)
	th, err := store.Create("SPEC-039", "technical_implementation", "@carlos", "can we use token bucket instead?", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	ana := &config.ResolvedConfig{SpecsRepoDir: dir, Team: scopedTeamConfig(),
		User: userCfg("Ana", "@ana", "engineer")}

	// Nobody has replied yet and Ana isn't mentioned — no row for her.
	data, err := Aggregate(context.Background(), ana, nil, "engineer")
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if len(data.Discussion) != 0 {
		t.Fatalf("Discussion (uninvolved) = %v, want none", data.Discussion)
	}

	// Carlos replies mentioning Ana — now it's her turn.
	if _, err := store.Reply("SPEC-039", th.ID, "@carlos", "@ana what do you think?", nil); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	data, err = Aggregate(context.Background(), ana, nil, "engineer")
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if len(data.Discussion) != 1 || data.Discussion[0].SpecID != "SPEC-039" {
		t.Fatalf("Discussion (Ana's turn) = %v, want one SPEC-039 row", data.Discussion)
	}

	// Ana replies — she spoke last, so it's no longer her turn.
	if _, err := store.Reply("SPEC-039", th.ID, "@ana", "sounds good", nil); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	data, err = Aggregate(context.Background(), ana, nil, "engineer")
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if len(data.Discussion) != 0 {
		t.Fatalf("Discussion (Ana replied last) = %v, want none", data.Discussion)
	}
}

// TestAggregate_Discussion_ClaimedSpecSurfacesWithoutMention proves that a
// viewer who has claimed a spec sees new comments on it in DISCUSSION even
// when nobody @-mentioned them.
func TestAggregate_Discussion_ClaimedSpecSurfacesWithoutMention(t *testing.T) {
	dir := t.TempDir()
	writeSpec(t, dir, "SPEC-040", "engineering", []string{"@ana"})

	store := thread.NewSidecarStore(dir)
	if _, err := store.Create("SPEC-040", "acceptance_criteria", "@carlos",
		"is the retry budget per request or per client?", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}

	ana := &config.ResolvedConfig{SpecsRepoDir: dir, Team: scopedTeamConfig(),
		User: userCfg("Ana", "@ana", "engineer")}

	data, err := Aggregate(context.Background(), ana, nil, "engineer")
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if len(data.Discussion) != 1 || data.Discussion[0].SpecID != "SPEC-040" {
		t.Fatalf("Discussion (claimant, no mention) = %v, want one SPEC-040 row", data.Discussion)
	}

	// A different engineer who has not claimed the spec sees nothing.
	ben := &config.ResolvedConfig{SpecsRepoDir: dir, Team: scopedTeamConfig(),
		User: userCfg("Ben", "@ben", "engineer")}
	data, err = Aggregate(context.Background(), ben, nil, "engineer")
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if len(data.Discussion) != 0 {
		t.Fatalf("Discussion (non-claimant, uninvolved) = %v, want none", data.Discussion)
	}
}
