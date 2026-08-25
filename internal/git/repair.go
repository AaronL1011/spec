package git

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aaronl1011/spec/internal/config"
)

// A truncated transfer (e.g. Wi-Fi drop mid-fetch) can leave the git object
// store in a "present but unreadable" state: loose object files and pack
// files truncated to zero bytes, plus stray partial-pack temp files from the
// interrupted index-pack. Git then treats those objects as present, so it
// never re-downloads them and every retry fails identically.
//
// Detection is deliberately NOT based on matching git's error text. Git's
// wording is not a stable API and is localized (under a non-English LANG the
// message isn't even in English), so string matching would silently miss the
// corruption. Instead we rely on filesystem evidence: a zero-byte object file
// is structurally invalid in every git version and every locale. The repair is
// therefore its own detector — on a fetch failure we scan the object store and
// only treat it as corruption if we actually removed truncated/stray
// artifacts. Committed history is never touched; the retry re-downloads the
// missing objects.

// fetchWithRepair runs a fetch and, if it fails, checks the object store for
// truncation artifacts (zero-byte objects, stray partial-pack temp files). Only
// when it actually removes such artifacts — hard evidence of a corrupt store,
// independent of git's error wording or locale — does it retry the fetch. A
// failure with a structurally-intact store is a genuine network/auth error and
// is surfaced unchanged. It returns whether a repair was performed (for audit
// attribution) and the final fetch error, if any.
func fetchWithRepair(ctx context.Context, dir string) (repaired bool, err error) {
	ferr := Fetch(ctx, dir)
	if ferr == nil {
		return false, nil
	}

	// Decide by filesystem evidence, not by the error string. If nothing was
	// structurally invalid, this is not object-store corruption — surface the
	// original fetch error (network/auth/etc.) untouched.
	removed, rerr := repairObjectStore(dir)
	if rerr != nil || removed == 0 {
		return false, ferr //nolint:nilerr // scan failure or clean store: original fetch error stands
	}

	if ferr := Fetch(ctx, dir); ferr != nil {
		return true, fmt.Errorf(
			"specs repo remained corrupt after auto-repair (cleaned %d truncated artifact(s)): %w — run 'spec nuke' to hard-reset the local clone",
			removed, redactToken(ferr))
	}

	// The same truncation can zero out tracked working-tree files; restore any
	// that were left empty now that the blobs are back in the object store.
	restoreTruncatedFiles(ctx, dir)
	return true, nil
}

// repairObjectStore removes structurally-invalid artifacts from a clone's
// object store: zero-byte loose/pack files and stray partial-pack temp files
// left by an interrupted transfer. It returns the number of artifacts removed
// (the positive signal that the store was genuinely corrupt) and an error only
// if it could not scan the object store at all. Removing nothing is not an
// error — it simply means the store was intact.
func repairObjectStore(dir string) (int, error) {
	objectsDir := filepath.Join(dir, ".git", "objects")
	if _, err := os.Stat(objectsDir); err != nil {
		return 0, fmt.Errorf("object store %s not accessible: %w", objectsDir, err)
	}

	removed, err := removeZeroByteObjects(objectsDir)
	if err != nil {
		return removed, err
	}
	return removed + removeStrayPackArtifacts(objectsDir), nil
}

// removeZeroByteObjects deletes every zero-byte regular file anywhere under the
// object store. A valid loose object or pack file is always non-empty, so a
// zero-byte file is unambiguously a truncation artifact — the exact state a
// mid-fetch network drop leaves behind.
func removeZeroByteObjects(objectsDir string) (int, error) {
	var removed int
	err := filepath.WalkDir(objectsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip unreadable entries; don't abort the whole walk
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil || info.Size() != 0 {
			return nil //nolint:nilerr // unreadable/non-empty: not a truncation artifact
		}
		if os.Remove(path) == nil {
			removed++
		}
		return nil
	})
	return removed, err
}

// removeStrayPackArtifacts deletes leftovers from an interrupted index-pack:
// tmp_pack_*/tmp_idx_* temp files, incoming-* quarantine directories, and any
// .pack whose paired .idx is missing (an un-indexed, therefore unusable, pack).
// It returns the number of artifacts removed.
func removeStrayPackArtifacts(objectsDir string) int {
	var removed int
	remove := func(path string) {
		if os.Remove(path) == nil {
			removed++
		}
	}

	// incoming-* quarantine directories from an aborted transfer.
	if entries, err := os.ReadDir(objectsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), "incoming-") {
				if os.RemoveAll(filepath.Join(objectsDir, e.Name())) == nil {
					removed++
				}
			}
		}
	}

	packDir := filepath.Join(objectsDir, "pack")
	entries, err := os.ReadDir(packDir)
	if err != nil {
		return removed
	}
	idxPresent := make(map[string]bool)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".idx") {
			idxPresent[strings.TrimSuffix(e.Name(), ".idx")] = true
		}
	}
	for _, e := range entries {
		name := e.Name()
		switch {
		case strings.HasPrefix(name, "tmp_pack_"), strings.HasPrefix(name, "tmp_idx_"):
			remove(filepath.Join(packDir, name))
		case strings.HasSuffix(name, ".pack") && !idxPresent[strings.TrimSuffix(name, ".pack")]:
			// An un-indexed pack is unusable; drop it (and its .rev sibling).
			base := strings.TrimSuffix(name, ".pack")
			remove(filepath.Join(packDir, name))
			remove(filepath.Join(packDir, base+".rev"))
		}
	}
	return removed
}

// restoreTruncatedFiles restores tracked working-tree files that were left
// empty by a truncation but are non-empty at HEAD. Best-effort: it never fails
// the caller. This heals the "3 truncated working-tree files" case alongside
// the object-store repair.
func restoreTruncatedFiles(ctx context.Context, dir string) {
	out, err := Run(ctx, dir, "ls-files", "-z")
	if err != nil {
		return
	}
	var truncated []string
	for _, rel := range strings.Split(out, "\x00") {
		if rel == "" {
			continue
		}
		info, statErr := os.Stat(filepath.Join(dir, rel))
		if statErr != nil || info.Size() != 0 {
			continue
		}
		// Only restore if the blob at HEAD is genuinely non-empty (i.e. the
		// on-disk emptiness is truncation, not a legitimately-empty file).
		if size, err := Run(ctx, dir, "cat-file", "-s", "HEAD:"+rel); err == nil && size != "0" {
			truncated = append(truncated, rel)
		}
	}
	for _, rel := range truncated {
		_, _ = Run(ctx, dir, "checkout", "HEAD", "--", rel)
	}
}

// RepoKey returns the "owner/repo" identifier used to scope store-side state
// (freshness timestamps, queued pushes) to one clone. Exported so callers that
// manage that state directly (e.g. `spec nuke`) key it identically.
func RepoKey(cfg *config.SpecsRepoConfig) string {
	return repoKey(cfg)
}

// LocalState summarizes local work that a destructive reset would discard,
// letting `spec nuke` warn before it removes the clone. Best-effort: fields
// default to false when the (possibly corrupt) repo can't be inspected.
type LocalState struct {
	Exists             bool
	UncommittedChanges bool
	UnpushedCommits    bool
}

// InspectLocalState reports whether the clone has local work at risk from a
// nuke. It never returns an error — a repo too corrupt to inspect simply
// reports no detectable local work, which is the safe default for a hard reset.
func InspectLocalState(ctx context.Context, cfg *config.SpecsRepoConfig) LocalState {
	dir := SpecsRepoDir(cfg)
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return LocalState{}
	}
	state := LocalState{Exists: true}
	if dirty, err := HasChanges(ctx, dir); err == nil {
		state.UncommittedChanges = dirty
	}
	if unpushed, err := HasUnpushedCommits(ctx, dir, remoteBranchRef(cfg)); err == nil {
		state.UnpushedCommits = unpushed
	}
	return state
}

// NukeSpecsRepo removes the local specs-repo clone entirely and re-clones it
// from origin — the last-resort hard reset for an object store too corrupt to
// auto-repair. It discards ALL local state in the clone (uncommitted edits and
// unpushed commits included), so callers must confirm intent first. Queued-push
// bookkeeping in the store is the caller's responsibility to clear.
func NukeSpecsRepo(ctx context.Context, cfg *config.SpecsRepoConfig) error {
	if err := validateToken(cfg); err != nil {
		return err
	}
	dir := SpecsRepoDir(cfg)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("removing corrupt clone %s: %w", dir, err)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return fmt.Errorf("recreating repos directory: %w", err)
	}
	if err := Clone(ctx, SpecsRepoURL(cfg), dir); err != nil {
		return fmt.Errorf("re-cloning specs repo %s/%s: %w", cfg.Owner, cfg.Repo, redactToken(err))
	}
	readRecorder.SetLastFetch(repoKey(cfg), time.Now().Unix())
	return nil
}
