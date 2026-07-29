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

// TestIsTerminalStage is the single question the bounty earn hook asks: has
// this transition landed the spec somewhere that settles a permanent record?
func TestIsTerminalStage(t *testing.T) {
	pipe := config.DefaultPipeline()
	tests := []struct {
		stage string
		want  bool
	}{
		{stage: "done", want: true},
		{stage: "closed", want: true},
		{stage: "build", want: false},
		{stage: "draft", want: false},
		{stage: "", want: false},
		{stage: "not-a-stage", want: false},
	}
	for _, tt := range tests {
		if got := IsTerminalStage(pipe, tt.stage); got != tt.want {
			t.Errorf("IsTerminalStage(%q) = %v, want %v", tt.stage, got, tt.want)
		}
	}
}

// TestTerminalStagesWithReasons_ExplainsEachRule: the reason is what `spec
// pipeline` and `spec config test` print, so each rule must be attributed
// correctly rather than guessed at by the caller.
func TestTerminalStagesWithReasons_ExplainsEachRule(t *testing.T) {
	tests := []struct {
		name string
		pipe config.PipelineConfig
		want map[string]string
	}{
		{
			name: "auto_archive and last required",
			pipe: config.DefaultPipeline(),
			want: map[string]string{
				"closed": TerminalReasonAutoArchive,
				"done":   TerminalReasonLastRequired,
			},
		},
		{
			name: "custom last required stage",
			pipe: config.PipelineConfig{Stages: []config.StageConfig{
				{Name: "draft"},
				{Name: "shipped"},
			}},
			want: map[string]string{"shipped": TerminalReasonLastRequired},
		},
		{
			name: "all stages optional falls back to stage names",
			pipe: config.PipelineConfig{Stages: []config.StageConfig{
				{Name: "draft", Optional: true},
				{Name: "done", Optional: true},
			}},
			want: map[string]string{"done": TerminalReasonNameFallback},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := make(map[string]string)
			for _, ts := range TerminalStagesWithReasons(tt.pipe) {
				got[ts.Name] = ts.Reason
			}
			if len(got) != len(tt.want) {
				t.Fatalf("terminal stages = %v, want %v", got, tt.want)
			}
			for name, reason := range tt.want {
				if got[name] != reason {
					t.Errorf("reason for %q = %q, want %q", name, got[name], reason)
				}
			}
		})
	}
}

// TestTerminalStages_MatchesWithReasons guards the two entry points against
// drift: the names list must always be the reasons list, in order.
func TestTerminalStages_MatchesWithReasons(t *testing.T) {
	pipe := config.DefaultPipeline()
	names := TerminalStages(pipe)
	withReasons := TerminalStagesWithReasons(pipe)

	if len(names) != len(withReasons) {
		t.Fatalf("TerminalStages = %v, TerminalStagesWithReasons = %v", names, withReasons)
	}
	for i, n := range names {
		if withReasons[i].Name != n {
			t.Errorf("index %d: name %q vs %q", i, n, withReasons[i].Name)
		}
		if withReasons[i].Reason == "" {
			t.Errorf("index %d (%s): empty reason", i, n)
		}
	}
}
