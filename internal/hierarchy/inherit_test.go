package hierarchy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const initiativeSpec = `---
id: SPEC-004
title: API rate limiting
status: engineering
---

# SPEC-004 — API rate limiting

## TL;DR

Rate-limit the public API so one tenant cannot starve the rest.

## 1. Problem Statement

A single tenant can saturate the gateway.

## 2. Goals & Non-Goals

### Goals

- Fairness

## 4. Proposed Solution

A token bucket per tenant, fronted by Redis.

## 6. Acceptance Criteria

- [ ] The gateway rejects over-limit requests with 429

## 7.3 PR Stack Plan

1. [gateway] Do not build this from a child spec
`

func TestInheritedContext(t *testing.T) {
	got := InheritedContext("SPEC-004", "API rate limiting", initiativeSpec)

	for _, want := range []string{
		"inherited from SPEC-004",
		"read-only context, do not implement",
		"## Initiative context — SPEC-004: API rate limiting",
		"### TL;DR",
		"one tenant cannot starve the rest",
		"### 1. Problem Statement",
		"saturate the gateway",
		"### 4. Proposed Solution",
		"token bucket per tenant",
		"<!-- end inherited context -->",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("inherited block is missing %q:\n%s", want, got)
		}
	}
}

// The bound is the point: the parent's acceptance criteria and PR stack
// describe nothing the slice should build, and an agent shown the initiative's
// §7.3 will implement the wrong slice.
func TestInheritedContext_ExcludesTheParentsWorkOrder(t *testing.T) {
	got := InheritedContext("SPEC-004", "API rate limiting", initiativeSpec)

	for _, forbidden := range []string{
		"Acceptance Criteria",
		"PR Stack Plan",
		"Do not build this from a child spec",
		"Goals & Non-Goals",
	} {
		if strings.Contains(got, forbidden) {
			t.Errorf("inherited block leaked the parent's %q:\n%s", forbidden, got)
		}
	}
}

func TestInheritedContext_EmptyParentYieldsNothing(t *testing.T) {
	empty := "---\nid: SPEC-004\n---\n\n# SPEC-004\n\n## TL;DR\n\n## 1. Problem Statement\n"
	if got := InheritedContext("SPEC-004", "Empty", empty); got != "" {
		t.Errorf("an initiative with no filled sections should contribute nothing, got:\n%s", got)
	}
}

func TestInheritedContext_UntitledParent(t *testing.T) {
	got := InheritedContext("SPEC-004", "", initiativeSpec)
	if !strings.Contains(got, "## Initiative context — SPEC-004\n") {
		t.Errorf("an untitled parent should render its ID alone:\n%s", got)
	}
}

func TestGraph_InheritedContextFor(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "SPEC-004.md"), []byte(initiativeSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSpec(t, root, "SPEC-009", "build", "SPEC-004")
	writeSpec(t, root, "SPEC-014", "draft", "")
	writeSpec(t, root, "SPEC-020", "draft", "SPEC-999")

	g, err := Load(root, "archive")
	if err != nil {
		t.Fatal(err)
	}

	if got := g.InheritedContextFor("SPEC-009"); !strings.Contains(got, "token bucket per tenant") {
		t.Errorf("a slice should inherit its initiative's solution, got:\n%s", got)
	}
	if got := g.InheritedContextFor("SPEC-014"); got != "" {
		t.Errorf("a standalone spec inherits nothing, got:\n%s", got)
	}
	if got := g.InheritedContextFor("SPEC-020"); got != "" {
		t.Errorf("a dangling parent inherits nothing, got:\n%s", got)
	}
	if got := g.InheritedContextFor("SPEC-999"); got != "" {
		t.Errorf("an unknown spec inherits nothing, got:\n%s", got)
	}
}

// An initiative that has been archived is still the source of its slices'
// rationale, so the block must survive the parent moving into archive/.
func TestGraph_InheritedContextFor_ArchivedInitiative(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "archive")
	if err := os.MkdirAll(archive, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archive, "SPEC-004.md"), []byte(initiativeSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSpec(t, root, "SPEC-009", "build", "SPEC-004")

	g, err := Load(root, "archive")
	if err != nil {
		t.Fatal(err)
	}
	if got := g.InheritedContextFor("SPEC-009"); !strings.Contains(got, "token bucket") {
		t.Errorf("an archived initiative must still supply context, got:\n%s", got)
	}
}
