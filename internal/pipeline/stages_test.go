package pipeline

import (
	"testing"

	"github.com/aaronl1011/spec/internal/config"
)

func TestTerminalStages_DefaultPipeline(t *testing.T) {
	pipe := config.DefaultPipeline()
	terminal := TerminalStages(pipe)

	// Default pipeline has "closed" with AutoArchive and "done" as last required
	hasArchive := false
	hasDone := false
	for _, s := range terminal {
		if s == "closed" {
			hasArchive = true
		}
		if s == "done" {
			hasDone = true
		}
	}
	if !hasArchive {
		t.Errorf("expected 'closed' (auto-archive) in terminal stages, got %v", terminal)
	}
	if !hasDone {
		t.Errorf("expected 'done' (last required) in terminal stages, got %v", terminal)
	}
}

func TestTerminalStages_CustomAutoArchive(t *testing.T) {
	pipe := config.PipelineConfig{
		Stages: []config.StageConfig{
			{Name: "draft"},
			{Name: "review"},
			{Name: "shipped", AutoArchive: true},
		},
	}
	terminal := TerminalStages(pipe)

	if len(terminal) == 0 {
		t.Fatal("expected at least one terminal stage")
	}
	if terminal[0] != "shipped" {
		t.Errorf("expected 'shipped', got %v", terminal)
	}
}

func TestTerminalStages_FallbackToDoneClosed(t *testing.T) {
	// All stages are optional and none have auto-archive — triggers fallback
	pipe := config.PipelineConfig{
		Stages: []config.StageConfig{
			{Name: "alpha", Optional: true},
			{Name: "done", Optional: true},
			{Name: "closed", Optional: true},
		},
	}
	terminal := TerminalStages(pipe)

	found := make(map[string]bool)
	for _, s := range terminal {
		found[s] = true
	}
	if !found["done"] || !found["closed"] {
		t.Errorf("expected fallback to done+closed, got %v", terminal)
	}
}

func TestTerminalStages_Empty(t *testing.T) {
	pipe := config.PipelineConfig{}
	terminal := TerminalStages(pipe)
	if len(terminal) != 0 {
		t.Errorf("expected no terminal stages for empty pipeline, got %v", terminal)
	}
}
