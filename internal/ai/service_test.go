package ai

import (
	"context"
	"fmt"
	"testing"
)

type mockAI struct {
	completeResult string
	completeErr    error
}

func (m *mockAI) Complete(ctx context.Context, prompt string, system string) (string, error) {
	return m.completeResult, m.completeErr
}

func TestService_IsAvailable(t *testing.T) {
	tests := []struct {
		name    string
		service *Service
		want    bool
	}{
		{"nil service", nil, false},
		{"nil adapter", NewService(nil, true), false},
		{"disabled", NewService(&mockAI{}, false), false},
		{"available", NewService(&mockAI{}, true), true},
	}
	for _, tt := range tests {
		if got := tt.service.IsAvailable(); got != tt.want {
			t.Errorf("%s: IsAvailable() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestService_Draft_Unavailable(t *testing.T) {
	svc := NewService(nil, false)
	result, err := svc.Draft(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
}

func TestService_Draft_Success(t *testing.T) {
	mock := &mockAI{completeResult: "Drafted content here."}
	svc := NewService(mock, true)

	result, err := svc.Draft(context.Background(), "Draft problem statement")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Drafted content here." {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestService_Draft_ProviderError_DegradesGracefully(t *testing.T) {
	mock := &mockAI{completeErr: fmt.Errorf("connection refused")}
	svc := NewService(mock, true)

	result, err := svc.Draft(context.Background(), "test")
	if err != nil {
		t.Fatalf("expected graceful degradation, got error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result on error, got %q", result)
	}
}

func TestSectionDraftPrompt(t *testing.T) {
	ctx := map[string]string{
		"user_stories": "As a user, I want...",
	}
	prompt := SectionDraftPrompt("problem_statement", ctx)

	if prompt == "" {
		t.Error("expected non-empty prompt")
	}
	if !contains(prompt, "problem statement") {
		t.Error("prompt should mention section name")
	}
	if !contains(prompt, "As a user") {
		t.Error("prompt should include existing context")
	}
}

func TestPRStackPrompt(t *testing.T) {
	prompt := PRStackPrompt("Build auth system", "Microservice arch", []string{"auth-service", "gateway"})
	if !contains(prompt, "auth-service") {
		t.Error("prompt should include repos")
	}
	if !contains(prompt, "numbered list") {
		t.Error("prompt should describe expected format")
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
