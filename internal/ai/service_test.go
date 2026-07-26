package ai

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/aaronl1011/spec/internal/adapter"
)

// mockAgent stands in for an agent adapter's completion plane. Only Generate
// and Capabilities matter here; Invoke is present to satisfy the interface.
type mockAgent struct {
	text        string
	tokens      adapter.TokenUsage
	model       string
	generateErr error
	// noCompletion models a session-only harness: Capabilities.Generate false.
	noCompletion bool
}

func (m *mockAgent) Invoke(ctx context.Context, req adapter.InvokeRequest) (*adapter.InvokeResult, error) {
	return &adapter.InvokeResult{}, nil
}

func (m *mockAgent) Generate(ctx context.Context, req adapter.GenerateRequest) (*adapter.GenerateResult, error) {
	if m.generateErr != nil {
		return nil, m.generateErr
	}
	return &adapter.GenerateResult{Text: m.text, Tokens: m.tokens, Model: m.model}, nil
}

func (m *mockAgent) Capabilities() adapter.Capabilities {
	return adapter.Capabilities{Generate: !m.noCompletion}
}

func TestService_IsAvailable(t *testing.T) {
	tests := []struct {
		name    string
		service *Service
		want    bool
	}{
		{"nil service", nil, false},
		{"nil adapter", NewService(nil, true), false},
		{"disabled", NewService(&mockAgent{}, false), false},
		{"session-only provider has no completion plane", NewService(&mockAgent{noCompletion: true}, true), false},
		{"available", NewService(&mockAgent{}, true), true},
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
	mock := &mockAgent{text: "Drafted content here."}
	svc := NewService(mock, true)

	result, err := svc.Draft(context.Background(), "Draft problem statement")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Drafted content here." {
		t.Errorf("unexpected result: %q", result)
	}
}

// A provider error is returned to the caller rather than printed: the service
// never writes to stdout/stderr, so the command decides how to degrade.
func TestService_Draft_ProviderError_ReturnsError(t *testing.T) {
	mock := &mockAgent{generateErr: fmt.Errorf("connection refused")}
	svc := NewService(mock, true)

	result, err := svc.Draft(context.Background(), "Draft problem statement")
	if err == nil {
		t.Fatal("expected an error from an unreachable provider")
	}
	if result != "" {
		t.Errorf("expected empty result on error, got %q", result)
	}
}

// ErrNotSupported is a capability gap, not a failure: it degrades to no draft.
func TestService_Draft_NotSupported_DegradesQuietly(t *testing.T) {
	mock := &mockAgent{generateErr: fmt.Errorf("wrapping: %w", adapter.ErrNotSupported)}
	svc := NewService(mock, true)

	result, err := svc.Draft(context.Background(), "Draft problem statement")
	if err != nil {
		t.Fatalf("ErrNotSupported should degrade quietly, got error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	if !errors.Is(mock.generateErr, adapter.ErrNotSupported) {
		t.Error("test fixture should wrap ErrNotSupported")
	}
}
