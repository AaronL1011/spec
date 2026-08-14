package markdown

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const parentlessSpec = `---
id: SPEC-009
title: Token bucket limiter
status: build
version: 0.1.0
author: aaron
cycle: Q1
revert_count: 0
created: 2024-01-01
updated: 2024-01-01
---

# SPEC-009 — Token bucket limiter

Body text.
`

func TestParseMeta_Parent(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter string
		want        string
	}{
		{name: "absent", frontmatter: "id: SPEC-009\nstatus: draft", want: ""},
		{name: "empty", frontmatter: "id: SPEC-009\nstatus: draft\nparent: \"\"", want: ""},
		{name: "set", frontmatter: "id: SPEC-009\nstatus: draft\nparent: SPEC-004", want: "SPEC-004"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, err := ParseMeta("---\n" + tt.frontmatter + "\n---\n\nbody\n")
			if err != nil {
				t.Fatalf("ParseMeta: %v", err)
			}
			if meta.Parent != tt.want {
				t.Errorf("Parent = %q, want %q", meta.Parent, tt.want)
			}
		})
	}
}

// A spec written before hierarchy existed must survive a read/write round-trip
// byte-identically apart from the `updated` stamp WriteMeta always refreshes.
func TestWriteMeta_ParentlessSpecGainsNoField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SPEC-009.md")
	if err := os.WriteFile(path, []byte(parentlessSpec), 0o644); err != nil {
		t.Fatal(err)
	}

	meta, err := ReadMeta(path)
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	if err := WriteMeta(path, meta); err != nil {
		t.Fatalf("WriteMeta: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "parent:") {
		t.Errorf("round-trip introduced a parent field:\n%s", data)
	}
}

func TestWriteMeta_ParentPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SPEC-009.md")
	if err := os.WriteFile(path, []byte(parentlessSpec), 0o644); err != nil {
		t.Fatal(err)
	}

	meta, err := ReadMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	meta.Parent = "SPEC-004"
	if err := WriteMeta(path, meta); err != nil {
		t.Fatal(err)
	}

	reread, err := ReadMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	if reread.Parent != "SPEC-004" {
		t.Errorf("Parent = %q after round-trip, want SPEC-004", reread.Parent)
	}
	if reread.Title != "Token bucket limiter" {
		t.Errorf("Title = %q, want the original", reread.Title)
	}
}

// Specs already in the specs repo say epic_key. The read fallback is what
// keeps them resolving; without it, every pre-hierarchy spec silently loses
// its PM link.
func TestParseMeta_PMKeyCoalescesLegacyEpicKey(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter string
		want        string
	}{
		{name: "legacy epic_key only", frontmatter: "id: SPEC-009\nepic_key: PLAT-12", want: "PLAT-12"},
		{name: "pm_key only", frontmatter: "id: SPEC-009\npm_key: PLAT-34", want: "PLAT-34"},
		{name: "both present prefers pm_key", frontmatter: "id: SPEC-009\npm_key: PLAT-34\nepic_key: PLAT-12", want: "PLAT-34"},
		{name: "neither", frontmatter: "id: SPEC-009", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, err := ParseMeta("---\n" + tt.frontmatter + "\n---\n\nbody\n")
			if err != nil {
				t.Fatalf("ParseMeta: %v", err)
			}
			if meta.PMKey != tt.want {
				t.Errorf("PMKey = %q, want %q", meta.PMKey, tt.want)
			}
			if meta.EpicKey != "" {
				t.Errorf("EpicKey should be cleared after coalescing, got %q", meta.EpicKey)
			}
		})
	}
}

// Coalescing on read means the next ordinary write migrates the field in
// place — no separate rewrite pass over the specs repo is ever needed.
func TestWriteMeta_MigratesEpicKeyInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SPEC-009.md")
	legacy := strings.Replace(parentlessSpec, "cycle: Q1\n", "cycle: Q1\nepic_key: PLAT-12\n", 1)
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	meta, err := ReadMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteMeta(path, meta); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Contains(got, "epic_key:") {
		t.Errorf("epic_key survived the round-trip:\n%s", got)
	}
	if !strings.Contains(got, "pm_key: PLAT-12") {
		t.Errorf("pm_key was not written:\n%s", got)
	}
}
