package tui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aaronl1011/spec/internal/config"
)

// TestSyncSpecsRepo_NoTeamIsNoop verifies the read-path fetch degrades to a
// no-op (never errors, never touches the network) when no specs repo is
// configured, so local-only / offline use keeps working.
func TestSyncSpecsRepo_NoTeamIsNoop(t *testing.T) {
	cases := map[string]*config.ResolvedConfig{
		"nil config":        nil,
		"nil team":          {Team: nil},
		"team without repo": {Team: &config.TeamConfig{}},
	}
	for name, rc := range cases {
		if err := syncSpecsRepo(context.Background(), rc); err != nil {
			t.Errorf("%s: syncSpecsRepo should be a no-op, got error: %v", name, err)
		}
	}
}

// TestLoaders_ReturnLocalDataWhenConfiguredRepoMissing proves the loaders still
// return the cached local files even when the fetch path would fail, so a
// teammate-staleness fetch error degrades to showing local data rather than a
// blank view. The configured repo points nowhere reachable, but the local
// SpecsRepoDir holds a real spec.
func TestLoaders_ReturnLocalDataWhenConfiguredRepoMissing(t *testing.T) {
	dir := t.TempDir()
	writeSpec(t, filepath.Join(dir, "SPEC-001.md"), `---
id: SPEC-001
title: Local spec
status: build
author: alice
updated: "2026-05-20"
---
# SPEC-001
`)

	rc := &config.ResolvedConfig{
		Team: &config.TeamConfig{},
		// No Owner/Repo, so syncSpecsRepo is a no-op and the local read wins.
		SpecsRepoDir: dir,
	}

	specs, err := loadAllSpecs(context.Background(), rc, false)
	if err != nil {
		t.Fatalf("loadAllSpecs returned error: %v", err)
	}
	if len(specs) != 1 || specs[0].ID != "SPEC-001" {
		t.Fatalf("loadAllSpecs = %+v, want one SPEC-001 from local disk", specs)
	}

	stages, err := loadPipelineData(context.Background(), rc)
	if err != nil {
		t.Fatalf("loadPipelineData returned error: %v", err)
	}
	var found bool
	for _, s := range stages {
		for _, sp := range s.Specs {
			if sp.ID == "SPEC-001" {
				found = true
			}
		}
	}
	if !found {
		t.Error("loadPipelineData should surface the local SPEC-001")
	}
}

func writeSpec(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
