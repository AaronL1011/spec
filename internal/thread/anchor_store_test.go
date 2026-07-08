package thread

import (
	"testing"
	"time"
)

func TestCreateQuoted_PersistsQuoteAnchor(t *testing.T) {
	s := newTestStore(t)
	got, err := s.CreateQuoted("SPEC-012", "technical_implementation", "@mike",
		"Why capped at three?", nil, "Retries are capped at three attempts.", "the gate")
	if err != nil {
		t.Fatalf("CreateQuoted: %v", err)
	}
	if got.Quote != "Retries are capped at three attempts." {
		t.Errorf("quote = %q, want the span", got.Quote)
	}
	if got.QuotePrefix != "the gate" {
		t.Errorf("quotePrefix = %q, want 'the gate'", got.QuotePrefix)
	}

	// Round-trips through the sidecar.
	loaded, err := s.List("SPEC-012")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(loaded) != 1 || loaded[0].Quote != got.Quote || loaded[0].QuotePrefix != got.QuotePrefix {
		t.Errorf("loaded = %+v, want quote anchor preserved", loaded)
	}
}

func TestCreateQuoted_EmptyQuoteDropsPrefix(t *testing.T) {
	s := newTestStore(t)
	got, err := s.CreateQuoted("SPEC-012", "problem_statement", "@mike", "q?", nil, "  ", "dangling prefix")
	if err != nil {
		t.Fatalf("CreateQuoted: %v", err)
	}
	if got.Quote != "" || got.QuotePrefix != "" {
		t.Errorf("empty quote must not persist a prefix, got quote=%q prefix=%q", got.Quote, got.QuotePrefix)
	}
}

func TestCreate_IsQuoteFreeCreateQuoted(t *testing.T) {
	s := newTestStore(t)
	got, err := s.Create("SPEC-012", "problem_statement", "@mike", "q?", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.Quote != "" || got.QuotePrefix != "" {
		t.Errorf("Create must produce a section-level thread, got %+v", got)
	}
}

func TestReanchor_MovesSection(t *testing.T) {
	s := newTestStore(t)
	created, err := s.Create("SPEC-012", "old_slug", "@mike", "severed?", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Reanchor("SPEC-012", created.ID, "technical_implementation")
	if err != nil {
		t.Fatalf("Reanchor: %v", err)
	}
	if got.Section != "technical_implementation" {
		t.Errorf("section = %q, want technical_implementation", got.Section)
	}

	loaded, err := s.List("SPEC-012")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if loaded[0].Section != "technical_implementation" {
		t.Error("re-anchor must persist to the sidecar")
	}
}

func TestReanchor_MissingThreadErrors(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Reanchor("SPEC-012", "T-404", "problem_statement"); err == nil {
		t.Error("re-anchoring a missing thread must error")
	}
}

func TestReanchor_SameSectionIdempotent(t *testing.T) {
	s := newTestStore(t)
	created, err := s.Create("SPEC-012", "problem_statement", "@mike", "q?", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Reanchor("SPEC-012", created.ID, "problem_statement")
	if err != nil {
		t.Fatalf("Reanchor (idempotent): %v", err)
	}
	if got.Section != "problem_statement" {
		t.Errorf("section = %q, want unchanged", got.Section)
	}
}

func TestLastActivity(t *testing.T) {
	created := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	th := Thread{ID: "T-1", Created: created}
	if !th.LastActivity().Equal(created) {
		t.Errorf("no replies: LastActivity = %v, want Created", th.LastActivity())
	}
	replyAt := created.Add(2 * time.Hour)
	th.Replies = []Reply{{Author: "@bob", At: replyAt, Body: "x"}}
	if !th.LastActivity().Equal(replyAt) {
		t.Errorf("with reply: LastActivity = %v, want last reply time", th.LastActivity())
	}
}

func TestMerge_PreservesQuoteAnchor(t *testing.T) {
	a := []Thread{{ID: "T-1", Section: "s", Status: StatusOpen, Author: "@a",
		Question: "q", Quote: "the quoted span", QuotePrefix: "pre"}}
	b := []Thread{{ID: "T-1", Section: "s", Status: StatusOpen, Author: "@a", Question: "q"}}
	merged := Merge(a, b)
	if len(merged) != 1 || merged[0].Quote != "the quoted span" {
		t.Errorf("merge dropped the quote anchor: %+v", merged)
	}
}
