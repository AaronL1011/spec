package git

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/aaronl1011/spec/internal/config"
)

const (
	maxPushRetries = 3

	// SpecsSubDir is the sub-directory within the specs repo where spec
	// files are stored. All spec, triage, and archive content lives under
	// this path.
	SpecsSubDir = "specs"

	// lockRetryInterval is how often a blocked lock acquisition re-polls.
	lockRetryInterval = 50 * time.Millisecond

	// fetchTTL bounds how long a cached fetch is considered fresh. Reads
	// within this window skip the network fetch entirely. Kept short so
	// rapid command sequences are fast without going stale (SPEC-013 axis 3).
	fetchTTL = 8 * time.Second

	// pushBackoffBase is the base unit of randomized backoff inserted
	// between rebase-retry attempts to reduce thrash under contention.
	pushBackoffBase = 300 * time.Millisecond
)

// backoff sleeps for a small randomized interval that grows with the attempt
// number, reducing synchronized-retry thrash when several clients push at once.
func backoff(attempt int) {
	jitter := time.Duration(rand.Int63n(int64(pushBackoffBase)))
	time.Sleep(time.Duration(attempt+1)*pushBackoffBase + jitter)
}

// repoKey returns the "owner/repo" identifier used to scope store-side state
// (freshness timestamps, queued pushes) to one clone.
func repoKey(cfg *config.SpecsRepoConfig) string {
	return cfg.Owner + "/" + cfg.Repo
}

// SpecsRepoDir returns the local path for the specs repo clone.
func SpecsRepoDir(cfg *config.SpecsRepoConfig) string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".spec", "repos", cfg.Owner, cfg.Repo)
}

// SpecsRepoURL returns the clone URL for the specs repo.
// If a token is configured, it is embedded in the URL for passwordless auth.
func SpecsRepoURL(cfg *config.SpecsRepoConfig) string {
	var host string
	switch cfg.Provider {
	case "gitlab":
		host = "gitlab.com"
	case "bitbucket":
		host = "bitbucket.org"
	default:
		host = "github.com"
	}

	if cfg.Token != "" {
		return fmt.Sprintf("https://x-access-token:%s@%s/%s/%s.git", cfg.Token, host, cfg.Owner, cfg.Repo)
	}
	return fmt.Sprintf("https://%s/%s/%s.git", host, cfg.Owner, cfg.Repo)
}

// readRecorder is an optional package-level recorder for read-path freshness
// bookkeeping. Read callers (list/search/pull) don't thread SyncOptions, so a
// process-wide recorder injected once via SetRecorder backs the TTL skip and
// last-fetch timestamp. It defaults to a no-op so git stays usable without a
// store.
var readRecorder Recorder = noopRecorder{}

// readSurface attributes read-path fetches in the audit log. Defaults to the
// CLI; the MCP server / TUI override it once at startup so freshness fetches
// are attributed to the right surface.
var readSurface = "cli"

// SetRecorder injects the process-wide recorder used by the read path for
// freshness bookkeeping. Callers that own a store inject it once at startup.
// Passing nil resets to a no-op. git never imports the store directly — the
// recorder is the only bridge (AGENTS.md adapter-isolation rules).
func SetRecorder(r Recorder) {
	if r == nil {
		readRecorder = noopRecorder{}
		return
	}
	readRecorder = r
}

// SetReadSurface sets the surface label attributed to read-path fetches.
func SetReadSurface(surface string) {
	if surface != "" {
		readSurface = surface
	}
}

// EnsureSpecsRepo clones the specs repo if not present, otherwise fetches the
// latest under a SHARED (read) lock. It is the non-destructive read path: it
// never runs `reset --hard` and never blocks on or discards local edits, so a
// working tree with in-flight changes survives a read command untouched
// (SPEC-013 axes 1–3). The destructive reset is reserved for the mutate path
// (WithSpecsRepo). A fetch is skipped entirely when one completed within the
// freshness TTL.
func EnsureSpecsRepo(ctx context.Context, cfg *config.SpecsRepoConfig) (string, error) {
	if err := validateToken(cfg); err != nil {
		return "", err
	}

	dir := SpecsRepoDir(cfg)

	if _, err := os.Stat(filepath.Join(dir, ".git")); os.IsNotExist(err) {
		// Clone
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return "", fmt.Errorf("creating repos directory: %w", err)
		}
		url := SpecsRepoURL(cfg)
		if err := Clone(ctx, url, dir); err != nil {
			return "", fmt.Errorf("cloning specs repo %s/%s: %w", cfg.Owner, cfg.Repo, redactToken(err))
		}
		readRecorder.SetLastFetch(repoKey(cfg), time.Now().Unix())
		return dir, nil
	}

	// Shared lock: multiple reads proceed together but a mutate's exclusive
	// critical section excludes them (AC-2 / AC-3).
	release, err := acquireShared(ctx, dir)
	if err != nil {
		return dir, err
	}
	defer release()

	// Ensure the remote URL has the current token
	if err := ensureRemoteURL(ctx, dir, cfg); err != nil {
		return dir, fmt.Errorf("updating remote URL: %w", err)
	}

	// Freshness TTL: skip the network fetch if we fetched recently (AC-5/AC-6).
	if !fetchFresh(cfg) {
		if err := Fetch(ctx, dir); err != nil {
			return dir, fmt.Errorf("fetching specs repo: %w", redactToken(err))
		}
		readRecorder.SetLastFetch(repoKey(cfg), time.Now().Unix())
		readRecorder.Record(AuditEvent{Op: OpFetch, Surface: readSurface, Trigger: "read", Outcome: OutcomeOK})
	}

	// Advance the working tree to the freshly fetched ref so reads reflect
	// teammates' pushes. `git fetch` alone only moves the remote-tracking ref;
	// the working-tree files the readers see stay frozen at the old HEAD until
	// the branch is fast-forwarded. This is the safe counterpart to the
	// destructive mutate path: it only advances when there is nothing local to
	// lose (clean tree, no unpushed commits), preserving SPEC-013's guarantee
	// that in-flight local edits are never clobbered by a read.
	if err := fastForwardClean(ctx, dir, cfg); err != nil {
		return dir, err
	}

	// Ensure specs sub-directory exists.
	specsDir := filepath.Join(dir, SpecsSubDir)
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		return dir, fmt.Errorf("creating specs directory: %w", err)
	}

	return dir, nil
}

// fastForwardClean advances the working tree to origin/<branch> only when it is
// safe to do so — i.e. the tree has no uncommitted changes and no local commits
// that haven't been pushed. When either is true the local clone holds work that
// a reset would destroy, so it is left untouched (the mutate path reconciles
// it) and the reader simply sees the last consistent local state. A clean,
// purely-behind clone is fast-forwarded so cross-machine reads converge.
func fastForwardClean(ctx context.Context, dir string, cfg *config.SpecsRepoConfig) error {
	remoteRef := remoteBranchRef(cfg)

	// Behind count of 0 (or unknown remote) means nothing to advance to.
	behind, err := CommitsBehind(ctx, dir, remoteRef)
	if err != nil || behind == 0 {
		return nil //nolint:nilerr // unknown/equal ref: leave the tree as-is.
	}

	// Never discard uncommitted local edits.
	if dirty, err := HasChanges(ctx, dir); err != nil || dirty {
		return nil //nolint:nilerr // dirty tree: preserve local work, skip FF.
	}

	// Never discard local commits that haven't reached the remote.
	if unpushed, err := HasUnpushedCommits(ctx, dir, remoteRef); err != nil || unpushed {
		return nil //nolint:nilerr // diverged: leave for the mutate path to reconcile.
	}

	// Safe: clean tree, strictly behind. Fast-forward to the fetched ref.
	if err := ResetHard(ctx, dir, remoteRef); err != nil {
		return fmt.Errorf("fast-forwarding specs repo to %s: %w", remoteRef, redactToken(err))
	}
	return nil
}

// Freshness summarizes how current the local view of the specs repo is.
type Freshness struct {
	LastFetch     time.Time // zero if never recorded
	CommitsBehind int       // new upstream commits since HEAD
	QueuedPushes  int       // committed-but-unpushed operations awaiting flush
}

// SyncFreshness reports the freshness/health of the specs-repo clone for the
// `spec status` line (AC-9). It does not fetch — it reads the cached last-fetch
// timestamp and counts new upstream commits against the already-fetched ref.
func SyncFreshness(ctx context.Context, cfg *config.SpecsRepoConfig, rec Recorder) Freshness {
	if rec == nil {
		rec = readRecorder
	}
	dir := SpecsRepoDir(cfg)
	var f Freshness
	if secs, ok := rec.LastFetch(repoKey(cfg)); ok {
		f.LastFetch = time.Unix(secs, 0)
	}
	if n, err := CommitsBehind(ctx, dir, remoteBranchRef(cfg)); err == nil {
		f.CommitsBehind = n
	}
	f.QueuedPushes = len(rec.Pending(repoKey(cfg)))
	return f
}

// fetchFresh reports whether the last fetch is within the TTL window.
func fetchFresh(cfg *config.SpecsRepoConfig) bool {
	last, ok := readRecorder.LastFetch(repoKey(cfg))
	if !ok {
		return false
	}
	return time.Since(time.Unix(last, 0)) < fetchTTL
}

// WithSpecsRepo is the legacy entry point: a committing operation attributed
// to the CLI with no audit recorder. Prefer WithSpecsRepoOpts for surface and
// trigger attribution.
func WithSpecsRepo(ctx context.Context, cfg *config.SpecsRepoConfig, mutate func(repoPath string) (commitMsg string, err error)) error {
	return WithSpecsRepoOpts(ctx, cfg, SyncOptions{}, mutate)
}

// errSameSection is the sentinel for a genuine same-section collision, which
// must abort rather than be reclassified as a queued push.
type sectionConflictError struct{ desc string }

func (e *sectionConflictError) Error() string {
	return fmt.Sprintf("%s was modified by another user while you were editing — pull the latest with 'spec pull' and retry", e.desc)
}

// WithSpecsRepoOpts runs a committing operation through the full lifecycle:
// exclusive-lock the local critical section (fetch → recover → mutate →
// commit), release it, then push under a separate narrower push-lock with the
// section-aware conflict check, randomized backoff, and queue-on-exhaustion.
//
// The exclusive lock covers only the local work, so a shared-lock reader
// (notably the same-host MCP agent) waits milliseconds, never the network push
// (SPEC-013 §Decision 008, AC-21). The commit is durable before the lock is
// released, so a reader that proceeds during the push sees committed state.
//
// Cross-teammate safety is git's atomic push + the section-aware rebase-retry,
// NOT the lock (which is single-host only).
func WithSpecsRepoOpts(ctx context.Context, cfg *config.SpecsRepoConfig, opts SyncOptions, mutate func(repoPath string) (commitMsg string, err error)) error {
	if err := validateToken(cfg); err != nil {
		return err
	}
	opts = opts.normalized(ctx)
	dir := SpecsRepoDir(cfg)
	remoteRef := remoteBranchRef(cfg)

	// Drain any queued (offline / contention) pushes first so the backlog
	// never starves behind new work.
	FlushQueue(ctx, cfg, opts)

	// --- Exclusive critical section: local work only ---
	commitSHA, baseRef, ourFiles, err := func() (string, string, []string, error) {
		release, err := acquireExclusive(ctx, dir)
		if err != nil {
			return "", "", nil, err
		}
		defer release()

		if err := ensureRemoteURL(ctx, dir, cfg); err != nil {
			return "", "", nil, fmt.Errorf("updating remote URL: %w", err)
		}

		// Auto-recover any stranded state before doing our own work.
		if err := autoRecover(ctx, cfg, dir, opts); err != nil {
			return "", "", nil, err
		}

		if err := Fetch(ctx, dir); err != nil {
			return "", "", nil, fmt.Errorf("fetching specs repo: %w", redactToken(err))
		}
		readRecorder.SetLastFetch(repoKey(cfg), time.Now().Unix())
		if err := ResetHard(ctx, dir, remoteRef); err != nil {
			return "", "", nil, fmt.Errorf("resetting specs repo: %w", redactToken(err))
		}

		base, err := RevParse(ctx, dir, "HEAD")
		if err != nil {
			return "", "", nil, fmt.Errorf("reading HEAD: %w", err)
		}

		commitMsg, err := mutate(dir)
		if err != nil {
			return "", "", nil, fmt.Errorf("mutation failed: %w", err)
		}

		hasChanges, err := HasChanges(ctx, dir)
		if err != nil {
			return "", "", nil, fmt.Errorf("checking changes: %w", err)
		}
		if !hasChanges {
			return "", base, nil, nil
		}

		if err := Commit(ctx, dir, commitMsg); err != nil {
			return "", "", nil, fmt.Errorf("committing: %w", err)
		}
		opts.record(OpCommit, OutcomeOK, commitMsg)

		sha, _ := RevParse(ctx, dir, "HEAD")
		files, err := CommittedFiles(ctx, dir, "HEAD")
		if err != nil {
			return "", "", nil, fmt.Errorf("listing committed files: %w", err)
		}
		return sha, base, files, nil
	}()
	if err != nil {
		return err
	}
	if commitSHA == "" {
		// Nothing to commit.
		return nil
	}

	// --- Push phase: outside the exclusive lock, under the push-lock ---
	return pushWithRecovery(ctx, cfg, dir, opts, baseRef, ourFiles, true)
}

// pushWithRecovery pushes committed work with section-aware rebase-retry. On a
// genuine same-section collision it aborts (resetHardOnConflict controls
// whether the working tree is reset to remote). On retry exhaustion from pure
// contention (online) or any transient/offline failure, it reclassifies the
// operation as queued and returns nil — the work is already durable.
func pushWithRecovery(ctx context.Context, cfg *config.SpecsRepoConfig, dir string, opts SyncOptions, baseRef string, ourFiles []string, resetHardOnConflict bool) error {
	remoteRef := remoteBranchRef(cfg)

	pushRelease, err := acquirePushLock(ctx, dir)
	if err != nil {
		return err
	}
	defer pushRelease()

	for attempt := 0; attempt <= maxPushRetries; attempt++ {
		pushErr := Push(ctx, dir, cfg.Branch)
		if pushErr == nil {
			opts.record(OpPush, OutcomeOK, "")
			return nil
		}

		if attempt >= maxPushRetries {
			// Retry exhaustion from contention is NOT a hard error — queue it.
			queuePush(ctx, cfg, dir, opts, "push exhausted retries under contention")
			return nil
		}

		// Fetch the new remote state.
		if err := Fetch(ctx, dir); err != nil {
			// Offline / transient: queue and drain later.
			queuePush(ctx, cfg, dir, opts, "offline: "+redactToken(err).Error())
			return nil
		}

		upstreamFiles, err := DiffNameOnly(ctx, dir, baseRef, remoteRef)
		if err != nil {
			return fmt.Errorf("checking upstream changes: %w", err)
		}

		// Section-aware collision check, shared with PushLocalEdits.
		conflict := sectionOverlap(ctx, dir, ourFiles, upstreamFiles, baseRef, remoteRef)
		if conflict != "" {
			if resetHardOnConflict {
				_ = ResetHard(ctx, dir, remoteRef)
			}
			opts.record(OpPush, OutcomeConflict, conflict)
			return &sectionConflictError{desc: conflict}
		}

		if err := Rebase(ctx, dir, remoteRef); err != nil {
			RebaseAbort(ctx, dir)
			return fmt.Errorf("rebasing after push conflict — resolve manually in %s: %w", dir, err)
		}

		if newBase, err := RevParse(ctx, dir, remoteRef); err == nil {
			baseRef = newBase
		}

		backoff(attempt)
	}

	queuePush(ctx, cfg, dir, opts, "push failed after retries")
	return nil
}

// queuePush records the (already-committed) operation as queued for a later
// online flush. The work is durable and not discarded, so queuing is always a
// success and there is nothing to report (SPEC-013 §Decision 009).
func queuePush(ctx context.Context, cfg *config.SpecsRepoConfig, dir string, opts SyncOptions, detail string) {
	sha, _ := RevParse(ctx, dir, "HEAD")
	opts.recorder().Enqueue(repoKey(cfg), cfg.Branch, sha, AuditEvent{
		Op:      OpPush,
		Actor:   opts.Actor,
		Surface: opts.Surface,
		Trigger: opts.Trigger,
		SpecID:  opts.SpecID,
		Outcome: OutcomeQueued,
		Detail:  detail,
	})
	opts.record(OpPush, OutcomeQueued, detail)
}

// PushLocalEditsOpts commits any uncommitted changes in the specs repo and
// pushes them. Unlike WithSpecsRepo, which resets to remote state before
// applying a mutation, it preserves existing local edits — it is the backing
// implementation for `spec push`. Returns true if changes were found and
// pushed. On a push conflict it fetches and rebases rather than hard-resetting,
// preserving the committed local work. Unlike
// WithSpecsRepoOpts it preserves already-committed local work on a conflict
// abort — it must never hard-reset away the user's pushed-intent commits
// (SPEC-013 §7.1 / §7.2). It shares the identical section-aware conflict check
// so `spec push` can no longer silently auto-merge concurrent same-section
// edits (closing the previously-unguarded path).
func PushLocalEditsOpts(ctx context.Context, cfg *config.SpecsRepoConfig, commitMsg string, opts SyncOptions) (bool, error) {
	if err := validateToken(cfg); err != nil {
		return false, err
	}
	opts = opts.normalized(ctx)
	dir := SpecsRepoDir(cfg)

	var baseRef string
	var ourFiles []string

	// Exclusive critical section: commit local edits, capture base + files.
	committed, err := func() (bool, error) {
		release, err := acquireExclusive(ctx, dir)
		if err != nil {
			return false, err
		}
		defer release()

		if err := ensureRemoteURL(ctx, dir, cfg); err != nil {
			return false, fmt.Errorf("updating remote URL: %w", err)
		}

		hasChanges, err := HasChanges(ctx, dir)
		if err != nil {
			return false, fmt.Errorf("checking local changes: %w", err)
		}
		if !hasChanges {
			return false, nil
		}

		// Record HEAD before committing as the base for the section check.
		base, err := RevParse(ctx, dir, "HEAD")
		if err != nil {
			return false, fmt.Errorf("reading HEAD: %w", err)
		}
		baseRef = base

		if err := Commit(ctx, dir, commitMsg); err != nil {
			return false, fmt.Errorf("committing local edits: %w", err)
		}
		opts.record(OpCommit, OutcomeOK, commitMsg)

		files, err := CommittedFiles(ctx, dir, "HEAD")
		if err != nil {
			return false, fmt.Errorf("listing committed files: %w", err)
		}
		ourFiles = files
		return true, nil
	}()
	if err != nil {
		return false, err
	}
	if !committed {
		return false, nil
	}

	// Push outside the exclusive lock, preserving local commits on conflict.
	if err := pushWithRecovery(ctx, cfg, dir, opts, baseRef, ourFiles, false); err != nil {
		return false, err
	}
	return true, nil
}

// ListSpecFiles returns all spec files in the specs/ directory of the specs
// repo, resolved from the fetched remote ref so the listing reflects upstream
// without a working-tree reset. Falls back to the working tree on error.
func ListSpecFiles(cfg *config.SpecsRepoConfig) ([]string, error) {
	dir := SpecsRepoDir(cfg)
	if files, err := listMarkdownFilesAtRef(dir, remoteBranchRef(cfg), SpecsSubDir); err == nil {
		return files, nil
	}
	return listMarkdownFiles(filepath.Join(dir, SpecsSubDir))
}

// ListTriageFiles returns all triage files in the specs/triage/ directory.
func ListTriageFiles(cfg *config.SpecsRepoConfig) ([]string, error) {
	dir := SpecsRepoDir(cfg)
	if files, err := listMarkdownFilesAtRef(dir, remoteBranchRef(cfg), SpecsSubDir+"/triage"); err == nil {
		return files, nil
	}
	triageDir := filepath.Join(dir, SpecsSubDir, "triage")
	if _, err := os.Stat(triageDir); os.IsNotExist(err) {
		return nil, nil
	}
	return listMarkdownFiles(triageDir)
}

// remoteBranchRef returns the origin tracking ref for the configured branch.
func remoteBranchRef(cfg *config.SpecsRepoConfig) string {
	return "origin/" + cfg.Branch
}

// listMarkdownFilesAtRef lists .md files directly under subDir at the given ref
// using `git ls-tree`, without touching the working tree.
func listMarkdownFilesAtRef(dir, ref, subDir string) ([]string, error) {
	out, err := Run(context.Background(), dir, "ls-tree", "--name-only", ref, subDir+"/")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || filepath.Ext(line) != ".md" {
			continue
		}
		files = append(files, filepath.Base(line))
	}
	return files, nil
}

// ArchiveSpec moves a spec from specs/ to archive/ and commits the change.
func ArchiveSpec(ctx context.Context, cfg *config.SpecsRepoConfig, specID, archiveDir string) error {
	return WithSpecsRepo(ctx, cfg, func(repoPath string) (string, error) {
		sd := filepath.Join(repoPath, SpecsSubDir)
		specPath := filepath.Join(sd, specID+".md")
		archivePath := filepath.Join(sd, archiveDir, specID+".md")

		if _, err := os.Stat(specPath); err != nil {
			return "", fmt.Errorf("spec %s not found in specs/: %w", specID, err)
		}

		if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
			return "", fmt.Errorf("creating archive directory: %w", err)
		}

		if _, err := Run(ctx, repoPath, "mv", specPath, archivePath); err != nil {
			return "", fmt.Errorf("moving %s to archive: %w", specID, err)
		}

		return fmt.Sprintf("archive: %s", specID), nil
	})
}

// RestoreSpec moves a spec from archive/ back to specs/ and commits the change.
func RestoreSpec(ctx context.Context, cfg *config.SpecsRepoConfig, specID, archiveDir string) error {
	return WithSpecsRepo(ctx, cfg, func(repoPath string) (string, error) {
		sd := filepath.Join(repoPath, SpecsSubDir)
		archivePath := filepath.Join(sd, archiveDir, specID+".md")
		specPath := filepath.Join(sd, specID+".md")

		if _, err := os.Stat(archivePath); err != nil {
			return "", fmt.Errorf("spec %s not found in archive: %w", specID, err)
		}

		if _, err := Run(ctx, repoPath, "mv", archivePath, specPath); err != nil {
			return "", fmt.Errorf("restoring %s: %w", specID, err)
		}

		return fmt.Sprintf("restore: %s", specID), nil
	})
}

// ListArchiveFiles returns all archived spec files.
func ListArchiveFiles(cfg *config.SpecsRepoConfig, archiveDir string) ([]string, error) {
	dir := SpecsRepoDir(cfg)
	if files, err := listMarkdownFilesAtRef(dir, remoteBranchRef(cfg), SpecsSubDir+"/"+archiveDir); err == nil {
		return files, nil
	}
	archivePath := filepath.Join(dir, SpecsSubDir, archiveDir)
	if _, err := os.Stat(archivePath); os.IsNotExist(err) {
		return nil, nil
	}
	return listMarkdownFiles(archivePath)
}

func listMarkdownFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading directory %s: %w", dir, err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".md" {
			files = append(files, e.Name())
		}
	}
	return files, nil
}

// TriageFilePath returns the absolute path to a triage file.
func TriageFilePath(cfg *config.SpecsRepoConfig, filename string) string {
	return filepath.Join(SpecsRepoDir(cfg), SpecsSubDir, "triage", filename)
}

// autoRecover turns the old blocking unpushed-state guard into a recovery
// step. It NEVER hard-fails on recoverable state and NEVER discards work
// (SPEC-013 §Decision 005). It must be called while holding the exclusive lock.
//
//   - Uncommitted changes in the specs sub-tree are auto-committed with a
//     clearly-labelled `recover:` message, then treated as unpushed commits.
//   - Unpushed commits are pushed via the same section-aware rebase-retry the
//     normal push uses (so recovery is as robust as a normal push, and a
//     genuine same-section conflict surfaces the actionable error — AC-16).
//   - A transient/offline failure leaves the work committed and marks it
//     queued; the next operation drains it.
//
// SPEC_FORCE is retained ONLY as an explicit discard escape and is documented
// as a last resort — it is no longer the normal path.
func autoRecover(ctx context.Context, cfg *config.SpecsRepoConfig, dir string, opts SyncOptions) error {
	// Explicit discard escape (last resort). Discards stranded work so a
	// subsequent reset is clean. Never reached on the happy path.
	if os.Getenv("SPEC_FORCE") != "" {
		_, _ = Run(ctx, dir, "checkout", "--", ".")
		_, _ = Run(ctx, dir, "clean", "-fd", SpecsSubDir)
		return nil
	}

	remoteRef := remoteBranchRef(cfg)

	// 1. Auto-commit stranded uncommitted changes (scoped to specs/ — never
	// sweep in unrelated junk).
	hasUncommitted, err := HasChanges(ctx, dir)
	if err != nil {
		return nil // can't check — let the caller proceed
	}
	if hasUncommitted {
		if _, err := Run(ctx, dir, "add", "-A", SpecsSubDir); err != nil {
			return fmt.Errorf("staging stranded edits for recovery: %w", err)
		}
		// Only commit if staging actually produced something.
		if staged, _ := Run(ctx, dir, "diff", "--cached", "--name-only"); staged != "" {
			if _, err := Run(ctx, dir, "commit", "-m", "recover: stranded local edits"); err != nil {
				return fmt.Errorf("committing stranded edits for recovery: %w", err)
			}
			opts.record(OpRecover, OutcomeOK, "auto-committed stranded local edits")
		}
	}

	// 2. Push any unpushed commits with the section-aware recovery push.
	hasUnpushed, err := HasUnpushedCommits(ctx, dir, remoteRef)
	if err != nil {
		// Remote tracking ref may not exist yet (fresh clone). Don't block.
		return nil
	}
	if !hasUnpushed {
		return nil
	}

	base, err := RevParse(ctx, dir, remoteRef)
	if err != nil {
		return nil
	}
	files, err := DiffNameOnly(ctx, dir, base, "HEAD")
	if err != nil {
		files = nil
	}

	recoverOpts := opts
	recoverOpts.Trigger = "auto-recover"
	if err := pushWithRecovery(ctx, cfg, dir, recoverOpts, base, files, false); err != nil {
		// A genuine same-section conflict surfaces here (AC-16). Work stays
		// committed locally — never discarded.
		var sc *sectionConflictError
		if asSectionConflict(err, &sc) {
			opts.record(OpRecover, OutcomeConflict, sc.desc)
			return fmt.Errorf(
				"stranded local work conflicts with %s — resolve in %s (your work is preserved, not discarded; set SPEC_FORCE=1 only to discard it)",
				sc.desc, dir,
			)
		}
		return err
	}
	opts.record(OpRecover, OutcomeOK, "pushed stranded commits")
	return nil
}

// FlushQueue drains queued (offline / contention-exhausted) pushes for the
// specs repo. It is best-effort and non-fatal: it is invoked opportunistically
// by read and mutate paths and by `spec status`. Each queued entry is
// reconciled independently — a same-section conflict marks only that entry
// needs-resolution and never strands the rest (SPEC-013 §Decision 010, AC-24).
//
// Because autoRecover already pushes any unpushed commits at the start of
// every committing operation, the common case is that the branch is pushable;
// FlushQueue resolves the queued markers once the branch lands.
func FlushQueue(ctx context.Context, cfg *config.SpecsRepoConfig, opts SyncOptions) {
	opts = opts.normalized(ctx)
	rec := opts.recorder()
	pending := rec.Pending(repoKey(cfg))
	if len(pending) == 0 {
		return
	}
	dir := SpecsRepoDir(cfg)

	if err := validateToken(cfg); err != nil {
		return
	}
	if err := ensureRemoteURL(ctx, dir, cfg); err != nil {
		return
	}

	remoteRef := remoteBranchRef(cfg)

	release, err := acquireExclusive(ctx, dir)
	if err != nil {
		return
	}
	defer release()

	hasUnpushed, err := HasUnpushedCommits(ctx, dir, remoteRef)
	if err != nil {
		return
	}
	if !hasUnpushed {
		// Nothing local to push — the commits already landed; clear markers.
		for _, item := range pending {
			rec.ResolveQueued(item.ID)
		}
		return
	}

	base, err := RevParse(ctx, dir, remoteRef)
	if err != nil {
		return
	}
	files, err := DiffNameOnly(ctx, dir, base, "HEAD")
	if err != nil {
		files = nil
	}

	flushOpts := opts
	flushOpts.Trigger = "queue-flush"
	err = pushWithRecovery(ctx, cfg, dir, flushOpts, base, files, false)

	var sc *sectionConflictError
	switch {
	case err == nil:
		opts.record(OpQueueFlush, OutcomeOK, fmt.Sprintf("flushed %d queued", len(pending)))
		for _, item := range pending {
			rec.ResolveQueued(item.ID)
		}
	case asSectionConflict(err, &sc):
		// Mark every entry touching the conflicting spec as needs-resolution;
		// the rest remain queued for the next flush.
		opts.record(OpQueueFlush, OutcomeConflict, sc.desc)
		for _, item := range pending {
			if item.SpecID != "" && strings.Contains(sc.desc, item.SpecID) {
				rec.MarkQueued(item.ID, "needs-resolution", sc.desc)
			}
		}
	default:
		// Still offline / transient — leave queued, try again next time.
		opts.record(OpQueueFlush, OutcomeQueued, "flush deferred")
	}
}

// IsSectionConflict reports whether err is (or wraps) a genuine same-section
// collision — the only push failure that must block a human (vs. a queued
// transient/contention failure, which is non-fatal).
func IsSectionConflict(err error) bool {
	var sc *sectionConflictError
	return asSectionConflict(err, &sc)
}

// asSectionConflict reports whether err is (or wraps) a sectionConflictError,
// storing the matched value in target.
func asSectionConflict(err error, target **sectionConflictError) bool {
	return errors.As(err, target)
}

// validateToken checks that the specs repo token is usable.
// Returns an actionable error if the token is missing or looks like an
// unresolved environment variable reference.
func validateToken(cfg *config.SpecsRepoConfig) error {
	token := cfg.Token
	if token == "" {
		return fmt.Errorf("specs repo token not configured — set SPEC_GITHUB_TOKEN (or legacy GITHUB_TOKEN) in your environment or add 'token' to specs_repo in spec.config.yaml")
	}
	if strings.HasPrefix(token, "${") {
		return fmt.Errorf("specs repo token %s is not set in your environment — export it before running spec", token)
	}
	return nil
}

// redactToken removes tokens from error messages to avoid leaking credentials.
func redactToken(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	// Redact x-access-token:TOKEN@ patterns
	redacted := tokenRedactPattern.ReplaceAllString(msg, "x-access-token:***@")
	return fmt.Errorf("%s", redacted)
}

var tokenRedactPattern = regexp.MustCompile(`x-access-token:[^@]+@`)

// ensureRemoteURL updates the origin remote URL if the token has changed
// since the repo was cloned. This ensures fetch/push use the current token.
func ensureRemoteURL(ctx context.Context, dir string, cfg *config.SpecsRepoConfig) error {
	expected := SpecsRepoURL(cfg)
	current, err := Run(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		return fmt.Errorf("getting current remote URL: %w", err)
	}
	if current != expected {
		if _, err := Run(ctx, dir, "remote", "set-url", "origin", expected); err != nil {
			return fmt.Errorf("setting remote URL: %w", err)
		}
	}
	return nil
}
