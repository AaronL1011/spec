package sync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/aaronl1011/spec-cli/internal/store"
)

type fakeDocs struct {
	sections      map[string]string
	pushedSpecID  string
	pushedContent string
	pushCount     int
	pushErr       error
}

func (f *fakeDocs) FetchSections(ctx context.Context, specID string) (map[string]string, error) {
	return f.sections, nil
}

func (f *fakeDocs) PushFull(ctx context.Context, specID string, content string) error {
	f.pushCount++
	if f.pushErr != nil {
		return f.pushErr
	}
	f.pushedSpecID = specID
	f.pushedContent = content
	return nil
}

func (f *fakeDocs) PageURL(ctx context.Context, specID string) (string, error) {
	return "", nil
}

func TestRun_Outbound_PushesFullSpecAndRecordsHashes(t *testing.T) {
	specPath := writeSpec(t, `---
id: SPEC-001
title: Test
status: draft
created: 2026-01-01
updated: 2026-01-01
---

## Problem Statement <!-- owner: tl -->
Local content
`)
	db := openMemoryDB(t)
	docs := &fakeDocs{}

	report, err := NewEngine(docs, db).Run(context.Background(), Options{
		SpecID:    "SPEC-001",
		SpecPath:  specPath,
		Direction: DirectionOut,
		UserName:  "alice",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !report.OutboundPushed {
		t.Fatal("Run() OutboundPushed = false")
	}
	if docs.pushedSpecID != "SPEC-001" || docs.pushedContent == "" {
		t.Fatalf("PushFull() spec/content = %q/%q", docs.pushedSpecID, docs.pushedContent)
	}
	state, err := db.SyncStateGet("SPEC-001", "problem_statement", "out")
	if err != nil {
		t.Fatalf("SyncStateGet() error = %v", err)
	}
	if state == nil || state.Hash != Hash("Local content") {
		t.Fatalf("stored hash = %#v, want local hash", state)
	}
}

func TestPrepare_Outbound_DefersDocsPushAndStatePersistence(t *testing.T) {
	specPath := writeSpec(t, `---
id: SPEC-001
title: Test
status: draft
created: 2026-01-01
updated: 2026-01-01
---

## Problem Statement <!-- owner: tl -->
Local content
`)
	db := openMemoryDB(t)
	docs := &fakeDocs{}

	prepared, err := NewEngine(docs, db).Prepare(context.Background(), Options{
		SpecID:    "SPEC-001",
		SpecPath:  specPath,
		Direction: DirectionOut,
		OwnerRole: "tl",
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if prepared == nil || prepared.outboundContent == "" {
		t.Fatal("Prepare() did not capture outbound content")
	}
	if docs.pushCount != 0 {
		t.Fatalf("PushFull() calls = %d, want 0 before finalize", docs.pushCount)
	}
	state, err := db.SyncStateGet("SPEC-001", "problem_statement", "out")
	if err != nil {
		t.Fatalf("SyncStateGet() error = %v", err)
	}
	if state != nil {
		t.Fatalf("state before finalize = %#v, want nil", state)
	}
}

func TestRun_Inbound_AppliesRemoteWhenLocalUnchanged(t *testing.T) {
	specPath := writeSpec(t, `---
id: SPEC-001
title: Test
status: draft
created: 2026-01-01
updated: 2026-01-01
---

## Problem Statement <!-- owner: tl -->
Old content
`)
	db := openMemoryDB(t)
	if err := db.SyncStateSet("SPEC-001", "problem_statement", "out", Hash("Old content")); err != nil {
		t.Fatalf("SyncStateSet(out) error = %v", err)
	}
	if err := db.SyncStateSet("SPEC-001", "problem_statement", "in", Hash("Old content")); err != nil {
		t.Fatalf("SyncStateSet(in) error = %v", err)
	}
	docs := &fakeDocs{sections: map[string]string{"problem_statement": "Remote content"}}

	report, err := NewEngine(docs, db).Run(context.Background(), Options{
		SpecID:    "SPEC-001",
		SpecPath:  specPath,
		Direction: DirectionIn,
		OwnerRole: "tl",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(report.InboundApplied) != 1 || report.InboundApplied[0] != "problem_statement" {
		t.Fatalf("InboundApplied = %#v", report.InboundApplied)
	}
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := string(data); !contains(got, "Remote content") {
		t.Fatalf("updated spec = %q, want remote content", got)
	}
}

func TestPrepare_InboundWriteFailure_DoesNotAdvanceState(t *testing.T) {
	specPath := writeSpec(t, `---
id: SPEC-001
title: Test
status: draft
created: 2026-01-01
updated: 2026-01-01
---

## Problem Statement <!-- owner: tl -->
Old content
`)
	db := openMemoryDB(t)
	if err := db.SyncStateSet("SPEC-001", "problem_statement", "out", Hash("Old content")); err != nil {
		t.Fatalf("SyncStateSet(out) error = %v", err)
	}
	if err := db.SyncStateSet("SPEC-001", "problem_statement", "in", Hash("Old content")); err != nil {
		t.Fatalf("SyncStateSet(in) error = %v", err)
	}
	engine := NewEngine(&fakeDocs{sections: map[string]string{"problem_statement": "Remote content"}}, db)
	engine.writeFile = func(string, []byte, os.FileMode) error {
		return errors.New("write failed")
	}

	_, err := engine.Prepare(context.Background(), Options{
		SpecID:    "SPEC-001",
		SpecPath:  specPath,
		Direction: DirectionIn,
		OwnerRole: "tl",
	})
	if err == nil {
		t.Fatal("Prepare() error = nil, want write failure")
	}
	state, err := db.SyncStateGet("SPEC-001", "problem_statement", "in")
	if err != nil {
		t.Fatalf("SyncStateGet() error = %v", err)
	}
	if state == nil || state.Hash != Hash("Old content") {
		t.Fatalf("state after write failure = %#v, want old hash", state)
	}
}

func TestFinalize_StatePersistenceFailure_ReturnsError(t *testing.T) {
	specPath := writeSpec(t, `---
id: SPEC-001
title: Test
status: draft
created: 2026-01-01
updated: 2026-01-01
---

## Problem Statement <!-- owner: tl -->
Local content
`)
	db := openMemoryDB(t)
	docs := &fakeDocs{}
	engine := NewEngine(docs, db)
	prepared, err := engine.Prepare(context.Background(), Options{
		SpecID:    "SPEC-001",
		SpecPath:  specPath,
		Direction: DirectionOut,
		OwnerRole: "tl",
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	err = engine.Finalize(context.Background(), prepared)
	if err == nil {
		t.Fatal("Finalize() error = nil, want sync_state persistence error")
	}
}

func TestFinalize_OutboundPushFailure_DoesNotAdvanceState(t *testing.T) {
	specPath := writeSpec(t, `---
id: SPEC-001
title: Test
status: draft
created: 2026-01-01
updated: 2026-01-01
---

## Problem Statement <!-- owner: tl -->
Local content
`)
	db := openMemoryDB(t)
	docs := &fakeDocs{pushErr: errors.New("push failed")}
	engine := NewEngine(docs, db)
	prepared, err := engine.Prepare(context.Background(), Options{
		SpecID:    "SPEC-001",
		SpecPath:  specPath,
		Direction: DirectionOut,
		OwnerRole: "tl",
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	err = engine.Finalize(context.Background(), prepared)
	if err == nil {
		t.Fatal("Finalize() error = nil, want push failure")
	}
	state, err := db.SyncStateGet("SPEC-001", "problem_statement", "out")
	if err != nil {
		t.Fatalf("SyncStateGet() error = %v", err)
	}
	if state != nil {
		t.Fatalf("state after push failure = %#v, want nil", state)
	}
}

func TestRun_Both_PushesPostInboundContent(t *testing.T) {
	specPath := writeSpec(t, `---
id: SPEC-001
title: Test
status: draft
created: 2026-01-01
updated: 2026-01-01
---

## Problem Statement <!-- owner: tl -->
Old content
`)
	db := openMemoryDB(t)
	if err := db.SyncStateSet("SPEC-001", "problem_statement", "out", Hash("Old content")); err != nil {
		t.Fatalf("SyncStateSet(out) error = %v", err)
	}
	if err := db.SyncStateSet("SPEC-001", "problem_statement", "in", Hash("Old content")); err != nil {
		t.Fatalf("SyncStateSet(in) error = %v", err)
	}
	docs := &fakeDocs{sections: map[string]string{"problem_statement": "Remote content"}}

	_, err := NewEngine(docs, db).Run(context.Background(), Options{
		SpecID:    "SPEC-001",
		SpecPath:  specPath,
		Direction: DirectionBoth,
		OwnerRole: "tl",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !contains(docs.pushedContent, "Remote content") {
		t.Fatalf("pushed content = %q, want post-inbound remote content", docs.pushedContent)
	}
}

func TestRun_Inbound_AbortOnConflict(t *testing.T) {
	specPath := writeSpec(t, `---
id: SPEC-001
title: Test
status: draft
created: 2026-01-01
updated: 2026-01-01
---

## Problem Statement <!-- owner: tl -->
Local edit
`)
	db := openMemoryDB(t)
	if err := db.SyncStateSet("SPEC-001", "problem_statement", "out", Hash("Base content")); err != nil {
		t.Fatalf("SyncStateSet(out) error = %v", err)
	}
	if err := db.SyncStateSet("SPEC-001", "problem_statement", "in", Hash("Base content")); err != nil {
		t.Fatalf("SyncStateSet(in) error = %v", err)
	}
	docs := &fakeDocs{sections: map[string]string{"problem_statement": "Remote edit"}}

	report, err := NewEngine(docs, db).Run(context.Background(), Options{
		SpecID:           "SPEC-001",
		SpecPath:         specPath,
		Direction:        DirectionIn,
		ConflictStrategy: ConflictAbort,
		OwnerRole:        "tl",
	})
	if err != ErrSyncConflict {
		t.Fatalf("Run() error = %v, want ErrSyncConflict", err)
	}
	if report == nil || len(report.Conflicts) != 1 {
		t.Fatalf("conflicts = %#v, want one conflict", report)
	}
}

func TestRun_Inbound_SkipsOwnedSectionForOtherRole(t *testing.T) {
	specPath := writeSpec(t, `---
id: SPEC-001
title: Test
status: draft
created: 2026-01-01
updated: 2026-01-01
---

## Acceptance Criteria <!-- owner: tl -->
Old content
`)
	db := openMemoryDB(t)
	if err := db.SyncStateSet("SPEC-001", "acceptance_criteria", "out", Hash("Old content")); err != nil {
		t.Fatalf("SyncStateSet(out) error = %v", err)
	}
	if err := db.SyncStateSet("SPEC-001", "acceptance_criteria", "in", Hash("Old content")); err != nil {
		t.Fatalf("SyncStateSet(in) error = %v", err)
	}
	docs := &fakeDocs{sections: map[string]string{"acceptance_criteria": "Remote content"}}

	report, err := NewEngine(docs, db).Run(context.Background(), Options{
		SpecID:    "SPEC-001",
		SpecPath:  specPath,
		Direction: DirectionIn,
		OwnerRole: "engineer",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(report.Skipped) != 1 || report.Skipped[0].Reason != "owned by tl" {
		t.Fatalf("Skipped = %#v", report.Skipped)
	}
}

func TestRun_Inbound_SkipsOwnedSectionForEmptyRole(t *testing.T) {
	specPath := writeSpec(t, `---
id: SPEC-001
title: Test
status: draft
created: 2026-01-01
updated: 2026-01-01
---

## Acceptance Criteria <!-- owner: tl -->
Old content
`)
	db := openMemoryDB(t)
	if err := db.SyncStateSet("SPEC-001", "acceptance_criteria", "out", Hash("Old content")); err != nil {
		t.Fatalf("SyncStateSet(out) error = %v", err)
	}
	if err := db.SyncStateSet("SPEC-001", "acceptance_criteria", "in", Hash("Old content")); err != nil {
		t.Fatalf("SyncStateSet(in) error = %v", err)
	}
	docs := &fakeDocs{sections: map[string]string{"acceptance_criteria": "Remote content"}}

	report, err := NewEngine(docs, db).Run(context.Background(), Options{
		SpecID:    "SPEC-001",
		SpecPath:  specPath,
		Direction: DirectionIn,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(report.Skipped) != 1 || report.Skipped[0].Reason != "owned by tl" {
		t.Fatalf("Skipped = %#v", report.Skipped)
	}
}

func openMemoryDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func writeSpec(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "SPEC-001.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func contains(s, substr string) bool {
	return len(substr) == 0 || (len(s) >= len(substr) && index(s, substr) >= 0)
}

func index(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
