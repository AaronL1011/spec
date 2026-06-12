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
		// (two reviewers replying concurrently) rather than treated as a
		// conflict. The actual union is applied during the rebase by
		// rebaseWithSidecarUnion — here we only verify both sides parse, so
		// the working tree stays clean for the rebase to start.
		if isThreadSidecar(file) {
			if sidecarMergeable(ctx, dir, file, remoteRef) {
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

// sidecarMergeable reports whether a thread sidecar that changed on both sides
// can be reconciled associatively — i.e. both the local (HEAD) and remote
// versions parse as thread sets. It is a pure check with no working-tree
// writes: the union itself is applied during the rebase by
// rebaseWithSidecarUnion, because writing it earlier would dirty the tree and
// make `git rebase` refuse to start.
func sidecarMergeable(ctx context.Context, dir, file, remoteRef string) bool {
	localBody, err := showFile(ctx, dir, "HEAD", file)
	if err != nil {
		return false
	}
	remoteBody, err := showFile(ctx, dir, remoteRef, file)
	if err != nil {
		return false
	}
	if _, err := thread.Parse([]byte(localBody)); err != nil {
		return false
	}
	if _, err := thread.Parse([]byte(remoteBody)); err != nil {
		return false
	}
	return true
}

// rebaseWithSidecarUnion rebases the current branch onto remoteRef, resolving
// any conflict that is confined to thread sidecars by replacing each
// conflicted sidecar with the associative union of both sides (thread.Merge —
// the same reconciliation sectionOverlap promised was safe). Any conflict
// touching a non-sidecar file returns the rebase error so the caller aborts
// conservatively, exactly as before.
func rebaseWithSidecarUnion(ctx context.Context, dir, remoteRef string) error {
	err := Rebase(ctx, dir, remoteRef)
	for err != nil {
		unmerged, uerr := unmergedFiles(ctx, dir)
		if uerr != nil || len(unmerged) == 0 {
			return err
		}
		for _, f := range unmerged {
			if !isThreadSidecar(f) || !writeSidecarUnion(ctx, dir, f) {
				return err
			}
			if _, aerr := Run(ctx, dir, "add", f); aerr != nil {
				return err
			}
		}
		err = continueRebase(ctx, dir)
	}
	return nil
}

// unmergedFiles lists paths currently in a conflicted (unmerged) state.
func unmergedFiles(ctx context.Context, dir string) ([]string, error) {
	out, err := Run(ctx, dir, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// writeSidecarUnion resolves one conflicted sidecar by unioning the two
// conflict stages (:2 ours/onto, :3 theirs/replayed — thread.Merge is
// symmetric so the distinction does not matter) and writing the result to the
// working tree. Returns false when either stage is missing or unparsable
// (e.g. a delete/modify conflict), in which case the caller aborts.
func writeSidecarUnion(ctx context.Context, dir, file string) bool {
	oursBody, err := Run(ctx, dir, "show", ":2:"+file)
	if err != nil {
		return false
	}
	theirsBody, err := Run(ctx, dir, "show", ":3:"+file)
	if err != nil {
		return false
	}
	ours, err := thread.Parse([]byte(oursBody))
	if err != nil {
		return false
	}
	theirs, err := thread.Parse([]byte(theirsBody))
	if err != nil {
		return false
	}
	merged, err := thread.Marshal(thread.Merge(ours, theirs))
	if err != nil {
		return false
	}
	return os.WriteFile(filepath.Join(dir, file), merged, 0o644) == nil
}

// continueRebase advances a rebase whose conflicts have been staged. core.editor
// is forced to true so no interactive editor can hijack a background push. A
// resolution identical to the onto side leaves nothing to commit; that patch is
// skipped — the content already landed upstream.
func continueRebase(ctx context.Context, dir string) error {
	_, err := Run(ctx, dir, "-c", "core.editor=true", "rebase", "--continue")
	if err == nil {
		return nil
	}
	if staged, serr := Run(ctx, dir, "diff", "--cached", "--name-only"); serr == nil && staged == "" {
		_, skipErr := Run(ctx, dir, "rebase", "--skip")
		return skipErr
	}
	return err
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
