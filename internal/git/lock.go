package git

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/gofrs/flock"
)

// Lock files live under .git/ so they never appear in `git status` or get
// swept into a commit by `add -A`.
const (
	// mutateLockFile serializes the local critical section
	// (fetch → recover → mutate → commit) and shared reads on one host.
	mutateLockFile = ".git/spec-lock"
	// pushLockFile serializes the network push phase separately from the
	// mutate critical section, so shared-lock readers never block on a push.
	pushLockFile = ".git/spec-push-lock"
)

// IMPORTANT: The advisory locks in this file are SINGLE-HOST ONLY.
//
// They use kernel-advisory flock(2), which serializes only processes on the
// same machine that share the same clone directory (CLI vs MCP vs `spec watch`).
// They provide NO cross-machine mutual exclusion. Cross-teammate safety is, and
// remains, git's atomic ref update on push plus the section-aware rebase-retry
// loop (see WithSpecsRepo / PushLocalEdits). Do not mistake these locks for a
// distributed lock; do not "fix" them into a pid-file scheme — a true flock is
// released by the kernel when the holding fd closes (including on crash), so
// there is no stale-lock-file hazard to clean up.

// releaseFunc unlocks a held lock. It is always safe to call, even on error.
type releaseFunc func()

func noopRelease() {}

// acquireExclusive takes the exclusive mutate lock for the clone at dir,
// blocking until it is available or ctx is cancelled. The returned release
// must be called to unlock.
func acquireExclusive(ctx context.Context, dir string) (releaseFunc, error) {
	return acquireLock(ctx, filepath.Join(dir, mutateLockFile), true)
}

// acquireShared takes a shared (read) lock for the clone at dir. Multiple
// shared holders proceed concurrently; an exclusive holder excludes them.
func acquireShared(ctx context.Context, dir string) (releaseFunc, error) {
	return acquireLock(ctx, filepath.Join(dir, mutateLockFile), false)
}

// acquirePushLock takes the narrower push-serialization lock for the clone at
// dir. It is held only around the network push so concurrent pushes on one
// host serialize without blocking shared-lock readers.
func acquirePushLock(ctx context.Context, dir string) (releaseFunc, error) {
	return acquireLock(ctx, filepath.Join(dir, pushLockFile), true)
}

func acquireLock(ctx context.Context, path string, exclusive bool) (releaseFunc, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	fl := flock.New(path)

	var ok bool
	var err error
	if exclusive {
		ok, err = fl.TryLockContext(ctx, lockRetryInterval)
	} else {
		ok, err = fl.TryRLockContext(ctx, lockRetryInterval)
	}
	if err != nil {
		return noopRelease, fmt.Errorf("acquiring specs-repo lock %s: %w", filepath.Base(path), err)
	}
	if !ok {
		return noopRelease, fmt.Errorf("could not acquire specs-repo lock %s", filepath.Base(path))
	}
	return func() { _ = fl.Unlock() }, nil
}
