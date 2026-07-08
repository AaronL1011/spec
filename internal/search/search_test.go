package search

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/store"
)

// newSearchIndexer builds an Indexer over a temp specs repo + in-memory DB
// and returns it with a cleanup. Each spec is written as minimal valid spec
// markdown so markdown.ParseMeta + ExtractSections parse cleanly.
func newSearchIndexer(t *testing.T, specs ...specDoc) (*Indexer, *config.ResolvedConfig) {
	t.Helper()
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "specs")
	arcDir := filepath.Join(repoDir, "archive")
	if err := os.MkdirAll(arcDir, 0o755); err != nil {
		t.Fatalf("mkdir archive: %v", err)
	}
	for _, s := range specs {
		path := filepath.Join(repoDir, s.id+".md")
		if s.archived {
			path = filepath.Join(arcDir, s.id+".md")
		}
		if err := os.WriteFile(path, []byte(specMarkdown(s)), 0o644); err != nil {
			t.Fatalf("write %s: %v", s.id, err)
		}
		if s.mtime != nil {
			_ = os.Chtimes(path, *s.mtime, *s.mtime)
		}
	}
	db, err := store.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	rc := &config.ResolvedConfig{
		Team:         &config.TeamConfig{},
		SpecsRepoDir: repoDir,
	}
	ix := NewIndexer(rc, db)
	return ix, rc
}

type specDoc struct {
	id        string
	title     string
	status    string
	author    string
	assignees []string
	epicKey   string
	body      string
	archived  bool
	mtime     *time.Time // optional explicit mtime
}

func specMarkdown(s specDoc) string {
	status := s.status
	if status == "" {
		status = "draft"
	}
	arch := ""
	if s.archived {
		arch = " (archived)"
	}
	metaLines := []string{
		"---",
		"id: " + s.id,
		"title: " + s.title + arch,
		"status: " + status,
	}
	if s.author != "" {
		metaLines = append(metaLines, "author: "+s.author)
	}
	for i, a := range s.assignees {
		if i == 0 {
			metaLines = append(metaLines, "assignees:")
		}
		metaLines = append(metaLines, "  - "+a)
	}
	if s.epicKey != "" {
		metaLines = append(metaLines, "epic_key: "+s.epicKey)
	}
	// H1 is present but skipped by the indexer (level != 2); only the single
	// ## TL;DR section is indexed, keeping one hit per spec for clean tests.
	return strings.Join(append(metaLines,
		"---",
		"",
		"# "+s.id+" — "+s.title,
		"",
		"## TL;DR",
		"",
		s.body,
	), "\n")
}

func TestReconcileIndexesAndIsIncremental(t *testing.T) {
	ix, _ := newSearchIndexer(t,
		specDoc{id: "SPEC-001", title: "Sync Gateway", body: "sync conflict strategy"},
		specDoc{id: "SPEC-002", title: "Payments", body: "payment retries"},
	)
	// Pin mtimes so the second reconcile is a no-op.
	pinMtimes(t, ix)

	st, err := ix.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	if st.Reindexed != 2 {
		t.Fatalf("first reconcile reindexed = %d, want 2 (stats: %+v)", st.Reindexed, st)
	}

	// Second reconcile with no changes → zero work (changed paths empty).
	st, err = ix.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	if st.Reindexed != 0 {
		t.Errorf("unchanged reconcile reindexed = %d, want 0 (stats: %+v)", st.Reindexed, st)
	}
	if st.Deleted != 0 {
		t.Errorf("unchanged reconcile deleted = %d, want 0", st.Deleted)
	}

	// A chtimes-only touch (mtime moved, content identical) must be skipped
	// via the hash check, not re-indexed.
	touch := time.Unix(2000, 0)
	for _, name := range []string{"SPEC-001.md", "SPEC-002.md"} {
		_ = os.Chtimes(filepath.Join(ix.rc.SpecsRepoDir, name), touch, touch)
	}
	st, err = ix.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("reconcile 3: %v", err)
	}
	if st.Reindexed != 0 {
		t.Errorf("mtime-only touch should not reindex, got %d", st.Reindexed)
	}
	if st.Skipped != 2 {
		t.Errorf("mtime-only touch skipped = %d, want 2 (hash matched)", st.Skipped)
	}
}

func TestReconcilePicksUpEditAndDelete(t *testing.T) {
	ix, rc := newSearchIndexer(t,
		specDoc{id: "SPEC-001", title: "Sync", body: "sync gateway"},
		specDoc{id: "SPEC-002", title: "Old", body: "legacy gateway"},
	)
	pinMtimes(t, ix)
	if _, err := ix.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}

	// Edit SPEC-001 content + bump its mtime.
	path := filepath.Join(rc.SpecsRepoDir, "SPEC-001.md")
	if err := os.WriteFile(path, []byte(specMarkdown(specDoc{id: "SPEC-001", title: "Sync", body: "sync conflict strategy"})), 0o644); err != nil {
		t.Fatalf("edit: %v", err)
	}
	future := time.Unix(5000, 0)
	_ = os.Chtimes(path, future, future)

	// Delete SPEC-002 entirely.
	if err := os.Remove(filepath.Join(rc.SpecsRepoDir, "SPEC-002.md")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	st, err := ix.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	if st.Reindexed != 1 {
		t.Errorf("reindexed = %d, want 1 (SPEC-001 edit)", st.Reindexed)
	}
	if st.Deleted != 1 {
		t.Errorf("deleted = %d, want 1 (SPEC-002 removed)", st.Deleted)
	}

	// The new content is now searchable; the old spec is gone.
	hits, err := ix.Search(context.Background(), "conflict", Options{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 || hits[0].SpecID != "SPEC-001" {
		t.Fatalf("after edit, top hit = %+v, want SPEC-001", hits)
	}
	for _, h := range hits {
		if h.SpecID != "SPEC-001" {
			t.Errorf("after edit, stray hit %q (want only SPEC-001)", h.SpecID)
		}
	}
	hits, _ = ix.Search(context.Background(), "legacy", Options{})
	if len(hits) != 0 {
		t.Errorf("deleted spec should return no hits, got %+v", hits)
	}
}

func TestSearchActiveArchiveScope(t *testing.T) {
	ix, _ := newSearchIndexer(t,
		specDoc{id: "SPEC-001", title: "Sync", body: "sync gateway", archived: false},
		specDoc{id: "SPEC-002", title: "Old Sync", body: "sync legacy", archived: true},
	)
	if _, err := ix.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	all, _ := ix.Search(context.Background(), "sync", Options{Scope: ScopeAll})
	if len(all) != 2 {
		t.Fatalf("scope all = %d, want 2", len(all))
	}
	active, _ := ix.Search(context.Background(), "sync", Options{Scope: ScopeActive})
	if len(active) != 1 || active[0].SpecID != "SPEC-001" {
		t.Fatalf("scope active = %+v, want SPEC-001", active)
	}
	archived, _ := ix.Search(context.Background(), "sync", Options{Scope: ScopeArchived})
	if len(archived) != 1 || !archived[0].Archived {
		t.Fatalf("scope archived = %+v, want archived SPEC-002", archived)
	}
}

func TestSearchMultiWordRanksAllTermsFirst(t *testing.T) {
	ix, _ := newSearchIndexer(t,
		specDoc{id: "SPEC-BOTH", title: "Sync", body: "sync conflict strategy warn"},
		specDoc{id: "SPEC-ONE", title: "Other", body: "sync unrelated notes about something else"},
	)
	if _, err := ix.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	hits, err := ix.Search(context.Background(), "sync conflict", Options{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected hits")
	}
	if hits[0].SpecID != "SPEC-BOTH" {
		t.Errorf("top hit = %s, want SPEC-BOTH (both terms)", hits[0].SpecID)
	}
}

func TestSearchPrefixFallbackOnZeroHits(t *testing.T) {
	ix, _ := newSearchIndexer(t,
		specDoc{id: "SPEC-001", title: "Conflict", body: "conflict resolution"},
	)
	if _, err := ix.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	// "confl" is not a complete token; the prefix retry must still match.
	hits, err := ix.Search(context.Background(), "confl", Options{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("prefix fallback should match 'confl' against 'conflict'")
	}
	if hits[0].SpecID != "SPEC-001" {
		t.Errorf("prefix hit = %s, want SPEC-001", hits[0].SpecID)
	}
}

func TestSearchColdIndexFallback(t *testing.T) {
	// Fresh indexer, never reconciled: index is empty.
	ix, _ := newSearchIndexer(t,
		specDoc{id: "SPEC-001", title: "Sync", body: "sync gateway"},
	)
	// No reconcile — index cold. Search must fall back to the live scan.
	hits, err := ix.Search(context.Background(), "sync", Options{})
	if err != nil {
		t.Fatalf("search (cold): %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("cold-index fallback = %d hits, want 1", len(hits))
	}
	if hits[0].SpecID != "SPEC-001" {
		t.Errorf("fallback hit = %s, want SPEC-001", hits[0].SpecID)
	}
	if hits[0].Snippet == "" {
		t.Error("fallback hit should carry an excerpt snippet")
	}
}

func TestNilDBUsesFallback(t *testing.T) {
	_, rc := newSearchIndexer(t,
		specDoc{id: "SPEC-001", title: "Sync", body: "sync gateway"},
	)
	// Hand the indexer a nil db over the same repo.
	ixNil := NewIndexer(rc, nil)
	hits, err := ixNil.Search(context.Background(), "sync", Options{})
	if err != nil {
		t.Fatalf("nil-db search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("nil-db fallback = %d hits, want 1", len(hits))
	}
}

func TestBuildMatchEscapesHostileInput(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"   ", ""},
		{`sync "conflict"`, `"sync" "conflict"`},
		{"sync NEAR/2 conflict", `"sync" "NEAR/2" "conflict"`},
		{`"*) OR (1`, `"*)" "OR" "(1"`},
		{"emoji 🚀 sync", `"emoji" "🚀" "sync"`},
	}
	for _, c := range cases {
		got := buildMatch(c.in)
		if got != c.want {
			t.Errorf("buildMatch(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// A hostile MATCH must not error when run through the index.
	ix, _ := newSearchIndexer(t,
		specDoc{id: "SPEC-001", title: "Sync", body: "sync gateway"},
	)
	if _, err := ix.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if _, err := ix.Search(context.Background(), `"*) OR (1`, Options{}); err != nil {
		t.Errorf("hostile query should not error, got: %v", err)
	}
}

func TestKeywordScanMatchesSubstring(t *testing.T) {
	_, rc := newSearchIndexer(t,
		specDoc{id: "SPEC-001", title: "Sync", body: "sync gateway"},
	)
	hits := KeywordScan(rc, "sync")
	if len(hits) != 1 || hits[0].SpecID != "SPEC-001" {
		t.Fatalf("KeywordScan = %+v, want SPEC-001", hits)
	}
}

// pinMtimes pins every spec file's mtime to a known value so incremental
// reconcile tests are deterministic.
func pinMtimes(t *testing.T, ix *Indexer) {
	t.Helper()
	rc := ix.rc
	pin := func(dir string) {
		entries, _ := os.ReadDir(dir)
		mt := time.Unix(1000, 0)
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
				continue
			}
			_ = os.Chtimes(filepath.Join(dir, e.Name()), mt, mt)
		}
	}
	pin(rc.SpecsRepoDir)
	pin(filepath.Join(rc.SpecsRepoDir, "archive"))
}

// BenchmarkSearch documents the headroom of an FTS query over a generated
// 200-spec corpus. No CI gate on timing; this exists to prove sub-millisecond
// query cost so the overlay's 80 ms debounce budget is comfortable.
func BenchmarkSearch(b *testing.B) {
	dir := b.TempDir()
	repoDir := filepath.Join(dir, "specs")
	_ = os.MkdirAll(repoDir, 0o755)
	for i := range 200 {
		body := "sync conflict strategy warn retry gateway config"
		if i%2 == 0 {
			body = "payments retry ledger reconcile"
		}
		_ = os.WriteFile(filepath.Join(repoDir, specID(i)+".md"),
			[]byte(specMarkdown(specDoc{id: specID(i), title: "Spec " + specID(i), body: body})), 0o644)
	}
	db, err := store.OpenMemory()
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	rc := &config.ResolvedConfig{Team: &config.TeamConfig{}, SpecsRepoDir: repoDir}
	ix := NewIndexer(rc, db)
	if _, err := ix.Reconcile(context.Background()); err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ResetTimer()
	for range b.N {
		if _, err := ix.Search(ctx, "sync conflict strategy", Options{}); err != nil {
			b.Fatal(err)
		}
	}
}

func specID(i int) string {
	return fmt.Sprintf("SPEC-%03d", i)
}

// TestSearchByMetadata asserts specs are findable by author, assignee,
// epic key, spec id, and title — not just body text (SPEC-028 follow-up).
func TestSearchByMetadata(t *testing.T) {
	ix, _ := newSearchIndexer(t,
		specDoc{id: "SPEC-001", title: "Sync Gateway", author: "aaron",
			assignees: []string{"priya", "marcus"}, epicKey: "PLAT-42",
			body: "gateway retries"},
		specDoc{id: "SPEC-002", title: "Payments", author: "dana",
			body: "payment retries"},
	)
	if _, err := ix.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	cases := []struct {
		name, query, wantSpec string
	}{
		{"author", "aaron", "SPEC-001"},
		{"assignee", "priya", "SPEC-001"},
		{"second assignee", "marcus", "SPEC-001"},
		{"epic key", "PLAT-42", "SPEC-001"},
		{"spec id", "SPEC-002", "SPEC-002"},
		{"title word", "payments", "SPEC-002"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hits, err := ix.Search(context.Background(), tc.query, Options{})
			if err != nil {
				t.Fatalf("search %q: %v", tc.query, err)
			}
			if len(hits) == 0 {
				t.Fatalf("search %q: no hits, want %s", tc.query, tc.wantSpec)
			}
			if hits[0].SpecID != tc.wantSpec {
				t.Errorf("search %q top hit = %s, want %s", tc.query, hits[0].SpecID, tc.wantSpec)
			}
		})
	}
}

// TestSearchHitCarriesAuthor asserts the Hit surface exposes the author for
// display in the overlay group header and CLI rows.
func TestSearchHitCarriesAuthor(t *testing.T) {
	ix, _ := newSearchIndexer(t,
		specDoc{id: "SPEC-001", title: "Sync", author: "aaron", body: "sync gateway"},
	)
	if _, err := ix.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	hits, err := ix.Search(context.Background(), "gateway", Options{})
	if err != nil || len(hits) == 0 {
		t.Fatalf("search: hits=%d err=%v", len(hits), err)
	}
	if hits[0].Author != "aaron" {
		t.Errorf("hit author = %q, want aaron", hits[0].Author)
	}
}

// TestSearchMetadataBeatsIncidentalBodyMention asserts an author-name query
// ranks the authored spec above a spec that merely mentions the name in prose.
func TestSearchMetadataBeatsIncidentalBodyMention(t *testing.T) {
	ix, _ := newSearchIndexer(t,
		specDoc{id: "SPEC-BY", title: "Sync", author: "priya", body: "gateway retries"},
		specDoc{id: "SPEC-MENTION", title: "Other", author: "dana",
			body: "as priya suggested in review, retries back off"},
	)
	if _, err := ix.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	hits, err := ix.Search(context.Background(), "priya", Options{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) < 2 {
		t.Fatalf("want both specs to match, got %d hits", len(hits))
	}
	if hits[0].SpecID != "SPEC-BY" {
		t.Errorf("top hit = %s, want SPEC-BY (author field outranks body mention)", hits[0].SpecID)
	}
}
