package git

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/aaronl1011/spec/internal/config"
)

// CounterKind identifies an independent allocation counter. Each kind maps to
// its own git ref (refs/spec/counters/<kind>) and is allocated independently;
// there is no cross-counter atomicity (SPEC-018 §2 Non-Goals).
type CounterKind string

const (
	// CounterSpec is the counter for SPEC-NNN IDs.
	CounterSpec CounterKind = "spec"
	// CounterTriage is the counter for TRIAGE-NNN IDs.
	CounterTriage CounterKind = "triage"
)

const (
	// maxClaimRetries is the dedicated retry budget for the claim loop. It is
	// independent of maxPushRetries and sized to comfortably absorb a
	// team-sized concurrent burst. On exhaustion the claim hard-fails — an
	// unclaimed ID has no number to queue for later (SPEC-018 §7.1).
	maxClaimRetries = 10

	// counterRefPrefix namespaces the counter refs. These live outside the
	// working branch so they never appear in `git status` or get swept into a
	// content commit (SPEC-018 §7.1).
	counterRefPrefix = "refs/spec/counters/"
)

// idPrefix returns the human-facing ID prefix for a counter kind.
func (k CounterKind) idPrefix() string {
	switch k {
	case CounterTriage:
		return "TRIAGE"
	default:
		return "SPEC"
	}
}

// counterRef returns the fully-qualified ref for a counter kind.
func (k CounterKind) counterRef() string {
	return counterRefPrefix + string(k)
}

// claimOfflineError signals that the claim could not reach the remote. Unlike a
// file push, a claim cannot degrade to queued — it is a hard stop for
// allocation (SPEC-018 §7.1, AC-5).
type claimOfflineError struct{ cause error }

func (e *claimOfflineError) Error() string {
	return fmt.Sprintf(
		"cannot allocate an ID while offline — the next number is claimed against the remote counter and will not be guessed; reconnect and retry (cause: %v)",
		redactToken(e.cause),
	)
}

func (e *claimOfflineError) Unwrap() error { return e.cause }

// claimExhaustedError signals the claim loop gave up after sustained
// contention. It is a hard error, never a queue (SPEC-018 §7.1, AC-8).
type claimExhaustedError struct {
	kind     CounterKind
	attempts int
}

func (e *claimExhaustedError) Error() string {
	return fmt.Sprintf(
		"could not allocate a %s ID after %d attempts under contention — no number was claimed; retry shortly",
		e.kind.idPrefix(), e.attempts,
	)
}

// ClaimNextID atomically claims the next sequential ID for the given counter
// kind against the remote and returns the claimed ID string (e.g. "SPEC-006").
//
// It is the authoritative allocator: the number is established by a winning
// compare-and-swap against the counter ref, never by a scan of a possibly-stale
// local working tree. Concurrent claims are totally ordered by git's atomic ref
// update — exactly one wins each round; losers refetch and retry with
// randomized backoff (SPEC-018 §4).
//
// bootstrapMax supplies the legacy high-water-mark (the max of existing
// filenames) used only to initialize the counter ref on first use against a
// repo that has none. It is never the authority on an established counter.
//
// Offline is a hard stop: when the fetch or CAS cannot reach the remote,
// ClaimNextID returns an actionable error and claims nothing (AC-5). Retry
// exhaustion is likewise a hard error, never queued (AC-8).
func ClaimNextID(ctx context.Context, cfg *config.SpecsRepoConfig, kind CounterKind, bootstrapMax int) (string, error) {
	if err := validateToken(cfg); err != nil {
		return "", err
	}
	dir := SpecsRepoDir(cfg)
	if err := ensureRemoteURL(ctx, dir, cfg); err != nil {
		return "", fmt.Errorf("updating remote URL: %w", err)
	}

	ref := kind.counterRef()

	// Serialize same-host claimants so two terminals on one machine don't
	// burn the network round-trips fighting each other; cross-machine safety
	// still comes from the CAS, not this lock.
	release, err := acquirePushLock(ctx, dir)
	if err != nil {
		return "", err
	}
	defer release()

	for attempt := 0; attempt <= maxClaimRetries; attempt++ {
		// Fetch the counter ref immediately before the CAS so the lease keys on
		// the SHA actually seen this attempt, never a stale tracking ref
		// (SPEC-018 §4.2 force-with-lease correctness edge, AC-10).
		expectedSHA, current, err := fetchCounter(ctx, dir, ref)
		if err != nil {
			return "", &claimOfflineError{cause: err}
		}

		next := current + 1
		// Bootstrap: if the ref is absent, seed from the legacy filename max so
		// existing archives adopt the counter without re-issuing an in-use
		// number (SPEC-018 §7.1, §10).
		if expectedSHA == "" && bootstrapMax >= current {
			next = bootstrapMax + 1
		}

		ok, err := casCounter(ctx, dir, ref, expectedSHA, next)
		if err != nil {
			// Distinguish a lost CAS race (rejected, retryable) from a genuine
			// transport failure (offline, hard stop).
			if isRefRejection(err) {
				backoff(attempt)
				continue
			}
			return "", &claimOfflineError{cause: err}
		}
		if ok {
			return fmt.Sprintf("%s-%03d", kind.idPrefix(), next), nil
		}
		backoff(attempt)
	}

	return "", &claimExhaustedError{kind: kind, attempts: maxClaimRetries + 1}
}

// fetchCounter reads the counter ref's current tip SHA and the integer value it
// points at, fetching the object from the remote first. A missing ref yields
// ("", 0, nil) so the caller can bootstrap. The remote read is mandatory every
// attempt so the lease keys on the freshly-observed tip — see AC-10.
func fetchCounter(ctx context.Context, dir, ref string) (sha string, value int, err error) {
	// ls-remote reads the authoritative tip without mutating any local ref, so
	// concurrent same-host claimants never collide on a shared tracking ref.
	out, lerr := Run(ctx, dir, "ls-remote", "origin", ref)
	if lerr != nil {
		return "", 0, lerr
	}
	out = strings.TrimSpace(out)
	if out == "" {
		// Ref absent on remote: bootstrap path.
		return "", 0, nil
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", 0, nil
	}
	sha = fields[0]

	// Ensure the tip object is present locally so we can read its value. A
	// shallow object fetch by SHA brings it in without touching any local ref.
	if _, rerr := RevParse(ctx, dir, sha+"^{commit}"); rerr != nil {
		if _, ferr := Run(ctx, dir, "fetch", "--no-write-fetch-head", "origin", sha); ferr != nil {
			// Some servers reject fetch-by-SHA; fall back to fetching the ref
			// into FETCH_HEAD (process-local, no named ref to contend on).
			if _, ferr2 := Run(ctx, dir, "fetch", "--no-write-fetch-head", "origin", ref); ferr2 != nil {
				return "", 0, ferr2
			}
		}
	}

	// The counter ref points at a commit whose subject holds the decimal
	// high-water-mark. A commit (not a bare blob) is used deliberately: two
	// claimants computing the same next value still produce distinct commit
	// objects (unique nonce), so an identical value can never make the second
	// CAS a silent no-op "success" that smuggles in a duplicate ID.
	msg, berr := Run(ctx, dir, "log", "-1", "--format=%s", sha)
	if berr != nil {
		return "", 0, fmt.Errorf("reading counter commit %s: %w", sha, berr)
	}
	value, perr := parseCounter(msg)
	if perr != nil {
		return "", 0, fmt.Errorf("parsing counter ref %s: %w", ref, perr)
	}
	return sha, value, nil
}

// casCounter creates a new counter commit holding `value` (parented on the
// expected tip) and pushes it to the counter ref, conditioned on the ref
// currently pointing at expectedSHA via force-with-lease. expectedSHA is ""
// when bootstrapping a previously-absent ref. Returns (true, nil) when the CAS
// lands, (false, nil) when it is rejected stale, and a non-nil error for a
// transport failure.
//
// Each commit is unique even for an identical value (distinct parent and
// committer timestamp), so a losing claimant that computed the same number can
// never produce an object that equals the ref tip and turns the CAS into a
// silent no-op (the duplicate-ID hazard).
func casCounter(ctx context.Context, dir, ref, expectedSHA string, value int) (bool, error) {
	commitSHA, err := commitCounter(ctx, dir, expectedSHA, value)
	if err != nil {
		return false, err
	}

	args := []string{"push"}
	if expectedSHA == "" {
		// Bootstrap: require the ref to not yet exist. force-with-lease with an
		// empty expected value means "expect ref absent".
		args = append(args, "--force-with-lease="+ref+":")
	} else {
		args = append(args, "--force-with-lease="+ref+":"+expectedSHA)
	}
	args = append(args, "origin", commitSHA+":"+ref)

	if _, err := Run(ctx, dir, args...); err != nil {
		if isRefRejection(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// commitCounter builds an empty-tree commit whose subject is the decimal value,
// parented on the expected tip (when present), and returns its SHA. The object
// is loose until referenced by the pushed ref. A unique committer timestamp
// guarantees object uniqueness even across identical values.
func commitCounter(ctx context.Context, dir, parentSHA string, value int) (string, error) {
	// An empty tree is the natural payload — the counter carries no files, only
	// the high-water-mark in the commit subject.
	emptyTree, err := runStdin(ctx, dir, "", "mktree")
	if err != nil {
		return "", fmt.Errorf("creating empty tree for counter commit: %w", err)
	}

	// The subject is the decimal value (parsed back on read). A random nonce in
	// the body makes the commit object unique even when two claimants compute
	// the same value against the same parent, so a losing claim can never
	// produce an object equal to the winner's tip and turn its CAS into a
	// silent no-op "success".
	nonce, err := randomNonce()
	if err != nil {
		return "", err
	}
	message := strconv.Itoa(value) + "\n\nclaim-nonce: " + nonce
	args := []string{"commit-tree", strings.TrimSpace(emptyTree), "-m", message}
	if parentSHA != "" {
		args = append(args, "-p", parentSHA)
	}
	sha, err := Run(ctx, dir, args...)
	if err != nil {
		return "", fmt.Errorf("creating counter commit: %w", err)
	}
	return strings.TrimSpace(sha), nil
}

// randomNonce returns a short random hex string used to make counter commits
// unique.
func randomNonce() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating claim nonce: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// parseCounter parses the decimal high-water-mark stored in a counter commit
// subject.
func parseCounter(s string) (int, error) {
	s = strings.TrimSpace(s)
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid counter value %q", s)
	}
	if n < 0 {
		return 0, fmt.Errorf("negative counter value %d", n)
	}
	return n, nil
}

// isRefRejection reports whether a push error is a non-fast-forward / stale-info
// rejection — i.e. a lost CAS race that should refetch and retry, not a
// transport failure.
func isRefRejection(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "stale info") ||
		strings.Contains(msg, "non-fast-forward") ||
		strings.Contains(msg, "fetch first") ||
		strings.Contains(msg, "rejected") ||
		strings.Contains(msg, "cannot lock ref") ||
		strings.Contains(msg, "failed to push some refs")
}
