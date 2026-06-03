// Package ai provides the AI service layer for content drafting. Every method
// returns nil when AI is unconfigured.
package ai

import (
	"context"
	"fmt"

	"github.com/aaronl1011/spec/internal/adapter"
)

// Service wraps an AIAdapter with null-safe semantics.
// Every method returns empty/nil when the adapter is nil or unconfigured.
type Service struct {
	adapter adapter.AIAdapter
	enabled bool
}

// NewService creates an AI service. If adapter is nil or disabled, all methods
// return nil — callers always handle the nil case.
func NewService(ai adapter.AIAdapter, enabled bool) *Service {
	return &Service{adapter: ai, enabled: enabled}
}

// IsAvailable returns true if the AI service is configured and enabled.
func (s *Service) IsAvailable() bool {
	return s != nil && s.adapter != nil && s.enabled
}

// Draft sends a prompt with context and returns the completion.
// Returns ("", nil) when AI is unavailable.
func (s *Service) Draft(ctx context.Context, prompt string, contextParts ...string) (string, error) {
	if !s.IsAvailable() {
		return "", nil
	}

	system := "You are a technical writing assistant helping draft spec sections. " +
		"Write clear, concise, professional content. Use markdown formatting."

	fullPrompt := prompt
	for _, part := range contextParts {
		if part != "" {
			fullPrompt += "\n\n---\n" + part
		}
	}

	result, err := s.adapter.Complete(ctx, fullPrompt, system)
	if err != nil {
		// Degrade gracefully: return nil, not an error
		fmt.Printf("AI provider unreachable. Proceeding without draft.\n")
		return "", nil
	}
	return result, nil
}
