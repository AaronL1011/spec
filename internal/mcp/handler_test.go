package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGenericHandler_ListTools(t *testing.T) {
	handler := NewGenericHandler(nil, ".")
	tools := handler.ListTools()

	expected := []string{
		"spec_list",
		"spec_read",
		"spec_status",
		"spec_decide",
		"spec_decide_resolve",
		"spec_search",
		"spec_pipeline",
		"spec_validate",
	}

	if len(tools) != len(expected) {
		t.Errorf("expected %d tools, got %d", len(expected), len(tools))
	}

	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	for _, name := range expected {
		if !toolNames[name] {
			t.Errorf("expected tool %q not found", name)
		}
	}
}

func TestGenericHandler_ListResources(t *testing.T) {
	handler := NewGenericHandler(nil, ".")
	resources := handler.ListResources()

	// Should have at least pipeline and dashboard
	if len(resources) < 2 {
		t.Errorf("expected at least 2 resources, got %d", len(resources))
	}

	hasResource := func(uri string) bool {
		for _, r := range resources {
			if r.URI == uri {
				return true
			}
		}
		return false
	}

	if !hasResource("spec://pipeline") {
		t.Error("expected spec://pipeline resource")
	}
	if !hasResource("spec://dashboard") {
		t.Error("expected spec://dashboard resource")
	}
}

func TestGenericHandler_ToolSearch(t *testing.T) {
	// Create temp dir with a spec
	dir := t.TempDir()
	specContent := `---
id: SPEC-001
title: Test Spec
status: build
---
# SPEC-001 — Test Spec

## 1. Problem Statement
This is a test problem statement about authentication.
`
	if err := os.WriteFile(filepath.Join(dir, "SPEC-001.md"), []byte(specContent), 0644); err != nil {
		t.Fatal(err)
	}

	handler := NewGenericHandler(nil, dir)

	args, _ := json.Marshal(map[string]string{"query": "authentication"})
	result, err := handler.CallTool("spec_search", args)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success, got: %s", result.Message)
	}

	if result.Message == "No matches found." {
		t.Error("expected to find matches for 'authentication'")
	}
}

func TestGenericHandler_ToolList(t *testing.T) {
	// Create temp dir with specs
	dir := t.TempDir()

	specs := []struct {
		id      string
		status  string
		title   string
	}{
		{"SPEC-001", "build", "First Spec"},
		{"SPEC-002", "review", "Second Spec"},
		{"SPEC-003", "build", "Third Spec"},
	}

	for _, s := range specs {
		content := "---\nid: " + s.id + "\ntitle: " + s.title + "\nstatus: " + s.status + "\n---\n# " + s.id + "\n"
		if err := os.WriteFile(filepath.Join(dir, s.id+".md"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	handler := NewGenericHandler(nil, dir)

	// List all
	args, _ := json.Marshal(map[string]string{})
	result, err := handler.CallTool("spec_list", args)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success, got: %s", result.Message)
	}

	// Filter by stage
	args, _ = json.Marshal(map[string]string{"stage": "build"})
	result, err = handler.CallTool("spec_list", args)
	if err != nil {
		t.Fatalf("filtered list failed: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success, got: %s", result.Message)
	}
}
