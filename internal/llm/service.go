package llm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aaronl1011/spec/internal/adapter"
)

// ErrUnavailable reports that no completion plane is configured or enabled.
// Callers decide the degradation message — the service never prints, so the same
// code path serves the CLI, the TUI, and tests.
var ErrUnavailable = errors.New("no agent completion plane configured")

// Service runs declared tasks against an agent adapter's completion plane.
type Service struct {
	agent   adapter.AgentAdapter
	enabled bool
	// maxTokens caps responses for providers that honour it; 0 = provider
	// default.
	maxTokens int
}

// NewService creates a task-running service over an agent adapter.
func NewService(agent adapter.AgentAdapter, enabled bool) *Service {
	return &Service{agent: agent, enabled: enabled}
}

// WithMaxTokens sets the response cap passed to providers that support one.
func (s *Service) WithMaxTokens(n int) *Service {
	if s != nil {
		s.maxTokens = n
	}
	return s
}

// IsAvailable reports whether a completion plane is configured and enabled. A
// session-only harness is not available here: it has no Generate.
func (s *Service) IsAvailable() bool {
	if s == nil || s.agent == nil || !s.enabled {
		return false
	}
	return s.agent.Capabilities().Generate
}

// Capabilities exposes the resolved agent's capability set so callers can gate
// affordances without reaching for the adapter themselves.
func (s *Service) Capabilities() adapter.Capabilities {
	if s == nil || s.agent == nil {
		return adapter.Capabilities{}
	}
	return s.agent.Capabilities()
}

// Result is one generation outcome plus what it cost. Duration and usage feed
// the review gate's status line and the activity-log telemetry, which is the
// data source for the latency evidence a future API fast path would need.
type Result struct {
	Text     string
	Model    string
	Tokens   adapter.TokenUsage
	Duration time.Duration
	// Raw carries a bounded provider tail when parsing yielded no text, so an
	// empty draft is debuggable rather than mysterious.
	Raw string
}

// Run assembles a task and generates once.
//
// It returns ErrUnavailable when no completion plane exists, so a caller can
// distinguish "not configured" from "the provider failed" and word its message
// accordingly. Provider errors are returned verbatim, never printed.
func (s *Service) Run(ctx context.Context, task Task, in Input) (*Result, error) {
	if !s.IsAvailable() {
		return nil, ErrUnavailable
	}

	req := Assemble(task, in).GenerateRequest(s.maxTokens)

	start := time.Now()
	res, err := s.agent.Generate(ctx, req)
	elapsed := time.Since(start)
	if err != nil {
		// A missing plane can also surface here if capabilities lied; treat it
		// as unavailable so callers degrade rather than showing a raw error.
		if errors.Is(err, adapter.ErrNotSupported) {
			return nil, ErrUnavailable
		}
		return nil, fmt.Errorf("task %s: %w", task.ID, err)
	}
	if res == nil {
		return &Result{Duration: elapsed}, nil
	}

	return &Result{
		Text:     res.Text,
		Model:    res.Model,
		Tokens:   res.Tokens,
		Duration: elapsed,
		Raw:      res.Raw,
	}, nil
}
