package build

import (
	"testing"

	"github.com/aaronl1011/spec-cli/internal/store"
)

func TestParsePRStack(t *testing.T) {
	content := `---
id: SPEC-042
title: Test
status: build
version: 0.1.0
author: Aaron
cycle: Cycle 7
revert_count: 0
created: "2026-04-01"
updated: "2026-04-01"
---

# SPEC-042 - Test

## Decision Log
| # | Question / Decision | Options Considered | Decision Made | Rationale | Decided By | Date |
|---|---|---|---|---|---|---|

## 1. Problem Statement           <!-- owner: pm -->
Test problem.

## 7. Technical Implementation    <!-- owner: engineer -->

### 7.3 PR Stack Plan
1. [auth-service] Add token bucket rate limiter
2. [auth-service] Integrate Redis backend
3. [api-gateway] Add rate limit middleware
4. [frontend] Add rate limit error handling
`

	steps, err := ParsePRStack(content)
	if err != nil {
		t.Fatalf("ParsePRStack: %v", err)
	}

	if len(steps) != 4 {
		t.Fatalf("steps = %d, want 4", len(steps))
	}

	tests := []struct {
		idx  int
		repo string
		desc string
	}{
		{0, "auth-service", "Add token bucket rate limiter"},
		{1, "auth-service", "Integrate Redis backend"},
		{2, "api-gateway", "Add rate limit middleware"},
		{3, "frontend", "Add rate limit error handling"},
	}

	for _, tt := range tests {
		if steps[tt.idx].Repo != tt.repo {
			t.Errorf("step[%d].Repo = %q, want %q", tt.idx, steps[tt.idx].Repo, tt.repo)
		}
		if steps[tt.idx].Description != tt.desc {
			t.Errorf("step[%d].Description = %q, want %q", tt.idx, steps[tt.idx].Description, tt.desc)
		}
		if steps[tt.idx].Number != tt.idx+1 {
			t.Errorf("step[%d].Number = %d, want %d", tt.idx, steps[tt.idx].Number, tt.idx+1)
		}
	}
}

func TestSessionCreateAndAdvance(t *testing.T) {
	t.Setenv("SPEC_HOME", t.TempDir())

	db, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	steps := []PRStep{
		{Number: 1, Repo: "auth-service", Description: "Step 1", Status: "pending"},
		{Number: 2, Repo: "api-gateway", Description: "Step 2", Status: "pending"},
	}

	session, err := CreateSession(db, "SPEC-042", steps, "/tmp/test")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if session.CurrentStep != 1 {
		t.Errorf("CurrentStep = %d, want 1", session.CurrentStep)
	}
	if session.Steps[0].Status != "in-progress" {
		t.Errorf("step 1 status = %q, want in-progress", session.Steps[0].Status)
	}

	// Advance
	if err := AdvanceStep(db, session); err != nil {
		t.Fatalf("AdvanceStep: %v", err)
	}

	if session.CurrentStep != 2 {
		t.Errorf("CurrentStep = %d, want 2", session.CurrentStep)
	}
	if session.Steps[0].Status != "complete" {
		t.Errorf("step 1 status = %q, want complete", session.Steps[0].Status)
	}
	if session.Steps[1].Status != "in-progress" {
		t.Errorf("step 2 status = %q, want in-progress", session.Steps[1].Status)
	}

	// Complete last step
	if err := AdvanceStep(db, session); err != nil {
		t.Fatalf("AdvanceStep: %v", err)
	}

	if !session.IsComplete() {
		t.Error("session should be complete")
	}
}

func TestSessionPersistence(t *testing.T) {
	t.Setenv("SPEC_HOME", t.TempDir())

	db, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	steps := []PRStep{
		{Number: 1, Repo: "test", Description: "Test step", Status: "pending"},
	}

	_, err = CreateSession(db, "SPEC-001", steps, "/tmp")
	if err != nil {
		t.Fatal(err)
	}

	// Load it back
	loaded, err := LoadSession(db, "SPEC-001")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if loaded == nil {
		t.Fatal("loaded session is nil")
	}
	if loaded.SpecID != "SPEC-001" {
		t.Errorf("SpecID = %q, want SPEC-001", loaded.SpecID)
	}
	if len(loaded.Steps) != 1 {
		t.Errorf("steps = %d, want 1", len(loaded.Steps))
	}
}

func TestMCPServerResources(t *testing.T) {
	session := &SessionState{
		SpecID:      "SPEC-042",
		CurrentStep: 1,
		Steps:       []PRStep{{Number: 1, Repo: "test", Description: "Test"}},
	}

	buildCtx := &BuildContext{
		SpecContent: "# Test Spec\n\n## Decision Log\n| # |\n",
		Conventions: "Use conventional commits.",
	}

	server := NewMCPServer(session, buildCtx, nil, "")
	resources := server.ListResources()

	if len(resources) < 3 {
		t.Errorf("resources = %d, want >= 3", len(resources))
	}

	// Check full spec resource
	found := false
	for _, r := range resources {
		if r.URI == "spec://current/full" {
			found = true
			if r.Content != buildCtx.SpecContent {
				t.Error("full spec content mismatch")
			}
		}
	}
	if !found {
		t.Error("spec://current/full resource not found")
	}
}
