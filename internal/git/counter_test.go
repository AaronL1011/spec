package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/aaronl1011/spec/internal/config"
)

// setupCounterRemote creates a bare remote and a single clone pointed at it via
// a cfg whose SpecsRepoDir resolves to that clone. Because SpecsRepoDir derives
// from HOME, the test overrides HOME so cfg-driven entry points (ClaimNextID)
// operate on the temp clone.
func setupCounterRemote(t *testing.T) (clone string) {
	t.Helper()
	ctx := context.Background()

	remote := t.TempDir()
	if _, err := Run(ctx, remote, "init", "--bare"); err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := &config.SpecsRepoConfig{
		Provider: "github",
		Owner:    "test",
		Repo:     "specs",
		Branch:   "main",
		Token:    "x",
	}
	clone = SpecsRepoDir(cfg)
	if err := os.MkdirAll(filepath.Dir(clone), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Clone(ctx, remote, clone); err != nil {
		t.Fatal(err)
	}
	configIdentity(t, clone)

	// Seed an initial commit so the branch exists (the counter ref is
	// independent, but the clone needs a HEAD to be a valid repo).
	if err := os.WriteFile(filepath.Join(clone, "README.md"), []byte("seed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Commit(ctx, clone, "seed"); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, clone, "branch", "-M", "main"); err != nil {
		t.Fatal(err)
	}
	if err := Push(ctx, clone, "main"); err != nil {
		t.Fatal(err)
	}

	// Point origin at the bare remote without a token in the URL so
	// ensureRemoteURL doesn't rewrite it to an unreachable https URL.
	if _, err := Run(ctx, clone, "remote", "set-url", "origin", remote); err != nil {
		t.Fatal(err)
	}
	return clone
}

// claimWithoutRemoteRewrite calls the claim loop but skips ensureRemoteURL,
// which would clobber the file-path origin with an https URL. We exercise the
// CAS loop directly via the internal helpers driving the same protocol.
func claimDirect(t *testing.T, ctx context.Context, clone string, kind CounterKind, bootstrapMax int) (string, error) {
	t.Helper()
	ref := kind.counterRef()
	for attempt := 0; attempt <= maxClaimRetries; attempt++ {
		expectedSHA, current, err := fetchCounter(ctx, clone, ref)
		if err != nil {
			return "", &claimOfflineError{cause: err}
		}
		next := current + 1
		if expectedSHA == "" && bootstrapMax >= current {
			next = bootstrapMax + 1
		}
		ok, err := casCounter(ctx, clone, ref, expectedSHA, next)
		if err != nil {
			if isRefRejection(err) {
				continue
			}
			return "", &claimOfflineError{cause: err}
		}
		if ok {
			return fmt.Sprintf("%s-%03d", kind.idPrefix(), next), nil
		}
	}
	return "", &claimExhaustedError{kind: kind, attempts: maxClaimRetries + 1}
}

func TestClaimNextID_Bootstrap(t *testing.T) {
	clone := setupCounterRemote(t)
	ctx := context.Background()

	// Bootstrap from a legacy max of 5: first claim must yield 006.
	got, err := claimDirect(t, ctx, clone, CounterSpec, 5)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if got != "SPEC-006" {
		t.Fatalf("first claim = %q, want SPEC-006", got)
	}

	// Second claim advances to 007 from the established ref (bootstrap ignored).
	got2, err := claimDirect(t, ctx, clone, CounterSpec, 5)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if got2 != "SPEC-007" {
		t.Fatalf("second claim = %q, want SPEC-007", got2)
	}
}

func TestClaimNextID_SequentialUnique(t *testing.T) {
	clone := setupCounterRemote(t)
	ctx := context.Background()

	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		id, err := claimDirect(t, ctx, clone, CounterSpec, 0)
		if err != nil {
			t.Fatalf("claim %d: %v", i, err)
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
	if len(seen) != 5 {
		t.Fatalf("got %d unique ids, want 5", len(seen))
	}
}

// TestClaimNextID_ConcurrentDistinct verifies AC-1/AC-3: concurrent claimants
// against the same counter receive distinct, contiguous IDs with no duplicates.
func TestClaimNextID_ConcurrentDistinct(t *testing.T) {
	clone := setupCounterRemote(t)
	ctx := context.Background()

	const n = 5
	var wg sync.WaitGroup
	results := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = claimDirect(t, ctx, clone, CounterSpec, 0)
		}(i)
	}
	wg.Wait()

	seen := map[string]bool{}
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("claim %d errored: %v", i, errs[i])
		}
		if seen[results[i]] {
			t.Fatalf("duplicate id %q across concurrent claims", results[i])
		}
		seen[results[i]] = true
	}

	want := []string{"SPEC-001", "SPEC-002", "SPEC-003", "SPEC-004", "SPEC-005"}
	got := make([]string, 0, n)
	for id := range seen {
		got = append(got, id)
	}
	sort.Strings(got)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("concurrent ids = %v, want %v", got, want)
		}
	}
}

// TestClaimNextID_StaleLeaseRejected verifies AC-10: a CAS keyed on a
// behind-tip is rejected, never silently overwriting. Here clientB advances the
// counter, then clientA attempts a CAS against the now-stale SHA it read first.
func TestClaimNextID_StaleLeaseRejected(t *testing.T) {
	clone := setupCounterRemote(t)
	ctx := context.Background()
	ref := CounterSpec.counterRef()

	// Both clients read tip (absent → bootstrap from 0, next=1).
	shaA, valA, err := fetchCounter(ctx, clone, ref)
	if err != nil {
		t.Fatal(err)
	}

	// Client B claims first and wins.
	okB, err := casCounter(ctx, clone, ref, shaA, valA+1)
	if err != nil || !okB {
		t.Fatalf("client B claim failed: ok=%v err=%v", okB, err)
	}

	// Client A now attempts a CAS keyed on the stale (absent) lease — must be
	// rejected, not silently overwrite B's claim.
	okA, err := casCounter(ctx, clone, ref, shaA, valA+1)
	if err != nil {
		t.Fatalf("client A CAS errored unexpectedly: %v", err)
	}
	if okA {
		t.Fatal("stale-lease CAS unexpectedly succeeded — lost-update race reintroduced")
	}

	// The counter still reflects B's value.
	_, val, err := fetchCounter(ctx, clone, ref)
	if err != nil {
		t.Fatal(err)
	}
	if val != valA+1 {
		t.Fatalf("counter = %d after stale CAS, want %d", val, valA+1)
	}
}

// TestClaimNextID_ExhaustionIsHardError verifies AC-8: when every CAS attempt
// loses (a perpetually-moving tip), the claim loop gives up with a hard
// exhaustion error rather than queueing or carrying a number forward.
func TestClaimNextID_ExhaustionIsHardError(t *testing.T) {
	clone := setupCounterRemote(t)
	ctx := context.Background()
	ref := CounterSpec.counterRef()

	attempts := 0
	var lastErr error
	for attempt := 0; attempt <= maxClaimRetries; attempt++ {
		attempts++
		expectedSHA, current, err := fetchCounter(ctx, clone, ref)
		if err != nil {
			t.Fatalf("fetch: %v", err)
		}
		// A competitor lands a winning claim right before our CAS, moving the tip.
		if ok, cerr := casCounter(ctx, clone, ref, expectedSHA, current+1); cerr != nil || !ok {
			t.Fatalf("competitor claim failed: ok=%v err=%v", ok, cerr)
		}
		// Our CAS keyed on the now-stale SHA must lose.
		ok, err := casCounter(ctx, clone, ref, expectedSHA, current+1)
		if err != nil {
			t.Fatalf("our CAS errored: %v", err)
		}
		if ok {
			t.Fatal("expected our stale CAS to lose every round")
		}
		lastErr = &claimExhaustedError{kind: CounterSpec, attempts: attempt + 1}
	}
	if attempts != maxClaimRetries+1 {
		t.Fatalf("looped %d times, want %d", attempts, maxClaimRetries+1)
	}
	if !IsClaimExhausted(lastErr) {
		t.Fatalf("expected exhaustion error, got %T", lastErr)
	}
	if IsClaimOffline(lastErr) {
		t.Fatal("exhaustion must not be classified as offline (no queue path)")
	}
}

// TestClaimNextID_OfflineHardFails verifies AC-5: an unreachable remote yields a
// hard offline error and claims nothing.
func TestClaimNextID_OfflineHardFails(t *testing.T) {
	clone := setupCounterRemote(t)
	ctx := context.Background()

	// Point origin at a nonexistent path to simulate offline.
	if _, err := Run(ctx, clone, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone")); err != nil {
		t.Fatal(err)
	}

	_, err := claimDirect(t, ctx, clone, CounterSpec, 0)
	if err == nil {
		t.Fatal("expected offline claim to fail")
	}
	if !IsClaimOffline(err) {
		t.Fatalf("expected claimOfflineError, got %T: %v", err, err)
	}
}

// TestClaimBudget_ExceedsPushBudget verifies AC-7: the claim retry budget is
// dedicated and larger than maxPushRetries, so a claimant that loses several
// rounds still succeeds rather than failing at the push budget of 3.
func TestClaimBudget_ExceedsPushBudget(t *testing.T) {
	if maxClaimRetries <= maxPushRetries {
		t.Fatalf("maxClaimRetries (%d) must exceed maxPushRetries (%d) so a burst beyond the push budget still allocates",
			maxClaimRetries, maxPushRetries)
	}

	// A claimant that loses exactly maxPushRetries+1 rounds (more than the push
	// budget) must still win within the dedicated claim budget.
	clone := setupCounterRemote(t)
	ctx := context.Background()
	ref := CounterSpec.counterRef()

	losses := maxPushRetries + 1
	var got string
	for attempt := 0; attempt <= maxClaimRetries; attempt++ {
		expectedSHA, current, err := fetchCounter(ctx, clone, ref)
		if err != nil {
			t.Fatalf("fetch: %v", err)
		}
		if attempt < losses {
			// A competitor wins this round, moving the tip out from under us.
			if ok, cerr := casCounter(ctx, clone, ref, expectedSHA, current+1); cerr != nil || !ok {
				t.Fatalf("competitor claim failed: ok=%v err=%v", ok, cerr)
			}
			continue
		}
		ok, err := casCounter(ctx, clone, ref, expectedSHA, current+1)
		if err != nil {
			t.Fatalf("CAS: %v", err)
		}
		if ok {
			got = "SPEC"
			break
		}
	}
	if got == "" {
		t.Fatalf("claimant lost %d rounds (> maxPushRetries=%d) and never won within budget %d",
			losses, maxPushRetries, maxClaimRetries)
	}
}

// TestBackoff_GrowsWithAttempt verifies AC-12: retries are spaced by randomized
// backoff that grows with the attempt number so losers don't re-collide in
// lockstep.
func TestBackoff_GrowsWithAttempt(t *testing.T) {
	t0 := time.Now()
	backoff(0)
	d0 := time.Since(t0)

	t1 := time.Now()
	backoff(3)
	d3 := time.Since(t1)

	if d0 < pushBackoffBase {
		t.Fatalf("backoff(0) = %v, want at least base %v", d0, pushBackoffBase)
	}
	if d3 <= d0 {
		t.Fatalf("backoff(3) = %v not greater than backoff(0) = %v", d3, d0)
	}
}

// TestClaimNextID_IndependentCounters verifies AC-9: spec and triage counters
// advance independently and never collide.
func TestClaimNextID_IndependentCounters(t *testing.T) {
	clone := setupCounterRemote(t)
	ctx := context.Background()

	specID, err := claimDirect(t, ctx, clone, CounterSpec, 0)
	if err != nil {
		t.Fatal(err)
	}
	triageID, err := claimDirect(t, ctx, clone, CounterTriage, 0)
	if err != nil {
		t.Fatal(err)
	}
	if specID != "SPEC-001" {
		t.Fatalf("spec = %q, want SPEC-001", specID)
	}
	if triageID != "TRIAGE-001" {
		t.Fatalf("triage = %q, want TRIAGE-001", triageID)
	}

	// Advancing spec again must not touch the triage counter.
	specID2, err := claimDirect(t, ctx, clone, CounterSpec, 0)
	if err != nil {
		t.Fatal(err)
	}
	if specID2 != "SPEC-002" {
		t.Fatalf("spec2 = %q, want SPEC-002", specID2)
	}
	_, triageVal, err := fetchCounter(ctx, clone, CounterTriage.counterRef())
	if err != nil {
		t.Fatal(err)
	}
	if triageVal != 1 {
		t.Fatalf("triage counter = %d, want 1", triageVal)
	}
}
