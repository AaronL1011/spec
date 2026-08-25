package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// zeroFile truncates a file to zero bytes, first making it writable since loose
// git objects are stored read-only.
func zeroFile(path string) error {
	if err := os.Chmod(path, 0o644); err != nil {
		return err
	}
	return os.Truncate(path, 0)
}

func TestRepairObjectStore_RemovesZeroByteObjectsAndPackTemps(t *testing.T) {
	local, _ := newTestClone(t)
	objectsDir := filepath.Join(local, ".git", "objects")

	// Simulate a truncation: overwrite one loose object with zero bytes.
	var victim string
	_ = filepath.Walk(objectsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || victim != "" {
			return nil //nolint:nilerr // best-effort walk; skip unreadable entries
		}
		rel, _ := filepath.Rel(objectsDir, path)
		if len(filepath.Dir(rel)) == 2 {
			victim = path
		}
		return nil
	})
	if victim == "" {
		t.Skip("no loose object to truncate (objects packed)")
	}
	if err := zeroFile(victim); err != nil {
		t.Fatal(err)
	}

	// Stray partial-pack artifacts from an interrupted transfer.
	packDir := filepath.Join(objectsDir, "pack")
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatal(err)
	}
	strayTmp := filepath.Join(packDir, "tmp_pack_ABC123")
	strayPack := filepath.Join(packDir, "pack-orphan.pack") // no matching .idx
	for _, p := range []string{strayTmp, strayPack} {
		if err := os.WriteFile(p, []byte("partial"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	incoming := filepath.Join(objectsDir, "incoming-999")
	if err := os.MkdirAll(incoming, 0o755); err != nil {
		t.Fatal(err)
	}

	removed, err := repairObjectStore(local)
	if err != nil {
		t.Fatalf("repairObjectStore: %v", err)
	}
	// victim + tmp_pack + orphan pack + incoming dir = 4 artifacts.
	if removed < 4 {
		t.Errorf("repairObjectStore removed %d artifacts, want >= 4", removed)
	}

	if _, err := os.Stat(victim); !os.IsNotExist(err) {
		t.Errorf("zero-byte object %s should have been removed", victim)
	}
	for _, p := range []string{strayTmp, strayPack, incoming} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("stray artifact %s should have been removed", p)
		}
	}
}

func TestRestoreTruncatedFiles(t *testing.T) {
	local, _ := newTestClone(t)
	ctx := context.Background()

	// Truncate a tracked working-tree file to zero bytes.
	target := filepath.Join(local, "specs", "SPEC-001.md")
	if err := os.Truncate(target, 0); err != nil {
		t.Fatal(err)
	}

	restoreTruncatedFiles(ctx, local)

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Error("truncated tracked file should have been restored from HEAD")
	}
}

// TestFetchWithRepair_GenuineFailureIsSurfaced proves the evidence-based
// decision: when a fetch fails for a NON-corruption reason (unreachable
// remote) and the object store is structurally intact, fetchWithRepair must
// surface the original error and never claim a repair — regardless of what
// git's (possibly localized) error text says.
func TestFetchWithRepair_GenuineFailureIsSurfaced(t *testing.T) {
	local, _ := newTestClone(t)
	ctx := context.Background()

	deadRemote := filepath.Join(t.TempDir(), "does-not-exist.git")
	if _, err := Run(ctx, local, "remote", "set-url", "origin", deadRemote); err != nil {
		t.Fatal(err)
	}

	repaired, err := fetchWithRepair(ctx, local)
	if err == nil {
		t.Fatal("expected fetch against a nonexistent remote to fail")
	}
	if repaired {
		t.Error("intact object store must not be reported as repaired")
	}
}

// TestRepairObjectStore_IntactStoreRemovesNothing confirms a healthy clone is
// left untouched (removed == 0), so a genuine network failure is never
// misclassified as corruption.
func TestRepairObjectStore_IntactStoreRemovesNothing(t *testing.T) {
	local, _ := newTestClone(t)
	removed, err := repairObjectStore(local)
	if err != nil {
		t.Fatalf("repairObjectStore: %v", err)
	}
	if removed != 0 {
		t.Errorf("repairObjectStore removed %d from an intact store, want 0", removed)
	}
}
