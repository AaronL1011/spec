package effects

import (
	"context"
	"testing"

	"github.com/nexl/spec-cli/internal/config"
)

// Mock notifier for testing
type mockNotifier struct {
	calls []struct {
		target  string
		message string
	}
	err error
}

func (m *mockNotifier) Notify(ctx context.Context, target, message string) error {
	m.calls = append(m.calls, struct {
		target  string
		message string
	}{target, message})
	return m.err
}

// Mock syncer for testing
type mockSyncer struct {
	calls []struct {
		direction string
		specID    string
	}
	err error
}

func (m *mockSyncer) Sync(ctx context.Context, direction, specID string) error {
	m.calls = append(m.calls, struct {
		direction string
		specID    string
	}{direction, specID})
	return m.err
}

func TestExecutor_DryRun(t *testing.T) {
	executor := NewExecutor(true)

	effects := []config.EffectConfig{
		{Notify: &config.NotifyEffect{Target: "@team"}},
		{Sync: "outbound"},
	}

	execCtx := ExecutionContext{
		SpecID:    "SPEC-001",
		FromStage: "draft",
		ToStage:   "review",
	}

	results := executor.Execute(context.Background(), effects, execCtx)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	for _, r := range results {
		if !r.Skipped {
			t.Errorf("expected skipped in dry-run, got success=%v", r.Success)
		}
		if r.SkipReason != "dry-run mode" {
			t.Errorf("expected skip reason 'dry-run mode', got %q", r.SkipReason)
		}
	}
}

func TestExecutor_Notify(t *testing.T) {
	executor := NewExecutor(false)
	notifier := &mockNotifier{}

	effects := []config.EffectConfig{
		{Notify: &config.NotifyEffect{Target: "@pm-team", Template: "Spec $spec_id is ready"}},
	}

	execCtx := ExecutionContext{
		SpecID:         "SPEC-042",
		SpecTitle:      "Test Feature",
		FromStage:      "draft",
		ToStage:        "review",
		TransitionType: TransitionAdvance,
		User:           "alice",
		Notifier:       notifier,
	}

	results := executor.Execute(context.Background(), effects, execCtx)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if !results[0].Success {
		t.Errorf("expected success, got error: %v", results[0].Error)
	}

	if len(notifier.calls) != 1 {
		t.Fatalf("expected 1 notification call, got %d", len(notifier.calls))
	}

	if notifier.calls[0].target != "@pm-team" {
		t.Errorf("expected target @pm-team, got %s", notifier.calls[0].target)
	}

	if notifier.calls[0].message != "Spec SPEC-042 is ready" {
		t.Errorf("expected message 'Spec SPEC-042 is ready', got %q", notifier.calls[0].message)
	}
}

func TestExecutor_NotifyNoAdapter(t *testing.T) {
	executor := NewExecutor(false)

	effects := []config.EffectConfig{
		{Notify: &config.NotifyEffect{Target: "@team"}},
	}

	execCtx := ExecutionContext{
		SpecID: "SPEC-001",
		// No Notifier set
	}

	results := executor.Execute(context.Background(), effects, execCtx)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if !results[0].Skipped {
		t.Error("expected skipped when no notifier")
	}

	if results[0].SkipReason != "no notifier configured" {
		t.Errorf("expected skip reason 'no notifier configured', got %q", results[0].SkipReason)
	}
}

func TestExecutor_Sync(t *testing.T) {
	executor := NewExecutor(false)
	syncer := &mockSyncer{}

	effects := []config.EffectConfig{
		{Sync: "outbound"},
	}

	execCtx := ExecutionContext{
		SpecID: "SPEC-001",
		Syncer: syncer,
	}

	results := executor.Execute(context.Background(), effects, execCtx)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if !results[0].Success {
		t.Errorf("expected success, got error: %v", results[0].Error)
	}

	if len(syncer.calls) != 1 {
		t.Fatalf("expected 1 sync call, got %d", len(syncer.calls))
	}

	if syncer.calls[0].direction != "outbound" {
		t.Errorf("expected direction 'outbound', got %s", syncer.calls[0].direction)
	}
}

func TestExecutor_Archive(t *testing.T) {
	executor := NewExecutor(false)

	effects := []config.EffectConfig{
		{Archive: true},
	}

	execCtx := ExecutionContext{
		SpecID: "SPEC-001",
	}

	results := executor.Execute(context.Background(), effects, execCtx)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if !results[0].Success {
		t.Errorf("expected success, got error: %v", results[0].Error)
	}

	if !ShouldArchive(results) {
		t.Error("expected ShouldArchive to return true")
	}
}

func TestExecutor_MultipleEffects(t *testing.T) {
	executor := NewExecutor(false)
	notifier := &mockNotifier{}
	syncer := &mockSyncer{}

	effects := []config.EffectConfig{
		{Notify: &config.NotifyEffect{Target: "@team"}},
		{Sync: "outbound"},
		{LogDecision: "Approved by $user"},
	}

	execCtx := ExecutionContext{
		SpecID:   "SPEC-001",
		User:     "bob",
		Notifier: notifier,
		Syncer:   syncer,
		// No Logger - should skip
	}

	results := executor.Execute(context.Background(), effects, execCtx)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Notify should succeed
	if !results[0].Success {
		t.Errorf("notify should succeed: %v", results[0].Error)
	}

	// Sync should succeed
	if !results[1].Success {
		t.Errorf("sync should succeed: %v", results[1].Error)
	}

	// LogDecision should be skipped (no logger)
	if !results[2].Skipped {
		t.Error("log decision should be skipped without logger")
	}
}

func TestHasErrors(t *testing.T) {
	tests := []struct {
		name    string
		results []Result
		want    bool
	}{
		{
			name: "no errors",
			results: []Result{
				{Success: true},
				{Success: true},
			},
			want: false,
		},
		{
			name: "has error",
			results: []Result{
				{Success: true},
				{Success: false},
			},
			want: true,
		},
		{
			name: "skipped not counted as error",
			results: []Result{
				{Success: true},
				{Success: false, Skipped: true},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasErrors(tt.results); got != tt.want {
				t.Errorf("HasErrors() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExpandTemplate(t *testing.T) {
	execCtx := ExecutionContext{
		SpecID:    "SPEC-123",
		SpecTitle: "My Feature",
		FromStage: "draft",
		ToStage:   "review",
		User:      "alice",
		UserRole:  "engineer",
	}

	tests := []struct {
		template string
		want     string
	}{
		{"$spec_id moved to $to_stage", "SPEC-123 moved to review"},
		{"$user ($role) approved", "alice (engineer) approved"},
		{"From $from_stage to $to_stage", "From draft to review"},
		{"No variables here", "No variables here"},
	}

	for _, tt := range tests {
		got := expandTemplate(tt.template, execCtx)
		if got != tt.want {
			t.Errorf("expandTemplate(%q) = %q, want %q", tt.template, got, tt.want)
		}
	}
}
