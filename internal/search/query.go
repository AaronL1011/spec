package search

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/aaronl1011/spec/internal/config"
	"github.com/aaronl1011/spec/internal/markdown"
)

// buildMatch converts free-form user input into a safe FTS5 MATCH expression.
// Each whitespace-delimited token is double-quoted (with any inner quotes
// stripped) and joined with spaces, so FTS5 applies implicit AND across terms.
// FTS5 syntax characters (parens, NEAR, *, etc.) inside a token are neutered
// by the quoting and never reach the parser. Returns "" for blank input.
func buildMatch(query string) string {
	tokens := tokens(query)
	if len(tokens) == 0 {
		return ""
	}
	parts := make([]string, 0, len(tokens))
	for _, t := range tokens {
		parts = append(parts, `"`+escapeToken(t)+`"`)
	}
	return strings.Join(parts, " ")
}

// buildPrefixMatch is the zero-hit retry: each token is quoted then followed
// by a bare `*` outside the quotes, so "confl"* matches "conflict". The star
// must sit outside the double quotes (verified against modernc sqlite).
func buildPrefixMatch(query string) string {
	tokens := tokens(query)
	if len(tokens) == 0 {
		return ""
	}
	parts := make([]string, 0, len(tokens))
	for _, t := range tokens {
		parts = append(parts, `"`+escapeToken(t)+`"*`)
	}
	return strings.Join(parts, " ")
}

// tokens splits the query on whitespace, dropping empties.
func tokens(query string) []string {
	var out []string
	for _, w := range strings.Fields(query) {
		if w != "" {
			out = append(out, w)
		}
	}
	return out
}

// escapeToken strips FTS5 special characters from a token so a quoted term is
// always a literal. Double quotes are removed outright (they would close the
// surrounding quotes); other metacharacters are kept but inert inside quotes.
func escapeToken(t string) string {
	t = strings.ReplaceAll(t, `"`, "")
	return t
}

// fallbackScan is the live substring scan used when the index is cold or the db
// is unavailable. It relocates the old cmd/search.go searchDir logic, scopes
// to active/archived/all, caps at limit, and returns unranked hits with empty
// section slugs (deep-links then fall back to the overview).
func fallbackScan(_ context.Context, rc *config.ResolvedConfig, query string, scope SearchScope, limit int) []Hit {
	if limit <= 0 {
		limit = DefaultLimit
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" || rc == nil || rc.SpecsRepoDir == "" {
		return nil
	}

	var hits []Hit
	if scope != ScopeArchived {
		hits = scanDir(rc.SpecsRepoDir, q, false, limit, hits)
	}
	if scope != ScopeActive {
		archDir := filepath.Join(rc.SpecsRepoDir, config.ArchiveDir(rc.Team))
		hits = scanDir(archDir, q, true, limit, hits)
	}
	return hits
}

// scanDir scans one directory for .md files containing q (case-insensitive),
// appends hits, and stops once limit total hits exist.
func scanDir(dir, q string, archived bool, limit int, hits []Hit) []Hit {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return hits // missing dir (empty archive) is not an error
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		if len(hits) >= limit {
			return hits
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := strings.ToLower(string(data))
		if !strings.Contains(content, q) {
			continue
		}
		meta, err := markdown.ReadMeta(path)
		if err != nil {
			continue
		}
		hits = append(hits, Hit{
			SpecID:   meta.ID,
			Title:    meta.Title,
			Status:   meta.Status,
			Author:   meta.Author,
			Archived: archived,
			Snippet:  fallbackExcerpt(string(data), q),
		})
	}
	return hits
}

// fallbackExcerpt returns a trimmed first matching line (no FTS5 highlighting).
func fallbackExcerpt(content, q string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(strings.ToLower(line), strings.ToLower(q)) {
			excerpt := strings.TrimSpace(line)
			if len(excerpt) > 80 {
				excerpt = excerpt[:80]
			}
			return excerpt
		}
	}
	return ""
}
