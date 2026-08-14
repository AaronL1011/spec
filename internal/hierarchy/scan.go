package hierarchy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aaronl1011/spec/internal/markdown"
)

// specIDPrefix is the filename prefix that marks a spec document.
const specIDPrefix = "SPEC-"

// triageSubDir is the intake sub-directory of the specs content directory.
const triageSubDir = "triage"

// Scan reads every spec under a specs content directory and returns the refs
// needed to build a Graph. It is the only impure function in this package.
//
// It searches the root, then triage/, then archive/ — the same three
// directories, in the same order, that the CLI's spec path resolver searches.
// That ordering is load-bearing rather than incidental: a hierarchy scan that
// skipped archive/ would report every closed initiative as a dangling parent,
// which is the exact bug class (two code paths disagreeing about where a spec
// lives once it is archived) that already bit the thread sidecar.
//
// Unreadable or unparseable files are skipped rather than failing the scan:
// one malformed document must not make hierarchy invisible for every other
// spec. A missing triage/ or archive/ directory is not an error.
func Scan(specsDir, archiveDir string) ([]SpecRef, error) {
	if specsDir == "" {
		return nil, fmt.Errorf("specs directory not configured — ensure spec.config.yaml has specs_repo settings")
	}

	refs, err := scanDir(specsDir, false)
	if err != nil {
		return nil, err
	}
	for _, sub := range []struct {
		dir      string
		archived bool
	}{
		{triageSubDir, false},
		{archiveDir, true},
	} {
		if sub.dir == "" {
			continue
		}
		more, err := scanDir(filepath.Join(specsDir, sub.dir), sub.archived)
		if err != nil {
			return nil, err
		}
		refs = append(refs, more...)
	}
	return refs, nil
}

// scanDir reads one directory of spec files. A directory that does not exist
// yields no refs and no error — triage/ and archive/ are created lazily.
func scanDir(dir string, archived bool) ([]SpecRef, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading specs from %s: %w", dir, err)
	}

	var refs []SpecRef
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" || !strings.HasPrefix(e.Name(), specIDPrefix) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		meta, err := markdown.ReadMeta(path)
		if err != nil || !strings.HasPrefix(meta.ID, specIDPrefix) {
			continue
		}
		refs = append(refs, SpecRef{
			ID:       meta.ID,
			Title:    meta.Title,
			Status:   meta.Status,
			Parent:   meta.Parent,
			Path:     path,
			Archived: archived,
		})
	}
	return refs, nil
}

// Find resolves a single spec by ID without scanning the whole repo, searching
// the root, then triage/, then archive/ — the same order, with the same
// first-match-wins precedence, as Scan and the CLI's spec path resolver.
//
// It exists for the surfaces that need one spec's title rather than the whole
// graph (a detail pane naming a parent, for instance), where a full scan would
// re-read every spec in the repo on every refresh.
func Find(specsDir, archiveDir, specID string) (SpecRef, bool) {
	if specsDir == "" || specID == "" {
		return SpecRef{}, false
	}
	candidates := []struct {
		path     string
		archived bool
	}{
		{filepath.Join(specsDir, specID+".md"), false},
		{filepath.Join(specsDir, triageSubDir, specID+".md"), false},
	}
	if archiveDir != "" {
		candidates = append(candidates, struct {
			path     string
			archived bool
		}{filepath.Join(specsDir, archiveDir, specID+".md"), true})
	}

	for _, c := range candidates {
		meta, err := markdown.ReadMeta(c.path)
		if err != nil {
			continue
		}
		return SpecRef{
			ID:       meta.ID,
			Title:    meta.Title,
			Status:   meta.Status,
			Parent:   meta.Parent,
			Path:     c.path,
			Archived: c.archived,
		}, true
	}
	return SpecRef{}, false
}

// Load scans a specs directory and returns the built graph.
func Load(specsDir, archiveDir string) (*Graph, error) {
	refs, err := Scan(specsDir, archiveDir)
	if err != nil {
		return nil, err
	}
	return Build(refs), nil
}
