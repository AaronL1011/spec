package hierarchy

import (
	"errors"
	"fmt"
	"io/fs"
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
// A spec-named file that will not parse is carried as a corrupt ref rather
// than skipped: skipping would make an unreadable parent indistinguishable
// from an absent one, and absence is the one thing a file on disk disproves.
// One malformed document degrades its own subtree to warnings; it must never
// fail the scan or block anyone else. A missing triage/ or archive/ directory
// is not an error.
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
			// The file exists but its contents cannot be trusted. The ID comes
			// from the filename — the one part a bad edit cannot mangle — and
			// the parent edge is salvaged best-effort so an initiative's
			// rollup fails closed rather than quietly shrinking. Title and
			// Status stay empty: nothing downstream may trust them.
			refs = append(refs, SpecRef{
				ID:       strings.TrimSuffix(e.Name(), ".md"),
				Parent:   salvageParent(path),
				Path:     path,
				Archived: archived,
				Corrupt:  true,
			})
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
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			// Present but unreadable. Returning a corrupt ref keeps Find's
			// precedence identical to Scan's: the same document wins whether a
			// caller asked for one ref or the whole graph.
			return SpecRef{
				ID:       specID,
				Parent:   salvageParent(c.path),
				Path:     c.path,
				Archived: c.archived,
				Corrupt:  true,
			}, true
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

// salvageParent best-effort extracts the parent field from a spec file whose
// frontmatter failed to parse. A corrupt slice must still count against its
// initiative's rollup — children_complete failing closed over an unreadable
// file beats passing over one — and the parent edge is the only field worth
// that recovery: a salvaged edge is used solely to warn and to count a slice
// as open, whereas a salvaged status would be trusted by gates.
//
// Only lines inside the frontmatter block (between the opening and closing
// ---) are considered, so prose mentioning "parent:" can never mint an edge.
func salvageParent(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			return ""
		}
		val, ok := strings.CutPrefix(strings.TrimSpace(line), "parent:")
		if !ok {
			continue
		}
		parent := strings.Trim(strings.TrimSpace(val), `"'`)
		if strings.HasPrefix(parent, specIDPrefix) {
			return parent
		}
		return ""
	}
	return ""
}

// Load scans a specs directory and returns the built graph.
func Load(specsDir, archiveDir string) (*Graph, error) {
	refs, err := Scan(specsDir, archiveDir)
	if err != nil {
		return nil, err
	}
	return Build(refs), nil
}
