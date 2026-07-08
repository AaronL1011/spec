package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// searchTestDB opens an in-memory DB with the search schema migrated.
func searchTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSearchMigrationCreatesTables(t *testing.T) {
	db := searchTestDB(t)
	var version int
	if err := db.conn.QueryRow("SELECT MAX(version) FROM migrations").Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != schemaVersion {
		t.Fatalf("schema version = %d, want %d", version, schemaVersion)
	}
	// spec_search is a virtual table; querying its structure should not error.
	if _, err := db.conn.Exec("SELECT spec_id FROM spec_search LIMIT 0"); err != nil {
		t.Fatalf("spec_search not queryable: %v", err)
	}
	if _, err := db.conn.Exec("SELECT path FROM spec_search_state LIMIT 0"); err != nil {
		t.Fatalf("spec_search_state not queryable: %v", err)
	}
}

func TestUpsertReplacesExistingRows(t *testing.T) {
	db := searchTestDB(t)
	ctx := context.Background()
	doc := SearchDoc{
		SpecID: "SPEC-001", Path: "/specs/SPEC-001.md",
		Title: "Auth", Status: "draft",
		Sections: []SearchSection{
			{Slug: "problem_statement", Heading: "Problem Statement", Body: "login is slow"},
			{Slug: "goals", Heading: "Goals", Body: "fast login"},
		},
	}
	if err := db.UpsertSpecSections(ctx, doc, 1, "hash-a"); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Re-index the same spec with a changed section and an extra section.
	doc.Sections = []SearchSection{
		{Slug: "problem_statement", Heading: "Problem Statement", Body: "login is slow and flaky"},
		{Slug: "acceptance", Heading: "Acceptance Criteria", Body: "login under 100ms"},
	}
	if err := db.UpsertSpecSections(ctx, doc, 2, "hash-b"); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}

	// No duplicate rows for the old "goals" section, and the new content wins.
	rows, err := db.conn.Query("SELECT section_slug, body FROM spec_search WHERE spec_id = ? ORDER BY section_slug", doc.SpecID)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	got := map[string]string{}
	for rows.Next() {
		var slug, body string
		if err := rows.Scan(&slug, &body); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[slug] = body
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 rows after replace, got %d (%v)", len(got), got)
	}
	if got["problem_statement"] != "login is slow and flaky" {
		t.Errorf("problem_statement body not replaced: %q", got["problem_statement"])
	}
	if _, ok := got["goals"]; ok {
		t.Error("stale 'goals' section still present after re-upsert")
	}
}

func TestQueryRanksAndReturnsSnippet(t *testing.T) {
	db := searchTestDB(t)
	ctx := context.Background()

	// Two specs: one section mentions both terms (should rank best), one only
	// one term.
	mustUpsert(t, db, SearchDoc{SpecID: "SPEC-A", Path: "/a.md", Title: "Sync", Status: "draft",
		Sections: []SearchSection{{Slug: "deps", Heading: "Dependencies", Body: "the sync conflict strategy is warn"}}})
	mustUpsert(t, db, SearchDoc{SpecID: "SPEC-B", Path: "/b.md", Title: "Other", Status: "draft",
		Sections: []SearchSection{{Slug: "deps", Heading: "Dependencies", Body: "the sync configuration only"}}})

	rows, err := db.QuerySpecSearch(ctx, `"sync" "conflict"`, ScopeAll, 50)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 hit (only SPEC-A has both terms), got %d", len(rows))
	}
	if rows[0].SpecID != "SPEC-A" {
		t.Errorf("top hit = %s, want SPEC-A", rows[0].SpecID)
	}
	if rows[0].SectionSlug != "deps" {
		t.Errorf("section slug = %q, want deps", rows[0].SectionSlug)
	}
	if rows[0].Snippet == "" || !contains(rows[0].Snippet, "⟨conflict⟩") {
		t.Errorf("snippet should highlight match, got %q", rows[0].Snippet)
	}
	if rows[0].Score > 0 {
		t.Errorf("bm25 score should be negative (lower=better), got %f", rows[0].Score)
	}
}

func TestQueryScopeFiltersArchived(t *testing.T) {
	db := searchTestDB(t)
	ctx := context.Background()
	mustUpsert(t, db, SearchDoc{SpecID: "SPEC-A", Path: "/a.md", Title: "Sync", Status: "done", Archived: false,
		Sections: []SearchSection{{Slug: "tl", Heading: "TL;DR", Body: "sync gateway"}}})
	mustUpsert(t, db, SearchDoc{SpecID: "SPEC-B", Path: "/b.md", Title: "Old Sync", Status: "closed", Archived: true,
		Sections: []SearchSection{{Slug: "tl", Heading: "TL;DR", Body: "sync legacy gateway"}}})

	all, err := db.QuerySpecSearch(ctx, `"sync"`, ScopeAll, 50)
	if err != nil {
		t.Fatalf("query all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("scope all = %d hits, want 2", len(all))
	}

	active, err := db.QuerySpecSearch(ctx, `"sync"`, ScopeActive, 50)
	if err != nil {
		t.Fatalf("query active: %v", err)
	}
	if len(active) != 1 || active[0].SpecID != "SPEC-A" {
		t.Fatalf("scope active = %+v, want only SPEC-A", active)
	}

	archived, err := db.QuerySpecSearch(ctx, `"sync"`, ScopeArchived, 50)
	if err != nil {
		t.Fatalf("query archived: %v", err)
	}
	if len(archived) != 1 || archived[0].SpecID != "SPEC-B" {
		t.Fatalf("scope archived = %+v, want only SPEC-B", archived)
	}
	if !archived[0].Archived {
		t.Error("archived hit should report Archived=true")
	}
}

func TestQueryRankingIsAscending(t *testing.T) {
	db := searchTestDB(t)
	ctx := context.Background()
	// A short, all-terms section should outrank a long, one-term section.
	mustUpsert(t, db, SearchDoc{SpecID: "SPEC-RICH", Path: "/rich.md", Title: "Big", Status: "draft",
		Sections: []SearchSection{{Slug: "tl", Heading: "TL;DR", Body: repeat("noise ", 200) + "sync"}}})
	mustUpsert(t, db, SearchDoc{SpecID: "SPEC-TIGHT", Path: "/tight.md", Title: "Sync", Status: "draft",
		Sections: []SearchSection{{Slug: "tl", Heading: "TL;DR", Body: "sync sync sync"}}})

	rows, err := db.QuerySpecSearch(ctx, `"sync"`, ScopeAll, 50)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) < 2 {
		t.Fatalf("expected 2 hits, got %d", len(rows))
	}
	// Scores must be non-increasing (ASC order).
	for i := 1; i < len(rows); i++ {
		if rows[i].Score < rows[i-1].Score {
			t.Errorf("row %d score %f < row %d score %f (not ascending)", i, rows[i].Score, i-1, rows[i-1].Score)
		}
	}
	if rows[0].SpecID != "SPEC-TIGHT" {
		t.Errorf("tight doc should rank first, got %s", rows[0].SpecID)
	}
}

func TestStateDiffDetectsChangedAndRemoved(t *testing.T) {
	db := searchTestDB(t)
	ctx := context.Background()
	// Index /1.md at mtime 1 and /2.md at mtime 2.
	if err := db.UpsertSpecSections(ctx, SearchDoc{SpecID: "SPEC-1", Path: "/1.md", Title: "A", Status: "draft",
		Sections: []SearchSection{{Slug: "tl", Heading: "TL;DR", Body: "x"}}}, 1, "h1"); err != nil {
		t.Fatalf("upsert 1: %v", err)
	}
	if err := db.UpsertSpecSections(ctx, SearchDoc{SpecID: "SPEC-2", Path: "/2.md", Title: "B", Status: "draft",
		Sections: []SearchSection{{Slug: "tl", Heading: "TL;DR", Body: "y"}}}, 2, "h2"); err != nil {
		t.Fatalf("upsert 2: %v", err)
	}

	// Disk matches both mtimes exactly → no changes, no removals.
	changed, removed, err := db.SearchStateDiff(ctx, map[string]int64{"/1.md": 1, "/2.md": 2})
	if err != nil {
		t.Fatalf("diff 1: %v", err)
	}
	if len(changed) != 0 {
		t.Errorf("unchanged repo should yield 0 changed, got %v", changed)
	}
	if len(removed) != 0 {
		t.Errorf("unchanged repo should yield 0 removed, got %v", removed)
	}

	// /1.md mtime moved; /2.md gone from disk (was indexed, now removed).
	changed, removed, err = db.SearchStateDiff(ctx, map[string]int64{"/1.md": 9})
	if err != nil {
		t.Fatalf("diff 2: %v", err)
	}
	if !containsStr(changed, "/1.md") {
		t.Errorf("moved-mtime file should be in changed, got %v", changed)
	}
	if !containsStr(removed, "/2.md") {
		t.Errorf("missing file should be in removed, got %v", removed)
	}

	// A brand-new file on disk appears in changed.
	changed, removed, err = db.SearchStateDiff(ctx, map[string]int64{"/1.md": 1, "/3.md": 3})
	if err != nil {
		t.Fatalf("diff 3: %v", err)
	}
	if !containsStr(changed, "/3.md") {
		t.Errorf("new file should be in changed, got %v", changed)
	}
	if !containsStr(removed, "/2.md") {
		t.Errorf("dropped /2.md should be in removed, got %v", removed)
	}
}

func TestStateHashSkipsMtimeOnlyTouches(t *testing.T) {
	db := searchTestDB(t)
	ctx := context.Background()
	doc := SearchDoc{SpecID: "SPEC-1", Path: "/1.md", Title: "A", Status: "draft",
		Sections: []SearchSection{{Slug: "tl", Heading: "TL;DR", Body: "original"}}}
	if err := db.UpsertSpecSections(ctx, doc, 1, "hash-1"); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	h, err := db.SearchStateHash(ctx, "/1.md")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if h != "hash-1" {
		t.Errorf("hash = %q, want hash-1", h)
	}
	// Unknown path returns empty.
	h, _ = db.SearchStateHash(ctx, "/missing.md")
	if h != "" {
		t.Errorf("unknown path hash = %q, want empty", h)
	}
}

func TestDeleteSpecSearchRemovesRows(t *testing.T) {
	db := searchTestDB(t)
	ctx := context.Background()
	mustUpsert(t, db, SearchDoc{SpecID: "SPEC-1", Path: "/1.md", Title: "A", Status: "draft",
		Sections: []SearchSection{{Slug: "tl", Heading: "TL;DR", Body: "search me"}}})
	mustUpsert(t, db, SearchDoc{SpecID: "SPEC-2", Path: "/2.md", Title: "B", Status: "draft",
		Sections: []SearchSection{{Slug: "tl", Heading: "TL;DR", Body: "search me"}}})

	if err := db.DeleteSpecSearch(ctx, "SPEC-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	rows, err := db.QuerySpecSearch(ctx, `"search"`, ScopeAll, 50)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 1 || rows[0].SpecID != "SPEC-2" {
		t.Fatalf("after delete, got %+v, want only SPEC-2", rows)
	}
}

func TestDeleteSpecSearchByPathRemovesSpec(t *testing.T) {
	db := searchTestDB(t)
	ctx := context.Background()
	mustUpsert(t, db, SearchDoc{SpecID: "SPEC-1", Path: filepath.Join("archive", "SPEC-1.md"),
		Title: "A", Status: "closed", Archived: true,
		Sections: []SearchSection{{Slug: "tl", Heading: "TL;DR", Body: "search me"}}})

	if err := db.DeleteSpecSearchByPath(ctx, filepath.Join("archive", "SPEC-1.md")); err != nil {
		t.Fatalf("delete by path: %v", err)
	}
	rows, err := db.QuerySpecSearch(ctx, `"search"`, ScopeAll, 50)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("after delete-by-path, got %d hits, want 0", len(rows))
	}
}

func TestSearchIndexEmpty(t *testing.T) {
	db := searchTestDB(t)
	ctx := context.Background()
	empty, err := db.SearchIndexEmpty(ctx)
	if err != nil {
		t.Fatalf("empty check: %v", err)
	}
	if !empty {
		t.Error("fresh index should report empty")
	}
	mustUpsert(t, db, SearchDoc{SpecID: "SPEC-1", Path: "/1.md", Title: "A", Status: "draft",
		Sections: []SearchSection{{Slug: "tl", Heading: "TL;DR", Body: "x"}}})
	empty, _ = db.SearchIndexEmpty(ctx)
	if empty {
		t.Error("populated index should report non-empty")
	}
}

func TestTruncateSearchIndexClearsAll(t *testing.T) {
	db := searchTestDB(t)
	ctx := context.Background()
	mustUpsert(t, db, SearchDoc{SpecID: "SPEC-1", Path: "/1.md", Title: "A", Status: "draft",
		Sections: []SearchSection{{Slug: "tl", Heading: "TL;DR", Body: "x"}}})
	if err := db.TruncateSearchIndex(ctx); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	empty, _ := db.SearchIndexEmpty(ctx)
	if !empty {
		t.Error("after truncate index should be empty")
	}
}

// mustUpsert indexes a doc with a dummy mtime/hash for test setup.
func mustUpsert(t *testing.T, db *DB, doc SearchDoc) {
	t.Helper()
	if err := db.UpsertSpecSections(context.Background(), doc, time.Now().Unix(), "h-"+doc.SpecID); err != nil {
		t.Fatalf("upsert %s: %v", doc.SpecID, err)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func containsStr(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func repeat(s string, n int) string {
	out := ""
	for range n {
		out += s
	}
	return out
}
