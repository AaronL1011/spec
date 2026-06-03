package thread

import (
	"os"
	"testing"
	"time"
)

// fixedClock returns a deterministic, monotonically increasing clock so tests
// get stable ordering and timestamps.
func fixedClock() func() time.Time {
	base := time.Date(2026, 5, 30, 9, 0, 0, 0, time.UTC)
	var n int
	return func() time.Time {
		t := base.Add(time.Duration(n) * time.Minute)
		n++
		return t
	}
}

func newTestStore(t *testing.T) *SidecarStore {
	t.Helper()
	s := NewSidecarStore(t.TempDir())
	s.now = fixedClock()
	return s
}

func TestCreate_AnchorsToSectionAndOpens(t *testing.T) {
	s := newTestStore(t)
	got, err := s.Create("SPEC-012", "technical_implementation", "@mike", "Why Redis?")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.Section != "technical_implementation" {
		t.Errorf("section = %q, want technical_implementation", got.Section)
	}
	if got.Status != StatusOpen {
		t.Errorf("status = %q, want open", got.Status)
	}
	if got.ID == "" || got.Question != "Why Redis?" || got.Author != "@mike" {
		t.Errorf("unexpected thread: %+v", got)
	}
}

func TestCreate_RejectsEmptyInput(t *testing.T) {
	s := newTestStore(t)
	tests := []struct {
		name, section, question string
	}{
		{"empty question", "sec", "   "},
		{"empty section", "  ", "q"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s.Create("SPEC-1", tt.section, "@a", tt.question); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestReply_AppendsInOrderAndKeepsOpen(t *testing.T) {
	s := newTestStore(t)
	th, _ := s.Create("SPEC-1", "sec", "@mike", "q?")
	if _, err := s.Reply("SPEC-1", th.ID, "@aaron", "first"); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	got, err := s.Reply("SPEC-1", th.ID, "@sara", "second")
	if err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if len(got.Replies) != 2 {
		t.Fatalf("replies = %d, want 2", len(got.Replies))
	}
	if got.Replies[0].Body != "first" || got.Replies[1].Body != "second" {
		t.Errorf("reply order wrong: %+v", got.Replies)
	}
	if !got.IsOpen() {
		t.Error("thread should stay open after replies")
	}
}

func TestReply_EmptyBodyRejected(t *testing.T) {
	s := newTestStore(t)
	th, _ := s.Create("SPEC-1", "sec", "@mike", "q?")
	if _, err := s.Reply("SPEC-1", th.ID, "@a", "  "); err == nil {
		t.Error("expected error for empty reply")
	}
}

func TestResolve_SetsStatusAndIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	th, _ := s.Create("SPEC-1", "sec", "@mike", "q?")
	got, err := s.Resolve("SPEC-1", th.ID, "@aaron")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Status != StatusResolved || got.ResolvedBy != "@aaron" || got.ResolvedAt == nil {
		t.Errorf("unexpected resolved thread: %+v", got)
	}
	// Idempotent: resolving again keeps the first resolver/time.
	again, err := s.Resolve("SPEC-1", th.ID, "@someone-else")
	if err != nil {
		t.Fatalf("Resolve again: %v", err)
	}
	if again.ResolvedBy != "@aaron" {
		t.Errorf("resolver changed on re-resolve: %q", again.ResolvedBy)
	}
}

func TestList_MissingSidecarIsEmptyNotError(t *testing.T) {
	s := newTestStore(t)
	got, err := s.List("SPEC-999")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("threads = %d, want 0", len(got))
	}
}

func TestSidecar_RoundTripsLosslessly(t *testing.T) {
	s := newTestStore(t)
	th, _ := s.Create("SPEC-1", "sec", "@mike", "q?")
	_, _ = s.Reply("SPEC-1", th.ID, "@aaron", "answer")
	_, _ = s.Resolve("SPEC-1", th.ID, "@mike")

	// Re-open a fresh store over the same dir and confirm identical content.
	s2 := NewSidecarStore(s.dir)
	got, err := s2.List("SPEC-1")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(got) != 1 || got[0].Question != "q?" || len(got[0].Replies) != 1 || got[0].IsOpen() {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestSave_StableSerialization(t *testing.T) {
	s := newTestStore(t)
	th, _ := s.Create("SPEC-1", "sec", "@mike", "q?")
	_, _ = s.Reply("SPEC-1", th.ID, "@aaron", "answer")

	first, err := os.ReadFile(s.SidecarPath("SPEC-1"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// Reload and re-save without changes; bytes must be identical.
	doc, _ := s.load("SPEC-1")
	if err := s.save("SPEC-1", doc); err != nil {
		t.Fatalf("save: %v", err)
	}
	second, _ := os.ReadFile(s.SidecarPath("SPEC-1"))
	if string(first) != string(second) {
		t.Errorf("serialization not stable:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

func TestSave_RemovesSidecarWhenEmpty(t *testing.T) {
	s := newTestStore(t)
	// Manually persist then empty.
	if err := s.save("SPEC-1", document{}); err != nil {
		t.Fatalf("save empty: %v", err)
	}
	if _, err := os.Stat(s.SidecarPath("SPEC-1")); !os.IsNotExist(err) {
		t.Error("empty sidecar should not exist on disk")
	}
}

func TestParseMarshal_RoundTripThroughMerge(t *testing.T) {
	// Simulates the sidecar conflict-resolution path: two serialized sidecars
	// are parsed, merged, and re-marshalled into a single reconciled document.
	at := time.Date(2026, 5, 30, 10, 0, 0, 0, time.UTC)
	localDoc, err := Marshal([]Thread{
		{ID: "T-1", Section: "s", Status: StatusOpen, Created: at, Question: "local"},
	})
	if err != nil {
		t.Fatalf("Marshal local: %v", err)
	}
	remoteDoc, err := Marshal([]Thread{
		{ID: "T-2", Section: "s", Status: StatusOpen, Created: at.Add(time.Minute), Question: "remote"},
	})
	if err != nil {
		t.Fatalf("Marshal remote: %v", err)
	}

	local, err := Parse(localDoc)
	if err != nil {
		t.Fatalf("Parse local: %v", err)
	}
	remote, err := Parse(remoteDoc)
	if err != nil {
		t.Fatalf("Parse remote: %v", err)
	}

	merged := Merge(local, remote)
	if len(merged) != 2 {
		t.Fatalf("merged threads = %d, want 2", len(merged))
	}

	// Re-marshalling the merged set must parse back to the same threads.
	out, err := Marshal(merged)
	if err != nil {
		t.Fatalf("Marshal merged: %v", err)
	}
	reparsed, err := Parse(out)
	if err != nil {
		t.Fatalf("Parse merged: %v", err)
	}
	if len(reparsed) != 2 {
		t.Fatalf("reparsed threads = %d, want 2", len(reparsed))
	}
}

func TestParse_EmptyIsEmpty(t *testing.T) {
	threads, err := Parse(nil)
	if err != nil {
		t.Fatalf("Parse(nil): %v", err)
	}
	if len(threads) != 0 {
		t.Fatalf("Parse(nil) = %d threads, want 0", len(threads))
	}
}

func TestMerge_UnionsThreadsAndReplies(t *testing.T) {
	at := time.Date(2026, 5, 30, 10, 0, 0, 0, time.UTC)
	resolvedAt := at.Add(time.Hour)
	a := []Thread{
		{ID: "T-1", Section: "s", Status: StatusOpen, Created: at, Question: "q1",
			Replies: []Reply{{Author: "@a", At: at, Body: "shared"}}},
		{ID: "T-2", Section: "s", Status: StatusOpen, Created: at.Add(time.Minute), Question: "q2"},
	}
	b := []Thread{
		{ID: "T-1", Section: "s", Status: StatusResolved, ResolvedBy: "@b", ResolvedAt: &resolvedAt, Created: at, Question: "q1",
			Replies: []Reply{
				{Author: "@a", At: at, Body: "shared"},               // dup
				{Author: "@b", At: at.Add(time.Minute), Body: "new"}, // unique
			}},
		{ID: "T-3", Section: "s", Status: StatusOpen, Created: at.Add(2 * time.Minute), Question: "q3"},
	}

	got := Merge(a, b)
	if len(got) != 3 {
		t.Fatalf("merged threads = %d, want 3", len(got))
	}
	// T-1 should be resolved (resolution wins) with 2 unioned replies.
	var t1 *Thread
	for i := range got {
		if got[i].ID == "T-1" {
			t1 = &got[i]
		}
	}
	if t1 == nil {
		t.Fatal("T-1 missing after merge")
	}
	if t1.IsOpen() {
		t.Error("T-1 should be resolved after merge")
	}
	if len(t1.Replies) != 2 {
		t.Errorf("T-1 replies = %d, want 2 (deduped)", len(t1.Replies))
	}
}
