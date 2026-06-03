package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aaronl1011/spec/internal/markdown"
	syncpkg "github.com/aaronl1011/spec/internal/sync"
	"github.com/aaronl1011/spec/internal/thread"
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
// "SPEC-013 §technical_implementation"), or "" when the rebase is safe. It is
// conservative by construction: any error resolving sections degrades to a
// file-granular conflict rather than surfacing, so it has no error return.
//
// This single helper is shared by WithSpecsRepo and PushLocalEdits so the two
// mutate paths cannot drift (SPEC-013 §Decision 003 / §7.1).
func sectionOverlap(ctx context.Context, dir string, ourFiles, upstreamFiles []string, baseRef, remoteRef string) string {
	upstream := make(map[string]struct{}, len(upstreamFiles))
	for _, f := range upstreamFiles {
		upstream[f] = struct{}{}
	}

	for _, file := range ourFiles {
		if _, changedUpstream := upstream[file]; !changedUpstream {
			continue
		}

		// Thread sidecars edited on both sides are reconciled associatively
		// (two reviewers editing offline) rather than treated as a conflict.
		if isThreadSidecar(file) {
			if mergeSidecar(ctx, dir, file, remoteRef) {
				continue
			}
			return file
		}

		// Non-spec / non-markdown file changed on both sides: conservative
		// whole-file conflict.
		if !isSpecMarkdown(file) {
			return file
		}

		section, err := collidingSection(ctx, dir, file, baseRef, remoteRef)
		if err != nil {
			// If we can't resolve sections, fall back to file-granular
			// conflict rather than silently auto-merging.
			return file
		}
		if section != "" {
			return fmt.Sprintf("%s §%s", specIDFromPath(file), section)
		}
	}
	return ""
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
		// The non-nil remoteErr is the signal, not an error to propagate.
		return "(file removed upstream)", nil //nolint:nilerr // deletion is the conflict, not a failure
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

// isThreadSidecar reports whether file is a thread sidecar (.threads.yaml)
// under the specs sub-tree.
func isThreadSidecar(file string) bool {
	slashed := filepath.ToSlash(file)
	return strings.HasPrefix(slashed, SpecsSubDir+"/") && strings.HasSuffix(slashed, ".threads.yaml")
}

// mergeSidecar reconciles a thread sidecar that changed on both sides by
// unioning the local (HEAD) and remote thread sets via thread.Merge and
// writing the result to the working tree. It returns true when the merge
// succeeded and the working tree now holds the reconciled sidecar; false when
// the caller should fall back to a conservative whole-file conflict.
func mergeSidecar(ctx context.Context, dir, file, remoteRef string) bool {
	localBody, err := showFile(ctx, dir, "HEAD", file)
	if err != nil {
		return false
	}
	remoteBody, err := showFile(ctx, dir, remoteRef, file)
	if err != nil {
		return false
	}
	local, err := thread.Parse([]byte(localBody))
	if err != nil {
		return false
	}
	remote, err := thread.Parse([]byte(remoteBody))
	if err != nil {
		return false
	}
	merged, err := thread.Marshal(thread.Merge(local, remote))
	if err != nil {
		return false
	}
	if err := os.WriteFile(filepath.Join(dir, file), merged, 0o644); err != nil {
		return false
	}
	return true
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
