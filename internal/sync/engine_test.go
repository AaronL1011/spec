package sync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/aaronl1011/spec/internal/store"
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
	if !errors.Is(err, ErrSyncConflict) {
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

// wipeSpec is a filled spec with an H1 title heading, mirroring the field
// report where inbound sync deleted every section below the title.
const wipeSpec = `---
id: SPEC-001
title: Test
status: plan_review
created: 2026-01-01
updated: 2026-01-01
---

# SPEC-001 - Test

## Problem Statement <!-- owner: tl -->
Local content
`

func TestRun_DefaultDirection_IsOutboundOnly(t *testing.T) {
	specPath := writeSpec(t, wipeSpec)
	db := openMemoryDB(t)
	// Prime state so an inbound leg WOULD apply the remote edit.
	if err := db.SyncStateSet("SPEC-001", "problem_statement", "out", Hash("Local content")); err != nil {
		t.Fatalf("SyncStateSet(out) error = %v", err)
	}
	if err := db.SyncStateSet("SPEC-001", "problem_statement", "in", Hash("Local content")); err != nil {
		t.Fatalf("SyncStateSet(in) error = %v", err)
	}
	docs := &fakeDocs{sections: map[string]string{"problem_statement": "Remote content"}}

	report, err := NewEngine(docs, db).Run(context.Background(), Options{
		SpecID:    "SPEC-001",
		SpecPath:  specPath,
		OwnerRole: "tl",
		// Direction deliberately empty: the default must be outbound-only.
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.Direction != DirectionOut {
		t.Fatalf("default direction = %q, want %q", report.Direction, DirectionOut)
	}
	if len(report.InboundApplied) != 0 {
		t.Fatalf("InboundApplied = %#v, want none by default", report.InboundApplied)
	}
	if !report.OutboundPushed {
		t.Fatal("OutboundPushed = false, want outbound publish by default")
	}
	data, _ := os.ReadFile(specPath)
	if !contains(string(data), "Local content") {
		t.Fatalf("local spec rewritten by default sync: %q", string(data))
	}
}

func TestRun_Inbound_EmptyRemote_RefusesToDeleteLocal(t *testing.T) {
	specPath := writeSpec(t, wipeSpec)
	db := openMemoryDB(t)
	// State says local is unchanged since last push — the exact condition
	// under which the old code auto-applied an empty remote section.
	if err := db.SyncStateSet("SPEC-001", "problem_statement", "out", Hash("Local content")); err != nil {
		t.Fatalf("SyncStateSet(out) error = %v", err)
	}
	if err := db.SyncStateSet("SPEC-001", "problem_statement", "in", Hash("Local content")); err != nil {
		t.Fatalf("SyncStateSet(in) error = %v", err)
	}
	docs := &fakeDocs{sections: map[string]string{"problem_statement": ""}}

	// Even ConflictForce must not let an empty remote delete local content.
	report, err := NewEngine(docs, db).Run(context.Background(), Options{
		SpecID:           "SPEC-001",
		SpecPath:         specPath,
		Direction:        DirectionIn,
		ConflictStrategy: ConflictForce,
		OwnerRole:        "tl",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(report.InboundApplied) != 0 {
		t.Fatalf("InboundApplied = %#v, want none", report.InboundApplied)
	}
	if len(report.Skipped) != 1 || report.Skipped[0].Section != "problem_statement" {
		t.Fatalf("Skipped = %#v, want empty-remote skip for problem_statement", report.Skipped)
	}
	data, _ := os.ReadFile(specPath)
	if !contains(string(data), "Local content") {
		t.Fatalf("empty remote deleted local content: %q", string(data))
	}
}

// TestRun_Inbound_TitleSection_NeverApplied reproduces the reported wipe: a
// filled spec whose H1 title slug maps remotely to an empty fragment (pages
// pushed by older builds carry a marker for the H1). The title section spans
// the whole document locally, so applying it deleted everything but the title.
func TestRun_Inbound_TitleSection_NeverApplied(t *testing.T) {
	specPath := writeSpec(t, wipeSpec)
	db := openMemoryDB(t)
	original, _ := os.ReadFile(specPath)
	// Whole-body hash recorded by an old outbound push primes the wipe.
	bodyHash := Hash("\n## Problem Statement <!-- owner: tl -->\nLocal content\n")
	for _, dir := range []string{"out", "in"} {
		if err := db.SyncStateSet("SPEC-001", "spec_001_test", dir, bodyHash); err != nil {
			t.Fatalf("SyncStateSet(%s) error = %v", dir, err)
		}
	}
	docs := &fakeDocs{sections: map[string]string{
		"spec_001_test":     "", // H1 slug from a legacy page: empty fragment
		"problem_statement": "Local content",
	}}

	report, err := NewEngine(docs, db).Run(context.Background(), Options{
		SpecID:           "SPEC-001",
		SpecPath:         specPath,
		Direction:        DirectionIn,
		ConflictStrategy: ConflictForce,
		OwnerRole:        "tl",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	for _, applied := range report.InboundApplied {
		if applied == "spec_001_test" {
			t.Fatal("title section was applied inbound")
		}
	}
	data, _ := os.ReadFile(specPath)
	if string(data) != string(original) {
		t.Fatalf("spec rewritten:\n got: %q\nwant: %q", string(data), string(original))
	}
}

func TestRun_Outbound_ExcludesTitleSectionFromState(t *testing.T) {
	specPath := writeSpec(t, wipeSpec)
	db := openMemoryDB(t)

	report, err := NewEngine(&fakeDocs{}, db).Run(context.Background(), Options{
		SpecID:    "SPEC-001",
		SpecPath:  specPath,
		Direction: DirectionOut,
		OwnerRole: "tl",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	for _, slug := range report.OutboundSections {
		if slug == "spec_001_test" {
			t.Fatal("title heading listed as an outbound section")
		}
	}
	state, err := db.SyncStateGet("SPEC-001", "spec_001_test", "out")
	if err != nil {
		t.Fatalf("SyncStateGet() error = %v", err)
	}
	if state != nil {
		t.Fatalf("state recorded for title section = %#v, want nil", state)
	}
}
