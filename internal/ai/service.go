// Package ai provides the AI service layer for content drafting. Every method
// returns nil when AI is unconfigured.
package ai

import (
	"context"
	"errors"

	"github.com/aaronl1011/spec/internal/adapter"
)

// Service wraps the agent adapter's completion plane with null-safe semantics.
// Every method returns empty/nil when the adapter is nil or unconfigured.
type Service struct {
	adapter adapter.AgentAdapter
	enabled bool
}

// NewService creates an AI service over an agent adapter's completion plane. If
// the adapter is nil or disabled, all methods return empty — callers always
// handle the empty case.
func NewService(agent adapter.AgentAdapter, enabled bool) *Service {
	return &Service{adapter: agent, enabled: enabled}
}

// IsAvailable reports whether a completion plane is configured and enabled.
// A session-only harness (no Capabilities.Generate) is not available here.
func (s *Service) IsAvailable() bool {
	if s == nil || s.adapter == nil || !s.enabled {
		return false
	}
	return s.adapter.Capabilities().Generate
}

// Draft sends a prompt with context and returns the completion.
// Returns ("", nil) when the completion plane is unavailable.
func (s *Service) Draft(ctx context.Context, prompt string, contextParts ...string) (string, error) {
	if !s.IsAvailable() {
		return "", nil
	}

	system := "You are a technical writing assistant helping draft spec sections. " +
		"Write clear, concise, professional content. Use markdown formatting."

	parts := make([]adapter.ContextPart, 0, len(contextParts))
	for _, part := range contextParts {
		if part != "" {
			parts = append(parts, adapter.ContextPart{Content: part})
		}
	}

	res, err := s.adapter.Generate(ctx, adapter.GenerateRequest{
		Task:    "draft-section",
		System:  system,
		Prompt:  prompt,
		Context: parts,
	})
	if err != nil {
		// A provider without a completion plane is a capability gap, not a
		// failure: degrade quietly. Any other error is the caller's to report.
		if errors.Is(err, adapter.ErrNotSupported) {
			return "", nil
		}
		return "", err
	}
	if res == nil {
		return "", nil
	}
	return res.Text, nil
}
