package git

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/aaronl1011/spec/internal/markdown"
	syncpkg "github.com/aaronl1011/spec/internal/sync"
)

// sectionOverlap decides whether a set of locally-changed files collides with
// upstream changes at a granularity that warrants aborting a rebase.
//
// For .md spec files it compares per-section content hashes (reusing
// sync.Hash + markdown.ExtractSections): a collision is reported only when the
// SAME section changed both locally (ours vs base) and upstream (remote vs
// base). Disjoint-section edits to one file are NOT a conflict. For non-.md
// files it falls back to whole-file overlap to stay conservative.
//
// It returns a human-readable description of the first collision (e.g.
// "SPEC-013 §technical_implementation"), or "" when the rebase is safe.
//
// This single helper is shared by WithSpecsRepo and PushLocalEdits so the two
// mutate paths cannot drift (SPEC-013 §Decision 003 / §7.1).
func sectionOverlap(ctx context.Context, dir string, ourFiles, upstreamFiles []string, baseRef, remoteRef string) (string, error) {
	upstream := make(map[string]struct{}, len(upstreamFiles))
	for _, f := range upstreamFiles {
		upstream[f] = struct{}{}
	}

	for _, file := range ourFiles {
		if _, changedUpstream := upstream[file]; !changedUpstream {
			continue
		}

		// Non-spec / non-markdown file changed on both sides: conservative
		// whole-file conflict.
		if !isSpecMarkdown(file) {
			return file, nil
		}

		section, err := collidingSection(ctx, dir, file, baseRef, remoteRef)
		if err != nil {
			// If we can't resolve sections, fall back to file-granular
			// conflict rather than silently auto-merging.
			return file, nil
		}
		if section != "" {
			return fmt.Sprintf("%s §%s", specIDFromPath(file), section), nil
		}
	}
	return "", nil
}

// collidingSection returns the slug of the first section that changed both
// locally (working tree / HEAD) and upstream (remote vs base), or "".
func collidingSection(ctx context.Context, dir, file, baseRef, remoteRef string) (string, error) {
	baseBody, baseErr := showFile(ctx, dir, baseRef, file)
	remoteBody, remoteErr := showFile(ctx, dir, remoteRef, file)
	localBody, localErr := showFile(ctx, dir, "HEAD", file)
	if localErr != nil {
		return "", localErr
	}
	// A file that did not exist at base (new on both sides) — treat any
	// shared section name as a collision via empty base.
	if baseErr != nil {
		baseBody = ""
	}
	if remoteErr != nil {
		// Upstream deleted the file but we changed it — conservative conflict.
		return "(file removed upstream)", nil
	}

	baseHashes := sectionHashes(baseBody)
	remoteHashes := sectionHashes(remoteBody)
	localHashes := sectionHashes(localBody)

	for slug, localHash := range localHashes {
		baseHash := baseHashes[slug]
		remoteHash := remoteHashes[slug]
		localChanged := localHash != baseHash
		remoteChanged := remoteHash != baseHash
		if localChanged && remoteChanged && localHash != remoteHash {
			return slug, nil
		}
	}
	return "", nil
}

// sectionHashes maps each section slug to the sync hash of its content.
func sectionHashes(content string) map[string]string {
	body := markdown.Body(content)
	sections := markdown.ExtractSections(body)
	hashes := make(map[string]string, len(sections))
	for _, s := range sections {
		// Skip the level-1 document title: its content spans the whole body,
		// so it would report a collision for any edit. Compare the owned
		// level-2+ sections, which is where real conflicts live.
		if s.Level < 2 {
			continue
		}
		hashes[s.Slug] = syncpkg.Hash(s.Content)
	}
	return hashes
}

// showFile returns the content of a file at a ref via `git show ref:path`.
func showFile(ctx context.Context, dir, ref, file string) (string, error) {
	return Run(ctx, dir, "show", ref+":"+file)
}

func isSpecMarkdown(file string) bool {
	if filepath.Ext(file) != ".md" {
		return false
	}
	// Only treat files under the specs sub-tree as section-aware.
	return strings.HasPrefix(filepath.ToSlash(file), SpecsSubDir+"/")
}

// specIDFromPath extracts a SPEC id-ish label from a file path for messages.
func specIDFromPath(file string) string {
	base := filepath.Base(file)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
